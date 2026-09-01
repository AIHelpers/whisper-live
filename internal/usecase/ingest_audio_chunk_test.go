package usecase

import "testing"

func TestSlidingWindowBuffer_EmitsWindowsAtHopInterval(t *testing.T) {
	sampleRate := 16000
	b := NewSlidingWindowBuffer(sampleRate)
	b.WindowMS = 4000
	b.HopMS = 2000

	// Feed exactly one window's worth of samples in one shot.
	oneWindow := make([]int16, sampleRate*4000/1000)
	windows := b.Append(oneWindow, 0)
	if len(windows) != 1 {
		t.Fatalf("expected 1 window after appending exactly WindowMS of audio, got %d", len(windows))
	}
	if windows[0].StartMS != 0 || windows[0].EndMS != 4000 {
		t.Errorf("unexpected window bounds: %+v", windows[0])
	}

	// Not enough new audio yet for the next window (need another HopMS).
	tooLittle := make([]int16, sampleRate*1000/1000) // 1s
	windows = b.Append(tooLittle, 4000)
	if len(windows) != 0 {
		t.Fatalf("expected 0 windows with insufficient new audio, got %d", len(windows))
	}

	// The remaining 1s to complete the next hop.
	enough := make([]int16, sampleRate*1000/1000)
	windows = b.Append(enough, 5000)
	if len(windows) != 1 {
		t.Fatalf("expected 1 window once hop is satisfied, got %d", len(windows))
	}
	if windows[0].StartMS != 2000 || windows[0].EndMS != 6000 {
		t.Errorf("unexpected second window bounds: %+v", windows[0])
	}
}

func TestSlidingWindowBuffer_MultipleWindowsFromOneLargeAppend(t *testing.T) {
	sampleRate := 16000
	b := NewSlidingWindowBuffer(sampleRate)
	b.WindowMS = 4000
	b.HopMS = 2000

	// 10s of audio in one shot should yield windows at [0-4],[2-6],[4-8]
	// (the [6-10] window needs a hop start at 6000 which requires samples
	// up to 10000ms — exactly available — so 4 windows total: 0,2,4,6).
	tenSeconds := make([]int16, sampleRate*10000/1000)
	windows := b.Append(tenSeconds, 0)

	wantStarts := []int{0, 2000, 4000, 6000}
	if len(windows) != len(wantStarts) {
		t.Fatalf("expected %d windows, got %d", len(wantStarts), len(windows))
	}
	for i, w := range windows {
		if w.StartMS != wantStarts[i] {
			t.Errorf("window %d: got StartMS=%d, want %d", i, w.StartMS, wantStarts[i])
		}
		if w.EndMS != w.StartMS+4000 {
			t.Errorf("window %d: EndMS %d is not StartMS+WindowMS", i, w.EndMS)
		}
		if len(w.PCM16) != sampleRate*4000/1000 {
			t.Errorf("window %d: wrong sample count %d", i, len(w.PCM16))
		}
	}
}

func TestSlidingWindowBuffer_TrimsConsumedSamples(t *testing.T) {
	sampleRate := 16000
	b := NewSlidingWindowBuffer(sampleRate)
	b.WindowMS = 4000
	b.HopMS = 2000

	tenSeconds := make([]int16, sampleRate*10000/1000)
	b.Append(tenSeconds, 0)

	// Internal buffer should be trimmed down to just what's needed for the
	// next not-yet-emitted window, not the full 10s — bounding memory use
	// for a long-running session.
	if len(b.samples) > sampleRate*4000/1000+sampleRate { // generous slack
		t.Errorf("buffer not trimmed: %d samples retained", len(b.samples))
	}
}
