// Package kv is the replicated state machine: a key-value map whose every
// mutation (and every linearizable read) goes through the Raft log.
package kv

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"

	"github.com/Lecrupus/raftkv/raft"
)

type OpType string

const (
	OpPut    OpType = "put"
	OpDelete OpType = "delete"
	OpGet    OpType = "get"
)

// Op is the command replicated through Raft. ClientID+Seq make writes
// idempotent: a retried request is applied at most once.
type Op struct {
	Type     OpType `json:"t"`
	Key      string `json:"k"`
	Value    string `json:"v,omitempty"`
	ClientID string `json:"c,omitempty"`
	Seq      uint64 `json:"s,omitempty"`
}

type Result struct {
	Value string `json:"value,omitempty"`
	Found bool   `json:"found"`
	Index uint64 `json:"index"`
}

var (
	ErrNotLeader      = errors.New("not the leader")
	ErrLeadershipLost = errors.New("leadership lost before the command committed")
	ErrStopped        = errors.New("store stopped")
)

type session struct {
	seq    uint64
	result Result
}

type waiter struct {
	term uint64
	ch   chan applied
}

type applied struct {
	term uint64
	res  Result
}

type Store struct {
	raft *raft.Raft

	mu           sync.Mutex
	data         map[string]string
	sessions     map[string]session
	waiters      map[uint64]waiter
	appliedIndex uint64
	dedupHits    uint64
	done         chan struct{}
}

// New starts consuming r's apply channel. The store stops when r stops.
func New(r *raft.Raft) *Store {
	s := &Store{
		raft:     r,
		data:     map[string]string{},
		sessions: map[string]session{},
		waiters:  map[uint64]waiter{},
		done:     make(chan struct{}),
	}
	go s.applyLoop()
	return s
}

// Do replicates op through Raft and returns its result once applied. Reads
// submitted this way are linearizable: they are ordered in the log with
// writes, and only a leader with a live majority can commit them.
func (s *Store) Do(ctx context.Context, op Op) (Result, error) {
	cmd, err := json.Marshal(op)
	if err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	idx, term, ok := s.raft.Propose(cmd)
	if !ok {
		s.mu.Unlock()
		return Result{}, ErrNotLeader
	}
	if old, exists := s.waiters[idx]; exists {
		old.ch <- applied{} // an earlier proposal at this index was overwritten
	}
	ch := make(chan applied, 1)
	s.waiters[idx] = waiter{term: term, ch: ch}
	s.mu.Unlock()

	select {
	case a := <-ch:
		if a.term != term {
			return Result{}, ErrLeadershipLost
		}
		return a.res, nil
	case <-ctx.Done():
		s.mu.Lock()
		if w, ok := s.waiters[idx]; ok && w.ch == ch {
			delete(s.waiters, idx)
		}
		s.mu.Unlock()
		return Result{}, ctx.Err()
	case <-s.done:
		return Result{}, ErrStopped
	}
}

// LocalGet reads this replica's state without consensus. It may be stale.
func (s *Store) LocalGet(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	return v, ok
}

// Keys returns this replica's keys (possibly stale), sorted.
func (s *Store) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

type Stats struct {
	Keys         int    `json:"keys"`
	AppliedIndex uint64 `json:"appliedIndex"`
	DedupHits    uint64 `json:"dedupHits"`
}

func (s *Store) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Stats{Keys: len(s.data), AppliedIndex: s.appliedIndex, DedupHits: s.dedupHits}
}

func (s *Store) applyLoop() {
	defer close(s.done)
	for msg := range s.raft.ApplyCh() {
		s.mu.Lock()
		var res Result
		if len(msg.Command) > 0 {
			var op Op
			if err := json.Unmarshal(msg.Command, &op); err == nil {
				res = s.applyLocked(op)
			}
		}
		res.Index = msg.Index
		s.appliedIndex = msg.Index
		if w, ok := s.waiters[msg.Index]; ok {
			w.ch <- applied{term: msg.Term, res: res}
			delete(s.waiters, msg.Index)
		}
		s.mu.Unlock()
	}
}

func (s *Store) applyLocked(op Op) Result {
	if op.Type == OpGet {
		v, ok := s.data[op.Key]
		return Result{Value: v, Found: ok}
	}
	if op.ClientID != "" {
		if sess, ok := s.sessions[op.ClientID]; ok && op.Seq <= sess.seq {
			s.dedupHits++
			return sess.result // duplicate of an already-applied request
		}
	}
	var res Result
	switch op.Type {
	case OpPut:
		s.data[op.Key] = op.Value
		res = Result{Value: op.Value, Found: true}
	case OpDelete:
		_, existed := s.data[op.Key]
		delete(s.data, op.Key)
		res = Result{Found: existed}
	}
	if op.ClientID != "" {
		s.sessions[op.ClientID] = session{seq: op.Seq, result: res}
	}
	return res
}
