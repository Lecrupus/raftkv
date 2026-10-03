package raft

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// FileStorage is a write-ahead log on disk. Every mutation is one or more
// records appended to a single file:
//
//	<crc32 hex> <json record>\n
//
// Records are hard-state updates, appended entries, or truncations. On open,
// the WAL is replayed and then rewritten in compacted form (current hard state
// plus live entries). A torn final record (crash mid-write) is detected by its
// checksum and discarded; corruption anywhere else is a hard error.
type FileStorage struct {
	mu   sync.Mutex
	dir  string
	f    *os.File
	sync bool

	// mirror of what is on disk, so Load is cheap and truncation can be checked
	hs      HardState
	entries []Entry
}

type walRecord struct {
	Kind  string     `json:"k"` // "hs", "ent", "trunc"
	HS    *HardState `json:"hs,omitempty"`
	Entry *Entry     `json:"e,omitempty"`
	Index uint64     `json:"i,omitempty"`
}

const walName = "raft.wal"

var ErrCorruptWAL = errors.New("wal: corrupt record before end of log")

// OpenFileStorage opens (or creates) the WAL in dir. With syncWrites, every
// write is fsynced before returning, which is what makes acknowledged writes
// survive power loss.
func OpenFileStorage(dir string, syncWrites bool) (*FileStorage, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &FileStorage{dir: dir, sync: syncWrites}
	if err := s.replay(); err != nil {
		return nil, err
	}
	if err := s.rewrite(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(s.path(), os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	s.f = f
	return s, nil
}

func (s *FileStorage) path() string { return filepath.Join(s.dir, walName) }

func (s *FileStorage) replay() error {
	f, err := os.Open(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 64<<20)
	torn := false
	for sc.Scan() {
		line := sc.Bytes()
		rec, ok := decodeRecord(line)
		if !ok {
			torn = true
			continue
		}
		if torn {
			// a valid record after a bad one: not a torn tail, real corruption
			return ErrCorruptWAL
		}
		if err := s.applyRecord(rec); err != nil {
			return err
		}
	}
	return sc.Err()
}

func decodeRecord(line []byte) (walRecord, bool) {
	var rec walRecord
	sp := bytes.IndexByte(line, ' ')
	if sp != 8 {
		return rec, false
	}
	want, err := strconv.ParseUint(string(line[:8]), 16, 32)
	if err != nil || crc32.ChecksumIEEE(line[9:]) != uint32(want) {
		return rec, false
	}
	if err := json.Unmarshal(line[9:], &rec); err != nil {
		return rec, false
	}
	return rec, true
}

func (s *FileStorage) applyRecord(rec walRecord) error {
	switch rec.Kind {
	case "hs":
		s.hs = *rec.HS
	case "ent":
		if rec.Entry.Index != uint64(len(s.entries))+1 {
			return fmt.Errorf("wal: non-contiguous entry %d after %d", rec.Entry.Index, len(s.entries))
		}
		s.entries = append(s.entries, *rec.Entry)
	case "trunc":
		if rec.Index >= 1 && rec.Index <= uint64(len(s.entries)) {
			s.entries = s.entries[:rec.Index-1]
		}
	default:
		return fmt.Errorf("wal: unknown record kind %q", rec.Kind)
	}
	return nil
}

func encodeRecord(buf *bytes.Buffer, rec walRecord) error {
	body, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	fmt.Fprintf(buf, "%08x ", crc32.ChecksumIEEE(body))
	buf.Write(body)
	buf.WriteByte('\n')
	return nil
}

// rewrite atomically replaces the WAL with a compacted copy.
func (s *FileStorage) rewrite() error {
	var buf bytes.Buffer
	hs := s.hs
	if err := encodeRecord(&buf, walRecord{Kind: "hs", HS: &hs}); err != nil {
		return err
	}
	for i := range s.entries {
		if err := encodeRecord(&buf, walRecord{Kind: "ent", Entry: &s.entries[i]}); err != nil {
			return err
		}
	}
	tmp := s.path() + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path())
}

func (s *FileStorage) write(buf *bytes.Buffer) error {
	if s.f == nil {
		return errors.New("wal: closed")
	}
	if _, err := s.f.Write(buf.Bytes()); err != nil {
		return err
	}
	if s.sync {
		return s.f.Sync()
	}
	return nil
}

func (s *FileStorage) Load() (HardState, []Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hs, append([]Entry(nil), s.entries...), nil
}

func (s *FileStorage) SaveHardState(hs HardState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var buf bytes.Buffer
	if err := encodeRecord(&buf, walRecord{Kind: "hs", HS: &hs}); err != nil {
		return err
	}
	if err := s.write(&buf); err != nil {
		return err
	}
	s.hs = hs
	return nil
}

func (s *FileStorage) Append(es []Entry) error {
	if len(es) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var buf bytes.Buffer
	next := uint64(len(s.entries)) + 1
	for i := range es {
		if es[i].Index != next+uint64(i) {
			return fmt.Errorf("wal: non-contiguous append at %d (expected %d)", es[i].Index, next+uint64(i))
		}
		if err := encodeRecord(&buf, walRecord{Kind: "ent", Entry: &es[i]}); err != nil {
			return err
		}
	}
	if err := s.write(&buf); err != nil {
		return err
	}
	s.entries = append(s.entries, es...)
	return nil
}

func (s *FileStorage) TruncateFrom(index uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index < 1 || index > uint64(len(s.entries)) {
		return nil
	}
	var buf bytes.Buffer
	if err := encodeRecord(&buf, walRecord{Kind: "trunc", Index: index}); err != nil {
		return err
	}
	if err := s.write(&buf); err != nil {
		return err
	}
	s.entries = s.entries[:index-1]
	return nil
}

func (s *FileStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	return err
}
