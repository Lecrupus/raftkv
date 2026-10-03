package lincheck

import "testing"

func TestLinearizableHistory(t *testing.T) {
	// put(x,1) overlaps a get that sees 1; a later get sees 1.
	h := []Op{
		{Key: "x", Kind: Put, Value: "1", Call: 0, Return: 10},
		{Key: "x", Kind: Get, Value: "1", Found: true, Call: 5, Return: 8},
		{Key: "x", Kind: Get, Value: "1", Found: true, Call: 12, Return: 15},
	}
	if r, _ := Check(h); !r.OK {
		t.Fatal("expected linearizable")
	}
}

func TestStaleReadDetected(t *testing.T) {
	// put(x,2) completes, then a read returns the old value 1: not linearizable.
	h := []Op{
		{Key: "x", Kind: Put, Value: "1", Call: 0, Return: 1},
		{Key: "x", Kind: Put, Value: "2", Call: 2, Return: 3},
		{Key: "x", Kind: Get, Value: "1", Found: true, Call: 4, Return: 5},
	}
	if r, _ := Check(h); r.OK {
		t.Fatal("stale read should be rejected")
	}
}

func TestUnknownWriteMayOrMayNotApply(t *testing.T) {
	h := []Op{
		{Key: "x", Kind: Put, Value: "1", Call: 0, Return: 1},
		{Key: "x", Kind: Put, Value: "2", Call: 2, Return: Unknown}, // timed out
		{Key: "x", Kind: Get, Value: "1", Found: true, Call: 3, Return: 4},
		{Key: "x", Kind: Get, Value: "2", Found: true, Call: 5, Return: 6},
	}
	if r, _ := Check(h); !r.OK {
		t.Fatal("timed-out write applied late is linearizable")
	}
	// ...but it cannot un-apply: seeing 2 then 1 again is a violation.
	h = append(h, Op{Key: "x", Kind: Get, Value: "1", Found: true, Call: 7, Return: 8})
	if r, _ := Check(h); r.OK {
		t.Fatal("value went backwards; should be rejected")
	}
}

func TestDeleteAndMissing(t *testing.T) {
	h := []Op{
		{Key: "x", Kind: Get, Found: false, Call: 0, Return: 1},
		{Key: "x", Kind: Put, Value: "a", Call: 2, Return: 3},
		{Key: "x", Kind: Delete, Call: 4, Return: 5},
		{Key: "x", Kind: Get, Found: false, Call: 6, Return: 7},
	}
	if r, _ := Check(h); !r.OK {
		t.Fatal("expected linearizable")
	}
}
