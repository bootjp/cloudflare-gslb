package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SlackNotifier implements the Notifier interface for Slack webhooks
type SlackNotifier struct {
	webhookURL string
	httpClient *http.Client
}

// NewSlackNotifier creates a new Slack notifier
func NewSlackNotifier(webhookURL string) *SlackNotifier {
	return &SlackNotifier{
		webhookURL: webhookURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// slackMessage represents the message structure for Slack webhook
type slackMessage struct {
	Text        string            `json:"text"`
	Attachments []slackAttachment `json:"attachments,omitempty"`
}

type slackAttachment struct {
	Color  string       `json:"color,omitempty"`
	Fields []slackField `json:"fields,omitempty"`
	Footer string       `json:"footer,omitempty"`
	Ts     int64        `json:"ts,omitempty"`
}

type slackField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

// Notify sends a notification to Slack
func (s *SlackNotifier) Notify(ctx context.Context, event FailoverEvent) error {
	color := slackColor(event)
	oldTarget, newTarget := notificationTargets(event)

	originName := displayOrigin(event.OriginName, event.ZoneName)
	message := slackMessage{
		Text: fmt.Sprintf("*DNS Failover Event* - %s", originName),
		Attachments: []slackAttachment{
			{
				Color: color,
				Fields: []slackField{
					{Title: "Origin", Value: fmt.Sprintf("%s (%s)", originName, event.RecordType), Short: true},
					{Title: oldTarget.Title, Value: oldTarget.Value, Short: true},
					{Title: newTarget.Title, Value: newTarget.Value, Short: true},
					{Title: "Event Type", Value: s.getEventType(event), Short: true},
					{Title: "Reason", Value: event.Reason, Short: false},
				},
				Footer: "Cloudflare GSLB",
				Ts:     event.Timestamp.Unix(),
			},
		},
	}

	if len(event.HealthCheckFailures) > 0 {
		message.Attachments[0].Fields = append(message.Attachments[0].Fields, slackField{Title: "Health Check Failures", Value: formatHealthCheckFailures(event.HealthCheckFailures)})
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal Slack message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.webhookURL, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create Slack request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Slack notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack webhook returned status: %d", resp.StatusCode)
	}

	return nil
}

func slackColor(event FailoverEvent) string {
	switch {
	case event.IsSamePriorityFailover:
		return "danger"
	case event.IsSamePriorityRecovery || event.ReturnToPriority && event.IsPriorityIP:
		return "good"
	case event.IsFailoverIP:
		return "danger"
	default:
		return "warning"
	}
}

func (s *SlackNotifier) getEventType(event FailoverEvent) string {
	switch {
	case event.IsSamePriorityFailover:
		return "Failover within Priority Level"
	case event.IsSamePriorityRecovery:
		return "Recovery within Priority Level"
	case event.ReturnToPriority && event.IsPriorityIP:
		return fmt.Sprintf("Recovery (Return to Priority %s)", destinationKind(event))
	case event.IsPriorityIP:
		return fmt.Sprintf("Failover to Priority %s", destinationKind(event))
	case event.IsFailoverIP:
		return fmt.Sprintf("Failover to Backup %s", destinationKind(event))
	default:
		return "Failover"
	}
}
