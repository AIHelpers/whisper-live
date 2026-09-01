// Package ws implements the WebSocket transport: clients stream raw
// PCM16LE mono audio frames in, and receive JSON-encoded TranscriptSegment
// events back, per spec section 8 (`WS /ws/sessions/:id/stream`).
package ws

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"whisper-live/internal/domain"
	"whisper-live/internal/usecase"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Live-captioning clients (the bundled web frontend, or any other
	// origin the operator points at this server) all need to connect;
	// tighten this with an allowlist before exposing the server publicly.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ClientMessage is the JSON control message a client can send at any time
// over the socket, interleaved with binary PCM frames. Only "start" is
// required before audio; PCM binary frames sent before "start" are
// ignored.
type ClientMessage struct {
	Type       string `json:"type"` // "start" | "stop"
	Track      string `json:"track,omitempty"`
	SampleRate int    `json:"sampleRate,omitempty"`
	Language   string `json:"language,omitempty"`
}

// ServerEvent is the JSON message sent back to the client for every
// TranscriptSegment produced, plus lifecycle events.
type ServerEvent struct {
	Type      string  `json:"type"` // "segment" | "started" | "stopped" | "error"
	SessionID string  `json:"sessionId,omitempty"`
	Track     string  `json:"track,omitempty"`
	Speaker   *string `json:"speaker,omitempty"`
	Text      string  `json:"text,omitempty"`
	StartMS   int     `json:"startMs,omitempty"`
	EndMS     int     `json:"endMs,omitempty"`
	Final     bool    `json:"final,omitempty"`
	Message   string  `json:"message,omitempty"`
}

// SessionFactory builds a usecase.SessionRunner for a newly-connected
// client. Wired in from cmd/server/main.go with the concrete
// Transcriber/VAD/etc adapters.
type SessionFactory func(sessionID string, tracks []string, language string, sink usecase.SegmentSink) *usecase.SessionRunner

// Handler serves the live-streaming WebSocket endpoint.
type Handler struct {
	NewSession SessionFactory
	// OnSessionEnd, if set, is called with the final segments once a
	// client's session ends (socket closes or "stop" received) — e.g. to
	// persist the transcript for later export.
	OnSessionEnd func(sessionID string, finalSegments []domain.TranscriptSegment)
}

// NewHandler builds a Handler.
func NewHandler(factory SessionFactory) *Handler {
	return &Handler{NewSession: factory}
}

// wsSink adapts a single client connection into a usecase.SegmentSink,
// serializing writes since gorilla/websocket connections aren't safe for
// concurrent writers.
type wsSink struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (s *wsSink) Emit(seg domain.TranscriptSegment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ev := ServerEvent{
		Type:      "segment",
		SessionID: seg.SessionID,
		Track:     seg.Track,
		Speaker:   seg.Speaker,
		Text:      seg.Text,
		StartMS:   seg.StartMS,
		EndMS:     seg.EndMS,
		Final:     seg.Final,
	}
	_ = s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := s.conn.WriteJSON(ev); err != nil {
		log.Printf("whisper-live: ws write error: %v", err)
	}
}

func (s *wsSink) writeEvent(ev ServerEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := s.conn.WriteJSON(ev); err != nil {
		log.Printf("whisper-live: ws write error: %v", err)
	}
}

// ServeSessionStream handles GET /ws/sessions/{id}/stream. It upgrades the
// connection, waits for a "start" control message to learn the track/
// sample rate/language, then alternates between JSON control messages and
// binary PCM16LE frames for the life of the connection.
func (h *Handler) ServeSessionStream(w http.ResponseWriter, r *http.Request) {
	sessionID := sessionIDFromPath(r.URL.Path)
	if sessionID == "" {
		http.Error(w, "missing session id in path", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("whisper-live: ws upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	sink := &wsSink{conn: conn}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	var runner *usecase.SessionRunner
	var track string
	var sampleRate int
	finished := false

	finishSession := func() {
		if runner == nil || finished {
			return
		}
		finished = true
		runner.Finish()
		if h.OnSessionEnd != nil {
			h.OnSessionEnd(sessionID, runner.FinalSegments())
		}
	}

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			break // client disconnected
		}

		switch msgType {
		case websocket.TextMessage:
			var cm ClientMessage
			if err := json.Unmarshal(data, &cm); err != nil {
				sink.writeEvent(ServerEvent{Type: "error", Message: "invalid control message: " + err.Error()})
				continue
			}
			switch cm.Type {
			case "start":
				track = cm.Track
				if track == "" {
					track = "mic"
				}
				sampleRate = cm.SampleRate
				if sampleRate == 0 {
					sampleRate = 16000
				}
				runner = h.NewSession(sessionID, []string{track}, cm.Language, sink)
				sink.writeEvent(ServerEvent{Type: "started", SessionID: sessionID, Track: track})
			case "stop":
				finishSession()
				sink.writeEvent(ServerEvent{Type: "stopped", SessionID: sessionID})
			}

		case websocket.BinaryMessage:
			if runner == nil {
				sink.writeEvent(ServerEvent{Type: "error", Message: "received audio before \"start\""})
				continue
			}
			pcm16 := bytesToPCM16LE(data)
			chunk := usecase.AudioChunk{
				Track:      track,
				SampleRate: sampleRate,
				PCM16:      pcm16,
				// The client is expected to send chunks in real time, so
				// wall-clock-since-session-start is a reasonable proxy for
				// each chunk's position; a production client would instead
				// stamp each frame with its true capture offset.
				TimestampMS: int(time.Since(runner.Session.StartedAt).Milliseconds()),
			}
			if err := runner.IngestAudioChunk(ctx, chunk); err != nil {
				log.Printf("whisper-live: ingest error: %v", err)
			}
		}
	}

	if runner != nil {
		finishSession()
	}
}

func sessionIDFromPath(path string) string {
	// Expects .../sessions/{id}/stream
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, p := range parts {
		if p == "sessions" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// bytesToPCM16LE reinterprets a little-endian byte slice as int16 samples.
func bytesToPCM16LE(b []byte) []int16 {
	n := len(b) / 2
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		out[i] = int16(uint16(b[2*i]) | uint16(b[2*i+1])<<8)
	}
	return out
}
