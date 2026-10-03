package raft

import (
	"fmt"
	"sync"
)

// Storage persists Raft's hard state and log. Every method must be durable
// when it returns: Raft replies to RPCs only after persisting.
type Storage interface {
	// Load returns the persisted hard state and log entries (indices 1..n).
	Load() (HardState, []Entry, error)
	SaveHardState(HardState) error
	// Append adds entries to the end of the log. Entries are contiguous and
	// start right after the current last index.
	Append([]Entry) error
	// TruncateFrom deletes every entry with Index >= index.
	TruncateFrom(index uint64) error
	Close() error
}

// MemoryStorage is a Storage kept in memory. It survives a simulated crash as
// long as the same instance is passed to the restarted node, which is how the
// tests model a disk.
type MemoryStorage struct {
	mu      sync.Mutex
	hs      HardState
	entries []Entry
}

func NewMemoryStorage() *MemoryStorage { return &MemoryStorage{} }

func (m *MemoryStorage) Load() (HardState, []Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hs, append([]Entry(nil), m.entries...), nil
}

func (m *MemoryStorage) SaveHardState(hs HardState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hs = hs
	return nil
}

func (m *MemoryStorage) Append(es []Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range es {
		if e.Index != uint64(len(m.entries))+1 {
			return fmt.Errorf("memory storage: non-contiguous append at %d (have %d)", e.Index, len(m.entries))
		}
		m.entries = append(m.entries, e)
	}
	return nil
}

func (m *MemoryStorage) TruncateFrom(index uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index == 0 {
		index = 1
	}
	if index <= uint64(len(m.entries)) {
		m.entries = m.entries[:index-1]
	}
	return nil
}

func (m *MemoryStorage) Close() error { return nil }
