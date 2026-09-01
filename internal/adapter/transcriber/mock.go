// Package transcriber provides usecase.Transcriber implementations.
package transcriber

import (
	"context"
	"fmt"
	"math"

	"whisper-live/internal/usecase"
)

// Mock is a deterministic, dependency-free Transcriber. It does not
// perform real speech recognition — it reports the audio window's RMS
// energy as a synthetic "transcript" — but it satisfies the exact same
// usecase.Transcriber port as the real whisper.cpp adapters, so the
// entire ingestion/VAD/sliding-window/reconciliation pipeline can be
// exercised end-to-end (including in this sandbox, where no model
// weights or GPU/whisper.cpp toolchain are available) without a real
// model. Swap in WhisperCppLocal or WhisperTinyDockerSidecar for real
// inference — no other code changes required, since both satisfy the
// same interface.
type Mock struct{}

// NewMock builds a Mock transcriber.
func NewMock() *Mock { return &Mock{} }

// Transcribe implements usecase.Transcriber.
func (m *Mock) Transcribe(ctx context.Context, pcm16 []int16, sampleRate int, language string) (usecase.TranscribeResult, error) {
	if len(pcm16) == 0 {
		return usecase.TranscribeResult{}, nil
	}
	var sumSq float64
	for _, s := range pcm16 {
		f := float64(s)
		sumSq += f * f
	}
	rms := math.Sqrt(sumSq / float64(len(pcm16)))
	text := fmt.Sprintf("[mock transcript: %d samples, rms=%.0f]", len(pcm16), rms)
	return usecase.TranscribeResult{Text: text, Confidence: 0.0}, nil
}
