package server

import (
	"log"
	"sync"
	"time"

	"github.com/veliborsimonovic/collab/store"
)

type deleter interface {
	Delete(doc string) error
}

type entry struct {
	room  *Room
	refs  int
	timer *time.Timer
	ready chan struct{}
	err   error
}

type Hub struct {
	mu        sync.Mutex
	rooms     map[string]*entry
	store     store.Store
	limits    Limits
	idle      time.Duration
	ephemeral bool
}

func NewHub(st store.Store, lim Limits, idle time.Duration) *Hub {
	return &Hub{
		rooms:  make(map[string]*entry),
		store:  st,
		limits: lim,
		idle:   idle,
	}
}

func (h *Hub) EnableEphemeral() {
	h.ephemeral = true
}

func (h *Hub) Largest() (items int, load time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, e := range h.rooms {
		if e.room == nil {
			continue
		}
		n, l := e.room.Size()
		if n > items {
			items = n
		}
		if l > load {
			load = l
		}
	}
	return items, load
}

func (h *Hub) Acquire(doc string) (*Room, func(), error) {
	h.mu.Lock()

	e, ok := h.rooms[doc]
	if !ok {
		e = &entry{ready: make(chan struct{}), refs: 1}
		h.rooms[doc] = e
		h.mu.Unlock()

		room, err := OpenRoom(doc, h.store, h.limits)

		h.mu.Lock()
		e.room, e.err = room, err
		if err != nil && h.rooms[doc] == e {
			delete(h.rooms, doc)
		}
		h.mu.Unlock()
		close(e.ready)
	} else {
		e.refs++
		if e.timer != nil {
			e.timer.Stop()
			e.timer = nil
		}
		h.mu.Unlock()

		<-e.ready
	}

	if e.err != nil {
		h.mu.Lock()
		e.refs--
		h.mu.Unlock()
		return nil, nil, e.err
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()

			e.refs--
			if e.refs == 0 {
				e.timer = time.AfterFunc(h.idle, func() {
					h.mu.Lock()
					defer h.mu.Unlock()
					if cur, ok := h.rooms[doc]; ok && cur == e && e.refs == 0 {
						delete(h.rooms, doc)
						if d, ok := h.store.(deleter); ok && h.ephemeral {
							if err := d.Delete(doc); err != nil {
								log.Printf("hub: delete evicted doc %q: %v", doc, err)
							}
						}
					}
				})
			}
		})
	}

	return e.room, release, nil
}

func (h *Hub) WriteQueue() int {
	if b, ok := h.store.(*store.Batched); ok {
		return b.Pending()
	}
	return 0
}

func (h *Hub) Stats() (rooms, conns int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	rooms = len(h.rooms)
	for _, e := range h.rooms {
		if e.room == nil {
			continue
		}
		conns += e.room.Count()
	}
	return rooms, conns
}
