// Package server exposes the pool over HTTP. It is the only component that
// talks to PostgreSQL. The CLI drives every endpoint here; none are meant to be
// hit by hand. All endpoints except /healthz require a bearer token.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/zilla/cmiyc-pool/internal/potfile"
	"github.com/zilla/cmiyc-pool/internal/store"
)

// maxUploadBytes caps a single potfile upload. Generous, since network hash
// dumps get large, but bounded so a bad actor cannot exhaust memory.
const maxUploadBytes = 512 << 20 // 512 MiB

// Server holds dependencies for the HTTP handlers.
type Server struct {
	store *store.Store
	token string
	log   *slog.Logger
}

// New builds a Server. token is the shared bearer secret; it must be non-empty.
func New(st *store.Store, token string, log *slog.Logger) (*Server, error) {
	if token == "" {
		return nil, fmt.Errorf("POOL_TOKEN must be set")
	}
	return &Server{store: st, token: token, log: log}, nil
}

// Handler wires routes. Go's ServeMux method+path patterns keep this dependency-free.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/submit", s.auth(s.handleSubmit))
	mux.HandleFunc("GET /v1/pending", s.auth(s.handlePending))
	mux.HandleFunc("GET /v1/all", s.auth(s.handleAll))
	mux.HandleFunc("POST /v1/mark", s.auth(s.handleMark))
	mux.HandleFunc("GET /v1/stats", s.auth(s.handleStats))
	return mux
}

// ListenAndServe binds 0.0.0.0:addr with timeouts suitable for large uploads.
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
		// ReadTimeout is intentionally 0: a slow client streaming a very large
		// potfile should not be cut off mid-upload. maxUploadBytes bounds abuse.
	}
	s.log.Info("pool listening", "addr", addr)
	return srv.ListenAndServe()
}

// --- middleware ---

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) != 1 {
			writeErr(w, http.StatusUnauthorized, "invalid or missing bearer token")
			return
		}
		next(w, r)
	}
}

// --- handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	author := strings.TrimSpace(r.URL.Query().Get("author"))
	if author == "" {
		author = strings.TrimSpace(r.Header.Get("X-Author"))
	}
	if author == "" {
		writeErr(w, http.StatusBadRequest, "author is required (?author= or X-Author header)")
		return
	}

	body := http.MaxBytesReader(w, r.Body, maxUploadBytes)
	lines, err := potfile.Parse(body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "parse potfile: "+err.Error())
		return
	}

	res, err := s.store.Submit(r.Context(), author, lines)
	if err != nil {
		s.log.Error("submit failed", "author", author, "err", err)
		writeErr(w, http.StatusInternalServerError, "submit failed")
		return
	}
	s.log.Info("submit",
		"author", author,
		"new", res.NewCracks,
		"batch_unique", res.BatchUnique,
		"total", res.TotalUnique,
		"pending", res.Pending)
	writeJSON(w, http.StatusOK, res)
}

// linesResponse is the wire shape for pull (pending and all).
type linesResponse struct {
	Lines []store.CrackLine `json:"lines"`
	Count int               `json:"count"`
}

func (s *Server) handlePending(w http.ResponseWriter, r *http.Request) {
	lines, err := s.store.Pending(r.Context())
	if err != nil {
		s.log.Error("pending failed", "err", err)
		writeErr(w, http.StatusInternalServerError, "pending failed")
		return
	}
	writeJSON(w, http.StatusOK, linesResponse{Lines: lines, Count: len(lines)})
}

func (s *Server) handleAll(w http.ResponseWriter, r *http.Request) {
	lines, err := s.store.All(r.Context())
	if err != nil {
		s.log.Error("all failed", "err", err)
		writeErr(w, http.StatusInternalServerError, "all failed")
		return
	}
	writeJSON(w, http.StatusOK, linesResponse{Lines: lines, Count: len(lines)})
}

type markRequest struct {
	IDs []int64 `json:"ids"`
}

func (s *Server) handleMark(w http.ResponseWriter, r *http.Request) {
	var req markRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "decode mark request: "+err.Error())
		return
	}
	marked, err := s.store.MarkSubmitted(r.Context(), req.IDs)
	if err != nil {
		s.log.Error("mark failed", "err", err)
		writeErr(w, http.StatusInternalServerError, "mark failed")
		return
	}
	s.log.Info("mark", "requested", len(req.IDs), "marked", marked)
	writeJSON(w, http.StatusOK, map[string]int64{"marked": marked})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.Stats(r.Context())
	if err != nil {
		s.log.Error("stats failed", "err", err)
		writeErr(w, http.StatusInternalServerError, "stats failed")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
