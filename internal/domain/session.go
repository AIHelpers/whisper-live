// Package domain contains the core business entities for whisper-live.
// These types have zero dependencies on transport, storage, or inference
// frameworks — per clean architecture, everything else depends on this
// package, never the other way around.
package domain

import "time"

// SessionStatus represents the lifecycle state of a live transcription session.
type SessionStatus string

const (
	SessionActive   SessionStatus = "active"
	SessionFinished SessionStatus = "finished"
	SessionErrored  SessionStatus = "errored"
)

// Session represents one live-captioning run. A session may ingest audio
// from one or more Tracks (e.g. "mic", "system", "audioproxy:stream1") and
// accumulates TranscriptSegments over its lifetime.
type Session struct {
	ID        string
	StartedAt time.Time
	EndedAt   *time.Time
	Tracks    []string // e.g., "mic", "system", "audioproxy:stream1"
	Language  string
	Status    SessionStatus
}

// NewSession constructs a fresh, active Session for the given tracks/language.
func NewSession(id string, tracks []string, language string) *Session {
	if language == "" {
		language = "auto"
	}
	return &Session{
		ID:        id,
		StartedAt: time.Now().UTC(),
		Tracks:    tracks,
		Language:  language,
		Status:    SessionActive,
	}
}

// Finish marks the session as finished and stamps the end time.
func (s *Session) Finish() {
	now := time.Now().UTC()
	s.EndedAt = &now
	s.Status = SessionFinished
}
