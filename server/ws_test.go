package server

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/veliborsimonovic/collab/crdt"
	"github.com/veliborsimonovic/collab/store"

	"github.com/coder/websocket"
)

type wsClient struct {
	t    *testing.T
	conn *websocket.Conn
	doc  *crdt.Doc
	ctx  context.Context
}

func dialClient(t *testing.T, ctx context.Context, baseURL, room string, clientID crdt.ClientID, name string) *wsClient {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(baseURL, "http") + "/ws?doc=" + room + "&name=" + name

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	wc := &wsClient{t: t, conn: conn, doc: crdt.NewDoc(clientID), ctx: ctx}
	wc.sendFrame(frame(MsgSyncStep1, crdt.EncodeSV(wc.doc.StateVector())))
	return wc
}

func (wc *wsClient) sendFrame(f []byte) {
	wc.t.Helper()
	if err := wc.conn.Write(wc.ctx, websocket.MessageBinary, f); err != nil {
		wc.t.Fatalf("Write: %v", err)
	}
}

func (wc *wsClient) handleFrame(f []byte) {
	wc.t.Helper()
	if len(f) == 0 {
		wc.t.Fatalf("empty frame")
	}
	switch f[0] {
	case MsgSyncStep1:
		sv, err := crdt.DecodeSV(f[1:])
		if err != nil {
			wc.t.Fatalf("DecodeSV: %v", err)
		}
		if diff := wc.doc.Diff(sv); len(diff) > 0 {
			wc.sendFrame(frame(MsgUpdate, crdt.EncodeOps(diff)))
		}
	case MsgUpdate:
		ops, err := crdt.DecodeOps(f[1:])
		if err != nil {
			wc.t.Fatalf("DecodeOps: %v", err)
		}
		wc.doc.Receive(ops...)
	}
}

func (wc *wsClient) readFrame(timeout time.Duration) bool {
	wc.t.Helper()
	ctx, cancel := context.WithTimeout(wc.ctx, timeout)
	defer cancel()
	_, msg, err := wc.conn.Read(ctx)
	if err != nil {
		return false
	}
	wc.handleFrame(msg)
	return true
}

func (wc *wsClient) waitForText(want string, timeout time.Duration) {
	wc.t.Helper()
	deadline := time.Now().Add(timeout)
	for wc.doc.String() != want {
		if time.Now().After(deadline) {
			wc.t.Fatalf("timed out waiting for %q, have %q", want, wc.doc.String())
		}
		if !wc.readFrame(200 * time.Millisecond) {
			continue
		}
	}
}

func (wc *wsClient) insert(pos int, r rune) {
	wc.t.Helper()
	op, err := wc.doc.LocalInsert(pos, r)
	if err != nil {
		wc.t.Fatalf("LocalInsert: %v", err)
	}
	wc.sendFrame(frame(MsgUpdate, crdt.EncodeOps([]crdt.Op{op})))
}

func (wc *wsClient) close() {
	wc.conn.Close(websocket.StatusNormalClosure, "")
}

func newWSTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st := store.NewMemory()
	t.Cleanup(func() { st.Close() })

	hub := NewHub(st, Limits{MaxClients: 100, MaxItems: 1_000_000}, 5*time.Minute)
	srv := NewServer(hub, nil, NewDevAuth())
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestWebSocketTwoClientsConverge(t *testing.T) {
	ts := newWSTestServer(t)
	ctx := context.Background()

	a := dialClient(t, ctx, ts.URL, "demo", 2, "a")
	defer a.close()
	b := dialClient(t, ctx, ts.URL, "demo", 3, "b")
	defer b.close()

	a.readFrame(time.Second)
	b.readFrame(time.Second)

	a.insert(0, 'h')
	a.insert(1, 'i')

	b.waitForText("hi", 2*time.Second)
	if a.doc.String() != "hi" {
		t.Fatalf("A text = %q, want %q", a.doc.String(), "hi")
	}
}

func TestWebSocketRestartReconnect(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "restart.db")

	st1, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	hub1 := NewHub(st1, Limits{MaxClients: 100, MaxItems: 1_000_000}, 5*time.Minute)
	srv1 := NewServer(hub1, nil, NewDevAuth())
	ts1 := httptest.NewServer(srv1.Handler())

	ctx := context.Background()
	a := dialClient(t, ctx, ts1.URL, "demo", 2, "a")
	a.readFrame(time.Second)

	witness := dialClient(t, ctx, ts1.URL, "demo", 3, "witness")
	witness.readFrame(time.Second)

	a.insert(0, 'h')
	a.insert(1, 'i')
	witness.waitForText("hi", 2*time.Second)

	a.close()
	witness.close()
	ts1.Close()
	if err := st1.Close(); err != nil {
		t.Fatalf("st1.Close: %v", err)
	}

	st2, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite (reopen): %v", err)
	}
	defer st2.Close()
	hub2 := NewHub(st2, Limits{MaxClients: 100, MaxItems: 1_000_000}, 5*time.Minute)
	srv2 := NewServer(hub2, nil, NewDevAuth())
	ts2 := httptest.NewServer(srv2.Handler())
	defer ts2.Close()

	b := dialClient(t, ctx, ts2.URL, "demo", 4, "b")
	defer b.close()
	b.waitForText("hi", 2*time.Second)
}

func TestWebSocketDocIndependence(t *testing.T) {
	ts := newWSTestServer(t)
	ctx := context.Background()

	a := dialClient(t, ctx, ts.URL, "roomA", 2, "a")
	defer a.close()
	other := dialClient(t, ctx, ts.URL, "roomB", 3, "other")
	defer other.close()

	a.readFrame(time.Second)
	other.readFrame(time.Second)

	a.insert(0, 'x')

	if other.readFrame(200 * time.Millisecond) {
		t.Fatalf("a connection in a different room received a frame meant for roomA")
	}
	if other.doc.String() != "" {
		t.Fatalf("roomB text changed: %q", other.doc.String())
	}

	a2 := dialClient(t, ctx, ts.URL, "roomA", 4, "a2")
	defer a2.close()
	a2.readFrame(time.Second)
	a2.waitForText("x", 2*time.Second)
}
