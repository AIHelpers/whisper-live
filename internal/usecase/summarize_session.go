package usecase

import (
	"context"
	"fmt"
	"strings"

	"whisper-live/internal/domain"
)

// SummarizeSession joins a session's Final segments into one transcript
// and asks the configured Summarizer (local or remote LLM backend, per
// spec section 4 "Meeting summary mode") to produce action items/notes.
func SummarizeSession(ctx context.Context, summarizer Summarizer, segments []domain.TranscriptSegment) (string, error) {
	if summarizer == nil {
		return "", fmt.Errorf("usecase: no Summarizer configured")
	}
	full := toPlain(segments)
	if strings.TrimSpace(full) == "" {
		return "", fmt.Errorf("usecase: nothing to summarize (empty transcript)")
	}
	return summarizer.Summarize(ctx, full)
}
