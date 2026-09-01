// Package usecase implements the application's use cases (interactors).
// It depends only on internal/domain and the interfaces ("ports") declared
// in this file. Concrete implementations live under internal/adapter/* and
// are wired in at cmd/*/main.go — the usecase layer never imports an
// adapter package directly.
package usecase

import (
	"context"

	"whisper-live/internal/domain"
)

// AudioChunk is a slice of raw PCM16LE mono audio samples at a fixed
// SampleRate, tagged with the track it came from.
type AudioChunk struct {
	Track      string
	SampleRate int
	PCM16      []int16
	// TimestampMS is the offset, in milliseconds from session start, of the
	// first sample in PCM16.
	TimestampMS int
}

// TranscribeResult is what a Transcriber produces for one audio window.
type TranscribeResult struct {
	Text       string
	Confidence float32
}

// Transcriber runs speech-to-text inference over a fixed audio window.
// Implementations: adapter/transcriber/whispercpp_local.go (in-process
// whisper.cpp bindings) and adapter/transcriber/whispertiny_docker_sidecar.go
// (subprocess/HTTP call into the existing docker-whisper-tiny image).
type Transcriber interface {
	Transcribe(ctx context.Context, pcm16 []int16, sampleRate int, language string) (TranscribeResult, error)
}

// VAD (voice activity detection) decides which parts of a PCM buffer
// contain speech, so silence is never sent to the (expensive) Transcriber.
type VAD interface {
	// IsSpeech reports whether the given frame likely contains speech.
	// frameMS is the duration represented by the frame (e.g. 20/30ms).
	IsSpeech(pcm16 []int16, sampleRate int) bool
}

// Diarizer assigns lightweight speaker labels to segments by clustering a
// per-segment voice embedding against known speaker centroids.
type Diarizer interface {
	Assign(ctx context.Context, pcm16 []int16, sampleRate int) (speakerID string, err error)
}

// Translator turns a finalized transcript segment's text into another
// language. Implementations may call a local model or a remote API.
type Translator interface {
	Translate(ctx context.Context, text, sourceLang, targetLang string) (string, error)
}

// Summarizer produces an end-of-session summary/action-items list from the
// full finalized transcript.
type Summarizer interface {
	Summarize(ctx context.Context, fullTranscript string) (string, error)
}

// AudioSource is anything that can feed AudioChunks into a session:
// a local mic/system capture device, or an AudioProxy sink connection.
type AudioSource interface {
	// Chunks returns a channel that is closed when the source is exhausted
	// or ctx is cancelled.
	Chunks(ctx context.Context) (<-chan AudioChunk, error)
	Track() string
}

// SegmentSink receives TranscriptSegment events as they are produced —
// typically the WebSocket handler, which forwards them to the client.
type SegmentSink interface {
	Emit(seg domain.TranscriptSegment)
}

// AlertNotifier fires when an AlertRule's keyword is spotted in a
// finalized segment.
type AlertNotifier interface {
	Notify(rule domain.AlertRule, seg domain.TranscriptSegment)
}
