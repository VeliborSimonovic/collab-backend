package server

import (
	"sync"
	"time"

	"github.com/veliborsimonovic/collab/store"
)

type entry struct {
	room  *Room
	refs  int
	timer *time.Timer
}

type Hub struct {
	mu     sync.Mutex
	rooms  map[string]*entry
	store  store.Store
	limits Limits
	idle   time.Duration
}

func NewHub(st store.Store, lim Limits, idle time.Duration) *Hub {
	return &Hub{
		rooms:  make(map[string]*entry),
		store:  st,
		limits: lim,
		idle:   idle,
	}
}

func (h *Hub) Largest() (items int, load time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, e := range h.rooms {
		n, l := e.room.Size()
		if n > items {
			items, load = n, l
		}
	}
	return items, load
}

func (h *Hub) Acquire(doc string) (*Room, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	e, ok := h.rooms[doc]
	if !ok {
		room, err := OpenRoom(doc, h.store, h.limits)
		if err != nil {
			return nil, nil, err
		}
		e = &entry{room: room}
		h.rooms[doc] = e
	}

	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	e.refs++

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
					}
				})
			}
		})
	}

	return e.room, release, nil
}

func (h *Hub) Stats() (rooms, conns int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	rooms = len(h.rooms)
	for _, e := range h.rooms {
		conns += e.room.Count()
	}
	return rooms, conns
}
