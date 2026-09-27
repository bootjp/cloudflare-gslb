package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DiscordNotifier implements the Notifier interface for Discord webhooks
type DiscordNotifier struct {
	webhookURL string
	httpClient *http.Client
}

// NewDiscordNotifier creates a new Discord notifier
func NewDiscordNotifier(webhookURL string) *DiscordNotifier {
	return &DiscordNotifier{
		webhookURL: webhookURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// discordMessage represents the message structure for Discord webhook
type discordMessage struct {
	Content string         `json:"content,omitempty"`
	Embeds  []discordEmbed `json:"embeds,omitempty"`
}

type discordEmbed struct {
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	Color       int            `json:"color,omitempty"`
	Fields      []discordField `json:"fields,omitempty"`
	Footer      *discordFooter `json:"footer,omitempty"`
	Timestamp   string         `json:"timestamp,omitempty"`
}

type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type discordFooter struct {
	Text string `json:"text"`
}

// Notify sends a notification to Discord
func (d *DiscordNotifier) Notify(ctx context.Context, event FailoverEvent) error {
	color := 16776960 // Yellow for warning
	if event.ReturnToPriority && event.IsPriorityIP {
		color = 5763719 // Green for success
	} else if event.IsFailoverIP {
		color = 15158332 // Red for danger
	}
	hostname := event.CheckerHostname
	if hostname == "" {
		hostname = "unknown"
	}

	message := discordMessage{
		Embeds: []discordEmbed{
			{
				Title:       fmt.Sprintf("🔄 DNS Failover Event - %s.%s", event.OriginName, event.ZoneName),
				Description: event.Reason,
				Color:       color,
				Fields: []discordField{
					{Name: "Origin", Value: fmt.Sprintf("%s.%s (%s)", event.OriginName, event.ZoneName, event.RecordType), Inline: true},
					{Name: "Event Type", Value: d.getEventType(event), Inline: true},
					{Name: "Old IPs", Value: formatDiscordIPList(event.OldIPs, event.OldIP), Inline: true},
					{Name: "New IPs", Value: formatDiscordIPList(event.NewIPs, event.NewIP), Inline: true},
					{Name: "Checker Host", Value: hostname, Inline: true},
				},
				Footer: &discordFooter{
					Text: "Cloudflare GSLB",
				},
				Timestamp: event.Timestamp.Format(time.RFC3339),
			},
		},
	}
	if len(event.HealthCheckFailures) > 0 {
		message.Embeds[0].Fields = append(message.Embeds[0].Fields, discordField{
			Name: "Health Check Failures", Value: formatHealthCheckFailures(event.HealthCheckFailures),
		})
	}

	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal Discord message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", d.webhookURL, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create Discord request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Discord notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("discord webhook returned status: %d", resp.StatusCode)
	}

	return nil
}

func formatHealthCheckFailures(failures []HealthCheckFailure) string {
	lines := make([]string, 0, len(failures))
	for _, failure := range failures {
		if failure.IP == "" {
			lines = append(lines, fmt.Sprintf("Priority %d: %s", failure.Priority, failure.Reason))
		} else {
			lines = append(lines, fmt.Sprintf("Priority %d, %s: %s", failure.Priority, failure.IP, failure.Reason))
		}
	}
	value := []rune(strings.Join(lines, "\n"))
	if len(value) > 1024 {
		return string(value[:1021]) + "..."
	}
	return string(value)
}

func (d *DiscordNotifier) getEventType(event FailoverEvent) string {
	switch {
	case event.ReturnToPriority && event.IsPriorityIP:
		return "✅ Recovery (Return to Priority IP)"
	case event.IsPriorityIP:
		return "⚠️ Failover to Priority IP"
	case event.IsFailoverIP:
		return "❌ Failover to Backup IP"
	default:
		return "⚠️ Failover"
	}
}

func formatDiscordIPList(ips []string, fallback string) string {
	if len(ips) == 0 {
		if fallback == "" {
			return "-"
		}
		return fallback
	}
	return strings.Join(ips, "\n")
}
