// Package alert implements usecase.AlertNotifier for keyword-spotting
// (spec section 4: "configure watch-words... that trigger a desktop
// notification when spoken live").
package alert

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"whisper-live/internal/domain"
	"whisper-live/internal/infra/logger"
)

// Notifier fires desktop notifications (best-effort, OS-specific — stubbed
// to a log line in this headless build; a Wails build would replace
// notifyDesktop with the OS-native notification call) or webhook POSTs.
type Notifier struct {
	Client *http.Client
}

// NewNotifier builds a Notifier with a default HTTP client for webhooks.
func NewNotifier() *Notifier {
	return &Notifier{Client: &http.Client{Timeout: 5 * time.Second}}
}

// Notify implements usecase.AlertNotifier.
func (n *Notifier) Notify(rule domain.AlertRule, seg domain.TranscriptSegment) {
	switch rule.Action {
	case domain.ActionWebhook:
		n.notifyWebhook(rule, seg)
	default:
		n.notifyDesktop(rule, seg)
	}
}

func (n *Notifier) notifyDesktop(rule domain.AlertRule, seg domain.TranscriptSegment) {
	// Desktop notification delivery is OS-native (Wails exposes this via
	// its runtime bindings on the frontend, or a package like
	// gen2brain/beeep on the backend) and isn't available in this headless
	// build — log so the trigger is still observable end-to-end.
	logger.Infof("ALERT keyword %q spoken (track=%s): %q", rule.Keyword, seg.Track, seg.Text)
}

func (n *Notifier) notifyWebhook(rule domain.AlertRule, seg domain.TranscriptSegment) {
	if rule.Target == "" {
		logger.Warnf("alert rule %q has Action=webhook but no Target URL", rule.ID)
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"ruleId":    rule.ID,
		"keyword":   rule.Keyword,
		"sessionId": seg.SessionID,
		"track":     seg.Track,
		"text":      seg.Text,
		"startMs":   seg.StartMS,
		"endMs":     seg.EndMS,
	})
	client := n.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Post(rule.Target, "application/json", bytes.NewReader(payload))
	if err != nil {
		logger.Errorf("alert webhook to %s failed: %v", rule.Target, err)
		return
	}
	defer resp.Body.Close()
}
