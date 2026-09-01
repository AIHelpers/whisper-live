// Package vad provides usecase.VAD implementations.
//
// EnergyVAD is a simple RMS-energy threshold detector: no external
// dependencies, works everywhere Go does, and is good enough to skip
// obvious silence before it reaches the (expensive) Transcriber.
//
// The dev plan (section 5) also names github.com/maxhawkins/go-webrtcvad
// as an option for a more accurate WebRTC-based detector for a later
// milestone; swapping it in only requires a new type in this package that
// satisfies usecase.VAD (IsSpeech(pcm16 []int16, sampleRate int) bool) —
// intentionally not wired in here to avoid a cgo dependency in this build.
package vad

import "math"

// EnergyVAD flags a frame as speech when its RMS energy exceeds Threshold.
// Threshold is in the same units as int16 PCM samples (0..32767); a quiet
// room's noise floor is typically well under 200, normal speech well
// over 500 — 300 is a reasonable default that errs toward not missing
// quiet speech.
type EnergyVAD struct {
	Threshold float64
}

// NewEnergyVAD builds an EnergyVAD with a sensible default threshold.
func NewEnergyVAD() *EnergyVAD {
	return &EnergyVAD{Threshold: 300}
}

// IsSpeech implements usecase.VAD.
func (v *EnergyVAD) IsSpeech(pcm16 []int16, sampleRate int) bool {
	if len(pcm16) == 0 {
		return false
	}
	var sumSq float64
	for _, s := range pcm16 {
		f := float64(s)
		sumSq += f * f
	}
	rms := math.Sqrt(sumSq / float64(len(pcm16)))
	return rms >= v.Threshold
}

// AlwaysSpeech is a no-op VAD that treats every frame as speech — useful
// for tests, or for deployments that would rather over-transcribe than
// risk dropping quiet speakers.
type AlwaysSpeech struct{}

func (AlwaysSpeech) IsSpeech(pcm16 []int16, sampleRate int) bool { return true }
