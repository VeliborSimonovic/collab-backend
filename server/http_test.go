package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/veliborsimonovic/collab/crdt"
	"github.com/veliborsimonovic/collab/store"

	"github.com/coder/websocket"
)

type httpEnv struct {
	t    *testing.T
	ts   *httptest.Server
	priv ed25519.PrivateKey
}

func newHTTPEnv(t *testing.T) *httpEnv {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	st := store.NewMemory()
	t.Cleanup(func() { st.Close() })

	hub := NewHub(st, Limits{MaxClients: 100, MaxItems: 1000}, 5*time.Minute)
	ts := httptest.NewServer(NewServer(hub, nil, NewTokenAuth(pub)).Handler())
	t.Cleanup(ts.Close)
	return &httpEnv{t: t, ts: ts, priv: priv}
}

func (e *httpEnv) token(doc, role string) string {
	return Sign(e.priv, Claims{
		Sub:  "u",
		Doc:  doc,
		Role: role,
		Name: "n",
		Exp:  time.Now().Add(time.Hour).Unix(),
	})
}

func (e *httpEnv) do(method, path, token, body string) (int, string) {
	e.t.Helper()
	req, err := http.NewRequest(method, e.ts.URL+path, strings.NewReader(body))
	if err != nil {
		e.t.Fatalf("NewRequest: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

const editHi = `{"pos":0,"delete":0,"insert":"hi"}`

func TestHTTPEditReachesWebSocket(t *testing.T) {
	env := newHTTPEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(env.ts.URL, "http") + "/ws?doc=d&token=" + env.token("d", "editor")
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	wc := &wsClient{t: t, conn: conn, doc: crdt.NewDoc(12345), ctx: ctx}
	defer wc.close()
	wc.sendFrame(frame(MsgSyncStep1, crdt.EncodeSV(wc.doc.StateVector())))

	code, _ := env.do("POST", "/v1/docs/d/edit", env.token("d", "editor"), editHi)
	if code != http.StatusNoContent {
		t.Fatalf("edit status = %d, want 204", code)
	}
	wc.waitForText("hi", 2*time.Second)
}

func TestHTTPTextMatches(t *testing.T) {
	env := newHTTPEnv(t)
	if code, _ := env.do("POST", "/v1/docs/d/edit", env.token("d", "editor"), editHi); code != http.StatusNoContent {
		t.Fatalf("edit status = %d, want 204", code)
	}
	code, body := env.do("GET", "/v1/docs/d/text", env.token("d", "viewer"), "")
	if code != http.StatusOK || body != "hi" {
		t.Fatalf("text = %d %q, want 200 %q", code, body, "hi")
	}
}

func TestHTTPViewerCannotEdit(t *testing.T) {
	env := newHTTPEnv(t)
	code, _ := env.do("POST", "/v1/docs/d/edit", env.token("d", "viewer"), editHi)
	if code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", code)
	}
}

func TestHTTPOutOfRange(t *testing.T) {
	env := newHTTPEnv(t)
	code, _ := env.do("POST", "/v1/docs/d/edit", env.token("d", "editor"), `{"pos":99,"delete":0,"insert":"x"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", code)
	}
}

func TestHTTPAuth(t *testing.T) {
	env := newHTTPEnv(t)
	if code, _ := env.do("GET", "/v1/docs/d/text", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401", code)
	}
	if code, _ := env.do("GET", "/v1/docs/d/text", env.token("other", "editor"), ""); code != http.StatusForbidden {
		t.Fatalf("wrong doc: status = %d, want 403", code)
	}
}
