package server

import (
	"encoding/json"
	"time"

	"github.com/veliborsimonovic/collab/crdt"
)

type Client struct {
	Role         string
	User         string
	Name         string
	Color        string
	send         chan []byte
	kick         func()
	presence     []byte
	lastPresence time.Time
}

const (
	maxPresenceBytes = 1024
	minPresenceGap   = 50 * time.Millisecond
)

func NewClient(role, user, name, color string, kick func()) *Client {
	return &Client{
		Role:  role,
		User:  user,
		Name:  name,
		Color: color,
		send:  make(chan []byte, 256),
		kick:  kick,
	}
}

func (c *Client) push(frame []byte) {
	select {
	case c.send <- frame:
	default:
		c.kick()
	}
}

func (c *Client) Out() <-chan []byte {
	return c.send
}

type Limits struct {
	MaxClients int
	MaxItems   int
}

type LimitsOption func(*Limits)

func WithMaxClients(n int) LimitsOption { return func(l *Limits) { l.MaxClients = n } }
func WithMaxItems(n int) LimitsOption   { return func(l *Limits) { l.MaxItems = n } }

func NewLimits(opts ...LimitsOption) Limits {
	l := Limits{MaxClients: 100, MaxItems: 1_000_000}
	for _, opt := range opts {
		opt(&l)
	}
	return l
}

func (r *Room) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.clients)
}

func (r *Room) handlePresence(c *Client, payload []byte) {

	if len(payload) > maxPresenceBytes || time.Since(c.lastPresence) < minPresenceGap {
		return
	}

	var data map[string]any
	err := json.Unmarshal(payload, &data)
	if err != nil {
		return
	}

	if _, ok := data["client"].(float64); !ok {
		return
	}

	data["user"] = c.User
	data["name"] = c.Name
	data["color"] = c.Color

	out, err := json.Marshal(data)
	if err != nil {
		return
	}

	c.lastPresence = time.Now()
	c.presence = out
	r.broadcast(frame(MsgPresence, out), c)

}

func (r *Room) Text() string {

	r.mu.Lock()
	defer r.mu.Unlock()

	return r.doc.String()

}

func (r *Room) Edit(pos, del int, ins string) error {

	r.mu.Lock()
	defer r.mu.Unlock()

	runes := []rune(ins)

	if r.doc.Len()+len(runes) > r.limits.MaxItems {
		return ErrDocTooBig
	}

	var ops []crdt.Op = make([]crdt.Op, 0)
	var currErr error = nil

	for i := 0; i < del; i++ {
		op, err := r.doc.LocalDelete(pos)
		if err != nil {
			currErr = err
			break
		}
		ops = append(ops, op)
	}

	if currErr == nil {
		for i, ru := range runes {
			op, err := r.doc.LocalInsert(pos+i, ru)
			if err != nil {
				currErr = err
				break
			}
			ops = append(ops, op)
		}
	}

	err := r.commit(ops, nil)
	if err != nil {
		return err
	}

	if currErr != nil {
		return currErr
	}

	return nil

}
