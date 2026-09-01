package domain

// TranscriptSegment is one piece of a running transcript. Segments start
// life as interim (Final=false) results emitted from a sliding window and
// are later reconciled into stable, Final=true segments once enough
// overlapping context has been seen (see usecase.OverlapReconciler).
type TranscriptSegment struct {
	SessionID string
	Track     string
	Speaker   *string
	Text      string
	StartMS   int
	EndMS     int
	Final     bool // interim vs. finalized after reconciliation
}

// Duration returns the segment length in milliseconds.
func (t TranscriptSegment) Duration() int {
	return t.EndMS - t.StartMS
}

// Overlaps reports whether two segments on the same track share any time range.
func (t TranscriptSegment) Overlaps(o TranscriptSegment) bool {
	if t.Track != o.Track {
		return false
	}
	return t.StartMS < o.EndMS && o.StartMS < t.EndMS
}
