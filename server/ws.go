package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"time"

	"github.com/veliborsimonovic/collab/web"

	"github.com/coder/websocket"
)

type Server struct {
	hub     *Hub
	origins []string
	auth    Authenticator
}

var (
	pingInterval = 30 * time.Second
	pingTimeout  = 10 * time.Second
)

func NewServer(hub *Hub, origins []string, auth Authenticator) *Server {
	return &Server{
		hub:     hub,
		origins: origins,
		auth:    auth,
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	doc := r.URL.Query().Get("doc")
	if doc == "" {
		http.Error(w, "missing ?doc=", http.StatusBadRequest)
		return
	}

	claims, err := s.auth.Authenticate(r, doc)
	if errors.Is(err, ErrForbidden) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: s.origins,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(8 << 20)

	ctx := r.Context()

	room, release, err := s.hub.Acquire(doc)
	if err != nil {
		conn.Close(websocket.StatusInternalError, "acquire failed")
		return
	}

	c := NewClient(claims.Role, claims.Sub, claims.Name, claims.Color, func() { conn.CloseNow() })

	if err := room.Join(c); err != nil {
		release()
		conn.Close(websocket.StatusTryAgainLater, "room full")
		return
	}

	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	go func() {

		for {
			select {
			case <-ctx.Done():
				return
			case _ = <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
				err := conn.Ping(ctx)
				cancel()
				if err != nil {
					conn.CloseNow()
					return
				}
			}
		}

	}()

	defer release()
	defer room.Leave(c)

	go func() {
		for {
			select {
			case msg := <-c.Out():
				if err := conn.Write(ctx, websocket.MessageBinary, msg); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if err := room.Handle(c, msg); err != nil {
			conn.Close(websocket.StatusPolicyViolation, "bad message")
			return
		}
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	rooms, conns := s.hub.Stats()

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	items, load := s.hub.Largest()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"rooms":     rooms,
		"conns":     conns,
		"heapMB":    float64(mem.HeapAlloc) / 1e6,
		"maxItems":  items,
		"maxLoadMs": float64(load) / float64(time.Millisecond),
	})
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServer(http.FS(web.FS)))
	mux.HandleFunc("GET /v1/docs/{id}/text", s.handleText)
	mux.HandleFunc("POST /v1/docs/{id}/edit", s.handleEdit)
	mux.HandleFunc("GET /ws", s.handleWS)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	return mux
}
