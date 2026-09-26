package demo

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/veliborsimonovic/collab/server"
)

type env struct {
	i   *Issuer
	mux *http.ServeMux
	pub ed25519.PublicKey
	now time.Time
	t0  time.Time
}

func newEnv(t *testing.T, cfg Config) *env {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Key = priv
	e := &env{pub: pub, t0: time.Unix(1_800_000_000, 0)}
	e.now = e.t0
	e.i = New(cfg)
	e.i.now = func() time.Time { return e.now }
	e.mux = http.NewServeMux()
	e.i.Register(e.mux)
	return e
}

func (e *env) post(path, remote, xff, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.RemoteAddr = remote
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)
	return w
}

func create(e *env, ip, name string) *httptest.ResponseRecorder {
	return e.post("/demo/rooms", ip+":1234", "", fmt.Sprintf(`{"name":%q}`, name))
}

func join(e *env, ip, code, name string) *httptest.ResponseRecorder {
	return e.post("/demo/rooms/join", ip+":1234", "", fmt.Sprintf(`{"code":%q,"name":%q}`, code, name))
}

type resp struct {
	Code      string `json:"code"`
	Doc       string `json:"doc"`
	ExpiresAt int64  `json:"expiresAt"`
	URL       string `json:"url"`
}

func parse(t *testing.T, w *httptest.ResponseRecorder) resp {
	t.Helper()
	var r resp
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatalf("bad response %q: %v", w.Body.String(), err)
	}
	return r
}

func (e *env) claims(t *testing.T, r resp) *server.Claims {
	t.Helper()
	u, err := url.Parse(r.URL)
	if err != nil {
		t.Fatal(err)
	}
	c, err := server.Verify(e.pub, u.Query().Get("token"), e.now)
	if err != nil {
		t.Fatalf("token does not verify: %v", err)
	}
	return c
}

func want(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d (%s)", w.Code, status, strings.TrimSpace(w.Body.String()))
	}
}

func wantBody(t *testing.T, w *httptest.ResponseRecorder, status int, sub string) {
	t.Helper()
	want(t, w, status)
	if !strings.Contains(w.Body.String(), sub) {
		t.Fatalf("body = %q, want it to contain %q", w.Body.String(), sub)
	}
}

func TestCreateRoom(t *testing.T) {
	e := newEnv(t, Config{})
	w := create(e, "1.1.1.1", "Ana")
	want(t, w, 201)
	r := parse(t, w)

	if !regexp.MustCompile(`^\d{6}$`).MatchString(r.Code) {
		t.Errorf("code = %q, want 6 digits", r.Code)
	}
	if !strings.HasPrefix(r.Doc, "demo-") {
		t.Errorf("doc = %q, want demo- prefix", r.Doc)
	}
	if !strings.Contains(r.URL, "&code="+r.Code) {
		t.Errorf("url = %q, want &code=", r.URL)
	}
	c := e.claims(t, r)
	if c.Name != "Ana" || c.Role != "editor" || c.Doc != r.Doc {
		t.Errorf("claims = %+v", c)
	}
	if c.Exp != e.t0.Add(e.i.cfg.TTL).Unix() || r.ExpiresAt != c.Exp {
		t.Errorf("exp = %d, expiresAt = %d, want %d", c.Exp, r.ExpiresAt, e.t0.Add(e.i.cfg.TTL).Unix())
	}
}

func TestJoinRoom(t *testing.T) {
	e := newEnv(t, Config{})
	a := parse(t, create(e, "1.1.1.1", "Ana"))

	w := join(e, "2.2.2.2", a.Code, "Marko")
	want(t, w, 200)
	m := parse(t, w)
	if m.Doc != a.Doc || m.ExpiresAt != a.ExpiresAt {
		t.Errorf("joined %+v, want doc and expiresAt of %+v", m, a)
	}
	ac, mc := e.claims(t, a), e.claims(t, m)
	if mc.Name != "Marko" || mc.Color == ac.Color || mc.Exp != ac.Exp {
		t.Errorf("ana = %+v, marko = %+v", ac, mc)
	}

	spaced := a.Code[:3] + " " + a.Code[3:]
	want(t, join(e, "3.3.3.3", spaced, "Iva"), 200)
}

func TestJoinLateGetsRemainingTime(t *testing.T) {
	e := newEnv(t, Config{TTL: 5 * time.Minute})
	a := parse(t, create(e, "1.1.1.1", "Ana"))

	e.now = e.t0.Add(3 * time.Minute)
	w := join(e, "2.2.2.2", a.Code, "Marko")
	want(t, w, 200)
	if got, exp := e.claims(t, parse(t, w)).Exp, e.t0.Add(5*time.Minute).Unix(); got != exp {
		t.Errorf("exp = %d, want %d", got, exp)
	}
}

func TestJoinErrors(t *testing.T) {
	e := newEnv(t, Config{MaxPeople: 2, PerIP: 10})
	a := parse(t, create(e, "1.1.1.1", "Ana"))

	want(t, join(e, "2.2.2.2", "12ab56", "Marko"), 400)
	want(t, join(e, "2.2.2.2", "12345", "Marko"), 400)

	wrong := "000000"
	if a.Code == wrong {
		wrong = "000001"
	}
	want(t, join(e, "2.2.2.2", wrong, "Marko"), 404)

	wantBody(t, join(e, "2.2.2.2", a.Code, "ANA"), 409, "taken")
	want(t, join(e, "2.2.2.2", a.Code, "Marko"), 200)
	wantBody(t, join(e, "3.3.3.3", a.Code, "Iva"), 409, "full")

	// 5 s before the end
	b := parse(t, create(e, "4.4.4.4", "Bob"))
	e.now = e.t0.Add(e.i.cfg.TTL - 5*time.Second)
	wantBody(t, join(e, "5.5.5.5", b.Code, "Cat"), 404, "just ended")

	// past TTL
	e.now = e.t0.Add(e.i.cfg.TTL)
	want(t, join(e, "2.2.2.2", a.Code, "Zed"), 404)
}

func TestJoinBruteForceLimit(t *testing.T) {
	e := newEnv(t, Config{MaxPeople: 3, PerIP: 10})
	full := parse(t, create(e, "1.1.1.1", "Ana"))
	want(t, join(e, "1.1.1.2", full.Code, "Bob"), 200)
	want(t, join(e, "1.1.1.3", full.Code, "Cat"), 200)
	ok := parse(t, create(e, "1.1.1.4", "Dan"))

	x := "9.9.9.9"
	for _, bad := range []string{"12ab56", "12345", "abcdef", ""} {
		want(t, join(e, x, bad, "Xy"), 400)
	}
	want(t, join(e, x, full.Code, "Xy"), 409)
	if n := len(e.i.failed[x]); n != 0 {
		t.Fatalf("malformed codes and a full room counted as %d failed guesses", n)
	}

	for n := 0; n < 10; n++ {
		code := fmt.Sprintf("%06d", n)
		if e.i.rooms[code] != nil {
			code = "999999"
		}
		want(t, join(e, x, code, "Xy"), 404)
	}

	w := join(e, x, ok.Code, "Xy")
	want(t, w, 429)
	if w.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After")
	}
	want(t, join(e, "8.8.8.8", ok.Code, "Eve"), 200)

	e.now = e.now.Add(e.i.cfg.JoinWindow + time.Second)
	want(t, join(e, x, ok.Code, "Xy"), 200)
}

func TestCreateRateLimitPerIP(t *testing.T) {
	e := newEnv(t, Config{})
	want(t, create(e, "1.1.1.1", "Ana"), 201)
	w := create(e, "1.1.1.1", "Ana")
	want(t, w, 429)
	if got := w.Header().Get("Retry-After"); got != "3600" {
		t.Errorf("Retry-After = %q, want 3600", got)
	}

	other := parse(t, create(e, "2.2.2.2", "Bob"))
	want(t, join(e, "1.1.1.1", other.Code, "Ana"), 200)

	e.now = e.t0.Add(59 * time.Minute)
	w = create(e, "1.1.1.1", "Ana")
	want(t, w, 429)
	if got := w.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q, want 60", got)
	}
	want(t, create(e, "3.3.3.3", "Cat"), 201)

	e.now = e.t0.Add(time.Hour)
	want(t, create(e, "1.1.1.1", "Ana"), 201)
}

func TestMaxActive(t *testing.T) {
	e := newEnv(t, Config{MaxActive: 2, PerIP: 10})
	want(t, create(e, "1.1.1.1", "A"), 201)
	want(t, create(e, "1.1.1.1", "B"), 201)
	want(t, create(e, "1.1.1.1", "C"), 503)

	e.now = e.t0.Add(e.i.cfg.TTL)
	want(t, create(e, "1.1.1.1", "C"), 201)
}

func TestBadInput(t *testing.T) {
	e := newEnv(t, Config{PerIP: 100})
	code := parse(t, create(e, "1.1.1.1", "Ana")).Code

	for _, name := range []string{"", strings.Repeat("a", 21), "a\nb"} {
		want(t, create(e, "2.2.2.2", name), 400)
		want(t, join(e, "2.2.2.2", code, name), 400)
	}
	want(t, e.post("/demo/rooms", "2.2.2.2:1", "", `{nope`), 400)
	want(t, e.post("/demo/rooms/join", "2.2.2.2:1", "", `{nope`), 400)

	big := `{"code":"` + code + `","name":"` + strings.Repeat("a", 5<<10) + `"}`
	want(t, e.post("/demo/rooms", "2.2.2.2:1", "", big), 413)
	want(t, e.post("/demo/rooms/join", "2.2.2.2:1", "", big), 413)
}

func TestTrustProxy(t *testing.T) {
	mk := func(e *env, xff string) *httptest.ResponseRecorder {
		return e.post("/demo/rooms", "10.0.0.1:1234", xff, `{"name":"Ana"}`)
	}
	bad := func(e *env, xff string) *httptest.ResponseRecorder {
		return e.post("/demo/rooms/join", "10.0.0.1:1234", xff, `{"code":"000000","name":"Ana"}`)
	}

	e := newEnv(t, Config{TrustProxy: true, JoinFails: 2})
	want(t, mk(e, "1.1.1.1"), 201)
	want(t, mk(e, "2.2.2.2, 10.0.0.1"), 201)
	want(t, mk(e, "1.1.1.1"), 429)
	want(t, bad(e, "5.5.5.5"), 404)
	want(t, bad(e, "5.5.5.5"), 404)
	want(t, bad(e, "5.5.5.5"), 429)
	want(t, bad(e, "6.6.6.6"), 404)

	e = newEnv(t, Config{JoinFails: 2})
	want(t, mk(e, "1.1.1.1"), 201)
	want(t, mk(e, "2.2.2.2"), 429)
	want(t, bad(e, "5.5.5.5"), 404)
	want(t, bad(e, "6.6.6.6"), 404)
	want(t, bad(e, "7.7.7.7"), 429)
}

func TestPruneKeepsMapsSmall(t *testing.T) {
	e := newEnv(t, Config{})
	for n := 0; n < 10; n++ {
		want(t, create(e, fmt.Sprintf("1.1.1.%d", n), "Ana"), 201)
	}
	for n := 0; n < 5; n++ {
		code := "000000"
		if e.i.rooms[code] != nil {
			code = "000001"
		}
		want(t, join(e, fmt.Sprintf("2.2.2.%d", n), code, "Bob"), 404)
	}

	e.now = e.t0.Add(2 * time.Hour)
	want(t, create(e, "3.3.3.3", "Cat"), 201)

	if len(e.i.rooms) != 1 {
		t.Errorf("len(rooms) = %d, want 1", len(e.i.rooms))
	}
	if len(e.i.failed) != 0 {
		t.Errorf("len(failed) = %d, want 0", len(e.i.failed))
	}
	if len(e.i.created) != 1 {
		t.Errorf("len(created) = %d, want 1", len(e.i.created))
	}
}

func TestCodesUnique(t *testing.T) {
	e := newEnv(t, Config{MaxActive: 50, PerIP: 50})
	seen := map[string]bool{}
	for n := 0; n < 50; n++ {
		w := create(e, "1.1.1.1", "Ana")
		want(t, w, 201)
		code := parse(t, w).Code
		if seen[code] {
			t.Fatalf("duplicate code %s", code)
		}
		seen[code] = true
	}
}
