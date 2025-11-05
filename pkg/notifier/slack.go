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
	color := "warning"
	if event.ReturnToPriority && event.IsPriorityIP {
		color = "good"
	} else if event.IsFailoverIP {
		color = "danger"
	}

	message := slackMessage{
		Text: fmt.Sprintf("*DNS Failover Event* - %s.%s", event.OriginName, event.ZoneName),
		Attachments: []slackAttachment{
			{
				Color: color,
				Fields: []slackField{
					{Title: "Origin", Value: fmt.Sprintf("%s.%s (%s)", event.OriginName, event.ZoneName, event.RecordType), Short: true},
					{Title: "Old IP", Value: event.OldIP, Short: true},
					{Title: "New IP", Value: event.NewIP, Short: true},
					{Title: "Event Type", Value: s.getEventType(event), Short: true},
					{Title: "Reason", Value: event.Reason, Short: false},
				},
				Footer: "Cloudflare GSLB",
				Ts:     event.Timestamp.Unix(),
			},
		},
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

func (s *SlackNotifier) getEventType(event FailoverEvent) string {
	switch {
	case event.ReturnToPriority && event.IsPriorityIP:
		return "Recovery (Return to Priority IP)"
	case event.IsPriorityIP:
		return "Failover to Priority IP"
	case event.IsFailoverIP:
		return "Failover to Backup IP"
	default:
		return "Failover"
	}
}
