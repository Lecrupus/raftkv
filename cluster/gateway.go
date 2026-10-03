package cluster

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	"github.com/Lecrupus/raftkv/bench"
)

//go:embed web
var webFS embed.FS

// Handler serves the dashboard and the control API:
//
//	GET  /api/cluster                       cluster view (nodes, events, failovers)
//	POST /api/nodes/{id}/{kill|start|isolate|heal}
//	GET|PUT|DELETE /api/kv/{key}            client API via the leader (or any node)
//	POST /api/bench?ops=N&clients=C&reads=R run a load test against the cluster
func (m *Manager) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web")
	mux.Handle("GET /", http.FileServer(http.FS(static)))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /api/cluster", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, m.View())
	})
	mux.HandleFunc("POST /api/nodes/{id}/{action}", m.handleAction)
	mux.HandleFunc("GET /api/kv/{key}", m.handleKV)
	mux.HandleFunc("PUT /api/kv/{key}", m.handleKV)
	mux.HandleFunc("DELETE /api/kv/{key}", m.handleKV)
	mux.HandleFunc("POST /api/bench", m.handleBench)
	return mux
}

func (m *Manager) handleAction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var err error
	switch r.PathValue("action") {
	case "kill":
		err = m.Kill(id)
	case "start":
		err = m.Start(id)
	case "isolate":
		err = m.SetIsolated(id, true)
	case "heal":
		err = m.SetIsolated(id, false)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (m *Manager) handleKV(w http.ResponseWriter, r *http.Request) {
	node, err := m.entryNode()
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": err.Error()})
		return
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/kv/" + r.PathValue("key")
	node.Handler().ServeHTTP(w, r2)
}

func (m *Manager) handleBench(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	if m.benchBusy {
		m.mu.Unlock()
		writeJSON(w, 429, map[string]string{"error": "a benchmark is already running"})
		return
	}
	m.benchBusy = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.benchBusy = false; m.mu.Unlock() }()

	q := r.URL.Query()
	ops := clamp(atoi(q.Get("ops"), 500), 1, 5000)
	clients := clamp(atoi(q.Get("clients"), 8), 1, 32)
	reads, _ := strconv.ParseFloat(q.Get("reads"), 64)
	targets := m.Targets()
	if len(targets) == 0 {
		writeJSON(w, 503, map[string]string{"error": "no nodes running"})
		return
	}
	res := bench.Run(context.Background(), bench.Config{
		Targets: targets, Clients: clients, MaxOps: ops,
		Duration: 20 * time.Second, ReadRatio: reads, Keys: 200,
	})
	writeJSON(w, 200, res)
}

func atoi(s string, def int) int {
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return def
}

func clamp(v, lo, hi int) int { return max(lo, min(v, hi)) }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
