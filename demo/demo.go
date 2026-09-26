package demo

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/veliborsimonovic/collab/server"
)

type Config struct {
	Key          ed25519.PrivateKey
	TTL          time.Duration
	MaxPeople    int
	PerIP        int
	CreateWindow time.Duration
	JoinFails    int
	JoinWindow   time.Duration
	MaxActive    int
	MaxText      int
	TrustProxy   bool
}

type room struct {
	doc     string
	expires time.Time
	names   []string
}

type Issuer struct {
	cfg     Config
	mu      sync.Mutex
	rooms   map[string]*room
	created map[string][]time.Time
	failed  map[string][]time.Time
	now     func() time.Time
}

func New(cfg Config) *Issuer {
	if cfg.TTL <= 0 {
		cfg.TTL = 30 * time.Minute
	}
	if cfg.MaxPeople <= 0 {
		cfg.MaxPeople = 5
	}
	if cfg.PerIP <= 0 {
		cfg.PerIP = 1
	}
	if cfg.CreateWindow <= 0 {
		cfg.CreateWindow = time.Hour
	}
	if cfg.JoinFails <= 0 {
		cfg.JoinFails = 10
	}
	if cfg.JoinWindow <= 0 {
		cfg.JoinWindow = 10 * time.Minute
	}
	if cfg.MaxActive <= 0 {
		cfg.MaxActive = 100
	}
	if cfg.MaxText <= 0 {
		cfg.MaxText = 10000
	}

	return &Issuer{
		cfg:     cfg,
		rooms:   make(map[string]*room),
		created: make(map[string][]time.Time),
		failed:  make(map[string][]time.Time),
		now:     time.Now,
	}
}

func (i *Issuer) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"enabled":    true,
		"ttlSeconds": int(i.cfg.TTL.Seconds()),
		"maxPeople":  i.cfg.MaxPeople,
		"maxText":    i.cfg.MaxText,
	})
}

type nameReq struct {
	Name string `json:"name"`
}

type joinReq struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type roomResp struct {
	Code      string `json:"code"`
	Doc       string `json:"doc"`
	ExpiresAt int64  `json:"expiresAt"`
	URL       string `json:"url"`
}

func (i *Issuer) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req nameReq
	if !decodeBody(w, r, &req) {
		return
	}
	name, err := validName(req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	docID, err := randomHex(16)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	ip := i.clientIP(r)
	i.mu.Lock()
	now := i.now()
	i.prune(now)

	if ts := i.created[ip]; len(ts) >= i.cfg.PerIP {
		wait := ts[len(ts)-i.cfg.PerIP].Add(i.cfg.CreateWindow).Sub(now)
		i.mu.Unlock()
		retryAfter(w, wait)
		http.Error(w, "you can start one room per hour", http.StatusTooManyRequests)
		return
	}

	if len(i.rooms) >= i.cfg.MaxActive {
		i.mu.Unlock()
		http.Error(w, "the demo is busy, try again in a minute", http.StatusServiceUnavailable)
		return
	}

	var code string
	for {
		code, err = newCode()
		if err != nil {
			i.mu.Unlock()
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if i.rooms[code] == nil {
			break
		}
	}

	doc := "demo-" + docID
	expires := now.Add(i.cfg.TTL)
	i.rooms[code] = &room{doc: doc, expires: expires, names: []string{name}}
	i.created[ip] = append(i.created[ip], now)
	i.mu.Unlock()

	token := i.mint(doc, name, 0, expires)
	writeRoom(w, http.StatusCreated, code, doc, expires, token)
}

func (i *Issuer) handleJoin(w http.ResponseWriter, r *http.Request) {
	var req joinReq
	if !decodeBody(w, r, &req) {
		return
	}
	code, ok := normalizeCode(req.Code)
	if !ok {
		http.Error(w, "the code has 6 digits", http.StatusBadRequest)
		return
	}
	name, err := validName(req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ip := i.clientIP(r)
	i.mu.Lock()
	now := i.now()
	i.prune(now)

	if ts := i.failed[ip]; len(ts) >= i.cfg.JoinFails {
		wait := ts[len(ts)-i.cfg.JoinFails].Add(i.cfg.JoinWindow).Sub(now)
		i.mu.Unlock()
		retryAfter(w, wait)
		http.Error(w, "too many wrong codes, try again later", http.StatusTooManyRequests)
		return
	}

	rm := i.rooms[code]
	if rm == nil {
		i.failed[ip] = append(i.failed[ip], now)
		i.mu.Unlock()
		http.Error(w, "no room with that code (it may have ended)", http.StatusNotFound)
		return
	}
	if rm.expires.Sub(now) < 10*time.Second {
		i.mu.Unlock()
		http.Error(w, "this room has just ended", http.StatusNotFound)
		return
	}
	if len(rm.names) >= i.cfg.MaxPeople {
		i.mu.Unlock()
		http.Error(w, "this room is full", http.StatusConflict)
		return
	}
	for _, n := range rm.names {
		if strings.EqualFold(n, name) {
			i.mu.Unlock()
			http.Error(w, "that name is already taken in this room", http.StatusConflict)
			return
		}
	}

	k := len(rm.names)
	rm.names = append(rm.names, name)
	doc, expires := rm.doc, rm.expires
	i.mu.Unlock()

	token := i.mint(doc, name, k, expires)
	writeRoom(w, http.StatusOK, code, doc, expires, token)
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "bad JSON", http.StatusBadRequest)
		}
		return false
	}
	return true
}

func retryAfter(w http.ResponseWriter, wait time.Duration) {
	secs := int(math.Ceil(wait.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func writeRoom(w http.ResponseWriter, status int, code, doc string, expires time.Time, token string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(roomResp{
		Code:      code,
		Doc:       doc,
		ExpiresAt: expires.Unix(),
		URL:       editorURL(doc, token, code),
	})
}

func (i *Issuer) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /demo/config", i.handleConfig)
	mux.HandleFunc("POST /demo/rooms", i.handleCreate)
	mux.HandleFunc("POST /demo/rooms/join", i.handleJoin)
}

func (i *Issuer) clientIP(r *http.Request) string {
	if i.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			return strings.TrimSpace(first)
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (i *Issuer) prune(now time.Time) {
	for code, r := range i.rooms {
		if !r.expires.After(now) {
			delete(i.rooms, code)
		}
	}
	pruneTimes(i.created, now, i.cfg.CreateWindow)
	pruneTimes(i.failed, now, i.cfg.JoinWindow)
}

func pruneTimes(m map[string][]time.Time, now time.Time, window time.Duration) {
	for ip, ts := range m {
		kept := ts[:0]
		for _, t := range ts {
			if now.Sub(t) < window {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(m, ip)
		} else {
			m[ip] = kept
		}
	}
}

func validName(s string) (string, error) {
	s = strings.TrimSpace(s)

	n := utf8.RuneCountInString(s)
	if n < 1 {
		return "", errors.New("name is empty")
	}
	if n > 20 {
		return "", errors.New("name is longer than 20 characters")
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", errors.New("name has control characters")
		}
	}

	return s, nil
}

func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%06d", n), nil
}

func normalizeCode(s string) (string, bool) {
	s = strings.NewReplacer(" ", "", "-", "").Replace(s)
	if len(s) != 6 {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return "", false
		}
	}

	return s, true
}

var palette = []string{"#e11d48", "#2563eb", "#16a34a", "#d97706"}

func (i *Issuer) mint(doc, name string, k int, exp time.Time) string {

	b := make([]byte, 8)

	if _, err := rand.Read(b); err != nil {
		log.Fatalf("failed to generate random bytes: %v", err)
	}

	hexStr := hex.EncodeToString(b)
	return server.Sign(i.cfg.Key, server.Claims{
		Sub:   "demo-" + hexStr,
		Doc:   doc,
		Role:  "editor",
		Name:  name,
		Color: palette[k%len(palette)],
		Exp:   exp.Unix(),
	})
}

func editorURL(doc, token, code string) string {
	return fmt.Sprintf("/?doc=%s&token=%s&code=%s", doc, token, code)
}
