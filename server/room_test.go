package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/veliborsimonovic/collab/crdt"
	"github.com/veliborsimonovic/collab/store"
)

type fakeClient struct {
	t      *testing.T
	room   *Room
	client *Client
	doc    *crdt.Doc
}

func newFakeClient(t *testing.T, room *Room, id crdt.ClientID, role string) *fakeClient {
	t.Helper()
	fc := &fakeClient{
		t:    t,
		room: room,
		doc:  crdt.NewDoc(id),
	}
	fc.client = NewClient(role, "u", "name", "color", func() {})

	if err := room.Join(fc.client); err != nil {
		t.Fatalf("Join: %v", err)
	}
	fc.sendSyncStep1()
	fc.drain()
	return fc
}

func (fc *fakeClient) sendSyncStep1() {
	fc.t.Helper()
	sv := crdt.EncodeSV(fc.doc.StateVector())
	if err := fc.room.Handle(fc.client, frame(MsgSyncStep1, sv)); err != nil {
		fc.t.Fatalf("Handle(SyncStep1): %v", err)
	}
}

func (fc *fakeClient) drain() {
	fc.t.Helper()
	for {
		select {
		case f := <-fc.client.Out():
			fc.handle(f)
		default:
			return
		}
	}
}

func (fc *fakeClient) handle(f []byte) {
	fc.t.Helper()
	if len(f) == 0 {
		fc.t.Fatalf("empty frame from server")
	}
	switch f[0] {
	case MsgSyncStep1:
		sv, err := crdt.DecodeSV(f[1:])
		if err != nil {
			fc.t.Fatalf("DecodeSV: %v", err)
		}
		if diff := fc.doc.Diff(sv); len(diff) > 0 {
			fc.send(diff)
		}
	case MsgUpdate:
		ops, err := crdt.DecodeOps(f[1:])
		if err != nil {
			fc.t.Fatalf("DecodeOps: %v", err)
		}
		fc.doc.Receive(ops...)
	default:
		fc.t.Fatalf("unexpected frame type %d", f[0])
	}
}

func (fc *fakeClient) send(ops []crdt.Op) {
	fc.t.Helper()
	if err := fc.room.Handle(fc.client, frame(MsgUpdate, crdt.EncodeOps(ops))); err != nil {
		fc.t.Fatalf("Handle(Update): %v", err)
	}
}

func (fc *fakeClient) randomEdit(rng *rand.Rand) {
	fc.t.Helper()
	letters := []rune("abcdefghij")
	length := len([]rune(fc.doc.String()))

	var op crdt.Op
	if length == 0 || rng.Float64() < 0.75 {
		o, err := fc.doc.LocalInsert(rng.Intn(length+1), letters[rng.Intn(len(letters))])
		if err != nil {
			fc.t.Fatalf("LocalInsert: %v", err)
		}
		op = o
	} else {
		o, err := fc.doc.LocalDelete(rng.Intn(length))
		if err != nil {
			fc.t.Fatalf("LocalDelete: %v", err)
		}
		op = o
	}
	fc.send([]crdt.Op{op})
}

func newTestRoom(t *testing.T) *Room {
	t.Helper()
	room, err := OpenRoom("doc1", store.NewMemory(), NewLimits(WithMaxClients(100), WithMaxItems(1000000)))
	if err != nil {
		t.Fatalf("OpenRoom: %v", err)
	}
	return room
}

func TestThreeClientsRandomEdits(t *testing.T) {
	room := newTestRoom(t)
	rng := rand.New(rand.NewSource(1))

	clients := []*fakeClient{
		newFakeClient(t, room, 2, "editor"),
		newFakeClient(t, room, 3, "editor"),
		newFakeClient(t, room, 4, "editor"),
	}

	for i := 0; i < 300; i++ {
		clients[rng.Intn(len(clients))].randomEdit(rng)
		if rng.Float64() < 0.5 {
			clients[rng.Intn(len(clients))].drain()
		}
	}
	for _, c := range clients {
		c.drain()
	}

	want := room.doc.String()
	for i, c := range clients {
		if c.doc.String() != want {
			t.Fatalf("client %d text mismatch:\n want: %q\n got:  %q", i, want, c.doc.String())
		}
		if c.doc.PendingLen() != 0 {
			t.Fatalf("client %d still has %d pending ops", i, c.doc.PendingLen())
		}
	}
}

func TestReconnectNoLostText(t *testing.T) {
	room := newTestRoom(t)
	rng := rand.New(rand.NewSource(2))

	a := newFakeClient(t, room, 2, "editor")
	b := newFakeClient(t, room, 3, "editor")

	room.Leave(b.client)

	for i := 0; i < 20; i++ {
		a.randomEdit(rng)
	}

	for i := 0; i < 10; i++ {
		length := len([]rune(b.doc.String()))
		if _, err := b.doc.LocalInsert(rng.Intn(length+1), 'x'); err != nil {
			t.Fatalf("offline LocalInsert: %v", err)
		}
	}

	if err := room.Join(b.client); err != nil {
		t.Fatalf("rejoin Join: %v", err)
	}
	b.sendSyncStep1()
	b.drain()
	a.drain()

	want := room.doc.String()
	if a.doc.String() != want {
		t.Fatalf("A text mismatch:\n want: %q\n got:  %q", want, a.doc.String())
	}
	if b.doc.String() != want {
		t.Fatalf("B text mismatch:\n want: %q\n got:  %q", want, b.doc.String())
	}
}

func TestRestartKeepsText(t *testing.T) {
	st := store.NewMemory()
	room1, err := OpenRoom("doc1", st, Limits{MaxClients: 100, MaxItems: 1_000_000})
	if err != nil {
		t.Fatalf("OpenRoom: %v", err)
	}

	a := newFakeClient(t, room1, 2, "editor")
	rng := rand.New(rand.NewSource(3))
	for i := 0; i < 50; i++ {
		a.randomEdit(rng)
	}
	want := room1.doc.String()

	room2, err := OpenRoom("doc1", st, Limits{MaxClients: 100, MaxItems: 1_000_000})
	if err != nil {
		t.Fatalf("OpenRoom (restart): %v", err)
	}
	if room2.doc.String() != want {
		t.Fatalf("restart text mismatch:\n want: %q\n got:  %q", want, room2.doc.String())
	}

}

func TestViewerUpdateRejected(t *testing.T) {
	room := newTestRoom(t)
	viewer := newFakeClient(t, room, 2, "viewer")
	before := room.doc.String()

	op, err := viewer.doc.LocalInsert(0, 'x')
	if err != nil {
		t.Fatalf("LocalInsert: %v", err)
	}

	err = room.Handle(viewer.client, frame(MsgUpdate, crdt.EncodeOps([]crdt.Op{op})))
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("want ErrReadOnly, got %v", err)
	}
	if room.doc.String() != before {
		t.Fatalf("server text changed after a rejected viewer update:\n before: %q\n after:  %q", before, room.doc.String())
	}
}

func TestGarbageBytesRejected(t *testing.T) {
	room := newTestRoom(t)
	editor := newFakeClient(t, room, 2, "editor")

	err := room.Handle(editor.client, frame(MsgUpdate, []byte{0xff, 0xff, 0xff}))
	if !errors.Is(err, crdt.ErrBadMessage) {
		t.Fatalf("want crdt.ErrBadMessage, got %v", err)
	}
}

func TestUnknownDependencyOpRejected(t *testing.T) {
	room := newTestRoom(t)
	editor := newFakeClient(t, room, 2, "editor")

	bogus := crdt.InsertOp{
		ID:          crdt.ID{Client: 99, Clock: 0},
		Origin:      crdt.ID{Client: 98, Clock: 0},
		RightOrigin: crdt.EndID,
		Content:     'z',
	}

	err := room.Handle(editor.client, frame(MsgUpdate, crdt.EncodeOps([]crdt.Op{bogus})))
	if err == nil {
		t.Fatalf("want an error for an op with an unknown dependency, got nil")
	}
	if room.doc.PendingLen() != 0 {
		t.Fatalf("PendingLen() = %d, want 0 after DropPending", room.doc.PendingLen())
	}
}

func TestUnresponsiveClientGetsKicked(t *testing.T) {
	room := newTestRoom(t)

	kicked := false
	victim := NewClient("editor", "u", "victim", "c", func() { kicked = true })
	if err := room.Join(victim); err != nil {
		t.Fatalf("Join: %v", err)
	}

	sender := newFakeClient(t, room, 2, "editor")
	for i := 0; i < 300; i++ {
		op, err := sender.doc.LocalInsert(0, 'a')
		if err != nil {
			t.Fatalf("LocalInsert: %v", err)
		}
		if err := room.Handle(sender.client, frame(MsgUpdate, crdt.EncodeOps([]crdt.Op{op}))); err != nil {
			t.Fatalf("Handle: %v", err)
		}
	}

	if !kicked {
		t.Fatalf("expected the unresponsive client to be kicked")
	}
}

func TestLimitsEnforced(t *testing.T) {
	t.Run("MaxClients", func(t *testing.T) {
		room, err := OpenRoom("doc-clients", store.NewMemory(), Limits{MaxClients: 2, MaxItems: 1_000_000})
		if err != nil {
			t.Fatalf("OpenRoom: %v", err)
		}
		_ = newFakeClient(t, room, 2, "editor")
		_ = newFakeClient(t, room, 3, "editor")

		third := NewClient("editor", "u", "third", "c", func() {})
		if err := room.Join(third); !errors.Is(err, ErrRoomFull) {
			t.Fatalf("want ErrRoomFull, got %v", err)
		}
	})

	t.Run("MaxItems", func(t *testing.T) {
		room, err := OpenRoom("doc-items", store.NewMemory(), Limits{MaxClients: 100, MaxItems: 5})
		if err != nil {
			t.Fatalf("OpenRoom: %v", err)
		}
		editor := newFakeClient(t, room, 2, "editor")

		for i := 0; i < 5; i++ {
			op, err := editor.doc.LocalInsert(i, 'a')
			if err != nil {
				t.Fatalf("LocalInsert: %v", err)
			}
			if err := room.Handle(editor.client, frame(MsgUpdate, crdt.EncodeOps([]crdt.Op{op}))); err != nil {
				t.Fatalf("Handle (within limit): %v", err)
			}
		}

		op, err := editor.doc.LocalInsert(5, 'z')
		if err != nil {
			t.Fatalf("LocalInsert: %v", err)
		}
		err = room.Handle(editor.client, frame(MsgUpdate, crdt.EncodeOps([]crdt.Op{op})))
		if !errors.Is(err, ErrDocTooBig) {
			t.Fatalf("want ErrDocTooBig, got %v", err)
		}
	})
}

func next(t *testing.T, c *Client) []byte {
	t.Helper()
	select {
	case f := <-c.Out():
		return f
	case <-time.After(time.Second):
		t.Fatal("no frame received within 1s")
		return nil
	}
}

func none(t *testing.T, c *Client) {
	t.Helper()
	select {
	case f := <-c.Out():
		t.Fatalf("unexpected frame of type %d", f[0])
	case <-time.After(100 * time.Millisecond):
	}
}

func newPresenceRoom(t *testing.T) *Room {
	t.Helper()
	room, err := OpenRoom("p", store.NewMemory(), Limits{MaxClients: 10, MaxItems: 1000})
	if err != nil {
		t.Fatal(err)
	}
	return room
}

func join(t *testing.T, room *Room, user, name string, drain bool) *Client {
	t.Helper()
	c := NewClient("editor", user, name, "#f00", func() {})
	if err := room.Join(c); err != nil {
		t.Fatal(err)
	}
	if drain {
		if f := next(t, c); f[0] != MsgSyncStep1 {
			t.Fatalf("first frame type %d, want SyncStep1", f[0])
		}
	}
	return c
}

func decode(t *testing.T, f []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(f[1:], &m); err != nil {
		t.Fatalf("bad presence JSON: %v", err)
	}
	return m
}

var presencePayload = []byte(`{"client":1048577,"anchor":[0,0],"head":[0,0],"name":"HACKER"}`)

func sendPresence(t *testing.T, room *Room, c *Client, payload []byte) {
	t.Helper()
	if err := room.Handle(c, append([]byte{MsgPresence}, payload...)); err != nil {
		t.Fatalf("Handle returned %v, want nil", err)
	}
}

func TestPresenceStamped(t *testing.T) {
	room := newPresenceRoom(t)
	a := join(t, room, "ua", "Alice", true)
	b := join(t, room, "ub", "Bob", true)

	sendPresence(t, room, a, presencePayload)

	f := next(t, b)
	if f[0] != MsgPresence {
		t.Fatalf("frame type %d, want %d", f[0], MsgPresence)
	}
	m := decode(t, f)
	if m["name"] != "Alice" || m["user"] != "ua" {
		t.Fatalf("got name=%v user=%v, want Alice ua", m["name"], m["user"])
	}
}

func TestPresenceGone(t *testing.T) {
	room := newPresenceRoom(t)
	a := join(t, room, "ua", "Alice", true)
	b := join(t, room, "ub", "Bob", true)

	sendPresence(t, room, a, presencePayload)
	next(t, b)

	room.Leave(a)

	f := next(t, b)
	if f[0] != MsgPresenceGone {
		t.Fatalf("frame type %d, want %d", f[0], MsgPresenceGone)
	}
	if m := decode(t, f); m["client"] != float64(1048577) {
		t.Fatalf("client = %v, want 1048577", m["client"])
	}
}

func TestPresenceTooBig(t *testing.T) {
	room := newPresenceRoom(t)
	a := join(t, room, "ua", "Alice", true)
	b := join(t, room, "ub", "Bob", true)

	sendPresence(t, room, a, bytes.Repeat([]byte("x"), 2048))
	none(t, b)
}

func TestPresenceRateLimit(t *testing.T) {
	room := newPresenceRoom(t)
	a := join(t, room, "ua", "Alice", true)
	b := join(t, room, "ub", "Bob", true)

	sendPresence(t, room, a, presencePayload)
	sendPresence(t, room, a, presencePayload)

	if f := next(t, b); f[0] != MsgPresence {
		t.Fatalf("frame type %d, want %d", f[0], MsgPresence)
	}
	none(t, b)
}

func TestPresenceNewcomer(t *testing.T) {
	room := newPresenceRoom(t)
	a := join(t, room, "ua", "Alice", true)
	sendPresence(t, room, a, presencePayload)

	c := join(t, room, "uc", "Carol", false)

	if f := next(t, c); f[0] != MsgSyncStep1 {
		t.Fatalf("first frame type %d, want SyncStep1", f[0])
	}
	f := next(t, c)
	if f[0] != MsgPresence {
		t.Fatalf("second frame type %d, want %d", f[0], MsgPresence)
	}
	if m := decode(t, f); m["name"] != "Alice" {
		t.Fatalf("name = %v, want Alice", m["name"])
	}
}

func TestServerEditSurvivesRestart(t *testing.T) {
	st := store.NewMemory()
	lim := Limits{MaxClients: 100, MaxItems: 1_000_000}

	room1, err := OpenRoom("d", st, lim)
	if err != nil {
		t.Fatalf("OpenRoom: %v", err)
	}
	if err := room1.Edit(0, 0, "abc"); err != nil {
		t.Fatalf("Edit 1: %v", err)
	}

	room2, err := OpenRoom("d", st, lim)
	if err != nil {
		t.Fatalf("OpenRoom (restart 1): %v", err)
	}
	if err := room2.Edit(3, 0, "def"); err != nil {
		t.Fatalf("Edit 2: %v", err)
	}

	room3, err := OpenRoom("d", st, lim)
	if err != nil {
		t.Fatalf("OpenRoom (restart 2): %v", err)
	}
	if got := room3.Text(); got != "abcdef" {
		t.Fatalf("Text() = %q, want %q", got, "abcdef")
	}
}
