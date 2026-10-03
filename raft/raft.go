package raft

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"sync"
	"time"
)

// Config tunes a Raft node. Zero durations fall back to DefaultConfig values.
type Config struct {
	ID    string
	Peers []string // IDs of every other member of the cluster

	ElectionTimeoutMin time.Duration
	ElectionTimeoutMax time.Duration
	HeartbeatInterval  time.Duration
	RPCTimeout         time.Duration
	MaxBatch           int // max entries per AppendEntries

	Logger  *log.Logger
	OnEvent func(Event) // optional; called without Raft's lock held
}

func DefaultConfig(id string, peers []string) Config {
	return Config{
		ID:                 id,
		Peers:              peers,
		ElectionTimeoutMin: 150 * time.Millisecond,
		ElectionTimeoutMax: 300 * time.Millisecond,
		HeartbeatInterval:  50 * time.Millisecond,
		RPCTimeout:         100 * time.Millisecond,
		MaxBatch:           256,
	}
}

var ErrStopped = errors.New("raft: node stopped")

// Raft is a single consensus participant. All mutable state is guarded by mu.
type Raft struct {
	mu        sync.Mutex
	cfg       Config
	id        string
	peers     []string
	transport Transport
	storage   Storage
	logger    *log.Logger

	// persistent (mirrored in storage)
	currentTerm uint64
	votedFor    string
	log         []Entry // log[0] is a sentinel {Index: 0, Term: 0}

	// volatile
	role             Role
	leaderID         string
	commitIndex      uint64
	lastApplied      uint64
	electionDeadline time.Time
	lastLeaderSeen   time.Time // last valid AppendEntries from a leader
	preVoteRound     uint64

	// leader-only
	nextIndex  map[string]uint64
	matchIndex map[string]uint64
	lastAck    map[string]time.Time
	triggers   map[string]chan struct{}

	elections     uint64
	termsAsLeader uint64

	applyCh   chan ApplyMsg
	applyCond *sync.Cond
	stopCh    chan struct{}
	stopped   bool
	wg        sync.WaitGroup
	events    chan Event

	// Group commit: the leader appends proposals in memory and a background
	// persister fsyncs them in batches, in parallel with replication.
	persistedIndex uint64 // highest index durable in storage
	persistCh      chan struct{}
}

// New restores state from storage and starts the node's background loops.
func New(cfg Config, transport Transport, storage Storage) (*Raft, error) {
	def := DefaultConfig(cfg.ID, cfg.Peers)
	if cfg.ElectionTimeoutMin == 0 {
		cfg.ElectionTimeoutMin = def.ElectionTimeoutMin
	}
	if cfg.ElectionTimeoutMax <= cfg.ElectionTimeoutMin {
		cfg.ElectionTimeoutMax = 2 * cfg.ElectionTimeoutMin
	}
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = def.HeartbeatInterval
	}
	if cfg.RPCTimeout == 0 {
		cfg.RPCTimeout = def.RPCTimeout
	}
	if cfg.MaxBatch == 0 {
		cfg.MaxBatch = def.MaxBatch
	}
	logger := cfg.Logger
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}

	hs, entries, err := storage.Load()
	if err != nil {
		return nil, fmt.Errorf("raft: load state: %w", err)
	}
	r := &Raft{
		cfg:         cfg,
		id:          cfg.ID,
		peers:       append([]string(nil), cfg.Peers...),
		transport:   transport,
		storage:     storage,
		logger:      logger,
		currentTerm: hs.Term,
		votedFor:    hs.VotedFor,
		log:         append([]Entry{{}}, entries...),
		role:        Follower,
		applyCh:     make(chan ApplyMsg, 1024),
		stopCh:      make(chan struct{}),
		events:      make(chan Event, 256),
		persistCh:   make(chan struct{}, 1),
	}
	r.applyCond = sync.NewCond(&r.mu)
	r.resetElectionTimerLocked()
	r.logger.Printf("[%s] restored term=%d votedFor=%q log=%d entries", r.id, r.currentTerm, r.votedFor, len(entries))

	r.persistedIndex = r.lastIndex()
	r.wg.Add(4)
	go r.persister()
	go r.ticker()
	go r.applier()
	go r.eventPump()
	return r, nil
}

// ApplyCh delivers committed entries in log order. It is closed on Stop.
func (r *Raft) ApplyCh() <-chan ApplyMsg { return r.applyCh }

// Stop halts the node and closes its storage. Safe to call more than once.
func (r *Raft) Stop() {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.stopped = true
	close(r.stopCh)
	r.applyCond.Broadcast()
	r.mu.Unlock()
	r.wg.Wait()
	r.storage.Close()
}

// Propose appends a command to the leader's log. It returns the index and term
// the command will occupy if it commits, or isLeader=false.
func (r *Raft) Propose(cmd []byte) (index, term uint64, isLeader bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped || r.role != Leader {
		return 0, r.currentTerm, false
	}
	e := r.appendLocked(cmd)
	r.kickReplicatorsLocked()
	if len(r.peers) == 0 {
		r.advanceCommitLocked()
	}
	return e.Index, e.Term, true
}

// Leader returns the ID of the current leader as known by this node.
func (r *Raft) Leader() (id string, isSelf bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.leaderID, r.role == Leader
}

func (r *Raft) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Status{
		ID:            r.id,
		Role:          r.role.String(),
		Term:          r.currentTerm,
		Leader:        r.leaderID,
		VotedFor:      r.votedFor,
		CommitIndex:   r.commitIndex,
		LastApplied:   r.lastApplied,
		LastLogIndex:  r.lastIndex(),
		LastLogTerm:   r.lastTerm(),
		Elections:     r.elections,
		TermsAsLeader: r.termsAsLeader,
	}
}

// ---- helpers (call with mu held) ----

func (r *Raft) lastIndex() uint64 { return r.log[len(r.log)-1].Index }
func (r *Raft) lastTerm() uint64  { return r.log[len(r.log)-1].Term }
func (r *Raft) quorum() int       { return (len(r.peers)+1)/2 + 1 }

func (r *Raft) resetElectionTimerLocked() {
	span := int64(r.cfg.ElectionTimeoutMax - r.cfg.ElectionTimeoutMin)
	r.electionDeadline = time.Now().Add(r.cfg.ElectionTimeoutMin + time.Duration(rand.Int63n(span)))
}

func (r *Raft) persistHardStateLocked() {
	if err := r.storage.SaveHardState(HardState{Term: r.currentTerm, VotedFor: r.votedFor}); err != nil {
		// A node that cannot persist its vote must not keep participating.
		panic(fmt.Sprintf("raft %s: persist hard state: %v", r.id, err))
	}
}

// appendLocked adds a leader entry to the in-memory log and wakes the
// persister. The entry is not durable yet; advanceCommitLocked only counts
// the leader itself once persistedIndex covers it.
func (r *Raft) appendLocked(cmd []byte) Entry {
	e := Entry{Index: r.lastIndex() + 1, Term: r.currentTerm, Command: cmd}
	r.log = append(r.log, e)
	select {
	case r.persistCh <- struct{}{}:
	default:
	}
	return e
}

// flushLocked makes every in-memory entry durable with a single write+fsync.
func (r *Raft) flushLocked() {
	if r.persistedIndex >= r.lastIndex() {
		return
	}
	if err := r.storage.Append(r.log[r.persistedIndex+1:]); err != nil {
		panic(fmt.Sprintf("raft %s: persist entries: %v", r.id, err))
	}
	r.persistedIndex = r.lastIndex()
}

func (r *Raft) persister() {
	defer r.wg.Done()
	for {
		select {
		case <-r.stopCh:
			return
		case <-r.persistCh:
		}
		r.mu.Lock()
		if !r.stopped {
			r.flushLocked()
			if r.role == Leader {
				r.advanceCommitLocked()
			}
		}
		r.mu.Unlock()
	}
}

func (r *Raft) emitLocked(kind, detail string) {
	ev := Event{Time: time.Now(), Node: r.id, Kind: kind, Term: r.currentTerm, Detail: detail}
	r.logger.Printf("[%s] term=%d %s %s", r.id, r.currentTerm, kind, detail)
	select {
	case r.events <- ev:
	default: // never block consensus on observers
	}
}

func (r *Raft) eventPump() {
	defer r.wg.Done()
	for {
		select {
		case <-r.stopCh:
			return
		case ev := <-r.events:
			if r.cfg.OnEvent != nil {
				r.cfg.OnEvent(ev)
			}
		}
	}
}

func (r *Raft) becomeFollowerLocked(term uint64, leader string) {
	prev := r.role
	if prev == Leader {
		r.flushLocked() // storage must match the in-memory log before follower writes
	}
	if term > r.currentTerm {
		r.currentTerm = term
		r.votedFor = ""
		r.persistHardStateLocked()
	}
	r.role = Follower
	if leader != "" || prev == Leader {
		r.leaderID = leader
	}
	if prev != Follower {
		r.emitLocked("follower", fmt.Sprintf("stepped down from %s", prev))
	}
}

// ---- election ----

func (r *Raft) ticker() {
	defer r.wg.Done()
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case <-t.C:
		}
		r.mu.Lock()
		if r.stopped {
			r.mu.Unlock()
			return
		}
		if r.role == Leader {
			r.checkQuorumLocked()
		} else if time.Now().After(r.electionDeadline) {
			r.startPreVoteLocked()
		}
		r.mu.Unlock()
	}
}

// checkQuorumLocked makes a leader that can no longer reach a majority step
// down, so a partitioned old leader stops claiming leadership (and clients
// are redirected) instead of silently accepting writes it can never commit.
func (r *Raft) checkQuorumLocked() {
	if len(r.peers) == 0 {
		return
	}
	reachable := 1
	cutoff := time.Now().Add(-r.cfg.ElectionTimeoutMax)
	for _, p := range r.peers {
		if r.lastAck[p].After(cutoff) {
			reachable++
		}
	}
	if reachable < r.quorum() {
		r.emitLocked("stepdown", "lost contact with majority")
		r.becomeFollowerLocked(r.currentTerm, "")
		r.resetElectionTimerLocked()
	}
}

// startPreVoteLocked runs the Pre-Vote phase (Raft dissertation §9.6): ask
// peers whether they would vote for us in term+1, and only start a real
// election if a majority says yes.
func (r *Raft) startPreVoteLocked() {
	r.resetElectionTimerLocked()
	if r.quorum() == 1 {
		r.startElectionLocked()
		return
	}
	r.preVoteRound++
	round, term := r.preVoteRound, r.currentTerm
	args := &RequestVoteArgs{Term: term + 1, CandidateID: r.id, LastLogIndex: r.lastIndex(), LastLogTerm: r.lastTerm(), PreVote: true}
	votes := 1
	for _, p := range r.peers {
		go func(peer string) {
			ctx, cancel := context.WithTimeout(context.Background(), r.cfg.RPCTimeout)
			defer cancel()
			reply, err := r.transport.RequestVote(ctx, peer, args)
			if err != nil {
				return
			}
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.stopped {
				return
			}
			if reply.Term > r.currentTerm {
				r.becomeFollowerLocked(reply.Term, "")
				return
			}
			if r.preVoteRound != round || r.currentTerm != term || r.role == Leader || !reply.VoteGranted {
				return
			}
			votes++
			if votes == r.quorum() {
				r.startElectionLocked()
			}
		}(p)
	}
}

func (r *Raft) startElectionLocked() {
	r.role = Candidate
	r.currentTerm++
	r.votedFor = r.id
	r.leaderID = ""
	r.elections++
	r.persistHardStateLocked()
	r.resetElectionTimerLocked()
	r.emitLocked("election", "timeout, requesting votes")

	term := r.currentTerm
	args := &RequestVoteArgs{Term: term, CandidateID: r.id, LastLogIndex: r.lastIndex(), LastLogTerm: r.lastTerm()}
	votes := 1
	if votes >= r.quorum() {
		r.becomeLeaderLocked()
		return
	}
	for _, p := range r.peers {
		go func(peer string) {
			ctx, cancel := context.WithTimeout(context.Background(), r.cfg.RPCTimeout)
			defer cancel()
			reply, err := r.transport.RequestVote(ctx, peer, args)
			if err != nil {
				return
			}
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.stopped {
				return
			}
			if reply.Term > r.currentTerm {
				r.becomeFollowerLocked(reply.Term, "")
				return
			}
			if r.role != Candidate || r.currentTerm != term || !reply.VoteGranted {
				return
			}
			votes++
			if votes >= r.quorum() {
				r.becomeLeaderLocked()
			}
		}(p)
	}
}

func (r *Raft) becomeLeaderLocked() {
	r.role = Leader
	r.leaderID = r.id
	r.termsAsLeader++
	now := time.Now()
	r.nextIndex = make(map[string]uint64)
	r.matchIndex = make(map[string]uint64)
	r.lastAck = make(map[string]time.Time)
	r.triggers = make(map[string]chan struct{})
	for _, p := range r.peers {
		r.nextIndex[p] = r.lastIndex() + 1
		r.lastAck[p] = now
	}
	r.emitLocked("leader", fmt.Sprintf("won election with log at %d", r.lastIndex()))

	// A no-op in the new term lets the leader commit entries from earlier
	// terms (Raft only counts replicas for current-term entries, Figure 8).
	r.appendLocked(nil)

	term := r.currentTerm
	for _, p := range r.peers {
		ch := make(chan struct{}, 1)
		ch <- struct{}{}
		r.triggers[p] = ch
		r.wg.Add(1)
		go r.replicator(p, term, ch)
	}
	if len(r.peers) == 0 {
		r.advanceCommitLocked()
	}
}

// HandleRequestVote is the receiver side of RequestVote.
func (r *Raft) HandleRequestVote(args *RequestVoteArgs) (*RequestVoteReply, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return nil, ErrStopped
	}
	upToDate := args.LastLogTerm > r.lastTerm() ||
		(args.LastLogTerm == r.lastTerm() && args.LastLogIndex >= r.lastIndex())
	if args.PreVote {
		// No state changes. Refuse while we still have a live leader
		// ("leader stickiness"), so a rejoining node can't depose it.
		hasLeader := r.role == Leader || time.Since(r.lastLeaderSeen) < r.cfg.ElectionTimeoutMin
		return &RequestVoteReply{
			Term:        r.currentTerm,
			VoteGranted: args.Term > r.currentTerm && upToDate && !hasLeader,
		}, nil
	}
	if args.Term > r.currentTerm {
		r.becomeFollowerLocked(args.Term, "")
	}
	reply := &RequestVoteReply{Term: r.currentTerm}
	if args.Term < r.currentTerm {
		return reply, nil
	}
	if (r.votedFor == "" || r.votedFor == args.CandidateID) && upToDate {
		r.votedFor = args.CandidateID
		r.persistHardStateLocked()
		r.resetElectionTimerLocked()
		reply.VoteGranted = true
	}
	return reply, nil
}

// ---- replication ----

func (r *Raft) kickReplicatorsLocked() {
	for _, ch := range r.triggers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// replicator owns replication to one peer for one leadership term. It keeps a
// single RPC in flight, sends heartbeats when idle, and pipelines batches
// back-to-back while the follower is behind.
func (r *Raft) replicator(peer string, term uint64, trigger chan struct{}) {
	defer r.wg.Done()
	hb := time.NewTicker(r.cfg.HeartbeatInterval)
	defer hb.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case <-hb.C:
		case <-trigger:
		}
		for more := true; more; {
			r.mu.Lock()
			if r.stopped || r.role != Leader || r.currentTerm != term {
				r.mu.Unlock()
				return
			}
			next := r.nextIndex[peer]
			end := r.lastIndex() + 1
			if end-next > uint64(r.cfg.MaxBatch) {
				end = next + uint64(r.cfg.MaxBatch)
			}
			args := &AppendEntriesArgs{
				Term:         term,
				LeaderID:     r.id,
				PrevLogIndex: next - 1,
				PrevLogTerm:  r.log[next-1].Term,
				Entries:      append([]Entry(nil), r.log[next:end]...),
				LeaderCommit: r.commitIndex,
			}
			r.mu.Unlock()

			ctx, cancel := context.WithTimeout(context.Background(), r.cfg.RPCTimeout)
			reply, err := r.transport.AppendEntries(ctx, peer, args)
			cancel()
			if err != nil {
				break
			}
			r.mu.Lock()
			more = r.handleAppendReplyLocked(peer, term, args, reply)
			r.mu.Unlock()
		}
	}
}

// handleAppendReplyLocked returns true if another AppendEntries should be
// sent right away (follower still behind).
func (r *Raft) handleAppendReplyLocked(peer string, term uint64, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	if r.stopped {
		return false
	}
	if reply.Term > r.currentTerm {
		r.becomeFollowerLocked(reply.Term, "")
		r.resetElectionTimerLocked()
		return false
	}
	if r.role != Leader || r.currentTerm != term {
		return false
	}
	r.lastAck[peer] = time.Now()

	if reply.Success {
		match := args.PrevLogIndex + uint64(len(args.Entries))
		if match > r.matchIndex[peer] {
			r.matchIndex[peer] = match
		}
		if match+1 > r.nextIndex[peer] {
			r.nextIndex[peer] = match + 1
		}
		r.advanceCommitLocked()
		return r.nextIndex[peer] <= r.lastIndex()
	}

	// Conflict: back up nextIndex using the follower's hint.
	next := reply.ConflictIndex
	if reply.ConflictTerm != 0 {
		for i := args.PrevLogIndex; i > 0; i-- {
			if t := r.log[i].Term; t == reply.ConflictTerm {
				next = i + 1
				break
			} else if t < reply.ConflictTerm {
				break
			}
		}
	}
	if next < 1 {
		next = 1
	}
	if next > r.lastIndex()+1 {
		next = r.lastIndex() + 1
	}
	r.nextIndex[peer] = next
	return true
}

// advanceCommitLocked commits the highest current-term index stored on a
// majority. Earlier-term entries commit indirectly (Raft Â§5.4.2).
func (r *Raft) advanceCommitLocked() {
	for n := r.lastIndex(); n > r.commitIndex; n-- {
		if r.log[n].Term != r.currentTerm {
			return
		}
		count := 0
		if r.persistedIndex >= n {
			count = 1
		}
		for _, p := range r.peers {
			if r.matchIndex[p] >= n {
				count++
			}
		}
		if count >= r.quorum() {
			r.commitIndex = n
			r.applyCond.Broadcast()
			r.kickReplicatorsLocked() // propagate the new commit index promptly
			return
		}
	}
}

// HandleAppendEntries is the receiver side of AppendEntries (also heartbeat).
func (r *Raft) HandleAppendEntries(args *AppendEntriesArgs) (*AppendEntriesReply, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return nil, ErrStopped
	}
	reply := &AppendEntriesReply{Term: r.currentTerm}
	if args.Term < r.currentTerm {
		return reply, nil
	}
	if args.Term > r.currentTerm || r.role != Follower {
		r.becomeFollowerLocked(args.Term, args.LeaderID)
	}
	if r.leaderID != args.LeaderID {
		r.leaderID = args.LeaderID
		r.emitLocked("follower", "following "+args.LeaderID)
	}
	r.lastLeaderSeen = time.Now()
	r.resetElectionTimerLocked()
	reply.Term = r.currentTerm

	if args.PrevLogIndex > r.lastIndex() {
		reply.ConflictIndex = r.lastIndex() + 1
		return reply, nil
	}
	if t := r.log[args.PrevLogIndex].Term; t != args.PrevLogTerm {
		reply.ConflictTerm = t
		i := args.PrevLogIndex
		for i > 1 && r.log[i-1].Term == t {
			i--
		}
		reply.ConflictIndex = i
		return reply, nil
	}

	r.flushLocked() // no-op unless we were just leader with unflushed entries

	// Skip entries we already have; truncate only on a real conflict, so a
	// delayed, shorter AppendEntries can never erase newer entries.
	for i, e := range args.Entries {
		if e.Index <= r.lastIndex() {
			if r.log[e.Index].Term == e.Term {
				continue
			}
			if e.Index <= r.commitIndex {
				panic(fmt.Sprintf("raft %s: leader tried to overwrite committed index %d", r.id, e.Index))
			}
			if err := r.storage.TruncateFrom(e.Index); err != nil {
				panic(fmt.Sprintf("raft %s: truncate: %v", r.id, err))
			}
			r.log = r.log[:e.Index]
			r.persistedIndex = min(r.persistedIndex, e.Index-1)
		}
		rest := args.Entries[i:]
		if err := r.storage.Append(rest); err != nil {
			panic(fmt.Sprintf("raft %s: persist entries: %v", r.id, err))
		}
		r.log = append(r.log, rest...)
		r.persistedIndex = r.lastIndex()
		break
	}

	if args.LeaderCommit > r.commitIndex {
		lastNew := args.PrevLogIndex + uint64(len(args.Entries))
		r.commitIndex = min(args.LeaderCommit, lastNew)
		r.applyCond.Broadcast()
	}
	reply.Success = true
	return reply, nil
}

// ---- apply ----

func (r *Raft) applier() {
	defer r.wg.Done()
	defer close(r.applyCh)
	for {
		r.mu.Lock()
		for !r.stopped && r.lastApplied >= r.commitIndex {
			r.applyCond.Wait()
		}
		if r.stopped {
			r.mu.Unlock()
			return
		}
		start, end := r.lastApplied+1, r.commitIndex
		msgs := make([]ApplyMsg, 0, end-start+1)
		for i := start; i <= end; i++ {
			msgs = append(msgs, ApplyMsg{Index: i, Term: r.log[i].Term, Command: r.log[i].Command})
		}
		r.mu.Unlock()

		for _, m := range msgs {
			select {
			case r.applyCh <- m:
			case <-r.stopCh:
				return
			}
		}
		r.mu.Lock()
		if end > r.lastApplied {
			r.lastApplied = end
		}
		r.mu.Unlock()
	}
}
