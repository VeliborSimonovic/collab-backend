package server

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/veliborsimonovic/collab/crdt"
	"github.com/veliborsimonovic/collab/store"
)

func TestTokens(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, otherPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Unix(1_000_000, 0)
	valid := Claims{Sub: "u1", Doc: "A", Role: "editor", Name: "Vevi", Color: "#f60", Exp: now.Unix() + 3600}
	tok := Sign(priv, valid)
	parts := strings.Split(tok, ".")
	b64 := base64.RawURLEncoding.EncodeToString

	realTok := func(doc, role string) string {
		return Sign(priv, Claims{Sub: "u1", Doc: doc, Role: role, Name: "Vevi", Color: "#f60",
			Exp: time.Now().Add(time.Hour).Unix()})
	}

	t.Run("valid", func(t *testing.T) {
		c, err := Verify(pub, tok, now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.Doc != "A" {
			t.Fatalf("Doc = %q, want %q", c.Doc, "A")
		}
	})

	t.Run("expired", func(t *testing.T) {
		if _, err := Verify(pub, tok, now.Add(2*time.Hour)); err == nil {
			t.Fatal("expired token accepted")
		}
	})

	t.Run("other key", func(t *testing.T) {
		if _, err := Verify(pub, Sign(otherPriv, valid), now); err == nil {
			t.Fatal("token signed by another key accepted")
		}
	})

	t.Run("payload edited", func(t *testing.T) {
		edited := valid
		edited.Doc = "B"
		b, _ := json.Marshal(edited)
		forged := parts[0] + "." + b64(b) + "." + parts[2]
		if _, err := Verify(pub, forged, now); err == nil {
			t.Fatal("edited payload accepted")
		}
	})

	t.Run("alg none", func(t *testing.T) {
		forged := b64([]byte(`{"alg":"none","typ":"JWT"}`)) + "." + parts[1] + "."
		if _, err := Verify(pub, forged, now); err == nil {
			t.Fatal("alg none accepted")
		}
	})

	t.Run("alg HS256", func(t *testing.T) {
		forged := b64([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + parts[1] + "." + parts[2]
		if _, err := Verify(pub, forged, now); err == nil {
			t.Fatal("alg HS256 accepted")
		}
	})

	t.Run("wrong doc", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/ws?doc=B&token="+realTok("A", "editor"), nil)
		_, err := NewTokenAuth(pub).Authenticate(req, "B")
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("err = %v, want ErrForbidden", err)
		}
	})

	t.Run("viewer can't edit", func(t *testing.T) {
		hub := NewHub(store.NewMemory(), Limits{MaxClients: 100, MaxItems: 1_000_000}, time.Minute)
		ts := httptest.NewServer(NewServer(hub, nil, NewTokenAuth(pub)).Handler())
		defer ts.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?doc=A&token=" + realTok("A", "viewer")
		conn, _, err := websocket.Dial(ctx, url, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.CloseNow()

		d := crdt.NewDoc(1<<20 + 5)
		op, err := d.LocalInsert(0, 'x')
		if err != nil {
			t.Fatal(err)
		}
		msg := append([]byte{MsgUpdate}, crdt.EncodeOps([]crdt.Op{op})...)
		if err := conn.Write(ctx, websocket.MessageBinary, msg); err != nil {
			t.Fatalf("write: %v", err)
		}

		for {
			if _, _, err := conn.Read(ctx); err != nil {
				if ctx.Err() != nil {
					t.Fatal("server did not close the viewer's connection within 2s")
				}
				return
			}
		}
	})
}
