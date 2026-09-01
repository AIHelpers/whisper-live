package usecase

import (
	"strings"
	"unicode"

	"whisper-live/internal/domain"
)

// trackReconcileState holds the running, per-track reconciliation state.
//
// Stitching model ("confirm on next overlap", a common streaming-ASR
// technique): each window's transcript is de-duplicated against
// everything emitted so far (stable + pending) using a word-level
// suffix/prefix alignment. Any genuinely new trailing words become the
// new *pending* (interim) segment. The *previous* round's pending words,
// having now survived into an overlapping later window without being
// contradicted, are promoted to *stable* (Final) at the start of this
// round. The very last pending words of a track are only finalized when
// the caller explicitly calls Flush (session end).
type trackReconcileState struct {
	stableWords    []string
	lastFinalEndMS int
	haveFinal      bool

	pendingWords   []string
	pendingStartMS int
	pendingEndMS   int
}

// OverlapReconciler stitches a sequence of overlapping sliding-window
// transcriptions into one stable, growing transcript per track. See
// trackReconcileState for the stitching model.
type OverlapReconciler struct {
	tracks map[string]*trackReconcileState
}

// NewOverlapReconciler builds an empty reconciler.
func NewOverlapReconciler() *OverlapReconciler {
	return &OverlapReconciler{tracks: map[string]*trackReconcileState{}}
}

func (r *OverlapReconciler) stateFor(track string) *trackReconcileState {
	st, ok := r.tracks[track]
	if !ok {
		st = &trackReconcileState{}
		r.tracks[track] = st
	}
	return st
}

// Reconcile ingests one interim window transcript for a track and returns
// zero, one, or two TranscriptSegments to emit, in order:
//  1. (optional) a Final segment finalizing the *previous* round's
//     provisional tail, now corroborated by this overlapping window.
//  2. (optional) a new interim segment for the words in this window that
//     have never been seen before.
func (r *OverlapReconciler) Reconcile(sessionID, track, text string, winStartMS, winEndMS int) []domain.TranscriptSegment {
	st := r.stateFor(track)

	newWords := tokenize(text)
	if len(newWords) == 0 {
		return nil
	}

	combined := make([]string, 0, len(st.stableWords)+len(st.pendingWords))
	combined = append(combined, st.stableWords...)
	combined = append(combined, st.pendingWords...)

	k := longestSuffixPrefixMatch(combined, newWords)
	tail := newWords[k:]

	var out []domain.TranscriptSegment

	if len(st.pendingWords) > 0 {
		startMS := st.pendingStartMS
		if st.haveFinal {
			startMS = st.lastFinalEndMS
		}
		endMS := st.pendingEndMS
		out = append(out, domain.TranscriptSegment{
			SessionID: sessionID,
			Track:     track,
			Text:      strings.Join(st.pendingWords, " "),
			StartMS:   startMS,
			EndMS:     endMS,
			Final:     true,
		})
		st.stableWords = append(st.stableWords, st.pendingWords...)
		st.lastFinalEndMS = endMS
		st.haveFinal = true
		st.pendingWords = nil
	}

	if len(tail) > 0 {
		st.pendingWords = tail
		st.pendingStartMS = winStartMS
		st.pendingEndMS = winEndMS
		out = append(out, interimSegment(sessionID, track, tail, winStartMS, winEndMS))
	}

	return out
}

// Flush finalizes any remaining provisional tail for a track (there will
// be no further overlapping window to corroborate it — typically called
// when a session ends). Returns nil if there is nothing pending.
func (r *OverlapReconciler) Flush(sessionID, track string) *domain.TranscriptSegment {
	st, ok := r.tracks[track]
	if !ok || len(st.pendingWords) == 0 {
		return nil
	}
	startMS := st.pendingStartMS
	if st.haveFinal {
		startMS = st.lastFinalEndMS
	}
	seg := domain.TranscriptSegment{
		SessionID: sessionID,
		Track:     track,
		Text:      strings.Join(st.pendingWords, " "),
		StartMS:   startMS,
		EndMS:     st.pendingEndMS,
		Final:     true,
	}
	st.stableWords = append(st.stableWords, st.pendingWords...)
	st.lastFinalEndMS = st.pendingEndMS
	st.haveFinal = true
	st.pendingWords = nil
	return &seg
}

// Tracks returns the set of track names seen so far (used by callers that
// need to Flush every track at session end).
func (r *OverlapReconciler) Tracks() []string {
	out := make([]string, 0, len(r.tracks))
	for t := range r.tracks {
		out = append(out, t)
	}
	return out
}

func interimSegment(sessionID, track string, words []string, startMS, endMS int) domain.TranscriptSegment {
	return domain.TranscriptSegment{
		SessionID: sessionID,
		Track:     track,
		Text:      strings.Join(words, " "),
		StartMS:   startMS,
		EndMS:     endMS,
		Final:     false,
	}
}

// longestSuffixPrefixMatch returns the largest k (0 <= k <= min(len(a), len(b)))
// such that the normalized last k words of a equal the normalized first k
// words of b. Returns 0 when there is no overlap.
func longestSuffixPrefixMatch(a, b []string) int {
	maxK := len(a)
	if len(b) < maxK {
		maxK = len(b)
	}
	for k := maxK; k > 0; k-- {
		if wordsEqualNormalized(a[len(a)-k:], b[:k]) {
			return k
		}
	}
	return 0
}

func wordsEqualNormalized(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if normalizeWord(a[i]) != normalizeWord(b[i]) {
			return false
		}
	}
	return true
}

// tokenize splits text into words on whitespace, dropping empty tokens.
func tokenize(text string) []string {
	fields := strings.Fields(text)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// normalizeWord lowercases and strips leading/trailing punctuation so that
// e.g. "transcript." and "Transcript" are treated as the same token for
// alignment purposes, while the original casing/punctuation is preserved
// in the emitted segment text.
func normalizeWord(w string) string {
	w = strings.ToLower(w)
	return strings.TrimFunc(w, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSpace(r)
	})
}
