package store

import (
	"math/rand"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/veliborsimonovic/collab/crdt"
)

func randomOps(t *testing.T, doc *crdt.Doc, n int) []crdt.Op {
	t.Helper()
	rng := rand.New(rand.NewSource(1))
	letters := []rune("abcdefghijklmnopqrstuvwxyz")

	var ops []crdt.Op
	for i := 0; i < n; i++ {
		length := len([]rune(doc.String()))
		if length == 0 || rng.Float64() < 0.75 {
			pos := 0
			if length > 0 {
				pos = rng.Intn(length + 1)
			}
			op, err := doc.LocalInsert(pos, letters[rng.Intn(len(letters))])
			if err != nil {
				t.Fatalf("LocalInsert: %v", err)
			}
			ops = append(ops, op)
		} else {
			pos := rng.Intn(length)
			op, err := doc.LocalDelete(pos)
			if err != nil {
				t.Fatalf("LocalDelete: %v", err)
			}
			ops = append(ops, op)
		}
	}
	return ops
}

func testRoundTrip(t *testing.T, s Store) {
	t.Helper()

	const docName = "roundtrip-doc"
	original := crdt.NewDoc(1)
	applied := randomOps(t, original, 200)

	if err := s.Append(docName, applied); err != nil {
		t.Fatalf("Append: %v", err)
	}

	loaded, err := s.Load(docName)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	replay := crdt.NewDoc(2)
	replay.Receive(loaded...)

	if replay.PendingLen() != 0 {
		t.Fatalf("replay has %d ops still pending after Receive", replay.PendingLen())
	}
	if replay.String() != original.String() {
		t.Fatalf("String mismatch:\n original: %q\n replay:   %q", original.String(), replay.String())
	}
	if !reflect.DeepEqual(replay.Order(), original.Order()) {
		t.Fatalf("Order mismatch:\n original: %v\n replay:   %v", original.Order(), replay.Order())
	}
}

func TestStoreRoundTrip(t *testing.T) {
	t.Run("Memory", func(t *testing.T) {
		testRoundTrip(t, NewMemory())
	})

	t.Run("SQLite", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "roundtrip.db")
		sq, err := OpenSQLite(dbPath)
		if err != nil {
			t.Fatalf("OpenSQLite: %v", err)
		}
		defer sq.Close()

		testRoundTrip(t, sq)
	})
}

func TestSQLiteReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "reopen.db")

	sq1, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite (first open): %v", err)
	}

	const docName = "reopen-doc"
	doc := crdt.NewDoc(1)
	ops := randomOps(t, doc, 50)

	if err := sq1.Append(docName, ops); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := sq1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	sq2, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite (reopen): %v", err)
	}
	defer sq2.Close()

	loaded, err := sq2.Load(docName)
	if err != nil {
		t.Fatalf("Load after reopen: %v", err)
	}

	replay := crdt.NewDoc(2)
	replay.Receive(loaded...)

	if replay.String() != doc.String() {
		t.Fatalf("String mismatch after reopen:\n original: %q\n reopened: %q", doc.String(), replay.String())
	}
	if !reflect.DeepEqual(replay.Order(), doc.Order()) {
		t.Fatalf("Order mismatch after reopen:\n original: %v\n reopened: %v", doc.Order(), replay.Order())
	}
}

func testDuplicateAppend(t *testing.T, s Store) {
	t.Helper()

	doc := crdt.NewDoc(1)
	op, err := doc.LocalInsert(0, 'x')
	if err != nil {
		t.Fatalf("LocalInsert: %v", err)
	}

	if err := s.Append("dup-doc", []crdt.Op{op}); err != nil {
		t.Fatalf("first Append: %v", err)
	}
	if err := s.Append("dup-doc", []crdt.Op{op}); err != nil {
		t.Fatalf("second Append: %v", err)
	}

	loaded, err := s.Load("dup-doc")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("want 1 stored op after appending the same op twice, got %d", len(loaded))
	}
}

func TestAppendDuplicateIsHarmless(t *testing.T) {
	t.Run("Memory", func(t *testing.T) {
		testDuplicateAppend(t, NewMemory())
	})

	t.Run("SQLite", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "dup.db")
		sq, err := OpenSQLite(dbPath)
		if err != nil {
			t.Fatalf("OpenSQLite: %v", err)
		}
		defer sq.Close()

		testDuplicateAppend(t, sq)

		var count int
		if err := sq.db.QueryRow(`SELECT COUNT(*) FROM ops`).Scan(&count); err != nil {
			t.Fatalf("count rows: %v", err)
		}
		if count != 1 {
			t.Fatalf("want exactly 1 row in the ops table after duplicate appends, got %d", count)
		}
	})
}
