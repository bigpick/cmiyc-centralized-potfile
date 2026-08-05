// Package server exposes the pool over HTTP. It is the only component that
// talks to PostgreSQL. The CLI drives every endpoint here; none are meant to be
// hit by hand. All endpoints except /healthz require a bearer token.
//
// The server binds and serves /healthz immediately, before the database is
// connected, so a platform health check (Railway) sees a listener right away.
// The store is injected later via SetStore once the DB is reachable; until then
// data endpoints return 503.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/zilla/cmiyc-pool/internal/potfile"
	"github.com/zilla/cmiyc-pool/internal/store"
)

// maxUploadBytes caps a single potfile upload. Generous, since network hash
// dumps get large, but bounded so a bad actor cannot exhaust memory.
const maxUploadBytes = 512 << 20 // 512 MiB

// Server holds dependencies for the HTTP handlers. The store is held in an
// atomic pointer because it is wired in asynchronously after the listener is
// already accepting connections.
type Server struct {
	store atomic.Pointer[store.Store]
	token string
	log   *slog.Logger
}

// New builds a Server. token is the shared bearer secret; it must be non-empty.
// The store is not required at construction time; call SetStore once the
// database is connected.
func New(token string, log *slog.Logger) (*Server, error) {
	if token == "" {
		return nil, fmt.Errorf("POOL_TOKEN must be set")
	}
	return &Server{token: token, log: log}, nil
}

// SetStore atomically installs the database-backed store, transitioning data
// endpoints from 503 to live. Safe to call from a goroutine while serving.
func (s *Server) SetStore(st *store.Store) { s.store.Store(st) }

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

// ListenAndServe binds 0.0.0.0:addr and serves until ctx is cancelled, at which
// point it shuts down gracefully. Timeouts are tuned for large uploads.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
		// ReadTimeout is intentionally 0: a slow client streaming a very large
		// potfile should not be cut off mid-upload. maxUploadBytes bounds abuse.
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	s.log.Info("pool listening", "addr", addr)
	err := srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
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

// requireStore returns the live store, or writes a 503 and returns nil if the
// database is not connected yet.
func (s *Server) requireStore(w http.ResponseWriter) *store.Store {
	st := s.store.Load()
	if st == nil {
		writeErr(w, http.StatusServiceUnavailable, "pool is starting up; database not ready yet")
		return nil
	}
	return st
}

// --- handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	// Liveness: 200 as soon as the process is listening, independent of the DB.
	// The db field is informational so a slow database is still visible.
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"db":     s.store.Load() != nil,
	})
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	st := s.requireStore(w)
	if st == nil {
		return
	}
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

	res, err := st.Submit(r.Context(), author, lines)
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
	st := s.requireStore(w)
	if st == nil {
		return
	}
	lines, err := st.Pending(r.Context())
	if err != nil {
		s.log.Error("pending failed", "err", err)
		writeErr(w, http.StatusInternalServerError, "pending failed")
		return
	}
	writeJSON(w, http.StatusOK, linesResponse{Lines: lines, Count: len(lines)})
}

func (s *Server) handleAll(w http.ResponseWriter, r *http.Request) {
	st := s.requireStore(w)
	if st == nil {
		return
	}
	lines, err := st.All(r.Context())
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
	st := s.requireStore(w)
	if st == nil {
		return
	}
	var req markRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "decode mark request: "+err.Error())
		return
	}
	marked, err := st.MarkSubmitted(r.Context(), req.IDs)
	if err != nil {
		s.log.Error("mark failed", "err", err)
		writeErr(w, http.StatusInternalServerError, "mark failed")
		return
	}
	s.log.Info("mark", "requested", len(req.IDs), "marked", marked)
	writeJSON(w, http.StatusOK, map[string]int64{"marked": marked})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	st := s.requireStore(w)
	if st == nil {
		return
	}
	stats, err := st.Stats(r.Context())
	if err != nil {
		s.log.Error("stats failed", "err", err)
		writeErr(w, http.StatusInternalServerError, "stats failed")
		return
	}
	writeJSON(w, http.StatusOK, stats)
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
