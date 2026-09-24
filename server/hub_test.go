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
