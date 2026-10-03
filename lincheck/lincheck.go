// Package lincheck verifies that a concurrent history of key-value operations
// is linearizable, in the spirit of Jepsen's Knossos and Porcupine.
//
// It uses the Wing & Gong search with memoization (Lowe's WGL variant):
// repeatedly pick an operation that could have taken effect first (its call
// precedes every pending return), apply it to a register model, and recurse.
// Keys are independent registers, so each key is checked separately
// (P-compositionality), which keeps the search small.
package lincheck

import (
	"fmt"
	"math"
	"sort"
)

type Kind int

const (
	Put Kind = iota
	Get
	Delete
)

// Op is one client operation. Return is math.MaxInt64 when the outcome is
// unknown (e.g. a write timed out: it may or may not have taken effect).
type Op struct {
	Key    string
	Kind   Kind
	Value  string // Put: value written. Get: value observed.
	Found  bool   // Get: whether the key existed.
	Call   int64
	Return int64
	Client int
}

const Unknown = math.MaxInt64

// MaxOpsPerKey bounds the search; the bitmask representation needs <= 64.
const MaxOpsPerKey = 64

type Result struct {
	OK      bool
	Key     string // first non-linearizable key, if any
	Checked int
}

func Check(history []Op) (Result, error) {
	byKey := map[string][]Op{}
	for _, op := range history {
		byKey[op.Key] = append(byKey[op.Key], op)
	}
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ops := byKey[k]
		if len(ops) > MaxOpsPerKey {
			return Result{}, fmt.Errorf("lincheck: key %q has %d ops (max %d)", k, len(ops), MaxOpsPerKey)
		}
		if !checkKey(ops) {
			return Result{OK: false, Key: k, Checked: len(history)}, nil
		}
	}
	return Result{OK: true, Checked: len(history)}, nil
}

type state struct {
	present bool
	value   string
}

func step(s state, op Op) (state, bool) {
	switch op.Kind {
	case Put:
		return state{true, op.Value}, true
	case Delete:
		return state{}, true
	case Get:
		if op.Found != s.present {
			return s, false
		}
		return s, !s.present || op.Value == s.value
	}
	return s, false
}

type memoKey struct {
	done uint64
	st   state
}

func checkKey(ops []Op) bool {
	sort.Slice(ops, func(i, j int) bool { return ops[i].Call < ops[j].Call })
	n := len(ops)
	full := uint64(1)<<uint(n) - 1
	if n == 64 {
		full = math.MaxUint64
	}
	seen := map[memoKey]bool{}

	var dfs func(done uint64, st state) bool
	dfs = func(done uint64, st state) bool {
		if done == full {
			return true
		}
		mk := memoKey{done, st}
		if seen[mk] {
			return false
		}
		seen[mk] = true

		minRet := int64(math.MaxInt64)
		for i := 0; i < n; i++ {
			if done&(1<<uint(i)) == 0 && ops[i].Return < minRet {
				minRet = ops[i].Return
			}
		}
		for i := 0; i < n; i++ {
			if done&(1<<uint(i)) != 0 {
				continue
			}
			if ops[i].Call > minRet {
				break // sorted by call: no later op can be first either
			}
			if next, ok := step(st, ops[i]); ok && dfs(done|1<<uint(i), next) {
				return true
			}
		}
		return false
	}
	return dfs(0, state{})
}
