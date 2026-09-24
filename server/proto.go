package server

import (
	"errors"
	"sync"

	"github.com/veliborsimonovic/collab/crdt"
	"github.com/veliborsimonovic/collab/store"
)

const (
	MsgSyncStep1    byte = 1
	MsgUpdate       byte = 2
	MsgPresence     byte = 3
	MsgPresenceGone byte = 4
)

func frame(t byte, payload []byte) []byte {
	out := make([]byte, 1+len(payload))
	out[0] = t
	copy(out[1:], payload)
	return out
}

var (
	ErrReadOnly  = errors.New("server: viewer cannot edit")
	ErrDocTooBig = errors.New("server: document would exceed MaxItems")
	ErrRoomFull  = errors.New("server: room is at MaxClients")
)

type Room struct {
	mu      sync.Mutex
	id      string
	doc     *crdt.Doc
	store   store.Store
	clients map[*Client]bool
	limits  Limits
}

func OpenRoom(id string, st store.Store, lim Limits) (*Room, error) {
	ops, err := st.Load(id)
	if err != nil {
		return nil, err
	}

	doc := crdt.NewDoc(1)
	doc.Receive(ops...)
	doc.DropPending()
	doc.ResumeClock()

	return &Room{
		id:      id,
		doc:     doc,
		store:   st,
		clients: make(map[*Client]bool),
		limits:  lim,
	}, nil
}

func (r *Room) broadcast(f []byte, except *Client) {
	for key := range r.clients {
		if key == except {
			continue
		}

		key.push(f)
	}
}

func (r *Room) commit(ops []crdt.Op, except *Client) error {
	if len(ops) == 0 {
		return nil
	}

	if err := r.store.Append(r.id, ops); err != nil {
		return err
	}
	r.broadcast(frame(MsgUpdate, crdt.EncodeOps(ops)), except)
	return nil

}
func (r *Room) Join(c *Client) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.clients) >= r.limits.MaxClients {
		return ErrRoomFull
	}

	r.clients[c] = true
	c.push(frame(MsgSyncStep1, crdt.EncodeSV(r.doc.StateVector())))

	for other := range r.clients {
		if other != c && other.presence != nil {
			c.push(frame(MsgPresence, other.presence))
		}
	}

	return nil
}

func (r *Room) Leave(c *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if c.presence != nil {
		r.broadcast(frame(MsgPresenceGone, c.presence), nil)
	}

	delete(r.clients, c)
}

func (r *Room) Handle(c *Client, msg []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(msg) == 0 {
		return crdt.ErrBadMessage
	}

	switch msg[0] {
	case MsgSyncStep1:
		sv, err := crdt.DecodeSV(msg[1:])
		if err != nil {
			return crdt.ErrBadMessage
		}
		diff := r.doc.Diff(sv)
		if len(diff) > 0 {
			c.push(frame(MsgUpdate, crdt.EncodeOps(diff)))
		}
		return nil

	case MsgUpdate:
		if c.Role == "viewer" {
			return ErrReadOnly
		}
		ops, err := crdt.DecodeOps(msg[1:])
		if err != nil {
			return crdt.ErrBadMessage
		}
		if r.doc.Len()+len(ops) > r.limits.MaxItems {
			return ErrDocTooBig
		}
		applied := r.doc.Receive(ops...)
		if r.doc.DropPending() > 0 {
			return crdt.ErrBadMessage
		}

		r.commit(applied, c)

		return nil
	case MsgPresence:
		r.handlePresence(c, msg[1:])
		return nil

	default:
		return crdt.ErrBadMessage
	}
}
