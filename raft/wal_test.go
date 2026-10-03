package raft

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWALRecovery(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenFileStorage(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	must(t, s.SaveHardState(HardState{Term: 3, VotedFor: "n2"}))
	must(t, s.Append([]Entry{{Index: 1, Term: 1, Command: []byte("a")}, {Index: 2, Term: 1, Command: []byte("b")}}))
	must(t, s.Append([]Entry{{Index: 3, Term: 2, Command: []byte("c")}}))
	must(t, s.TruncateFrom(3)) // conflicting entry removed
	must(t, s.Append([]Entry{{Index: 3, Term: 3, Command: []byte("d")}}))
	must(t, s.Close())

	// simulate a crash in the middle of writing a record
	f, err := os.OpenFile(filepath.Join(dir, walName), os.O_WRONLY|os.O_APPEND, 0)
	must(t, err)
	f.WriteString(`deadbeef {"k":"ent","e":{"i":4,"t":3`)
	f.Close()

	s, err = OpenFileStorage(dir, true)
	if err != nil {
		t.Fatalf("torn tail should be tolerated: %v", err)
	}
	defer s.Close()
	hs, es, _ := s.Load()
	if hs.Term != 3 || hs.VotedFor != "n2" {
		t.Fatalf("hard state = %+v", hs)
	}
	if len(es) != 3 || string(es[2].Command) != "d" || es[2].Term != 3 {
		t.Fatalf("entries = %+v", es)
	}
	// the WAL keeps working after recovery
	must(t, s.Append([]Entry{{Index: 4, Term: 3, Command: []byte("e")}}))
}

func TestWALMidLogCorruptionIsFatal(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenFileStorage(dir, false)
	must(t, err)
	must(t, s.Append([]Entry{{Index: 1, Term: 1}, {Index: 2, Term: 1}}))
	s.Close()
	path := filepath.Join(dir, walName)
	data, _ := os.ReadFile(path)
	// flip a byte inside the first entry record (second line)
	lines := 0
	for i, b := range data {
		if b == '\n' {
			lines++
			if lines == 1 {
				data[i+12] ^= 0xff
				break
			}
		}
	}
	os.WriteFile(path, data, 0o644)
	if _, err := OpenFileStorage(dir, false); err != ErrCorruptWAL {
		t.Fatalf("expected ErrCorruptWAL, got %v", err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
