package server

import (
	"context"
	"crypto/ed25519"
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

func TestPingDropsDeadClient(t *testing.T) {
	oldInterval, oldTimeout := pingInterval, pingTimeout
	pingInterval, pingTimeout = 50*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { pingInterval, pingTimeout = oldInterval, oldTimeout })

	st := store.NewMemory()
	t.Cleanup(func() { st.Close() })
	hub := NewHub(st, Limits{MaxClients: 100, MaxItems: 1_000_000}, 5*time.Minute)
	ts := httptest.NewServer(NewServer(hub, nil, NewDevAuth()).Handler())
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// A never reads, so it never answers pings.
	a := dialClient(t, ctx, ts.URL, "demo", 2, "a")

	// B keeps reading, which lets the library answer pings.
	b := dialClient(t, ctx, ts.URL, "demo", 3, "b")
	go func() {
		for {
			if _, _, err := b.conn.Read(ctx); err != nil {
				return
			}
		}
	}()

	time.Sleep(500 * time.Millisecond)

	if _, conns := hub.Stats(); conns != 1 {
		t.Fatalf("conns = %d, want 1 (only B)", conns)
	}

	readCtx, readCancel := context.WithTimeout(ctx, time.Second)
	defer readCancel()
	// Frames the server sent before dropping A may still be buffered; drain
	// them until the close shows up as an error.
	for {
		if _, _, err := a.conn.Read(readCtx); err != nil {
			if readCtx.Err() != nil {
				t.Fatal("A was never disconnected: read only ended on our own timeout")
			}
			break
		}
	}
}

func TestHealthDuringBusyRoom(t *testing.T) {
	hub := NewHub(store.NewMemory(), NewLimits(), time.Minute)
	srv := NewServer(hub, nil, NewDevAuth())

	room, release, err := hub.Acquire("busy")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer release()

	room.mu.Lock()
	locked := true
	unlock := func() {
		if locked {
			locked = false
			room.mu.Unlock()
		}
	}
	defer unlock()

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		unlock()
		<-done
		t.Fatal("/healthz did not answer within 100ms while a room was locked")
	}

	if rec.Code != 200 {
		t.Fatalf("/healthz status = %d, want 200", rec.Code)
	}
}

func TestTokenExpiryClosesConnection(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(store.NewMemory(), NewLimits(), time.Minute)
	ts := httptest.NewServer(NewServer(hub, nil, NewTokenAuth(pub)).Handler())
	defer ts.Close()

	// exp is in whole seconds, so now+2 leaves between 1 and 2 s before expiry.
	tok := Sign(priv, Claims{Sub: "u1", Doc: "A", Role: "editor", Name: "Vevi", Color: "#f60",
		Exp: time.Now().Unix() + 2})

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?doc=A&token=" + tok
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.CloseNow()

	for {
		if _, _, err := conn.Read(ctx); err != nil {
			if ctx.Err() != nil {
				t.Fatal("server did not close the connection within 4s of the token expiring")
			}
			if got := websocket.CloseStatus(err); got != CloseTokenExpired {
				t.Fatalf("close status = %d, want %d (CloseTokenExpired); err: %v", got, CloseTokenExpired, err)
			}
			return
		}
	}
}

func TestDevModeConnectionStaysOpen(t *testing.T) {
	hub := NewHub(store.NewMemory(), NewLimits(), time.Minute)
	ts := httptest.NewServer(NewServer(hub, nil, NewDevAuth()).Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?doc=A&name=dev"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.CloseNow()

	time.Sleep(1500 * time.Millisecond)

	if _, conns := hub.Stats(); conns != 1 {
		t.Fatalf("Stats conns = %d after 1.5 s, want 1 (dev mode has no expiry)", conns)
	}
}

func dialDev(t *testing.T, ctx context.Context, hub *Hub, setup func(*Server)) *websocket.Conn {
	t.Helper()
	srv := NewServer(hub, nil, NewDevAuth())
	if setup != nil {
		setup(srv)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws?doc=A&name=dev", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

// readUntilClosed reads until the server closes the connection and returns that error.
func readUntilClosed(t *testing.T, ctx context.Context, conn *websocket.Conn) error {
	t.Helper()
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			if ctx.Err() != nil {
				t.Fatal("server did not close the connection in time")
			}
			return err
		}
	}
}

func TestCloseDocFull(t *testing.T) {
	hub := NewHub(store.NewMemory(), NewLimits(WithMaxText(5)), time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn := dialDev(t, ctx, hub, nil)

	d := crdt.NewDoc(1<<20 + 7)
	var ops []crdt.Op
	for i := 0; i < 6; i++ {
		op, err := d.LocalInsert(i, 'a')
		if err != nil {
			t.Fatalf("LocalInsert: %v", err)
		}
		ops = append(ops, op)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, append([]byte{MsgUpdate}, crdt.EncodeOps(ops)...)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	err := readUntilClosed(t, ctx, conn)
	if got := websocket.CloseStatus(err); got != CloseDocFull {
		t.Fatalf("close status = %d, want %d (CloseDocFull); err: %v", got, CloseDocFull, err)
	}
}

func TestMaxMessage(t *testing.T) {
	hub := NewHub(store.NewMemory(), NewLimits(), time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn := dialDev(t, ctx, hub, func(s *Server) { s.SetMaxMessage(1024) })

	big := make([]byte, 2000)
	big[0] = MsgUpdate
	if err := conn.Write(ctx, websocket.MessageBinary, big); err != nil {
		t.Fatalf("Write: %v", err)
	}

	err := readUntilClosed(t, ctx, conn)
	if got := websocket.CloseStatus(err); got != websocket.StatusMessageTooBig {
		t.Fatalf("close status = %d, want %d (message too big); err: %v", got, websocket.StatusMessageTooBig, err)
	}
}
