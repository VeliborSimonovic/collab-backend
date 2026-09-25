package store

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/veliborsimonovic/collab/crdt"
)

var errBatchedClosed = errors.New("store: batched store is closed")

type Batched struct {
	db         *SQLite
	mu         sync.Mutex
	pending    map[string][]crdt.Op
	pendingOps int
	flushNow   chan chan error
	stop       chan struct{}
	done       chan struct{}
	lastErr    error
}

func NewBatched(db *SQLite, every time.Duration) *Batched {
	batch := &Batched{
		db:       db,
		pending:  make(map[string][]crdt.Op),
		flushNow: make(chan chan error),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}

	go batch.writer(every)

	return batch
}

func (b *Batched) writer(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			b.flush()
		case reply := <-b.flushNow:
			reply <- b.flush()
		case <-b.stop:
			b.flush()
			close(b.done)
			return
		}
	}
}

func (b *Batched) flush() error {
	b.mu.Lock()
	batch := b.pending
	b.pending = make(map[string][]crdt.Op)
	b.pendingOps = 0
	b.mu.Unlock()

	if len(batch) == 0 {
		return nil
	}

	err := b.db.AppendBatch(batch)

	b.mu.Lock()
	defer b.mu.Unlock()

	if err != nil {
		// Put the failed ops back in front of anything newer; the next tick retries.
		for doc, ops := range batch {
			b.pending[doc] = append(ops[:len(ops):len(ops)], b.pending[doc]...)
			b.pendingOps += len(ops)
		}
		b.lastErr = err
		log.Printf("store: flush failed, will retry: %v", err)
		return err
	}

	b.lastErr = nil
	return nil
}

func (b *Batched) Append(doc string, ops []crdt.Op) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.pending[doc] = append(b.pending[doc], ops...)
	b.pendingOps += len(ops)

	return b.lastErr
}

func (b *Batched) Load(doc string) ([]crdt.Op, error) {
	reply := make(chan error, 1)

	select {
	case b.flushNow <- reply:
	case <-b.done:
		return nil, errBatchedClosed
	}

	if err := <-reply; err != nil {
		return nil, err
	}

	return b.db.Load(doc)
}

func (b *Batched) Close() error {
	close(b.stop)
	<-b.done

	return b.db.Close()
}

func (b *Batched) Pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.pendingOps
}

func (b *Batched) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.lastErr
}
