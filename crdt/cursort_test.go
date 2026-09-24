package crdt

import "testing"

func TestCursorFollowsInsert(t *testing.T) {
	a := NewDoc(1<<20 + 1)
	b := NewDoc(1<<20 + 2)

	for i, r := range []rune("hello") {
		if _, err := a.LocalInsert(i, r); err != nil {
			t.Fatal(err)
		}
	}
	b.Receive(a.Diff(b.StateVector())...)

	id, err := a.CursorID(5)
	if err != nil {
		t.Fatal(err)
	}

	op, err := b.LocalInsert(0, '>')
	if err != nil {
		t.Fatal(err)
	}
	a.Receive(op)
	if got := a.String(); got != ">hello" {
		t.Fatalf("text = %q, want %q", got, ">hello")
	}

	if pos, ok := a.CursorPos(id); !ok || pos != 6 {
		t.Fatalf("after insert: got %d %v, want 6 true", pos, ok)
	}

	if _, err := a.LocalDelete(5); err != nil {
		t.Fatal(err)
	}

	if pos, ok := a.CursorPos(id); !ok || pos != 5 {
		t.Fatalf("after delete: got %d %v, want 5 true", pos, ok)
	}
}
