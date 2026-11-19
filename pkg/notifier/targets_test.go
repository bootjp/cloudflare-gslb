package notifier

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNamedCNAMENotifications(t *testing.T) {
	for _, provider := range []string{"discord", "slack"} {
		t.Run(provider, func(t *testing.T) {
			payloads := make(chan []byte, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				payloads <- b
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			var n Notifier = NewDiscordNotifier(server.URL)
			if provider == "slack" {
				n = NewSlackNotifier(server.URL)
			}
			event := FailoverEvent{OriginName: "app.example.com", ZoneName: "example.com", RecordType: "CNAME", OldTargetName: "primary", NewTargetName: "backup", OldIPs: []string{"primary.example.net"}, NewIPs: []string{"backup.example.org"}, OldPriority: 100, NewPriority: 0, IsFailoverIP: true, HealthCheckFailures: []HealthCheckFailure{{TargetName: "primary", Priority: 100, IP: "primary.example.net", Reason: "connection refused"}}}
			if err := n.Notify(context.Background(), event); err != nil {
				t.Fatal(err)
			}
			var payload struct {
				Embeds []struct {
					Fields []struct{ Name, Value string }
				}
				Attachments []struct {
					Fields []struct{ Title, Value string }
				}
			}
			if err := json.Unmarshal(<-payloads, &payload); err != nil {
				t.Fatal(err)
			}
			fields := map[string]string{}
			if provider == "discord" {
				for _, f := range payload.Embeds[0].Fields {
					fields[f.Name] = f.Value
				}
			} else {
				for _, f := range payload.Attachments[0].Fields {
					fields[f.Title] = f.Value
				}
			}
			for title, want := range map[string]string{"Old Target": "primary (priority 100)\nprimary.example.net", "New Target": "backup (priority 0)\nbackup.example.org", "Health Check Failures": "Priority 100, primary (primary.example.net): connection refused"} {
				if got := fields[title]; got != want {
					t.Errorf("%s=%q want %q", title, got, want)
				}
			}
			if _, ok := fields["Old IPs"]; ok {
				t.Fatal("CNAME notification uses IP labels")
			}
		})
	}
}

func TestCNAMENotificationFallsBackToHostname(t *testing.T) {
	old, next := notificationTargets(FailoverEvent{RecordType: "CNAME", OldIP: "old.example.net", NewIP: "new.example.net"})
	if old.Title != "Old Target" || old.Value != "old.example.net" || next.Title != "New Target" || next.Value != "new.example.net" {
		t.Fatalf("unexpected fallback: %+v %+v", old, next)
	}
}

func TestCNAMEEventTypeLabels(t *testing.T) {
	event := FailoverEvent{RecordType: "CNAME", IsFailoverIP: true}
	if got := (&DiscordNotifier{}).getEventType(event); got != "❌ Failover to Backup Target" {
		t.Fatal(got)
	}
	if got := (&SlackNotifier{}).getEventType(event); got != "Failover to Backup Target" {
		t.Fatal(got)
	}
	event.IsFailoverIP = false
	event.IsPriorityIP = true
	event.ReturnToPriority = true
	if got := (&DiscordNotifier{}).getEventType(event); got != "✅ Recovery (Return to Priority Target)" {
		t.Fatal(got)
	}
	if got := (&SlackNotifier{}).getEventType(event); got != "Recovery (Return to Priority Target)" {
		t.Fatal(got)
	}
}
