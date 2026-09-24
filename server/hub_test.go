package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/veliborsimonovic/collab/crdt"
	"github.com/veliborsimonovic/collab/store"
)

func TestHubEvictsIdleRoomAndReloads(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "hub.db")
	st, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer st.Close()

	hub := NewHub(st, Limits{MaxClients: 100, MaxItems: 1_000_000}, 30*time.Millisecond)

	room, release, err := hub.Acquire("doc1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	op, err := crdt.NewDoc(2).LocalInsert(0, 'x')
	if err != nil {
		t.Fatalf("LocalInsert: %v", err)
	}
	if err := room.commit([]crdt.Op{op}, nil); err != nil {
		t.Fatalf("commit: %v", err)
	}

	release()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if rooms, _ := hub.Stats(); rooms == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("room was never evicted")
		}
		time.Sleep(10 * time.Millisecond)
	}

	room2, release2, err := hub.Acquire("doc1")
	if err != nil {
		t.Fatalf("Acquire (reload): %v", err)
	}
	defer release2()

	if room2.doc.String() != "x" {
		t.Fatalf("reloaded room text = %q, want %q", room2.doc.String(), "x")
	}
}

func TestLargestRoom(t *testing.T) {
	hub := NewHub(store.NewMemory(), Limits{MaxClients: 100, MaxItems: 1_000_000}, time.Minute)

	a, releaseA, err := hub.Acquire("a")
	if err != nil {
		t.Fatalf("Acquire a: %v", err)
	}
	defer releaseA()
	if err := a.Edit(0, 0, "hello"); err != nil {
		t.Fatalf("Edit a: %v", err)
	}

	b, releaseB, err := hub.Acquire("b")
	if err != nil {
		t.Fatalf("Acquire b: %v", err)
	}
	defer releaseB()
	if err := b.Edit(0, 0, "hi"); err != nil {
		t.Fatalf("Edit b: %v", err)
	}

	items, load := hub.Largest()
	if items != 5 {
		t.Fatalf("Largest items = %d, want 5", items)
	}
	if load < 0 {
		t.Fatalf("Largest load = %v, want >= 0", load)
	}
}
