// Package raft implements the Raft consensus algorithm (Ongaro & Ousterhout,
// "In Search of an Understandable Consensus Algorithm", 2014): leader
// election, log replication, commitment rules and durable state.
package raft

import (
	"context"
	"time"
)

// Role is the current role of a Raft node.
type Role int

const (
	Follower Role = iota
	Candidate
	Leader
)

func (r Role) String() string {
	switch r {
	case Follower:
		return "follower"
	case Candidate:
		return "candidate"
	case Leader:
		return "leader"
	}
	return "unknown"
}

// Entry is a single replicated log entry. A nil Command is a leader no-op.
type Entry struct {
	Index   uint64 `json:"i"`
	Term    uint64 `json:"t"`
	Command []byte `json:"c,omitempty"`
}

// HardState is the state Raft must persist before answering any RPC.
type HardState struct {
	Term     uint64 `json:"term"`
	VotedFor string `json:"vote"`
}

type RequestVoteArgs struct {
	Term         uint64 `json:"term"`
	CandidateID  string `json:"candidate"`
	LastLogIndex uint64 `json:"lastLogIndex"`
	LastLogTerm  uint64 `json:"lastLogTerm"`
	// PreVote asks "would you vote for me?" without anyone changing state.
	// A node cut off from the cluster keeps failing pre-votes, so its term
	// never inflates and it cannot force a needless election when it rejoins.
	PreVote bool `json:"preVote,omitempty"`
}

type RequestVoteReply struct {
	Term        uint64 `json:"term"`
	VoteGranted bool   `json:"granted"`
}

type AppendEntriesArgs struct {
	Term         uint64  `json:"term"`
	LeaderID     string  `json:"leader"`
	PrevLogIndex uint64  `json:"prevIndex"`
	PrevLogTerm  uint64  `json:"prevTerm"`
	Entries      []Entry `json:"entries,omitempty"`
	LeaderCommit uint64  `json:"commit"`
}

// AppendEntriesReply carries the "fast backup" hint from section 5.3 of the
// paper so a leader can skip a whole conflicting term per round trip.
type AppendEntriesReply struct {
	Term          uint64 `json:"term"`
	Success       bool   `json:"success"`
	ConflictIndex uint64 `json:"conflictIndex"`
	ConflictTerm  uint64 `json:"conflictTerm"`
}

// Transport delivers RPCs to peers. Implementations: HTTP (production) and
// MemNetwork (tests, with partitions and message loss).
type Transport interface {
	RequestVote(ctx context.Context, peer string, args *RequestVoteArgs) (*RequestVoteReply, error)
	AppendEntries(ctx context.Context, peer string, args *AppendEntriesArgs) (*AppendEntriesReply, error)
}

// ApplyMsg is delivered on the apply channel once an entry is committed.
type ApplyMsg struct {
	Index   uint64
	Term    uint64
	Command []byte
}

// Event is a notable state change, surfaced to dashboards and logs.
type Event struct {
	Time   time.Time `json:"time"`
	Node   string    `json:"node"`
	Kind   string    `json:"kind"` // election, leader, follower, stepdown
	Term   uint64    `json:"term"`
	Detail string    `json:"detail"`
}

// Status is a point-in-time snapshot of a node's Raft state.
type Status struct {
	ID            string `json:"id"`
	Role          string `json:"role"`
	Term          uint64 `json:"term"`
	Leader        string `json:"leader"`
	VotedFor      string `json:"votedFor"`
	CommitIndex   uint64 `json:"commitIndex"`
	LastApplied   uint64 `json:"lastApplied"`
	LastLogIndex  uint64 `json:"lastLogIndex"`
	LastLogTerm   uint64 `json:"lastLogTerm"`
	Elections     uint64 `json:"elections"`
	TermsAsLeader uint64 `json:"termsAsLeader"`
}
