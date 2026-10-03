package raft

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"
)

// MemNetwork is an in-process network for tests. It can partition nodes into
// groups, drop a fraction of messages, and add random latency, which is how
// the test suite simulates the failures a real network produces.
type MemNetwork struct {
	mu       sync.RWMutex
	nodes    map[string]*Raft
	group    map[string]int // nodes talk only within the same group
	dropRate float64
	maxDelay time.Duration
}

var ErrUnreachable = errors.New("memnet: peer unreachable")

func NewMemNetwork() *MemNetwork {
	return &MemNetwork{nodes: map[string]*Raft{}, group: map[string]int{}}
}

func (n *MemNetwork) Register(id string, r *Raft) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.nodes[id] = r
}

func (n *MemNetwork) Unregister(id string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.nodes, id)
}

// Partition splits the cluster: each argument is one side. Nodes not listed
// keep group 0.
func (n *MemNetwork) Partition(groups ...[]string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.group = map[string]int{}
	for g, ids := range groups {
		for _, id := range ids {
			n.group[id] = g + 1
		}
	}
}

// Isolate cuts one node off from everyone else.
func (n *MemNetwork) Isolate(id string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.group[id] = -1 - len(n.group)
}

func (n *MemNetwork) Heal() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.group = map[string]int{}
}

// SetUnreliable sets message drop probability and max one-way delay.
func (n *MemNetwork) SetUnreliable(dropRate float64, maxDelay time.Duration) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.dropRate, n.maxDelay = dropRate, maxDelay
}

func (n *MemNetwork) route(from, to string) (*Raft, bool, time.Duration) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	target := n.nodes[to]
	if target == nil || n.group[from] != n.group[to] || rand.Float64() < n.dropRate {
		return nil, false, 0
	}
	var d time.Duration
	if n.maxDelay > 0 {
		d = time.Duration(rand.Int63n(int64(n.maxDelay)))
	}
	return target, true, d
}

func (n *MemNetwork) deliver(ctx context.Context, from, to string, call func(*Raft) error) error {
	target, ok, d := n.route(from, to)
	if !ok {
		// a lost message looks like a timeout to the sender
		select {
		case <-time.After(5 * time.Millisecond):
		case <-ctx.Done():
		}
		return ErrUnreachable
	}
	if d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := call(target); err != nil {
		return err
	}
	// the reply can be lost too
	if _, ok, _ := n.route(to, from); !ok {
		return ErrUnreachable
	}
	return ctx.Err()
}

// Transport returns the endpoint used by node `from`.
func (n *MemNetwork) Transport(from string) Transport { return &memTransport{net: n, from: from} }

type memTransport struct {
	net  *MemNetwork
	from string
}

func (t *memTransport) RequestVote(ctx context.Context, peer string, args *RequestVoteArgs) (*RequestVoteReply, error) {
	var reply *RequestVoteReply
	err := t.net.deliver(ctx, t.from, peer, func(r *Raft) (err error) {
		reply, err = r.HandleRequestVote(args)
		return
	})
	return reply, err
}

func (t *memTransport) AppendEntries(ctx context.Context, peer string, args *AppendEntriesArgs) (*AppendEntriesReply, error) {
	var reply *AppendEntriesReply
	err := t.net.deliver(ctx, t.from, peer, func(r *Raft) (err error) {
		reply, err = r.HandleAppendEntries(args)
		return
	})
	return reply, err
}
