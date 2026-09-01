package usecase

import (
	"context"
	"log"
	"strings"
	"sync"

	"whisper-live/internal/domain"
)

// SessionRunner owns one live session: it wires together per-track sliding
// windows, VAD gating, transcription, and overlap reconciliation, and
// forwards resulting segments to a SegmentSink (typically the WebSocket
// handler). This is the orchestration root for usecase/ingest_audio_chunk.go
// and usecase/finalize_segment.go.
type SessionRunner struct {
	Session *domain.Session

	transcriber Transcriber
	vad         VAD
	diarizer    Diarizer // optional, may be nil
	sink        SegmentSink
	alertRules  []domain.AlertRule
	notifier    AlertNotifier // optional, may be nil

	mu       sync.Mutex
	buffers  map[string]*SlidingWindowBuffer
	recon    *OverlapReconciler
	finalSeg []domain.TranscriptSegment // accumulated Final segments, in emission order
}

// SessionDeps bundles the adapters a SessionRunner needs. Only Transcriber
// is required; everything else is optional (nil-safe) so the MVP can run
// without diarization/alerting configured.
type SessionDeps struct {
	Transcriber Transcriber
	VAD         VAD
	Diarizer    Diarizer
	Sink        SegmentSink
	Notifier    AlertNotifier
	AlertRules  []domain.AlertRule
}

// StartSession creates a new domain.Session and its SessionRunner.
func StartSession(id string, tracks []string, language string, deps SessionDeps) *SessionRunner {
	if deps.Transcriber == nil {
		panic("usecase: StartSession requires a non-nil Transcriber")
	}
	return &SessionRunner{
		Session:     domain.NewSession(id, tracks, language),
		transcriber: deps.Transcriber,
		vad:         deps.VAD,
		diarizer:    deps.Diarizer,
		sink:        deps.Sink,
		notifier:    deps.Notifier,
		alertRules:  deps.AlertRules,
		buffers:     map[string]*SlidingWindowBuffer{},
		recon:       NewOverlapReconciler(),
	}
}

// IngestAudioChunk feeds one chunk of PCM audio into the session's
// sliding-window buffer for its track, transcribing and reconciling any
// windows that become ready as a result. Safe for concurrent calls across
// different tracks; calls for the *same* track should come from a single
// goroutine (typical for one WebSocket connection reading in a loop).
func (r *SessionRunner) IngestAudioChunk(ctx context.Context, chunk AudioChunk) error {
	buf := r.bufferFor(chunk.Track, chunk.SampleRate)
	windows := buf.Append(chunk.PCM16, chunk.TimestampMS)

	for _, w := range windows {
		if !ContainsSpeech(r.vad, w) {
			continue // pure silence: skip the (expensive) transcription call
		}

		result, err := r.transcriber.Transcribe(ctx, w.PCM16, w.SampleRate, r.Session.Language)
		if err != nil {
			log.Printf("whisper-live: transcribe error track=%s: %v", chunk.Track, err)
			continue
		}
		text := strings.TrimSpace(result.Text)
		if text == "" {
			continue
		}

		segs := r.recon.Reconcile(r.Session.ID, chunk.Track, text, w.StartMS, w.EndMS)
		for _, seg := range segs {
			if r.diarizer != nil {
				if spk, err := r.diarizer.Assign(ctx, w.PCM16, w.SampleRate); err == nil && spk != "" {
					seg.Speaker = &spk
				}
			}
			r.deliver(seg)
		}
	}
	return nil
}

func (r *SessionRunner) bufferFor(track string, sampleRate int) *SlidingWindowBuffer {
	r.mu.Lock()
	defer r.mu.Unlock()
	buf, ok := r.buffers[track]
	if !ok {
		buf = NewSlidingWindowBuffer(sampleRate)
		r.buffers[track] = buf
	}
	return buf
}

func (r *SessionRunner) deliver(seg domain.TranscriptSegment) {
	if seg.Final {
		r.mu.Lock()
		r.finalSeg = append(r.finalSeg, seg)
		r.mu.Unlock()
		r.checkAlerts(seg)
	}
	if r.sink != nil {
		r.sink.Emit(seg)
	}
}

func (r *SessionRunner) checkAlerts(seg domain.TranscriptSegment) {
	if r.notifier == nil || len(r.alertRules) == 0 {
		return
	}
	lower := strings.ToLower(seg.Text)
	for _, rule := range r.alertRules {
		if rule.Keyword == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(rule.Keyword)) {
			r.notifier.Notify(rule, seg)
		}
	}
}

// FinalSegments returns a snapshot of every Final segment produced so far,
// in emission order.
func (r *SessionRunner) FinalSegments() []domain.TranscriptSegment {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.TranscriptSegment, len(r.finalSeg))
	copy(out, r.finalSeg)
	return out
}

// Finish flushes any trailing provisional transcript (no further
// overlapping window will arrive to corroborate it) and marks the
// underlying session finished.
func (r *SessionRunner) Finish() {
	r.mu.Lock()
	recon := r.recon
	r.mu.Unlock()

	for _, track := range recon.Tracks() {
		if seg := recon.Flush(r.Session.ID, track); seg != nil {
			r.deliver(*seg)
		}
	}
	r.Session.Finish()
}
