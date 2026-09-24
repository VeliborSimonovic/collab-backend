package crdt

import (
	"math/rand"
	"slices"
	"testing"
)

func checkInvariants(t *testing.T, d *Doc) {
	t.Helper()

	var fwd []*Item
	for it := d.start; it != nil; it = it.right {
		fwd = append(fwd, it)
		if len(fwd) > len(d.items)+5 {
			t.Fatalf("forward walk does not end (cycle?)")
		}
	}
	var bwd []*Item
	for it := d.end; it != nil; it = it.left {
		bwd = append(bwd, it)
		if len(bwd) > len(d.items)+5 {
			t.Fatalf("backward walk does not end (cycle?)")
		}
	}
	slices.Reverse(bwd)

	if len(fwd) == 0 || fwd[0] != d.start || fwd[len(fwd)-1] != d.end {
		t.Fatalf("forward walk must go from start to end, got %d items", len(fwd))
	}
	if !slices.Equal(fwd, bwd) {
		t.Fatalf("forward walk and backward walk disagree: broken left/right pointers")
	}
	if d.start.left != nil || d.end.right != nil {
		t.Fatalf("start.left and end.right must be nil")
	}
	if len(d.items) != len(fwd) {
		t.Fatalf("items map has %d entries but the list has %d (sentinels included)", len(d.items), len(fwd))
	}
	for _, it := range fwd {
		if d.items[it.ID] != it {
			t.Fatalf("item %v is in the list but not registered in d.items", it.ID)
		}
		if it == d.start || it == d.end {
			continue
		}
		if d.items[it.Origin] == nil || d.items[it.RightOrigin] == nil {
			t.Fatalf("item %v has an Origin or RightOrigin that does not exist", it.ID)
		}
	}
}

func typeText(t *testing.T, d *Doc, s string) {
	t.Helper()
	n := len([]rune(d.String()))
	for i, r := range []rune(s) {
		if _, err := d.LocalInsert(n+i, r); err != nil {
			t.Fatalf("LocalInsert(%d, %q): %v", n+i, r, err)
		}
		checkInvariants(t, d)
	}
}

func mustText(t *testing.T, d *Doc, want string) {
	t.Helper()
	if got := d.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestNewDocEmpty(t *testing.T) {
	d := NewDoc(1)
	checkInvariants(t, d)
	mustText(t, d, "")
	if got := d.Order(); len(got) != 0 {
		t.Fatalf("Order() of an empty doc = %v, want empty", got)
	}
}

func TestNewDocZeroPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewDoc(0) should panic")
		}
	}()
	NewDoc(0)
}

func TestHelloJello(t *testing.T) {
	d := NewDoc(1)
	typeText(t, d, "hello")
	mustText(t, d, "hello")

	if _, err := d.LocalDelete(0); err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, d)
	if _, err := d.LocalInsert(0, 'J'); err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, d)
	mustText(t, d, "Jello")
}

func TestInsertMiddleAndEnd(t *testing.T) {
	d := NewDoc(1)
	typeText(t, d, "ac")
	if _, err := d.LocalInsert(1, 'b'); err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, d)
	mustText(t, d, "abc")

	if _, err := d.LocalInsert(3, 'd'); err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, d)
	mustText(t, d, "abcd")

	if _, err := d.LocalInsert(0, 'Z'); err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, d)
	mustText(t, d, "Zabcd")
}

func TestInsertOpFields(t *testing.T) {
	d := NewDoc(7)
	a, _ := d.LocalInsert(0, 'a')
	b, _ := d.LocalInsert(1, 'b')

	if a.Origin != StartID || a.RightOrigin != EndID {
		t.Fatalf("a: origin=%v rightOrigin=%v, want START and END", a.Origin, a.RightOrigin)
	}
	if b.Origin != a.ID || b.RightOrigin != EndID {
		t.Fatalf("b: origin=%v rightOrigin=%v, want a.ID and END", b.Origin, b.RightOrigin)
	}
	if a.ID.Client != 7 || b.ID.Client != 7 {
		t.Fatalf("op IDs must carry the doc's client ID")
	}
	if a.Content != 'a' || b.Content != 'b' {
		t.Fatalf("op Content is wrong")
	}
}

func TestInsertRightOriginIsTombstone(t *testing.T) {
	d := NewDoc(1)
	typeText(t, d, "abc")
	del, _ := d.LocalDelete(1)
	bID := del.Target

	op, err := d.LocalInsert(1, 'X')
	if err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, d)
	mustText(t, d, "aXc")
	if op.RightOrigin != bID {
		t.Fatalf("RightOrigin = %v, want the tombstone %v", op.RightOrigin, bID)
	}
}

func TestDelete(t *testing.T) {
	cases := []struct {
		name string
		pos  int
		want string
	}{
		{"first", 0, "bcde"},
		{"middle", 2, "abde"},
		{"last", 4, "abcd"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := NewDoc(1)
			typeText(t, d, "abcde")
			op, err := d.LocalDelete(c.pos)
			if err != nil {
				t.Fatal(err)
			}
			checkInvariants(t, d)
			mustText(t, d, c.want)
			if op.Target == (ID{}) {
				t.Fatalf("Target must be the ID of the deleted item")
			}
		})
	}
}

func TestDeleteEverything(t *testing.T) {
	d := NewDoc(1)
	typeText(t, d, "abc")
	for i := 0; i < 3; i++ {
		if _, err := d.LocalDelete(0); err != nil {
			t.Fatal(err)
		}
		checkInvariants(t, d)
	}
	mustText(t, d, "")
	if _, err := d.LocalDelete(0); err == nil {
		t.Fatal("deleting from an empty text should fail")
	}
}

func TestOrderKeepsTombstones(t *testing.T) {
	d := NewDoc(1)
	typeText(t, d, "abcde")
	d.LocalDelete(1)
	d.LocalDelete(1)
	mustText(t, d, "ade")
	if got := len(d.Order()); got != 5 {
		t.Fatalf("len(Order()) = %d, want 5 (tombstones included)", got)
	}
}

func TestClockTicksForInsertAndDelete(t *testing.T) {
	d := NewDoc(3)
	a, _ := d.LocalInsert(0, 'a')
	b, _ := d.LocalInsert(1, 'b')
	del, _ := d.LocalDelete(0)
	c, _ := d.LocalInsert(1, 'c')
	got := []uint64{a.ID.Clock, b.ID.Clock, del.ID.Clock, c.ID.Clock}
	want := []uint64{0, 1, 2, 3}
	if !slices.Equal(got, want) {
		t.Fatalf("clocks = %v, want %v", got, want)
	}
	if del.ID.Client != 3 {
		t.Fatalf("delete op must carry the doc's client ID")
	}
}

func TestOutOfRange(t *testing.T) {
	d := NewDoc(1)
	typeText(t, d, "abc")

	if _, err := d.LocalInsert(-1, 'x'); err == nil {
		t.Error("LocalInsert(-1) should fail")
	}
	if _, err := d.LocalInsert(4, 'x'); err == nil {
		t.Error("LocalInsert(len+1) should fail")
	}
	if _, err := d.LocalDelete(-1); err == nil {
		t.Error("LocalDelete(-1) should fail")
	}
	if _, err := d.LocalDelete(3); err == nil {
		t.Error("LocalDelete(len) should fail")
	}

	checkInvariants(t, d)
	mustText(t, d, "abc")
	next, err := d.LocalInsert(3, 'd')
	if err != nil {
		t.Fatal(err)
	}
	if next.ID.Clock != 3 {
		t.Fatalf("next clock = %d, want 3 (errors must not tick the clock)", next.ID.Clock)
	}
}

func TestOutOfRangeOnEmptyDoc(t *testing.T) {
	d := NewDoc(1)
	if _, err := d.LocalDelete(0); err == nil {
		t.Error("LocalDelete(0) on an empty doc should fail")
	}
	if _, err := d.LocalInsert(1, 'x'); err == nil {
		t.Error("LocalInsert(1) on an empty doc should fail")
	}
	if _, err := d.LocalInsert(0, 'x'); err != nil {
		t.Errorf("LocalInsert(0) on an empty doc must work: %v", err)
	}
}

func TestVisibleAt(t *testing.T) {
	d := NewDoc(1)
	typeText(t, d, "abcd")
	d.LocalDelete(1)
	want := []rune{'a', 'c', 'd'}
	for k, r := range want {
		it := d.visibleAt(k)
		if it == nil || it.Content != r {
			t.Fatalf("visibleAt(%d) wrong, want %q", k, r)
		}
	}
	if d.visibleAt(3) != nil {
		t.Fatal("visibleAt(len) must be nil")
	}
	if d.visibleAt(-1) != nil {
		t.Fatal("visibleAt(-1) must be nil")
	}
	if it := d.visibleAt(0); it == d.start {
		t.Fatal("START must never count as a visible character")
	}
}

func TestUnicode(t *testing.T) {
	d := NewDoc(1)
	typeText(t, d, "aš")
	if _, err := d.LocalInsert(2, '😀'); err != nil {
		t.Fatal(err)
	}
	if _, err := d.LocalInsert(1, 'b'); err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, d)
	mustText(t, d, "abš😀")
	if _, err := d.LocalDelete(2); err != nil {
		t.Fatal(err)
	}
	mustText(t, d, "ab😀")
}

func TestRandomAgainstSliceModel(t *testing.T) {
	for seed := int64(0); seed < 100; seed++ {
		rng := rand.New(rand.NewSource(seed))
		d := NewDoc(1)
		var model []rune

		for step := 0; step < 300; step++ {
			if len(model) == 0 || rng.Intn(4) != 0 {
				pos := rng.Intn(len(model) + 1)
				r := rune('a' + rng.Intn(26))
				if _, err := d.LocalInsert(pos, r); err != nil {
					t.Fatalf("seed %d step %d: insert(%d): %v", seed, step, pos, err)
				}
				model = slices.Insert(model, pos, r)
			} else {
				pos := rng.Intn(len(model))
				if _, err := d.LocalDelete(pos); err != nil {
					t.Fatalf("seed %d step %d: delete(%d): %v", seed, step, pos, err)
				}
				model = slices.Delete(model, pos, pos+1)
			}
			if got := d.String(); got != string(model) {
				t.Fatalf("seed %d step %d: doc %q, model %q", seed, step, got, string(model))
			}
			checkInvariants(t, d)
		}
	}
}

func permutations(n int) [][]int {
	var out [][]int
	used := make([]bool, n)
	var rec func(cur []int)
	rec = func(cur []int) {
		if len(cur) == n {
			out = append(out, slices.Clone(cur))
			return
		}
		for i := 0; i < n; i++ {
			if !used[i] {
				used[i] = true
				rec(append(cur, i))
				used[i] = false
			}
		}
	}
	rec(nil)
	return out
}

func runAllOrders(t *testing.T, ops []Op, want string) {
	t.Helper()
	orders := permutations(len(ops))
	if len(orders) != 6 {
		t.Fatalf("expected 6 orders for 3 ops, got %d", len(orders))
	}
	for _, order := range orders {
		d := NewDoc(9)
		for _, i := range order {
			d.Receive(ops[i])
		}
		if got := d.String(); got != want {
			t.Errorf("order %v: got %q, want %q", order, got, want)
		}
		if n := d.PendingLen(); n != 0 {
			t.Errorf("order %v: PendingLen() = %d, want 0", order, n)
		}
	}
}

func TestCounterexampleMLQ(t *testing.T) {
	l := ID{Client: 3, Clock: 0}
	runAllOrders(t, []Op{
		InsertOp{ID: l, Origin: StartID, RightOrigin: EndID, Content: 'l'},
		InsertOp{ID: ID{Client: 3, Clock: 1}, Origin: l, RightOrigin: EndID, Content: 'q'},
		InsertOp{ID: ID{Client: 2, Clock: 0}, Origin: StartID, RightOrigin: EndID, Content: 'm'},
	}, "mlq")
}

func TestCounterexamplePVC(t *testing.T) {
	v := ID{Client: 1, Clock: 0}
	runAllOrders(t, []Op{
		InsertOp{ID: v, Origin: StartID, RightOrigin: EndID, Content: 'v'},
		InsertOp{ID: ID{Client: 3, Clock: 0}, Origin: StartID, RightOrigin: v, Content: 'p'},
		InsertOp{ID: ID{Client: 2, Clock: 0}, Origin: StartID, RightOrigin: EndID, Content: 'c'},
	}, "pvc")
}

func TestDuplicateChangesNothing(t *testing.T) {
	op := InsertOp{ID: ID{Client: 3, Clock: 0}, Origin: StartID, RightOrigin: EndID, Content: 'x'}

	d := NewDoc(9)
	if applied := d.Receive(op); len(applied) != 1 {
		t.Fatalf("first Receive applied %d ops, want 1", len(applied))
	}
	for i := 0; i < 3; i++ {
		if applied := d.Receive(op); len(applied) != 0 {
			t.Fatalf("duplicate %d applied %d ops, want 0", i, len(applied))
		}
	}
	if got := d.String(); got != "x" {
		t.Fatalf("String() = %q, want %q", got, "x")
	}
	if n := len(d.Order()); n != 1 {
		t.Fatalf("len(Order()) = %d, want 1", n)
	}
	if n := d.PendingLen(); n != 0 {
		t.Fatalf("PendingLen() = %d, want 0", n)
	}
}

func TestPendingWaitsForDependency(t *testing.T) {
	a := ID{Client: 3, Clock: 0}
	opA := InsertOp{ID: a, Origin: StartID, RightOrigin: EndID, Content: 'a'}
	opB := InsertOp{ID: ID{Client: 3, Clock: 1}, Origin: a, RightOrigin: EndID, Content: 'b'}

	d := NewDoc(9)

	if applied := d.Receive(opB); len(applied) != 0 {
		t.Fatalf("Receive(b) applied %d ops, want 0", len(applied))
	}
	if n := d.PendingLen(); n != 1 {
		t.Fatalf("PendingLen() = %d, want 1", n)
	}
	if got := d.String(); got != "" {
		t.Fatalf("String() = %q, want empty", got)
	}

	if applied := d.Receive(opA); len(applied) != 2 {
		t.Fatalf("Receive(a) applied %d ops, want 2", len(applied))
	}
	if got := d.String(); got != "ab" {
		t.Fatalf("String() = %q, want %q", got, "ab")
	}
	if n := d.PendingLen(); n != 0 {
		t.Fatalf("PendingLen() = %d, want 0", n)
	}
}

func TestPendingDeleteWaitsForTarget(t *testing.T) {
	a := ID{Client: 3, Clock: 0}
	opA := InsertOp{ID: a, Origin: StartID, RightOrigin: EndID, Content: 'a'}
	del := DeleteOp{ID: ID{Client: 4, Clock: 0}, Target: a}

	d := NewDoc(9)
	d.Receive(del)
	if n := d.PendingLen(); n != 1 {
		t.Fatalf("PendingLen() = %d, want 1", n)
	}
	d.Receive(opA)
	if got := d.String(); got != "" {
		t.Fatalf("String() = %q, want empty (the insert should arrive already deleted)", got)
	}
	if n := len(d.Order()); n != 1 {
		t.Fatalf("len(Order()) = %d, want 1 (the tombstone)", n)
	}
	if n := d.PendingLen(); n != 0 {
		t.Fatalf("PendingLen() = %d, want 0", n)
	}
}

func TestStateVectorIsACopy(t *testing.T) {
	d := NewDoc(9)
	d.LocalInsert(0, 'a')

	sv := d.StateVector()
	if sv[9] != 1 {
		t.Fatalf("sv[9] = %d, want 1", sv[9])
	}
	sv[9] = 99
	sv[1234] = 7

	if again := d.StateVector(); again[9] != 1 || again[1234] != 0 {
		t.Fatalf("StateVector() returned a live reference, not a copy: %v", again)
	}
}

func TestDiff(t *testing.T) {
	d := NewDoc(9)
	d.LocalInsert(0, 'a')
	d.LocalInsert(1, 'b')
	d.Receive(InsertOp{ID: ID{Client: 5, Clock: 0}, Origin: StartID, RightOrigin: EndID, Content: 'z'})

	if got := len(d.Diff(nil)); got != 3 {
		t.Fatalf("Diff(nil) returned %d ops, want 3", got)
	}

	partial := d.Diff(map[ClientID]uint64{9: 2})
	if len(partial) != 1 {
		t.Fatalf("Diff returned %d ops, want 1", len(partial))
	}
	if id := partial[0].OpID(); id.Client != 5 {
		t.Fatalf("Diff returned client %d, want the third party 5", id.Client)
	}

	if got := len(d.Diff(d.StateVector())); got != 0 {
		t.Fatalf("Diff of our own state vector returned %d ops, want 0", got)
	}

	if got := len(d.Diff(map[ClientID]uint64{9: 1 << 40, 5: 99, 777: 3})); got != 0 {
		t.Fatalf("Diff with an out-of-range state vector returned %d ops, want 0", got)
	}
}

func TestTwoReplicasConverge(t *testing.T) {
	a := NewDoc(1)
	b := NewDoc(2)

	a.LocalInsert(0, 'h')
	a.LocalInsert(1, 'i')
	b.LocalInsert(0, 'y')
	b.LocalInsert(1, 'o')

	b.Receive(a.Diff(b.StateVector())...)
	a.Receive(b.Diff(a.StateVector())...)

	if a.String() != b.String() {
		t.Fatalf("replicas diverged: a = %q, b = %q", a.String(), b.String())
	}
	if !slices.Equal(a.Order(), b.Order()) {
		t.Fatalf("replicas have different item order")
	}
	if a.PendingLen() != 0 || b.PendingLen() != 0 {
		t.Fatalf("pending not empty: a = %d, b = %d", a.PendingLen(), b.PendingLen())
	}
}

func TestReceiveGarbage(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	randID := func() ID {
		return ID{Client: ClientID(rng.Intn(4)), Clock: uint64(rng.Intn(6))}
	}
	for i := 0; i < 3000; i++ {
		d := NewDoc(1)
		typeText(t, d, "ab")
		ops := make([]Op, 0, 20)
		for j := 0; j < 20; j++ {
			if rng.Intn(2) == 0 {
				ops = append(ops, InsertOp{ID: randID(), Origin: randID(), RightOrigin: randID(), Content: 'x'})
			} else {
				ops = append(ops, DeleteOp{ID: randID(), Target: randID()})
			}
		}
		d.Receive(ops...)
		_ = d.String()
	}
}
