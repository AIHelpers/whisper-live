// Package httpapi implements the plain-HTTP parts of the API surface
// (spec section 8): session bookkeeping and transcript export/summarize.
// The live streaming itself is handled by internal/adapter/ws.
package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"whisper-live/internal/domain"
	"whisper-live/internal/usecase"
)

// Store is an in-memory record of finished sessions' final transcripts,
// keyed by session ID, populated by ws.Handler.OnSessionEnd. A real
// deployment would swap this for a persistent store without touching the
// handlers below (they only depend on the Store interface).
type Store interface {
	Save(sessionID string, segments []domain.TranscriptSegment)
	Get(sessionID string) ([]domain.TranscriptSegment, bool)
}

// MemStore is the default in-process Store implementation.
type MemStore struct {
	mu   sync.RWMutex
	data map[string][]domain.TranscriptSegment
}

func NewMemStore() *MemStore { return &MemStore{data: map[string][]domain.TranscriptSegment{}} }

func (s *MemStore) Save(sessionID string, segments []domain.TranscriptSegment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[sessionID] = segments
}

func (s *MemStore) Get(sessionID string) ([]domain.TranscriptSegment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	segs, ok := s.data[sessionID]
	return segs, ok
}

// API wires the plain-HTTP endpoints.
type API struct {
	Store      Store
	Summarizer usecase.Summarizer // optional; nil disables /summarize
}

// NewAPI builds an API using the given store (and optional summarizer).
func NewAPI(store Store, summarizer usecase.Summarizer) *API {
	return &API{Store: store, Summarizer: summarizer}
}

type createSessionResponse struct {
	ID string `json:"id"`
}

// HandleCreateSession implements POST /api/sessions — allocates a session
// ID for the client to then open /ws/sessions/{id}/stream against.
func (a *API) HandleCreateSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := newSessionID()
	if err != nil {
		http.Error(w, "failed to allocate session id", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, createSessionResponse{ID: id})
}

// HandleTranscript implements GET /api/sessions/{id}/transcript.{srt,vtt,txt}.
func (a *API) HandleTranscript(w http.ResponseWriter, r *http.Request) {
	id, format, ok := parseTranscriptPath(r.URL.Path)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	segs, found := a.Store.Get(id)
	if !found {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	var ef usecase.ExportFormat
	var contentType string
	switch format {
	case "srt":
		ef, contentType = usecase.FormatSRT, "application/x-subrip"
	case "vtt":
		ef, contentType = usecase.FormatVTT, "text/vtt"
	default:
		ef, contentType = usecase.FormatPlain, "text/plain"
	}

	out, err := usecase.ExportTranscript(segs, ef)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType+"; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(out))
}

type summarizeResponse struct {
	Summary string `json:"summary"`
}

// HandleSummarize implements POST /api/sessions/{id}/summarize.
func (a *API) HandleSummarize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, ok := parseSummarizePath(r.URL.Path)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if a.Summarizer == nil {
		http.Error(w, "summarization is not configured on this server", http.StatusNotImplemented)
		return
	}
	segs, found := a.Store.Get(id)
	if !found {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	summary, err := usecase.SummarizeSession(r.Context(), a.Summarizer, segs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, summarizeResponse{Summary: summary})
}

func parseTranscriptPath(path string) (id, format string, ok bool) {
	// /api/sessions/{id}/transcript.{ext}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "sessions" {
		return "", "", false
	}
	last := parts[3]
	if !strings.HasPrefix(last, "transcript.") {
		return "", "", false
	}
	format = strings.TrimPrefix(last, "transcript.")
	return parts[2], format, true
}

func parseSummarizePath(path string) (id string, ok bool) {
	// /api/sessions/{id}/summarize
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "sessions" || parts[3] != "summarize" {
		return "", false
	}
	return parts[2], true
}

func newSessionID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
