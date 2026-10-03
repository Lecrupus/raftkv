package kv

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Lecrupus/raftkv/lincheck"
	"github.com/Lecrupus/raftkv/raft"
)

// cluster is an in-memory RaftKV cluster for end-to-end tests.
type cluster struct {
	t       *testing.T
	net     *raft.MemNetwork
	ids     []string
	storage map[string]*raft.MemoryStorage

	mu     sync.RWMutex
	rafts  map[string]*raft.Raft
	stores map[string]*Store
}

func newCluster(t *testing.T, n int) *cluster {
	c := &cluster{
		t: t, net: raft.NewMemNetwork(),
		storage: map[string]*raft.MemoryStorage{},
		rafts:   map[string]*raft.Raft{}, stores: map[string]*Store{},
	}
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("n%d", i)
		c.ids = append(c.ids, id)
		c.storage[id] = raft.NewMemoryStorage()
	}
	for _, id := range c.ids {
		c.start(id)
	}
	t.Cleanup(func() {
		for _, id := range c.ids {
			c.crash(id)
		}
	})
	return c
}

func (c *cluster) start(id string) {
	var peers []string
	for _, p := range c.ids {
		if p != id {
			peers = append(peers, p)
		}
	}
	r, err := raft.New(raft.DefaultConfig(id, peers), c.net.Transport(id), c.storage[id])
	if err != nil {
		c.t.Fatal(err)
	}
	s := New(r)
	c.mu.Lock()
	c.rafts[id], c.stores[id] = r, s
	c.mu.Unlock()
	c.net.Register(id, r)
}

func (c *cluster) crash(id string) {
	c.net.Unregister(id)
	c.mu.Lock()
	r := c.rafts[id]
	delete(c.rafts, id)
	delete(c.stores, id)
	c.mu.Unlock()
	if r != nil {
		r.Stop()
	}
}

func (c *cluster) store(id string) *Store {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stores[id]
}

func (c *cluster) up() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.rafts)
}

// TestLinearizableUnderChaos runs concurrent clients against a 5-node cluster
// while a nemesis crashes/restarts nodes and partitions the network, then
// verifies the recorded history is linearizable.
func TestLinearizableUnderChaos(t *testing.T) {
	runs := 2
	if v, err := strconv.Atoi(os.Getenv("CHAOS_RUNS")); err == nil {
		runs = v
	}
	if testing.Short() {
		runs = 1
	}
	for run := 0; run < runs; run++ {
		t.Run(fmt.Sprintf("seed%d", run), func(t *testing.T) { linearizabilityRun(t, int64(run)) })
	}
}

func linearizabilityRun(t *testing.T, seed int64) {
	c := newCluster(t, 5)
	c.net.SetUnreliable(0.02, 3*time.Millisecond)
	rng := rand.New(rand.NewSource(seed))
	start := time.Now()
	now := func() int64 { return int64(time.Since(start)) }

	const (
		clients     = 6
		keys        = 4
		opsPerKey   = 48 // keeps each key's search space bounded
		testTimeout = 8 * time.Second
	)
	var (
		histMu   sync.Mutex
		history  []lincheck.Op
		perKey   [keys]int32
		okOps    atomic.Int64
		unknown  atomic.Int64
		valueSeq atomic.Int64
	)
	record := func(op lincheck.Op) {
		histMu.Lock()
		history = append(history, op)
		histMu.Unlock()
	}

	stop := make(chan struct{})
	var nemesis sync.WaitGroup
	nemesis.Add(1)
	go func() {
		defer nemesis.Done()
		for {
			select {
			case <-stop:
				return
			case <-time.After(time.Duration(150+rng.Intn(250)) * time.Millisecond):
			}
			id := c.ids[rng.Intn(len(c.ids))]
			switch rng.Intn(4) {
			case 0:
				if c.store(id) != nil && c.up() > 3 {
					c.crash(id)
				}
			case 1:
				if c.store(id) == nil {
					c.start(id)
				}
			case 2:
				c.net.Isolate(id)
			case 3:
				c.net.Heal()
			}
		}
	}()

	deadline := time.Now().Add(testTimeout)
	var wg sync.WaitGroup
	for cl := 0; cl < clients; cl++ {
		wg.Add(1)
		go func(cl int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(seed*100 + int64(cl)))
			for time.Now().Before(deadline) {
				k := r.Intn(keys)
				if atomic.AddInt32(&perKey[k], 1) > opsPerKey {
					atomic.AddInt32(&perKey[k], -1)
					if allFull(&perKey, opsPerKey) {
						return
					}
					continue
				}
				key := fmt.Sprintf("k%d", k)
				var op Op
				switch r.Intn(10) {
				case 0, 1, 2, 3:
					op = Op{Type: OpGet, Key: key}
				case 4:
					op = Op{Type: OpDelete, Key: key}
				default:
					op = Op{Type: OpPut, Key: key, Value: strconv.FormatInt(valueSeq.Add(1), 10)}
				}
				id := c.ids[r.Intn(len(c.ids))]
				s := c.store(id)
				if s == nil {
					atomic.AddInt32(&perKey[k], -1)
					time.Sleep(5 * time.Millisecond)
					continue
				}
				call := now()
				ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				res, err := s.Do(ctx, op)
				cancel()
				ret := now()

				lop := lincheck.Op{Key: key, Call: call, Return: ret, Client: cl}
				switch op.Type {
				case OpPut:
					lop.Kind, lop.Value = lincheck.Put, op.Value
				case OpDelete:
					lop.Kind = lincheck.Delete
				case OpGet:
					lop.Kind, lop.Value, lop.Found = lincheck.Get, res.Value, res.Found
				}
				switch {
				case err == nil:
					okOps.Add(1)
					record(lop)
				case errors.Is(err, ErrNotLeader), errors.Is(err, ErrLeadershipLost):
					// definitely did not take effect
					atomic.AddInt32(&perKey[k], -1)
					time.Sleep(10 * time.Millisecond)
				default:
					// timeout / node stopped: a write may still commit later
					if op.Type == OpGet {
						atomic.AddInt32(&perKey[k], -1)
					} else {
						lop.Return = lincheck.Unknown
						unknown.Add(1)
						record(lop)
					}
				}
			}
		}(cl)
	}
	wg.Wait()
	close(stop)
	nemesis.Wait()

	res, err := lincheck.Check(history)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("seed %d: %d ops recorded (%d ok, %d unknown outcome)", seed, len(history), okOps.Load(), unknown.Load())
	if okOps.Load() < 20 {
		t.Fatalf("too few successful operations (%d) to be meaningful", okOps.Load())
	}
	if !res.OK {
		t.Fatalf("history is NOT linearizable (key %s)", res.Key)
	}
}

func allFull(perKey *[4]int32, max int32) bool {
	for i := range perKey {
		if atomic.LoadInt32(&perKey[i]) < max {
			return false
		}
	}
	return true
}

func TestDedupIdempotentRetry(t *testing.T) {
	c := newCluster(t, 3)
	op := Op{Type: OpPut, Key: "a", Value: "1", ClientID: "client-1", Seq: 1}
	var leader *Store
	deadline := time.Now().Add(3 * time.Second)
	for leader == nil && time.Now().Before(deadline) {
		for _, id := range c.ids {
			if _, err := c.store(id).Do(context.Background(), op); err == nil {
				leader = c.store(id)
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if leader == nil {
		t.Fatal("no leader")
	}
	// a newer write, then a retry of seq 1 (as if the first reply was lost)
	if _, err := leader.Do(context.Background(), Op{Type: OpPut, Key: "a", Value: "2", ClientID: "client-1", Seq: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := leader.Do(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	res, err := leader.Do(context.Background(), Op{Type: OpGet, Key: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Value != "2" {
		t.Fatalf("retried request was re-applied: got %q, want 2", res.Value)
	}
	if leader.Stats().DedupHits != 1 {
		t.Fatalf("dedup hits = %d, want 1", leader.Stats().DedupHits)
	}
}
