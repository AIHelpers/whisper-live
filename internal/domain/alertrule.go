package domain

// AlertAction is the action taken when an AlertRule's keyword is spotted.
type AlertAction string

const (
	ActionNotify  AlertAction = "notify"
	ActionWebhook AlertAction = "webhook"
)

// AlertRule is a watch-word configured by the user; when Keyword is heard
// in a finalized TranscriptSegment, Action fires (desktop notification or
// webhook call).
type AlertRule struct {
	ID      string
	Keyword string
	Action  AlertAction
	Target  string // webhook URL when Action == ActionWebhook; unused for notify
}
