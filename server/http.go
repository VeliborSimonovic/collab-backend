package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/veliborsimonovic/collab/crdt"
)

type editRequest struct {
	Pos    int    `json:"pos"`
	Delete int    `json:"delete"`
	Insert string `json:"insert"`
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request, needEditor bool) (doc string, ok bool) {
	docId := r.PathValue("id")
	claims, err := s.auth.Authenticate(r, docId)

	if errors.Is(err, ErrForbidden) {
		http.Error(w, "ERROR", 403)
		return "", false
	}
	if err != nil {
		http.Error(w, "ERROR", 401)
		return "", false
	}

	if needEditor && claims.Role != "editor" {
		http.Error(w, "ERROR", 403)
		return "", false
	}

	return docId, true

}

func (s *Server) handleText(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.authorize(w, r, false)
	if !ok {
		return
	}

	room, release, err := s.hub.Acquire(doc)
	if err != nil {
		http.Error(w, "ERROR", http.StatusInternalServerError)
		return
	}
	defer release()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, room.Text())
}

func (s *Server) handleEdit(w http.ResponseWriter, r *http.Request) {
	doc, ok := s.authorize(w, r, true)
	if !ok {
		return
	}

	var req editRequest
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "ERROR", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "ERROR", http.StatusBadRequest)
		}
		return
	}
	if req.Pos < 0 || req.Delete < 0 {
		http.Error(w, "ERROR", http.StatusBadRequest)
		return
	}

	room, release, err := s.hub.Acquire(doc)
	if err != nil {
		http.Error(w, "ERROR", http.StatusInternalServerError)
		return
	}
	defer release()

	switch err := room.Edit(req.Pos, req.Delete, req.Insert); {
	case errors.Is(err, crdt.ErrOutOfRange):
		http.Error(w, "ERROR", http.StatusBadRequest)
	case errors.Is(err, ErrDocTooBig):
		http.Error(w, "ERROR", http.StatusRequestEntityTooLarge)
	case err != nil:
		http.Error(w, "ERROR", http.StatusInternalServerError)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
