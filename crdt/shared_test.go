package crdt

import (
	"fmt"
	"maps"
	"reflect"
	"strings"
	"testing"
)

func syncAll(docs ...*Doc) {
	for pass := 0; pass < 2; pass++ {
		for _, a := range docs {
			for _, b := range docs {
				if a != b {
					b.Receive(a.Diff(b.StateVector())...)
				}
			}
		}
	}
}

func js(s string) []byte { return []byte(`"` + s + `"`) }

var mRoot = RootParent("m", KindMap)

func wantJSON(t *testing.T, d *Doc, p Parent, want any) {
	t.Helper()
	got := d.ToJSON(p)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("client %d: ToJSON = %v, want %v", d.Client, got, want)
	}
}

func TestDefaultTextIsZeroParent(t *testing.T) {
	if DefaultText() != (Parent{}) {
		t.Fatal("DefaultText() != Parent{}")
	}
}

func TestMapConcurrentSetHigherClientWins(t *testing.T) {
	a, b := NewDoc(2), NewDoc(5)
	a.MapSetJSON(mRoot, "color", js("red"))
	b.MapSetJSON(mRoot, "color", js("blue"))
	syncAll(a, b)
	wantJSON(t, a, mRoot, "map[color:blue]")
	wantJSON(t, b, mRoot, "map[color:blue]")
}

func TestMapSetBeatsConcurrentDelete(t *testing.T) {
	a, b, c := NewDoc(2), NewDoc(5), NewDoc(7)
	a.MapSetJSON(mRoot, "k", js("x"))
	syncAll(a, b, c)
	if _, err := b.MapDelete(mRoot, "k"); err != nil {
		t.Fatal(err)
	}
	c.MapSetJSON(mRoot, "k", js("y"))
	syncAll(a, b, c)
	for _, d := range []*Doc{a, b, c} {
		wantJSON(t, d, mRoot, "map[k:y]")
	}
}

func TestMapDeleteOnlyDeletesSeenValue(t *testing.T) {
	a, b := NewDoc(2), NewDoc(5)
	a.MapSetJSON(mRoot, "k", []byte("1"))
	syncAll(a, b)
	a.MapSetJSON(mRoot, "k", []byte("2"))
	if _, err := b.MapDelete(mRoot, "k"); err != nil {
		t.Fatal(err)
	}
	syncAll(a, b)
	wantJSON(t, a, mRoot, "map[k:2]")
	wantJSON(t, b, mRoot, "map[k:2]")
}

func TestMapDeleteMissingKey(t *testing.T) {
	d := NewDoc(2)
	before := maps.Clone(d.StateVector())
	if _, err := d.MapDelete(mRoot, "nope"); err != ErrNoKey {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(before, d.StateVector()) {
		t.Fatal("state vector changed")
	}
}

func TestNestedPendingUntilParent(t *testing.T) {
	a, b := NewDoc(2), NewDoc(5)
	slides := RootParent("slides", KindArray)
	op1, err := a.ArrayInsertType(slides, 0, KindMap)
	if err != nil {
		t.Fatal(err)
	}
	op2, err := a.MapSetJSON(ItemParent(op1.ID), "title", js("Intro"))
	if err != nil {
		t.Fatal(err)
	}
	b.Receive(op2)
	if b.PendingLen() != 1 {
		t.Fatalf("PendingLen = %d", b.PendingLen())
	}
	b.Receive(op1)
	wantJSON(t, b, slides, "[map[title:Intro]]")
	if b.PendingLen() != 0 {
		t.Fatalf("PendingLen = %d", b.PendingLen())
	}

	// test 8: delete the container while the other side edits inside it
	nested := ItemParent(op1.ID)
	if _, err := a.ArrayDelete(slides, 0, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := b.MapSetJSON(nested, "title", js("Hi")); err != nil {
		t.Fatal(err)
	}
	syncAll(a, b)
	wantJSON(t, a, slides, "[]")
	wantJSON(t, b, slides, "[]")
	if a.PendingLen() != 0 || b.PendingLen() != 0 {
		t.Fatal("pending ops left")
	}
}

func TestSameNameDifferentKinds(t *testing.T) {
	d := NewDoc(2)
	d.TextInsert(RootParent("x", KindText), 0, "hi")
	d.MapSetJSON(RootParent("x", KindMap), "a", []byte("1"))
	got, err := d.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"x:map":{"a":1},"x:text":"hi"}`; string(got) != want {
		t.Fatalf("JSON = %s, want %s", got, want)
	}
}

func TestReceiveRejectsWrongShapes(t *testing.T) {
	d := NewDoc(2)
	d.TextInsert(DefaultText(), 0, "ok")
	ops := []Op{
		InsertOp{ID: ID{9, 0}, Origin: StartID, RightOrigin: EndID, Parent: DefaultText(), Key: "k", Content: 'x'},
		InsertOp{ID: ID{9, 0}, Origin: StartID, RightOrigin: EndID, Parent: RootParent("arr", KindArray), CKind: ContentRune, Content: 'x'},
		InsertOp{ID: ID{9, 0}, Origin: StartID, RightOrigin: EndID, Parent: mRoot, Key: "k", CKind: ContentJSON, JSON: []byte("{bad")},
	}
	for _, op := range ops {
		d.Receive(op)
	}
	if d.String() != "ok" || d.PendingLen() != 0 || d.StateVector()[9] != 0 {
		t.Fatal("bad op was not dropped")
	}
}

func TestReceiveRejectsCrossContainerOrigin(t *testing.T) {
	d := NewDoc(2)
	ra, rb := RootParent("a", KindArray), RootParent("b", KindArray)
	op, _ := d.ArrayInsertJSON(rb, 0, []byte("1"))
	bad := InsertOp{ID: ID{9, 0}, Origin: op.ID, RightOrigin: EndID, Parent: ra, CKind: ContentJSON, JSON: []byte("2")}
	d.Receive(bad)
	if d.PendingLen() != 0 || d.StateVector()[9] != 0 {
		t.Fatal("cross-container op accepted")
	}
	if d.lookup(ra) != nil {
		t.Fatal("container created by dropped op")
	}
}

func TestLocalValidation(t *testing.T) {
	d := NewDoc(2)
	before := maps.Clone(d.StateVector())
	if _, err := d.MapSetJSON(mRoot, "", []byte("1")); err != ErrBadKey {
		t.Fatalf("err = %v", err)
	}
	if _, err := d.MapSetJSON(mRoot, strings.Repeat("a", 256), []byte("1")); err != ErrBadKey {
		t.Fatalf("err = %v", err)
	}
	if _, err := d.MapSetJSON(mRoot, "k", []byte("{bad")); err != ErrBadValue {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(before, d.StateVector()) {
		t.Fatal("state vector changed")
	}
}

func TestTextInsertIntoMapWrongKind(t *testing.T) {
	d := NewDoc(2)
	if _, err := d.TextInsert(mRoot, 0, "x"); err != ErrWrongKind {
		t.Fatalf("err = %v", err)
	}
}

func TestMapSetAfterDeletedItem(t *testing.T) {
	a, b := NewDoc(2), NewDoc(5)
	b.MapSetJSON(mRoot, "k", js("a"))
	b.MapDelete(mRoot, "k")
	syncAll(a, b)
	a.MapSetJSON(mRoot, "k", js("b"))
	syncAll(a, b)
	wantJSON(t, a, mRoot, "map[k:b]")
	wantJSON(t, b, mRoot, "map[k:b]")
}

func TestCursorInArray(t *testing.T) {
	d := NewDoc(2)
	arr := RootParent("a", KindArray)
	op, _ := d.ArrayInsertJSON(arr, 0, []byte("1"))
	id, err := d.CursorIDIn(arr, 1)
	if err != nil || id != op.ID {
		t.Fatalf("id=%v err=%v", id, err)
	}
	if p, ok := d.CursorPosIn(arr, id); !ok || p != 1 {
		t.Fatal("pos")
	}
	if _, ok := d.CursorPosIn(DefaultText(), id); ok {
		t.Fatal("caret from another container accepted")
	}
	if _, err := d.CursorIDIn(mRoot, 0); err != ErrWrongKind {
		t.Fatalf("err = %v", err)
	}
}
