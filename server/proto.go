package server

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/veliborsimonovic/collab/crdt"
	"github.com/veliborsimonovic/collab/store"
)

const (
	MsgSyncStep1      byte                 = 1
	MsgUpdate         byte                 = 2
	MsgPresence       byte                 = 3
	MsgPresenceGone   byte                 = 4
	CloseTokenExpired websocket.StatusCode = 4001
	CloseDocFull      websocket.StatusCode = 4002
	CloseTooFast      websocket.StatusCode = 4003
)

func frame(t byte, payload []byte) []byte {
	out := make([]byte, 1+len(payload))
	out[0] = t
	copy(out[1:], payload)
	return out
}

var (
	ErrReadOnly  = errors.New("server: viewer cannot edit")
	ErrDocTooBig = errors.New("server: document is full")
	ErrRoomFull  = errors.New("server: room is at MaxClients")
	ErrTooFast   = errors.New("server: too many edits")
)

type Room struct {
	mu          sync.Mutex
	id          string
	doc         *crdt.Doc
	store       store.Store
	clients     map[*Client]bool
	limits      Limits
	loadTime    time.Duration
	clientCount atomic.Int64
	itemCount   atomic.Int64
}

func OpenRoom(id string, st store.Store, lim Limits) (*Room, error) {
	startTime := time.Now()
	ops, err := st.Load(id)
	if err != nil {
		return nil, err
	}

	doc := crdt.NewDoc(1)
	doc.Receive(ops...)
	doc.DropPending()
	doc.ResumeClock()

	duration := time.Since(startTime)

	r := &Room{
		id:       id,
		doc:      doc,
		store:    st,
		clients:  make(map[*Client]bool),
		limits:   lim,
		loadTime: duration,
	}
	r.itemCount.Store(int64(doc.Len()))

	return r, nil
}

func (r *Room) Size() (items int, load time.Duration) {
	return int(r.itemCount.Load()), r.loadTime
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
	c.tokens = float64(r.limits.Burst)
	c.lastRefill = time.Now()
	c.push(frame(MsgSyncStep1, crdt.EncodeSV(r.doc.StateVector())))
	r.clientCount.Add(1)
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
	if r.clients[c] {
		delete(r.clients, c)
		r.clientCount.Add(-1)
	}
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

		ops, err := crdt.DecodeOps(msg[1:])
		if err != nil {
			return crdt.ErrBadMessage
		}

		if len(ops) == 0 {
			return nil
		}

		if c.Role == "viewer" {
			return ErrReadOnly
		}

		if r.limits.Rate > 0 {
			now := time.Now()
			c.tokens = math.Min(float64(r.limits.Burst), c.tokens+r.limits.Rate*now.Sub(c.lastRefill).Seconds())
			c.lastRefill = now

			cost := float64(len(ops))
			if c.tokens < cost {
				return ErrTooFast
			}
			c.tokens -= cost
		}

		if r.limits.MaxOps > 0 && r.doc.OpCount()+len(ops) > r.limits.MaxOps {
			return ErrDocTooBig
		}

		if r.limits.MaxText > 0 {
			I := 0
			D := 0
			for _, v := range ops {
				switch v.(type) {
				case crdt.InsertOp:
					I++

				case crdt.DeleteOp:
					D++
				}
			}

			if I > D && r.doc.VisibleLen()+I-D > r.limits.MaxText {
				return ErrDocTooBig
			}

		}

		if r.doc.Len()+len(ops) > r.limits.MaxItems {
			return ErrDocTooBig
		}

		applied := r.doc.Receive(ops...)
		dropped := r.doc.DropPending()
		r.itemCount.Store(int64(r.doc.Len()))
		if dropped > 0 {
			return crdt.ErrBadMessage
		}

		return r.commit(applied, c)

	case MsgPresence:
		r.handlePresence(c, msg[1:])
		return nil

	default:
		return crdt.ErrBadMessage
	}
}
