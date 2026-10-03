// Package server wires a Raft node, the KV state machine and the WAL into an
// HTTP service: the client API, the internal Raft RPC endpoints, status and
// Prometheus metrics.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Lecrupus/raftkv/kv"
	"github.com/Lecrupus/raftkv/raft"
)

type Config struct {
	ID         string
	Peers      map[string]string // every member incl. self: id -> base URL
	DataDir    string
	SyncWrites bool
	Logger     *log.Logger
	OnEvent    func(raft.Event)
	Raft       *raft.Config // optional timing overrides
}

type Node struct {
	cfg       Config
	Raft      *raft.Raft
	Store     *kv.Store
	transport *HTTPTransport
	isolated  atomic.Bool
	started   time.Time
	m         metrics
}

type metrics struct {
	puts, gets, deletes, staleGets atomic.Uint64
	forwarded, notLeader, errors   atomic.Uint64
	latencyNanos, latencyCount     atomic.Uint64
}

func NewNode(cfg Config) (*Node, error) {
	if _, ok := cfg.Peers[cfg.ID]; !ok {
		return nil, fmt.Errorf("peer list must include self (%s)", cfg.ID)
	}
	storage, err := raft.OpenFileStorage(cfg.DataDir, cfg.SyncWrites)
	if err != nil {
		return nil, err
	}
	n := &Node{cfg: cfg, started: time.Now()}
	others := map[string]string{}
	var peerIDs []string
	for id, url := range cfg.Peers {
		if id != cfg.ID {
			others[id] = url
			peerIDs = append(peerIDs, id)
		}
	}
	sort.Strings(peerIDs)
	n.transport = NewHTTPTransport(others, &n.isolated)

	rc := raft.DefaultConfig(cfg.ID, peerIDs)
	if cfg.Raft != nil {
		rc = *cfg.Raft
		rc.ID, rc.Peers = cfg.ID, peerIDs
	}
	rc.Logger, rc.OnEvent = cfg.Logger, cfg.OnEvent
	r, err := raft.New(rc, n.transport, storage)
	if err != nil {
		storage.Close()
		return nil, err
	}
	n.Raft = r
	n.Store = kv.New(r)
	return n, nil
}

func (n *Node) Stop() { n.Raft.Stop() }

// SetIsolated simulates a network partition: all Raft traffic in and out of
// this node is dropped, while its client API stays reachable.
func (n *Node) SetIsolated(v bool) { n.isolated.Store(v) }
func (n *Node) Isolated() bool     { return n.isolated.Load() }

type NodeStatus struct {
	raft.Status
	KV       kv.Stats `json:"kv"`
	Isolated bool     `json:"isolated"`
	Uptime   string   `json:"uptime"`
	RPCs     uint64   `json:"rpcs"`
}

func (n *Node) Status() NodeStatus {
	return NodeStatus{
		Status:   n.Raft.Status(),
		KV:       n.Store.Stats(),
		Isolated: n.Isolated(),
		Uptime:   time.Since(n.started).Round(time.Second).String(),
		RPCs:     n.transport.rpcs.Load(),
	}
}

func (n *Node) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /raft/vote", n.handleVote)
	mux.HandleFunc("POST /raft/append", n.handleAppend)
	mux.HandleFunc("GET /kv/{key}", n.handleKV)
	mux.HandleFunc("PUT /kv/{key}", n.handleKV)
	mux.HandleFunc("DELETE /kv/{key}", n.handleKV)
	mux.HandleFunc("GET /keys", n.handleKeys)
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, n.Status()) })
	mux.HandleFunc("GET /metrics", n.handleMetrics)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") })
	return mux
}

// ---- Raft RPC endpoints ----

func (n *Node) handleVote(w http.ResponseWriter, r *http.Request) {
	if n.isolated.Load() {
		http.Error(w, errIsolated.Error(), http.StatusServiceUnavailable)
		return
	}
	var args raft.RequestVoteArgs
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	reply, err := n.Raft.HandleRequestVote(&args)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, 200, reply)
}

func (n *Node) handleAppend(w http.ResponseWriter, r *http.Request) {
	if n.isolated.Load() {
		http.Error(w, errIsolated.Error(), http.StatusServiceUnavailable)
		return
	}
	var args raft.AppendEntriesArgs
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	reply, err := n.Raft.HandleAppendEntries(&args)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, 200, reply)
}

// ---- client API ----

const forwardedHeader = "X-RaftKV-Forwarded-By"

type kvResponse struct {
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
	Found  bool   `json:"found"`
	Index  uint64 `json:"index,omitempty"`
	Node   string `json:"servedBy"`
	Leader string `json:"leader,omitempty"`
	Stale  bool   `json:"stale,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (n *Node) handleKV(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		n.m.latencyNanos.Add(uint64(time.Since(start)))
		n.m.latencyCount.Add(1)
	}()
	key := r.PathValue("key")
	leader, _ := n.Raft.Leader()
	resp := kvResponse{Key: key, Node: n.cfg.ID, Leader: leader}

	// Stale reads are served locally by any replica: fast, possibly old.
	if r.Method == http.MethodGet && r.URL.Query().Get("consistency") == "stale" {
		n.m.staleGets.Add(1)
		resp.Value, resp.Found = n.Store.LocalGet(key)
		resp.Stale = true
		writeJSON(w, statusFor(resp.Found), resp)
		return
	}

	op := kv.Op{Key: key, ClientID: r.Header.Get("X-Client-ID")}
	op.Seq, _ = strconv.ParseUint(r.Header.Get("X-Request-Seq"), 10, 64)
	var body []byte
	switch r.Method {
	case http.MethodGet:
		op.Type = kv.OpGet
		n.m.gets.Add(1)
	case http.MethodDelete:
		op.Type = kv.OpDelete
		n.m.deletes.Add(1)
	case http.MethodPut:
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		op.Type, op.Value = kv.OpPut, string(body)
		n.m.puts.Add(1)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	res, err := n.Store.Do(ctx, op)
	switch {
	case errors.Is(err, kv.ErrNotLeader):
		n.m.notLeader.Add(1)
		if r.Header.Get(forwardedHeader) == "" && leader != "" && leader != n.cfg.ID {
			n.forward(w, r, leader, body)
			return
		}
		resp.Error = "not leader"
		writeJSON(w, http.StatusServiceUnavailable, resp)
	case err != nil:
		n.m.errors.Add(1)
		resp.Error = err.Error()
		writeJSON(w, http.StatusServiceUnavailable, resp)
	default:
		resp.Value, resp.Found, resp.Index = res.Value, res.Found, res.Index
		code := 200
		if op.Type == kv.OpGet {
			code = statusFor(res.Found)
		}
		writeJSON(w, code, resp)
	}
}

// forward proxies a client request to the leader once, so clients can talk to
// any node.
func (n *Node) forward(w http.ResponseWriter, r *http.Request, leader string, body []byte) {
	n.m.forwarded.Add(1)
	url := n.cfg.Peers[leader] + r.URL.RequestURI()
	req, err := http.NewRequestWithContext(r.Context(), r.Method, url, bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, h := range []string{"X-Client-ID", "X-Request-Seq", "Content-Type"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	req.Header.Set(forwardedHeader, n.cfg.ID)
	resp, err := n.transport.client.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, kvResponse{Key: r.PathValue("key"), Node: n.cfg.ID, Leader: leader, Error: "forward to leader failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set(forwardedHeader, n.cfg.ID)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (n *Node) handleKeys(w http.ResponseWriter, r *http.Request) {
	keys := n.Store.Keys()
	if p := r.URL.Query().Get("prefix"); p != "" {
		filtered := keys[:0]
		for _, k := range keys {
			if strings.HasPrefix(k, p) {
				filtered = append(filtered, k)
			}
		}
		keys = filtered
	}
	if len(keys) > 1000 {
		keys = keys[:1000]
	}
	writeJSON(w, 200, map[string]any{"node": n.cfg.ID, "keys": keys, "stale": true})
}

func (n *Node) handleMetrics(w http.ResponseWriter, r *http.Request) {
	s := n.Status()
	isLeader := 0
	if s.Role == "leader" {
		isLeader = 1
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	lbl := fmt.Sprintf(`{node=%q}`, n.cfg.ID)
	fmt.Fprintf(w, "# HELP raft_term Current Raft term.\n# TYPE raft_term gauge\nraft_term%s %d\n", lbl, s.Term)
	fmt.Fprintf(w, "# TYPE raft_is_leader gauge\nraft_is_leader%s %d\n", lbl, isLeader)
	fmt.Fprintf(w, "# TYPE raft_commit_index gauge\nraft_commit_index%s %d\n", lbl, s.CommitIndex)
	fmt.Fprintf(w, "# TYPE raft_last_log_index gauge\nraft_last_log_index%s %d\n", lbl, s.LastLogIndex)
	fmt.Fprintf(w, "# TYPE raft_elections_total counter\nraft_elections_total%s %d\n", lbl, s.Elections)
	fmt.Fprintf(w, "# TYPE raft_rpcs_total counter\nraft_rpcs_total%s %d\n", lbl, n.transport.rpcs.Load())
	fmt.Fprintf(w, "# TYPE raft_rpc_failures_total counter\nraft_rpc_failures_total%s %d\n", lbl, n.transport.failures.Load())
	fmt.Fprintf(w, "# TYPE kv_keys gauge\nkv_keys%s %d\n", lbl, s.KV.Keys)
	fmt.Fprintf(w, "# TYPE kv_dedup_hits_total counter\nkv_dedup_hits_total%s %d\n", lbl, s.KV.DedupHits)
	fmt.Fprintf(w, "# TYPE kv_requests_total counter\n")
	for _, m := range []struct {
		op string
		v  uint64
	}{{"put", n.m.puts.Load()}, {"get", n.m.gets.Load()}, {"delete", n.m.deletes.Load()}, {"stale_get", n.m.staleGets.Load()}} {
		fmt.Fprintf(w, "kv_requests_total{node=%q,op=%q} %d\n", n.cfg.ID, m.op, m.v)
	}
	fmt.Fprintf(w, "# TYPE kv_forwarded_total counter\nkv_forwarded_total%s %d\n", lbl, n.m.forwarded.Load())
	fmt.Fprintf(w, "# TYPE kv_errors_total counter\nkv_errors_total%s %d\n", lbl, n.m.errors.Load())
	fmt.Fprintf(w, "# TYPE kv_request_seconds summary\nkv_request_seconds_sum%s %f\nkv_request_seconds_count%s %d\n",
		lbl, float64(n.m.latencyNanos.Load())/1e9, lbl, n.m.latencyCount.Load())
}

func statusFor(found bool) int {
	if found {
		return 200
	}
	return 404
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
