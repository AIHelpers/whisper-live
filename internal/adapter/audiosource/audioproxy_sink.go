package audiosource

import (
	"context"
	"sync"

	"whisper-live/internal/usecase"
)

// AudioProxySink implements the "sink" contract AudioProxy expects from a
// routable destination (spec sections 1/3/6: "register as a named sink
// that AudioProxy can route any managed stream into"). AudioProxy's own
// interface definition lives in its own repo; this adapter deliberately
// depends on nothing from it beyond the shape of a push callback, so
// whisper-live doesn't need AudioProxy as a build dependency — AudioProxy
// only needs to know how to call PushPCM with whatever stream it's
// routing.
//
// Internally this is just a buffered fan-in: PushPCM (called by
// AudioProxy) writes into a channel that Chunks (called by our own
// ingestion pipeline) reads from, so AudioProxySink also satisfies
// usecase.AudioSource and can be handed straight to SessionRunner.
type AudioProxySink struct {
	trackName  string
	sampleRate int

	mu     sync.Mutex
	ch     chan usecase.AudioChunk
	closed bool
}

// NewAudioProxySink registers a new sink for the given AudioProxy stream
// name (used as the resulting track name, e.g. "audioproxy:stream1").
func NewAudioProxySink(streamName string, sampleRate int) *AudioProxySink {
	return &AudioProxySink{
		trackName:  "audioproxy:" + streamName,
		sampleRate: sampleRate,
		ch:         make(chan usecase.AudioChunk, 64),
	}
}

// Track implements usecase.AudioSource.
func (s *AudioProxySink) Track() string { return s.trackName }

// Chunks implements usecase.AudioSource.
func (s *AudioProxySink) Chunks(ctx context.Context) (<-chan usecase.AudioChunk, error) {
	go func() {
		<-ctx.Done()
		s.Close()
	}()
	return s.ch, nil
}

// PushPCM is the callback AudioProxy invokes with each routed audio
// buffer. timestampMS is milliseconds since session start, matching
// usecase.AudioChunk's convention.
func (s *AudioProxySink) PushPCM(pcm16 []int16, timestampMS int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	chunk := usecase.AudioChunk{
		Track:       s.trackName,
		SampleRate:  s.sampleRate,
		PCM16:       pcm16,
		TimestampMS: timestampMS,
	}
	select {
	case s.ch <- chunk:
	default:
		// Backpressure: drop the oldest buffered chunk rather than block
		// AudioProxy's routing goroutine indefinitely.
		select {
		case <-s.ch:
		default:
		}
		select {
		case s.ch <- chunk:
		default:
		}
	}
}

// Close stops accepting further pushes and closes the chunk channel.
func (s *AudioProxySink) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}
