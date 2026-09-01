package usecase

import (
	"context"
	"math"
	"strings"
	"testing"

	"whisper-live/internal/domain"
)

// scriptedTranscriber is a deterministic fake usecase.Transcriber: rather
// than running real inference, it returns a canned sentence-fragment
// based on which "chapter" of a synthetic timeline the window falls in.
// This lets us assert on the exact reconciled output, the way a real
// integration test would assert word-error-rate against a reference
// transcript for a fixed WAV fixture (spec section 12).
type scriptedTranscriber struct {
	// script maps a window's start time (rounded down to the nearest
	// HopMS) to the words spoken during that window.
	fullSentence []string
	wordsPerSec  float64
}

func newScriptedTranscriber(sentence string, wordsPerSec float64) *scriptedTranscriber {
	return &scriptedTranscriber{
		fullSentence: strings.Fields(sentence),
		wordsPerSec:  wordsPerSec,
	}
}

// Transcribe ignores the actual PCM content (this is a fixture, not a
// model) and instead derives which words "would have been spoken" during
// this window purely from its position, encoded into the PCM via a
// per-window marker amplitude set up by the test — see synthesizeWindowMarkerAudio.
func (s *scriptedTranscriber) Transcribe(ctx context.Context, pcm16 []int16, sampleRate int, language string) (TranscribeResult, error) {
	if len(pcm16) == 0 {
		return TranscribeResult{}, nil
	}
	// Decode the marker: the window's start time in ms was encoded as the
	// integer value of the first sample by the test's synthetic source.
	startMS := int(pcm16[0])
	winMS := 4000
	startSec := float64(startMS) / 1000.0
	endSec := float64(startMS+winMS) / 1000.0
	startWord := int(math.Floor(startSec * s.wordsPerSec))
	endWord := int(math.Ceil(endSec * s.wordsPerSec))
	if startWord < 0 {
		startWord = 0
	}
	if endWord > len(s.fullSentence) {
		endWord = len(s.fullSentence)
	}
	if startWord >= endWord {
		return TranscribeResult{}, nil
	}
	return TranscribeResult{Text: strings.Join(s.fullSentence[startWord:endWord], " ")}, nil
}

func TestIntegration_FullStreamingPipeline_ProducesCoherentTranscript(t *testing.T) {
	const sentence = "the quick brown fox jumps over the lazy dog while the sun sets slowly behind the distant mountains"
	const wordsPerSec = 2.0 // ~how fast the "speaker" talks
	const sampleRate = 16000

	tr := newScriptedTranscriber(sentence, wordsPerSec)
	sink := &collectingSink{}

	runner := StartSession("integration-1", []string{"mic"}, "en", SessionDeps{
		Transcriber: tr,
		VAD:         nil, // no VAD gating: every window is "speech" for this fixture
		Sink:        sink,
	})

	// Feed 20 seconds of audio in 500ms chunks, exactly like a real
	// WebSocket client streaming PCM frames would. Each sample's *value*
	// is meaningless for real inference but is used here purely as an
	// out-of-band marker so scriptedTranscriber can determine window
	// position deterministically (a stand-in for "the WAV fixture's
	// actual audio content").
	const chunkMS = 500
	const totalMS = 20000
	for t0 := 0; t0 < totalMS; t0 += chunkMS {
		n := sampleRate * chunkMS / 1000
		chunk := make([]int16, n)
		for i := range chunk {
			chunk[i] = int16(t0) // marker: chunk start time
		}
		if err := runner.IngestAudioChunk(context.Background(), AudioChunk{
			Track:       "mic",
			SampleRate:  sampleRate,
			PCM16:       chunk,
			TimestampMS: t0,
		}); err != nil {
			t.Fatalf("IngestAudioChunk: %v", err)
		}
	}
	runner.Finish()

	final := runner.FinalSegments()
	if len(final) == 0 {
		t.Fatal("expected at least one Final segment from a 20s session")
	}

	var got strings.Builder
	for _, s := range final {
		if got.Len() > 0 {
			got.WriteString(" ")
		}
		got.WriteString(s.Text)
	}

	// The reconciled transcript should be a prefix-consistent, gap-free
	// reconstruction of the scripted sentence (allowing that the session
	// may end mid-sentence if 20s wasn't enough to cover all the words,
	// but never inserting duplicate/out-of-order fragments).
	gotWords := strings.Fields(got.String())
	wantWords := strings.Fields(sentence)
	if len(gotWords) == 0 {
		t.Fatal("reconciled transcript is empty")
	}
	if len(gotWords) > len(wantWords) {
		t.Fatalf("reconciled transcript longer than source script: got %d words, source has %d", len(gotWords), len(wantWords))
	}
	for i, w := range gotWords {
		if w != wantWords[i] {
			t.Fatalf("word %d mismatch: got %q, want %q\nfull got: %q", i, w, wantWords[i], got.String())
		}
	}

	// Sink should have observed the same final segments SessionRunner
	// tracked internally, plus (typically) at least one interim segment
	// along the way.
	if sink.finalCount == 0 {
		t.Error("sink never received a Final segment")
	}
	if sink.interimCount == 0 {
		t.Error("sink never received an interim segment")
	}
}

// collectingSink is a minimal usecase.SegmentSink for tests: it counts
// interim vs. final segments as they're emitted, in real time, just as
// the WebSocket handler's sink would.
type collectingSink struct {
	finalCount   int
	interimCount int
}

func (c *collectingSink) Emit(seg domain.TranscriptSegment) {
	if seg.Final {
		c.finalCount++
	} else {
		c.interimCount++
	}
}
