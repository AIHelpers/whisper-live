package usecase

import "sync"

// Default sliding-window parameters (spec section 3: "2-4s rolling windows
// with overlap"). Window = 4s, Hop = 2s => 2s (50%) overlap between
// consecutive transcription windows, which is what OverlapReconciler
// stitches back together.
const (
	DefaultWindowMS = 4000
	DefaultHopMS    = 2000
	// VadFrameMS is the frame size VAD implementations are evaluated over.
	VadFrameMS = 20
)

// SlidingWindowBuffer accumulates PCM16 samples for one track and yields
// fixed-length, overlapping windows as enough audio arrives.
type SlidingWindowBuffer struct {
	mu sync.Mutex

	SampleRate int
	WindowMS   int
	HopMS      int

	samples []int16 // ring-ish buffer; trimmed as windows are consumed
	// baseMS is the timestamp (ms since session start) of samples[0].
	baseMS int
	// nextWindowStartMS is the timestamp of the next window to emit.
	nextWindowStartMS int
	started           bool
}

// NewSlidingWindowBuffer builds a buffer with the given sample rate and
// default window/hop sizes.
func NewSlidingWindowBuffer(sampleRate int) *SlidingWindowBuffer {
	return &SlidingWindowBuffer{
		SampleRate: sampleRate,
		WindowMS:   DefaultWindowMS,
		HopMS:      DefaultHopMS,
	}
}

func (b *SlidingWindowBuffer) msToSamples(ms int) int {
	return ms * b.SampleRate / 1000
}

func (b *SlidingWindowBuffer) samplesToMS(n int) int {
	return n * 1000 / b.SampleRate
}

// Window is one extracted, ready-to-transcribe span of audio.
type Window struct {
	PCM16      []int16
	StartMS    int
	EndMS      int
	SampleRate int
}

// Append adds newly-arrived PCM samples (tagged with the timestamp of
// their first sample) and returns every window that has become ready as a
// result, in order. Multiple windows can become ready from one Append if
// a large/late chunk arrives.
func (b *SlidingWindowBuffer) Append(pcm16 []int16, timestampMS int) []Window {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.started {
		b.baseMS = timestampMS
		b.nextWindowStartMS = timestampMS
		b.started = true
	}
	b.samples = append(b.samples, pcm16...)

	var windows []Window
	for {
		winStartSample := b.msToSamples(b.nextWindowStartMS - b.baseMS)
		winLenSamples := b.msToSamples(b.WindowMS)
		if winStartSample < 0 {
			// shouldn't happen; guard against clock skew.
			break
		}
		if winStartSample+winLenSamples > len(b.samples) {
			break // not enough audio yet for the next window
		}
		win := make([]int16, winLenSamples)
		copy(win, b.samples[winStartSample:winStartSample+winLenSamples])
		windows = append(windows, Window{
			PCM16:      win,
			StartMS:    b.nextWindowStartMS,
			EndMS:      b.nextWindowStartMS + b.WindowMS,
			SampleRate: b.SampleRate,
		})
		b.nextWindowStartMS += b.HopMS
	}

	// Trim samples we'll never need again (everything before the start of
	// the next window we'll ever emit) to keep memory bounded.
	trimSample := b.msToSamples(b.nextWindowStartMS - b.baseMS)
	if trimSample > 0 && trimSample <= len(b.samples) {
		b.samples = b.samples[trimSample:]
		b.baseMS = b.nextWindowStartMS
	}

	return windows
}

// ContainsSpeech runs a VAD frame-by-frame over the window and reports
// true if any frame looks like speech. Windows with no speech at all are
// skipped by IngestPipeline so silence never reaches the (expensive)
// Transcriber.
func ContainsSpeech(vad VAD, w Window) bool {
	if vad == nil {
		return true
	}
	frameLen := w.SampleRate * VadFrameMS / 1000
	if frameLen <= 0 || frameLen > len(w.PCM16) {
		return vad.IsSpeech(w.PCM16, w.SampleRate)
	}
	for i := 0; i+frameLen <= len(w.PCM16); i += frameLen {
		if vad.IsSpeech(w.PCM16[i:i+frameLen], w.SampleRate) {
			return true
		}
	}
	return false
}
