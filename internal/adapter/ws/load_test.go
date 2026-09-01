package ws

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"whisper-live/internal/domain"
	"whisper-live/internal/usecase"
)

// noopTranscriber satisfies usecase.Transcriber without doing any real
// work — enough for a transport-layer load test that's only checking
// concurrency safety and resource cleanup, not transcription quality.
type noopTranscriber struct{}

func (noopTranscriber) Transcribe(ctx context.Context, pcm16 []int16, sampleRate int, language string) (usecase.TranscribeResult, error) {
	if len(pcm16) == 0 {
		return usecase.TranscribeResult{}, nil
	}
	return usecase.TranscribeResult{Text: "chunk"}, nil
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	factory := func(sessionID string, tracks []string, language string, sink usecase.SegmentSink) *usecase.SessionRunner {
		return usecase.StartSession(sessionID, tracks, language, usecase.SessionDeps{
			Transcriber: noopTranscriber{},
			Sink:        sink,
		})
	}
	h := NewHandler(factory)
	var mu sync.Mutex
	ended := 0
	h.OnSessionEnd = func(sessionID string, finalSegments []domain.TranscriptSegment) {
		mu.Lock()
		ended++
		mu.Unlock()
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeSessionStream(w, r)
	}))
	return srv
}

// TestLoad_ConcurrentSessions_NoGoroutineLeak opens many concurrent
// WebSocket sessions, streams a few seconds of audio through each, closes
// them all, and checks that the server's goroutine count returns to
// baseline afterward — the sliding-window buffering and per-connection
// state must not leak (spec section 12).
func TestLoad_ConcurrentSessions_NoGoroutineLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in -short mode")
	}

	srv := newTestServer(t)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/sessions/%s/stream"

	baseline := runtime.NumGoroutine()

	const numSessions = 40
	var wg sync.WaitGroup
	errs := make(chan error, numSessions)

	for i := 0; i < numSessions; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := runOneSession(fmt.Sprintf(wsURL, fmt.Sprintf("load-%d", i))); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("session error: %v", err)
	}

	// Allow goroutines spawned for connection teardown (read/write pumps,
	// context-cancellation watchers) a moment to actually exit.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseline+5 { // small slack for test harness goroutines
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	after := runtime.NumGoroutine()
	if after > baseline+5 {
		t.Errorf("possible goroutine leak: baseline=%d after=%d (numSessions=%d)", baseline, after, numSessions)
	}
}

func runOneSession(url string) error {
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(ClientMessage{Type: "start", Track: "mic", SampleRate: 16000}); err != nil {
		return fmt.Errorf("write start: %w", err)
	}

	// Stream ~1s of audio in 100ms binary frames.
	frame := make([]byte, 16000/10*2) // 100ms of 16kHz mono 16-bit silence
	for i := 0; i < 10; i++ {
		if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			return fmt.Errorf("write audio frame %d: %w", i, err)
		}
	}

	if err := conn.WriteJSON(ClientMessage{Type: "stop"}); err != nil {
		return fmt.Errorf("write stop: %w", err)
	}

	// Drain a couple of events (started/segments/stopped) with a bound so
	// a misbehaving server can't hang the test forever.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var ev ServerEvent
		if err := conn.ReadJSON(&ev); err != nil {
			break // socket closed or deadline hit — either ends the drain
		}
		if ev.Type == "stopped" {
			break
		}
	}
	return nil
}

var _ = binary.LittleEndian // reserved: frame encoding helper parity with production client
