package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/veliborsimonovic/collab/crdt"
)

// appendDocs appends n random ops to each named doc and returns the source docs.
func appendDocs(t *testing.T, b *Batched, names []string, n int) map[string]*crdt.Doc {
	t.Helper()

	docs := make(map[string]*crdt.Doc)
	for i, name := range names {
		doc := crdt.NewDoc(crdt.ClientID(i + 1))
		docs[name] = doc
		if err := b.Append(name, randomOps(t, doc, n)); err != nil {
			t.Fatalf("Append %s: %v", name, err)
		}
	}
	return docs
}

func checkLoaded(t *testing.T, load func(string) ([]crdt.Op, error), docs map[string]*crdt.Doc, n int) {
	t.Helper()

	for name, original := range docs {
		ops, err := load(name)
		if err != nil {
			t.Fatalf("Load %s: %v", name, err)
		}
		if len(ops) != n {
			t.Fatalf("%s: want %d ops, got %d", name, n, len(ops))
		}
		replay := crdt.NewDoc(99)
		replay.Receive(ops...)
		if replay.String() != original.String() {
			t.Fatalf("%s: want %q, got %q", name, original.String(), replay.String())
		}
	}
}

func TestBatchedFlushes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "flush.db")
	sq, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	b := NewBatched(sq, 10*time.Millisecond)
	defer b.Close()

	docs := appendDocs(t, b, []string{"a", "b", "c"}, 100)

	time.Sleep(50 * time.Millisecond)

	fresh, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite (fresh): %v", err)
	}
	defer fresh.Close()

	checkLoaded(t, fresh.Load, docs, 100)
}

func TestBatchedLoadSeesPending(t *testing.T) {
	sq, err := OpenSQLite(filepath.Join(t.TempDir(), "load.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	b := NewBatched(sq, time.Hour)
	defer b.Close()

	docs := appendDocs(t, b, []string{"a", "b", "c"}, 100)
	if got := b.Pending(); got != 300 {
		t.Fatalf("Pending: want 300, got %d", got)
	}

	checkLoaded(t, b.Load, docs, 100)
}

func TestBatchedCloseFlushes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "close.db")
	sq, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	b := NewBatched(sq, time.Hour)

	docs := appendDocs(t, b, []string{"a", "b", "c"}, 100)

	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite (reopen): %v", err)
	}
	defer reopened.Close()

	checkLoaded(t, reopened.Load, docs, 100)
}

func TestBatchedFlushRetriesAfterError(t *testing.T) {
	sq, err := OpenSQLite(filepath.Join(t.TempDir(), "retry.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	b := NewBatched(sq, time.Hour) // flushes only when the test calls flush
	defer b.Close()

	// Break the disk: writes fail while the table has another name.
	if _, err := sq.db.Exec(`ALTER TABLE ops RENAME TO ops_broken`); err != nil {
		t.Fatalf("break table: %v", err)
	}

	src := crdt.NewDoc(1)
	first := randomOps(t, src, 20)
	if err := b.Append("doc", first); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if err := b.flush(); err == nil {
		t.Fatal("flush on a broken disk: want error, got nil")
	}
	if got := b.Pending(); got != len(first) {
		t.Fatalf("after a failed flush Pending = %d, want %d (ops put back)", got, len(first))
	}

	// Newer ops arrive while the disk is still broken; Append reports the error.
	second := randomOps(t, src, 20)
	if err := b.Append("doc", second); err == nil {
		t.Fatal("Append with a failed flush: want lastErr, got nil")
	}
	if err := b.flush(); err == nil {
		t.Fatal("second flush on a broken disk: want error, got nil")
	}
	if got := b.Pending(); got != len(first)+len(second) {
		t.Fatalf("Pending = %d, want %d", got, len(first)+len(second))
	}

	// Repair the disk: the retry saves everything, in order, and clears the error.
	if _, err := sq.db.Exec(`ALTER TABLE ops_broken RENAME TO ops`); err != nil {
		t.Fatalf("repair table: %v", err)
	}
	if err := b.flush(); err != nil {
		t.Fatalf("flush after repair: %v", err)
	}
	if got := b.Pending(); got != 0 {
		t.Fatalf("Pending after a good flush = %d, want 0", got)
	}
	if err := b.Append("doc", nil); err != nil {
		t.Fatalf("Append after recovery: want nil, got %v", err)
	}

	loaded, err := sq.Load("doc")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := append(append([]crdt.Op{}, first...), second...)
	if len(loaded) != len(want) {
		t.Fatalf("stored %d ops, want %d", len(loaded), len(want))
	}
	for i := range want {
		if loaded[i].OpID() != want[i].OpID() {
			t.Fatalf("op %d: stored %v, want %v (order lost)", i, loaded[i].OpID(), want[i].OpID())
		}
	}
}
