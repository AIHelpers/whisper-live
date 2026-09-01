package usecase

import (
	"fmt"
	"strings"

	"whisper-live/internal/domain"
)

// ExportFormat selects the rendering used by ExportTranscript.
type ExportFormat string

const (
	FormatSRT   ExportFormat = "srt"
	FormatVTT   ExportFormat = "vtt"
	FormatPlain ExportFormat = "plain"
)

// ExportTranscript renders a session's Final segments (only — interim
// segments are never persisted) into the requested subtitle/text format.
// Segments are expected in chronological order (as returned by
// SessionRunner.FinalSegments).
func ExportTranscript(segments []domain.TranscriptSegment, format ExportFormat) (string, error) {
	switch format {
	case FormatSRT:
		return toSRT(segments), nil
	case FormatVTT:
		return toVTT(segments), nil
	case FormatPlain:
		return toPlain(segments), nil
	default:
		return "", fmt.Errorf("usecase: unknown export format %q", format)
	}
}

func toSRT(segments []domain.TranscriptSegment) string {
	var b strings.Builder
	for i, s := range segments {
		fmt.Fprintf(&b, "%d\n", i+1)
		fmt.Fprintf(&b, "%s --> %s\n", srtTimestamp(s.StartMS), srtTimestamp(s.EndMS))
		b.WriteString(speakerPrefix(s))
		b.WriteString(s.Text)
		b.WriteString("\n\n")
	}
	return b.String()
}

func toVTT(segments []domain.TranscriptSegment) string {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for _, s := range segments {
		fmt.Fprintf(&b, "%s --> %s\n", vttTimestamp(s.StartMS), vttTimestamp(s.EndMS))
		b.WriteString(speakerPrefix(s))
		b.WriteString(s.Text)
		b.WriteString("\n\n")
	}
	return b.String()
}

func toPlain(segments []domain.TranscriptSegment) string {
	var b strings.Builder
	for _, s := range segments {
		fmt.Fprintf(&b, "[%s] ", plainTimestamp(s.StartMS))
		b.WriteString(speakerPrefix(s))
		b.WriteString(s.Text)
		b.WriteString("\n")
	}
	return b.String()
}

func speakerPrefix(s domain.TranscriptSegment) string {
	if s.Speaker != nil && *s.Speaker != "" {
		return "[" + *s.Speaker + "] "
	}
	return ""
}

func srtTimestamp(ms int) string {
	h, m, s, msRem := splitMS(ms)
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, msRem)
}

func vttTimestamp(ms int) string {
	h, m, s, msRem := splitMS(ms)
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, msRem)
}

func plainTimestamp(ms int) string {
	h, m, s, _ := splitMS(ms)
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func splitMS(ms int) (h, m, s, msRem int) {
	if ms < 0 {
		ms = 0
	}
	msRem = ms % 1000
	totalSec := ms / 1000
	s = totalSec % 60
	totalMin := totalSec / 60
	m = totalMin % 60
	h = totalMin / 60
	return
}
