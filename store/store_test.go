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

func TestAppendBatch(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "batch.db")
	sq, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer sq.Close()

	docA := crdt.NewDoc(1)
	var opsA []crdt.Op
	for i, r := range "hello" {
		op, err := docA.LocalInsert(i, r)
		if err != nil {
			t.Fatalf("LocalInsert: %v", err)
		}
		opsA = append(opsA, op)
	}
	docB := crdt.NewDoc(2)
	var opsB []crdt.Op
	for i, r := range "world" {
		op, err := docB.LocalInsert(i, r)
		if err != nil {
			t.Fatalf("LocalInsert: %v", err)
		}
		opsB = append(opsB, op)
	}

	// opsA[0] appears twice.
	batch := map[string][]crdt.Op{
		"doc-a": append(append([]crdt.Op{}, opsA...), opsA[0]),
		"doc-b": opsB,
	}
	if err := sq.AppendBatch(batch); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}

	for name, want := range map[string]string{"doc-a": "hello", "doc-b": "world"} {
		loaded, err := sq.Load(name)
		if err != nil {
			t.Fatalf("Load %s: %v", name, err)
		}
		fresh := crdt.NewDoc(99)
		fresh.Receive(loaded...)
		if fresh.String() != want {
			t.Fatalf("%s: want %q, got %q", name, want, fresh.String())
		}
	}

	var count int
	if err := sq.db.QueryRow(`SELECT COUNT(*) FROM ops WHERE doc = 'doc-a'`).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != len(opsA) {
		t.Fatalf("want %d rows for doc-a (duplicate stored once), got %d", len(opsA), count)
	}
}

// replayOps is the same op log as in crdt.BenchmarkReplay30k: five docs type at
// random positions and sync every syncEvery inserts; the log is in the order
// docs[0] applied the ops.
func replayOps(tb testing.TB, docsN, insertsEach, syncEvery int) []crdt.Op {
	tb.Helper()

	rng := rand.New(rand.NewSource(1))
	docs := make([]*crdt.Doc, docsN)
	for i := range docs {
		docs[i] = crdt.NewDoc(crdt.ClientID(i + 1))
	}

	var log []crdt.Op
	for done := 0; done < insertsEach; done += syncEvery {
		batches := make([][]crdt.Op, docsN)
		for i, d := range docs {
			for k := 0; k < syncEvery; k++ {
				op, err := d.LocalInsert(rng.Intn(d.Len()+1), rune('a'+rng.Intn(26)))
				if err != nil {
					tb.Fatalf("LocalInsert: %v", err)
				}
				batches[i] = append(batches[i], op)
			}
		}
		log = append(log, batches[0]...)

		for i, batch := range batches {
			for j, d := range docs {
				if i == j {
					continue
				}
				applied := d.Receive(batch...)
				if j == 0 {
					log = append(log, applied...)
				}
			}
		}
	}
	return log
}

func BenchmarkSQLiteLoad30k(b *testing.B) {
	ops := replayOps(b, 5, 6000, 50)

	sq, err := OpenSQLite(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatalf("OpenSQLite: %v", err)
	}
	defer sq.Close()

	if err := sq.Append("bench", ops); err != nil {
		b.Fatalf("Append: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		loaded, err := sq.Load("bench")
		if err != nil {
			b.Fatalf("Load: %v", err)
		}
		if len(loaded) != len(ops) {
			b.Fatalf("want %d ops, got %d", len(ops), len(loaded))
		}
	}
}
