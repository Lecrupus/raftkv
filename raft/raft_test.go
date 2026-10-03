package raft

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

// harness runs an in-memory cluster and checks that every node applies the
// same command at the same index (the State Machine Safety property).
type harness struct {
	t       *testing.T
	net     *MemNetwork
	ids     []string
	nm      sync.RWMutex
	nodes   map[string]*Raft
	storage map[string]*MemoryStorage

	mu      sync.Mutex
	applied map[string][]string // per node, command applied at each index (1-based)
	global  map[uint64]string   // index -> command, as first applied anywhere
}

func newHarness(t *testing.T, n int) *harness {
	h := &harness{
		t: t, net: NewMemNetwork(),
		nodes: map[string]*Raft{}, storage: map[string]*MemoryStorage{},
		applied: map[string][]string{}, global: map[uint64]string{},
	}
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("n%d", i)
		h.ids = append(h.ids, id)
		h.storage[id] = NewMemoryStorage()
	}
	for _, id := range h.ids {
		h.start(id)
	}
	t.Cleanup(h.stopAll)
	return h
}

func (h *harness) peersOf(id string) []string {
	var ps []string
	for _, p := range h.ids {
		if p != id {
			ps = append(ps, p)
		}
	}
	return ps
}

func (h *harness) start(id string) {
	cfg := DefaultConfig(id, h.peersOf(id))
	r, err := New(cfg, h.net.Transport(id), h.storage[id])
	if err != nil {
		h.t.Fatal(err)
	}
	h.mu.Lock()
	h.applied[id] = nil // state machine is rebuilt from the log on restart
	h.mu.Unlock()
	h.nm.Lock()
	h.nodes[id] = r
	h.nm.Unlock()
	h.net.Register(id, r)
	go func() {
		for m := range r.ApplyCh() {
			h.onApply(id, m)
		}
	}()
}

func (h *harness) onApply(id string, m ApplyMsg) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cmd := string(m.Command)
	if want := uint64(len(h.applied[id])) + 1; m.Index != want {
		h.t.Errorf("%s applied index %d, expected %d", id, m.Index, want)
	}
	h.applied[id] = append(h.applied[id], cmd)
	if prev, ok := h.global[m.Index]; ok && prev != cmd {
		h.t.Errorf("SAFETY VIOLATION: index %d applied as %q and %q (%s)", m.Index, prev, cmd, id)
	} else {
		h.global[m.Index] = cmd
	}
}

func (h *harness) crash(id string) {
	h.net.Unregister(id)
	h.nm.Lock()
	r := h.nodes[id]
	delete(h.nodes, id)
	h.nm.Unlock()
	if r != nil {
		r.Stop()
	}
}

func (h *harness) stopAll() {
	for _, id := range h.ids {
		h.crash(id)
	}
}

func (h *harness) node(id string) *Raft {
	h.nm.RLock()
	defer h.nm.RUnlock()
	return h.nodes[id]
}

func (h *harness) live() map[string]*Raft {
	h.nm.RLock()
	defer h.nm.RUnlock()
	m := make(map[string]*Raft, len(h.nodes))
	for k, v := range h.nodes {
		m[k] = v
	}
	return m
}

// leader waits until exactly one leader exists in the highest term.
func (h *harness) leader() string {
	h.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		byTerm := map[uint64][]string{}
		var top uint64
		for id, r := range h.live() {
			s := r.Status()
			if s.Role == "leader" {
				byTerm[s.Term] = append(byTerm[s.Term], id)
				top = max(top, s.Term)
			}
		}
		for term, ls := range byTerm {
			if len(ls) > 1 {
				h.t.Fatalf("term %d has %d leaders: %v", term, len(ls), ls)
			}
		}
		if ls := byTerm[top]; len(ls) == 1 {
			return ls[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.t.Fatal("no leader elected")
	return ""
}

// propose submits cmd to whichever node is leader, retrying until it is
// applied on at least `want` nodes, and returns its index.
func (h *harness) propose(cmd string, want int) uint64 {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, id := range h.ids {
			r := h.node(id)
			if r == nil {
				continue
			}
			idx, _, ok := r.Propose([]byte(cmd))
			if !ok {
				continue
			}
			wait := time.Now().Add(time.Second)
			for time.Now().Before(wait) {
				if n, got := h.appliedCount(idx); n >= want && got == cmd {
					return idx
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	h.t.Fatalf("command %q never committed on %d nodes", cmd, want)
	return 0
}

func (h *harness) appliedCount(idx uint64) (int, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	n, cmd := 0, ""
	live := h.live()
	for _, id := range h.ids {
		if _, up := live[id]; !up {
			continue
		}
		if a := h.applied[id]; uint64(len(a)) >= idx {
			n++
			cmd = a[idx-1]
		}
	}
	return n, cmd
}

func TestInitialElection(t *testing.T) {
	h := newHarness(t, 3)
	l := h.leader()
	term := h.node(l).Status().Term
	time.Sleep(500 * time.Millisecond)
	if l2 := h.leader(); l2 != l || h.node(l2).Status().Term != term {
		t.Fatalf("leadership changed without failures: %s/%d -> %s", l, term, l2)
	}
}

func TestReElectionAfterLeaderFailure(t *testing.T) {
	h := newHarness(t, 3)
	old := h.leader()
	start := time.Now()
	h.net.Isolate(old)
	nl := h.leaderAmong(h.peersOf(old))
	t.Logf("new leader %s elected %v after isolating %s", nl, time.Since(start).Round(time.Millisecond), old)

	h.net.Heal()
	time.Sleep(500 * time.Millisecond)
	if l := h.leader(); h.node(old).Status().Role == "leader" && l == old {
		t.Fatalf("stale leader %s still leads after rejoining", old)
	}
}

func (h *harness) leaderAmong(ids []string) string {
	h.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, id := range ids {
			if r := h.node(id); r != nil && r.Status().Role == "leader" {
				return id
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("no leader among %v", ids)
	return ""
}

// With Pre-Vote, a follower that was partitioned away must not bump its term
// and force an election when it reconnects.
func TestRejoiningNodeDoesNotDisruptLeader(t *testing.T) {
	h := newHarness(t, 3)
	l := h.leader()
	term := h.node(l).Status().Term
	victim := h.peersOf(l)[0]
	h.net.Isolate(victim)
	time.Sleep(1500 * time.Millisecond) // many election timeouts
	if vt := h.node(victim).Status().Term; vt != term {
		t.Fatalf("isolated node inflated its term %d -> %d", term, vt)
	}
	h.net.Heal()
	time.Sleep(500 * time.Millisecond)
	if l2 := h.leader(); l2 != l || h.node(l2).Status().Term != term {
		t.Fatalf("rejoin disrupted the leader: %s/t%d -> %s/t%d", l, term, l2, h.node(l2).Status().Term)
	}
	h.propose("after-rejoin", 3)
}

func TestLogReplication(t *testing.T) {
	h := newHarness(t, 5)
	for i := 0; i < 50; i++ {
		h.propose(fmt.Sprintf("cmd-%d", i), 5)
	}
}

func TestMinorityLeaderCannotCommit(t *testing.T) {
	h := newHarness(t, 5)
	h.propose("before", 5)
	old := h.leader()
	others := h.peersOf(old)
	// old leader + one follower on the minority side
	h.net.Partition([]string{old, others[0]}, others[1:])

	idx, _, ok := h.node(old).Propose([]byte("lost-write"))
	if !ok {
		t.Fatal("old leader should still accept the proposal before noticing the partition")
	}
	newL := h.leaderAmong(others[1:])
	h.propose("majority-write", 3)

	time.Sleep(300 * time.Millisecond)
	if n, cmd := h.appliedCount(idx); cmd == "lost-write" && n > 0 {
		t.Fatalf("minority write committed on %d nodes", n)
	}
	if s := h.node(old).Status(); s.Role == "leader" {
		t.Fatalf("partitioned leader %s did not step down (check-quorum)", old)
	}

	h.net.Heal()
	h.propose("after-heal", 5) // forces the old leader's conflicting entry to be overwritten
	t.Logf("old leader %s, new leader %s", old, newL)
}

func TestCrashRecoveryAllNodes(t *testing.T) {
	h := newHarness(t, 3)
	for i := 0; i < 20; i++ {
		h.propose(fmt.Sprintf("v%d", i), 3)
	}
	for _, id := range h.ids {
		h.crash(id)
	}
	for _, id := range h.ids {
		h.start(id)
	}
	h.leader()
	idx := h.propose("after-restart", 3)
	if idx < 21 {
		t.Fatalf("log lost after restart: new entry at index %d", idx)
	}
}

// TestChaos crashes, restarts and partitions nodes at random on a lossy,
// slow network while proposing commands, then checks every node converges on
// one log. CHAOS_RUNS controls the number of seeded runs.
func TestChaos(t *testing.T) {
	runs := 3
	if v, err := strconv.Atoi(os.Getenv("CHAOS_RUNS")); err == nil {
		runs = v
	}
	if testing.Short() {
		runs = 1
	}
	for run := 0; run < runs; run++ {
		t.Run(fmt.Sprintf("run%d", run), func(t *testing.T) { chaosRun(t, int64(run)) })
	}
}

func chaosRun(t *testing.T, seed int64) {
	rng := rand.New(rand.NewSource(seed))
	h := newHarness(t, 5)
	h.net.SetUnreliable(0.05, 5*time.Millisecond)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // fault injector
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			case <-time.After(time.Duration(100+rng.Intn(200)) * time.Millisecond):
			}
			id := h.ids[rng.Intn(len(h.ids))]
			switch rng.Intn(3) {
			case 0:
				if up := h.node(id) != nil; up && len(h.live()) > 3 {
					h.crash(id)
				} else if h.node(id) == nil {
					h.start(id)
				}
			case 1:
				h.net.Isolate(id)
			case 2:
				h.net.Heal()
			}
		}
	}()

	for i := 0; i < 30; i++ {
		for _, id := range h.ids {
			if r := h.node(id); r != nil {
				r.Propose([]byte(fmt.Sprintf("s%d-c%d", seed, i)))
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(stop)
	wg.Wait()

	// repair everything, then demand full convergence
	h.net.Heal()
	h.net.SetUnreliable(0, 0)
	for _, id := range h.ids {
		if h.node(id) == nil {
			h.start(id)
		}
	}
	final := h.propose(fmt.Sprintf("s%d-final", seed), 5)
	for _, id := range h.ids {
		s := h.node(id).Status()
		if s.CommitIndex < final {
			t.Errorf("%s commit=%d < %d", id, s.CommitIndex, final)
		}
	}
}
