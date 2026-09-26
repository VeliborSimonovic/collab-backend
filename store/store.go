package store

import (
	"sync"

	"github.com/veliborsimonovic/collab/crdt"
)

var _ Store = (*Memory)(nil)

type Store interface {
	Load(doc string) ([]crdt.Op, error)
	Append(doc string, ops []crdt.Op) error
	Close() error
}

type Memory struct {
	mu   sync.Mutex
	ops  map[string][]crdt.Op
	seen map[string]map[crdt.ID]bool
}

func NewMemory() *Memory {
	out := Memory{
		ops:  make(map[string][]crdt.Op),
		seen: map[string]map[crdt.ID]bool{},
	}

	return &out
}

func (m *Memory) Delete(doc string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.ops, doc)
	delete(m.seen, doc)
	return nil
}

func (m *Memory) Load(doc string) ([]crdt.Op, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.ops[doc], nil
}

func (m *Memory) Append(doc string, ops []crdt.Op) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.seen[doc] == nil {
		m.seen[doc] = make(map[crdt.ID]bool)
	}

	for _, op := range ops {
		if m.seen[doc][op.OpID()] {
			continue
		}

		m.seen[doc][op.OpID()] = true
		m.ops[doc] = append(m.ops[doc], op)
	}

	return nil

}

func (m *Memory) Close() error {
	return nil

}
