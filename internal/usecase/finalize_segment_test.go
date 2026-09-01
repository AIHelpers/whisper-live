package usecase

import (
	"strings"
	"testing"

	"whisper-live/internal/domain"
)

type trackEmit struct {
	text  string
	final bool
}

// stableText returns only the Final segments' text, joined — i.e. the
// coherent, de-duplicated running transcript a reader/exporter would see.
func stableText(emits []trackEmit) string {
	var sb strings.Builder
	for _, e := range emits {
		if !e.final {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(e.text)
	}
	return sb.String()
}

// reconcileAll feeds a sequence of overlapping window transcripts through
// a fresh reconciler (as SessionRunner.IngestAudioChunk would, one window
// at a time) and then flushes the trailing pending tail (as
// SessionRunner.Finish would at session end), returning every segment
// emitted along the way in order.
func reconcileAll(t *testing.T, windows []string, winMS int, hopMS int) []trackEmit {
	t.Helper()
	r := NewOverlapReconciler()
	var out []trackEmit
	for i, w := range windows {
		start := i * hopMS
		end := start + winMS
		segs := r.Reconcile("sess1", "mic", w, start, end)
		for _, s := range segs {
			out = append(out, trackEmit{text: s.Text, final: s.Final})
		}
	}
	if seg := r.Flush("sess1", "mic"); seg != nil {
		out = append(out, trackEmit{text: seg.Text, final: seg.Final})
	}
	return out
}

func TestReconcile_SingleWindow_AllInterim(t *testing.T) {
	r := NewOverlapReconciler()
	segs := r.Reconcile("s", "mic", "hello there world", 0, 4000)
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if segs[0].Final {
		t.Errorf("first-ever window should be interim, got Final=true")
	}
	if segs[0].Text != "hello there world" {
		t.Errorf("unexpected text: %q", segs[0].Text)
	}
}

func TestReconcile_OverlappingWindows_StitchTogether(t *testing.T) {
	// Simulates a sentence spoken across 3 overlapping 4s windows with 2s hop:
	// window0 [0-4s]:  "the quick brown fox jumps"
	// window1 [2-6s]:  "brown fox jumps over the lazy"
	// window2 [4-8s]:  "jumps over the lazy dog today"
	windows := []string{
		"the quick brown fox jumps",
		"brown fox jumps over the lazy",
		"jumps over the lazy dog today",
	}
	emits := reconcileAll(t, windows, 4000, 2000)

	got := stableText(emits)
	// The reconciled *stable* transcript should read as one continuous
	// sentence, with the overlapping words ("brown fox jumps", "jumps over
	// the lazy") appearing only once each — not duplicated.
	want := "the quick brown fox jumps over the lazy dog today"
	if got != want {
		t.Errorf("reconciled transcript mismatch:\n got:  %q\n want: %q", got, want)
	}
}

func TestReconcile_NoOverlap_TreatedAsDisjoint(t *testing.T) {
	// Two completely unrelated windows (e.g. separated by a silence gap
	// that reset context) should not try to force an alignment, and both
	// should still end up in the final transcript in order.
	windows := []string{
		"good morning everyone",
		"the meeting starts now",
	}
	emits := reconcileAll(t, windows, 4000, 2000)
	got := stableText(emits)
	want := "good morning everyone the meeting starts now"
	if got != want {
		t.Errorf("disjoint transcript mismatch:\n got:  %q\n want: %q", got, want)
	}
}

func TestReconcile_PunctuationAndCaseDoNotBreakAlignment(t *testing.T) {
	windows := []string{
		"Hello, world. This is",
		"world. This is a test",
	}
	emits := reconcileAll(t, windows, 4000, 2000)
	got := stableText(emits)
	want := "Hello, world. This is a test"
	if got != want {
		t.Errorf("case/punctuation mismatch:\n got:  %q\n want: %q", got, want)
	}
}

func TestReconcile_FinalSegmentsHaveNonDecreasingTimestamps(t *testing.T) {
	windows := []string{
		"one two three four five six",
		"four five six seven eight nine ten",
		"eight nine ten eleven twelve thirteen",
	}
	r := NewOverlapReconciler()
	lastEnd := -1
	check := func(s domain.TranscriptSegment) {
		if !s.Final {
			return
		}
		if s.StartMS < lastEnd {
			t.Errorf("final segment StartMS %d went backwards before lastEnd %d", s.StartMS, lastEnd)
		}
		if s.EndMS < s.StartMS {
			t.Errorf("segment EndMS %d before StartMS %d", s.EndMS, s.StartMS)
		}
		lastEnd = s.EndMS
	}
	for i, w := range windows {
		start := i * 2000
		end := start + 4000
		for _, s := range r.Reconcile("s", "mic", w, start, end) {
			check(s)
		}
	}
	if seg := r.Flush("s", "mic"); seg != nil {
		check(*seg)
	}
}

func TestReconcile_EmptyWindowText_NoOutput(t *testing.T) {
	r := NewOverlapReconciler()
	segs := r.Reconcile("s", "mic", "   ", 0, 4000)
	if len(segs) != 0 {
		t.Errorf("expected no segments for blank window text, got %d", len(segs))
	}
}

func TestReconcile_IndependentTracksDoNotInterfere(t *testing.T) {
	r := NewOverlapReconciler()
	r.Reconcile("s", "mic", "hello from the mic track", 0, 4000)
	segs := r.Reconcile("s", "system", "hello from the system track", 0, 4000)
	if len(segs) != 1 || segs[0].Final {
		t.Fatalf("second track's first window should be independent/interim, got %+v", segs)
	}
}

func TestReconcile_FlushWithNothingPending_ReturnsNil(t *testing.T) {
	r := NewOverlapReconciler()
	if seg := r.Flush("s", "never-seen-track"); seg != nil {
		t.Errorf("expected nil flush for unknown track, got %+v", seg)
	}
}

func TestLongestSuffixPrefixMatch(t *testing.T) {
	cases := []struct {
		a, b []string
		want int
	}{
		{[]string{"a", "b", "c"}, []string{"b", "c", "d"}, 2},
		{[]string{"a", "b", "c"}, []string{"x", "y", "z"}, 0},
		{[]string{"a", "b", "c"}, []string{"a", "b", "c"}, 3},
		{[]string{}, []string{"a"}, 0},
		{[]string{"a"}, []string{}, 0},
	}
	for _, c := range cases {
		got := longestSuffixPrefixMatch(c.a, c.b)
		if got != c.want {
			t.Errorf("longestSuffixPrefixMatch(%v, %v) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
