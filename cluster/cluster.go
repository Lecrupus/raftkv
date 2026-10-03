// Package cluster runs a whole RaftKV cluster inside one process, every node
// on its own loopback port talking real HTTP, behind a gateway that serves the
// live dashboard and fault-injection controls. This is what the hosted demo
// runs, since free hosting gives you a single public port.
package cluster

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Lecrupus/raftkv/raft"
	"github.com/Lecrupus/raftkv/server"
)

type member struct {
	id   string
	addr string
	node *server.Node
	srv  *http.Server
}

type Failover struct {
	OldLeader string    `json:"oldLeader"`
	NewLeader string    `json:"newLeader"`
	Term      uint64    `json:"term"`
	Millis    float64   `json:"ms"`
	Cause     string    `json:"cause"`
	At        time.Time `json:"at"`
}

type Manager struct {
	dataRoot string
	sync     bool
	logger   *log.Logger
	peers    map[string]string
	order    []string

	mu        sync.Mutex
	members   map[string]*member
	events    []raft.Event
	failovers []Failover
	// pending failure of a leader, to time how long re-election takes
	lostLeader string
	lostAt     time.Time
	lostCause  string
	benchBusy  bool
}

const maxEvents = 200

func NewManager(n, basePort int, dataRoot string, syncWrites bool, logger *log.Logger) *Manager {
	m := &Manager{
		dataRoot: dataRoot, sync: syncWrites, logger: logger,
		peers: map[string]string{}, members: map[string]*member{},
	}
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("n%d", i)
		m.order = append(m.order, id)
		m.peers[id] = fmt.Sprintf("http://127.0.0.1:%d", basePort+i-1)
	}
	return m
}

func (m *Manager) StartAll() error {
	for _, id := range m.order {
		if err := m.Start(id); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) Start(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	base, ok := m.peers[id]
	if !ok {
		return fmt.Errorf("unknown node %q", id)
	}
	if m.members[id] != nil {
		return fmt.Errorf("%s is already running", id)
	}
	addr := base[len("http://"):]
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	node, err := server.NewNode(server.Config{
		ID: id, Peers: m.peers, SyncWrites: m.sync, Logger: m.logger,
		DataDir: filepath.Join(m.dataRoot, id),
		OnEvent: m.onEvent,
	})
	if err != nil {
		ln.Close()
		return err
	}
	srv := &http.Server{Handler: node.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(ln)
	hadQuorum := m.hasQuorumLocked()
	m.members[id] = &member{id: id, addr: addr, node: node, srv: srv}
	m.quorumMaybeRestoredLocked(hadQuorum)
	m.addEventLocked(raft.Event{Time: time.Now(), Node: id, Kind: "started", Detail: "recovered state from WAL"})
	return nil
}

// Kill simulates a crash: the node stops and its HTTP listener closes. Its
// WAL stays on disk, so Start brings it back with its log intact.
func (m *Manager) Kill(id string) error {
	m.mu.Lock()
	mem := m.members[id]
	if mem == nil {
		m.mu.Unlock()
		return fmt.Errorf("%s is not running", id)
	}
	delete(m.members, id)
	wasLeader := mem.node.Status().Role == "leader"
	if wasLeader {
		m.markLeaderLostLocked(id, "leader killed")
	}
	m.addEventLocked(raft.Event{Time: time.Now(), Node: id, Kind: "killed", Detail: "process crashed (simulated)"})
	m.mu.Unlock()

	mem.srv.Close()
	mem.node.Stop()
	return nil
}

func (m *Manager) SetIsolated(id string, iso bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	mem := m.members[id]
	if mem == nil {
		return fmt.Errorf("%s is not running", id)
	}
	hadQuorum := m.hasQuorumLocked()
	mem.node.SetIsolated(iso)
	if !iso {
		m.quorumMaybeRestoredLocked(hadQuorum)
	}
	kind, detail := "healed", "network restored"
	if iso {
		kind, detail = "isolated", "network partitioned from peers"
		if mem.node.Status().Role == "leader" {
			m.markLeaderLostLocked(id, "leader partitioned")
		}
	}
	m.addEventLocked(raft.Event{Time: time.Now(), Node: id, Kind: kind, Detail: detail})
	return nil
}

// hasQuorumLocked reports whether a majority of nodes is up and connected.
func (m *Manager) hasQuorumLocked() bool {
	healthy := 0
	for _, mem := range m.members {
		if !mem.node.Isolated() {
			healthy++
		}
	}
	return healthy > len(m.order)/2
}

// quorumMaybeRestoredLocked restarts the failover clock when a majority comes
// back: without one no election can succeed, and that wait is not "failover
// time" but unavailability by design (CP).
func (m *Manager) quorumMaybeRestoredLocked(hadQuorum bool) {
	if m.lostLeader != "" && !hadQuorum && m.hasQuorumLocked() {
		m.lostAt, m.lostCause = time.Now(), "quorum restored"
	}
}

func (m *Manager) markLeaderLostLocked(id, cause string) {
	m.lostLeader, m.lostAt, m.lostCause = id, time.Now(), cause
}

func (m *Manager) onEvent(ev raft.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ev.Kind == "leader" && m.lostLeader != "" && ev.Node != m.lostLeader {
		ms := float64(ev.Time.Sub(m.lostAt).Microseconds()) / 1000
		m.failovers = append(m.failovers, Failover{
			OldLeader: m.lostLeader, NewLeader: ev.Node, Term: ev.Term,
			Millis: ms, Cause: m.lostCause, At: ev.Time,
		})
		if len(m.failovers) > 50 {
			m.failovers = m.failovers[1:]
		}
		ev.Detail = fmt.Sprintf("%s — %.0f ms after %s", ev.Detail, ms, m.lostCause)
		m.lostLeader = ""
	}
	m.addEventLocked(ev)
}

func (m *Manager) addEventLocked(ev raft.Event) {
	m.events = append(m.events, ev)
	if len(m.events) > maxEvents {
		m.events = m.events[len(m.events)-maxEvents:]
	}
}

type NodeView struct {
	ID      string             `json:"id"`
	Up      bool               `json:"up"`
	Address string             `json:"address"`
	Status  *server.NodeStatus `json:"status,omitempty"`
}

type View struct {
	Nodes     []NodeView   `json:"nodes"`
	Leader    string       `json:"leader"`
	Events    []raft.Event `json:"events"`
	Failovers []Failover   `json:"failovers"`
	Now       time.Time    `json:"now"`
}

func (m *Manager) View() View {
	m.mu.Lock()
	members := make(map[string]*member, len(m.members))
	for k, v := range m.members {
		members[k] = v
	}
	v := View{
		Events:    append([]raft.Event(nil), m.events...),
		Failovers: append([]Failover(nil), m.failovers...),
		Now:       time.Now(),
	}
	m.mu.Unlock()

	var bestTerm uint64
	for _, id := range m.order {
		nv := NodeView{ID: id, Address: m.peers[id]}
		if mem := members[id]; mem != nil {
			s := mem.node.Status()
			nv.Up, nv.Status = true, &s
			if s.Role == "leader" && s.Term >= bestTerm {
				bestTerm, v.Leader = s.Term, id
			}
		}
		v.Nodes = append(v.Nodes, nv)
	}
	return v
}

// entryNode picks a running node to receive client requests, preferring the leader.
func (m *Manager) entryNode() (*server.Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var fallback *server.Node
	for _, id := range m.order {
		mem := m.members[id]
		if mem == nil {
			continue
		}
		if mem.node.Status().Role == "leader" && !mem.node.Isolated() {
			return mem.node, nil
		}
		if fallback == nil {
			fallback = mem.node
		}
	}
	if fallback == nil {
		return nil, errors.New("no nodes running")
	}
	return fallback, nil
}

func (m *Manager) Targets() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var t []string
	for _, id := range m.order {
		if m.members[id] != nil {
			t = append(t, m.peers[id])
		}
	}
	return t
}

func (m *Manager) Shutdown(ctx context.Context) {
	for _, id := range m.order {
		m.Kill(id)
	}
}

// ResetData wipes every node's WAL; used at boot so the demo starts clean.
func ResetData(root string) error { return os.RemoveAll(root) }
