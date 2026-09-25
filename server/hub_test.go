package server

import (
	"errors"
	"path/filepath"
	"sync"
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

// slowStore wraps Memory: Load sleeps for the "slow" doc, sleeps briefly and
// then fails for the "bad" doc, and every call is counted.
type slowStore struct {
	*store.Memory
	mu    sync.Mutex
	loads map[string]int
}

func newSlowStore() *slowStore {
	return &slowStore{Memory: store.NewMemory(), loads: make(map[string]int)}
}

func (s *slowStore) Load(doc string) ([]crdt.Op, error) {
	s.mu.Lock()
	s.loads[doc]++
	s.mu.Unlock()

	switch doc {
	case "slow":
		time.Sleep(500 * time.Millisecond)
	case "bad":
		time.Sleep(100 * time.Millisecond)
		return nil, errors.New("load failed")
	}
	return s.Memory.Load(doc)
}

func (s *slowStore) loadCount(doc string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loads[doc]
}

func newTestHub(st store.Store) *Hub {
	return NewHub(st, Limits{MaxClients: 100, MaxItems: 1_000_000}, time.Minute)
}

func TestLoadDoesNotBlockOtherDocs(t *testing.T) {
	hub := newTestHub(newSlowStore())

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, release, err := hub.Acquire("slow"); err == nil {
			release()
		}
	}()

	time.Sleep(20 * time.Millisecond)

	start := time.Now()
	_, release, err := hub.Acquire("fast")
	if err != nil {
		t.Fatalf("Acquire fast: %v", err)
	}
	release()
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("Acquire(fast) took %v while slow was loading, want < 100ms", d)
	}

	<-done
}

func TestConcurrentAcquireLoadsOnce(t *testing.T) {
	st := newSlowStore()
	hub := newTestHub(st)

	const n = 10
	rooms := make([]*Room, n)
	errs := make([]error, n)
	start := make(chan struct{})

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			room, release, err := hub.Acquire("slow")
			if err == nil {
				defer release()
			}
			rooms[i], errs[i] = room, err
		}()
	}
	close(start)
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("Acquire %d: %v", i, errs[i])
		}
		if rooms[i] != rooms[0] {
			t.Fatalf("Acquire %d returned a different *Room", i)
		}
	}
	if got := st.loadCount("slow"); got != 1 {
		t.Fatalf("Load called %d times, want 1", got)
	}
}

func TestLoadErrorReachesWaiters(t *testing.T) {
	st := newSlowStore()
	hub := newTestHub(st)

	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, errs[i] = hub.Acquire("bad")
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err == nil {
			t.Fatalf("Acquire %d: want error, got nil", i)
		}
	}
	if got := st.loadCount("bad"); got != 1 {
		t.Fatalf("Load called %d times for two concurrent Acquires, want 1", got)
	}

	hub.mu.Lock()
	_, left := hub.rooms["bad"]
	hub.mu.Unlock()
	if left {
		t.Fatal("failed doc was left in the hub map")
	}

	if _, _, err := hub.Acquire("bad"); err == nil {
		t.Fatal("later Acquire: want error, got nil")
	}
	if got := st.loadCount("bad"); got != 2 {
		t.Fatalf("later Acquire should retry Load: want 2 calls, got %d", got)
	}
}
