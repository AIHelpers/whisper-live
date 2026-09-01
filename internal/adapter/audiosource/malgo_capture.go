// Package audiosource provides usecase.AudioSource implementations.
package audiosource

import (
	"context"
	"fmt"

	"whisper-live/internal/usecase"
)

// MalgoCapture captures live mic/system audio via
// github.com/gen2brain/malgo (cross-platform miniaudio bindings), per
// spec section 5. It is declared here with the exact usecase.AudioSource
// shape the pipeline expects; the malgo dependency itself is not vendored
// into this build because it requires cgo + a platform audio backend
// (CoreAudio/WASAPI/ALSA) that isn't available in this sandbox — wiring
// it up on a real desktop build is a matter of filling in start() with
// malgo.InitContext / malgo.NewDevice, pushing captured frames into the
// same chunks channel this type already exposes.
type MalgoCapture struct {
	TrackName  string
	SampleRate int
	// ChunkMS is how much audio is batched into each AudioChunk pushed to
	// the pipeline.
	ChunkMS int
}

// NewMalgoCapture builds a capture source for the given track/sample rate.
func NewMalgoCapture(trackName string, sampleRate int) *MalgoCapture {
	return &MalgoCapture{
		TrackName:  trackName,
		SampleRate: sampleRate,
		ChunkMS:    100,
	}
}

// Track implements usecase.AudioSource.
func (m *MalgoCapture) Track() string { return m.TrackName }

// Chunks implements usecase.AudioSource. In this build it returns a
// channel that is immediately closed with an explanatory error, since
// real device capture needs the native malgo/cgo toolchain this sandbox
// doesn't have. On a real desktop build (`wails build`), replace the body
// with a malgo device callback that appends captured int16 frames and
// sends usecase.AudioChunk on the returned channel every ChunkMS.
func (m *MalgoCapture) Chunks(ctx context.Context) (<-chan usecase.AudioChunk, error) {
	return nil, fmt.Errorf("audiosource: MalgoCapture requires the native malgo/cgo toolchain, unavailable in this build — use the WebSocket PCM ingest path (browser mic capture) or wire malgo in a full desktop build")
}
