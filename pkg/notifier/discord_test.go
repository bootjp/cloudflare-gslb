package notifier

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDiscordNotifier_Notify(t *testing.T) {
	tests := []struct {
		name          string
		event         FailoverEvent
		expectedColor int
		expectedOld   string
		expectedNew   string
		wantError     bool
		statusCode    int
	}{
		{
			name: "successful notification - failover to backup IP",
			event: FailoverEvent{
				OriginName:   "www",
				ZoneName:     "example.com",
				RecordType:   "A",
				OldIP:        "192.168.1.1",
				NewIP:        "192.168.1.2",
				OldIPs:       []string{"192.168.1.1", "192.168.1.10"},
				NewIPs:       []string{"192.168.1.2", "192.168.1.20"},
				Reason:       "Health check failed",
				Timestamp:    time.Now(),
				IsFailoverIP: true,
			},
			expectedColor: 15158332,
			expectedOld:   "192.168.1.1\n192.168.1.10",
			expectedNew:   "192.168.1.2\n192.168.1.20",
			wantError:     false,
			statusCode:    http.StatusOK,
		},
		{
			name: "successful notification - return to priority",
			event: FailoverEvent{
				OriginName:       "www",
				ZoneName:         "example.com",
				RecordType:       "A",
				OldIP:            "192.168.1.2",
				NewIP:            "192.168.1.1",
				OldIPs:           []string{"192.168.1.2"},
				NewIPs:           []string{"192.168.1.1", "192.168.1.3"},
				Reason:           "Priority IP is healthy again",
				Timestamp:        time.Now(),
				IsPriorityIP:     true,
				ReturnToPriority: true,
			},
			expectedColor: 5763719,
			expectedOld:   "192.168.1.2",
			expectedNew:   "192.168.1.1\n192.168.1.3",
			wantError:     false,
			statusCode:    http.StatusOK,
		},
		{
			name: "successful notification with NoContent status",
			event: FailoverEvent{
				OriginName: "www",
				ZoneName:   "example.com",
				RecordType: "A",
				OldIP:      "192.168.1.1",
				NewIP:      "192.168.1.2",
				Reason:     "Health check failed",
				Timestamp:  time.Now(),
			},
			wantError:  false,
			statusCode: http.StatusNoContent,
		},
		{
			name: "failed notification - bad status code",
			event: FailoverEvent{
				OriginName: "www",
				ZoneName:   "example.com",
				RecordType: "A",
				OldIP:      "192.168.1.1",
				NewIP:      "192.168.1.2",
				Reason:     "Health check failed",
				Timestamp:  time.Now(),
			},
			wantError:  true,
			statusCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					t.Errorf("Expected POST request, got %s", r.Method)
				}

				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("Expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
				}

				body, _ := io.ReadAll(r.Body)
				var msg discordMessage
				if err := json.Unmarshal(body, &msg); err != nil {
					t.Errorf("Failed to unmarshal request body: %v", err)
				}

				if !tt.wantError && tt.statusCode == http.StatusOK {
					if len(msg.Embeds) == 0 {
						t.Error("Expected embeds in message")
					} else if msg.Embeds[0].Color != tt.expectedColor {
						t.Errorf("Expected color %d, got %d", tt.expectedColor, msg.Embeds[0].Color)
					} else {
						var oldValue, newValue string
						for _, field := range msg.Embeds[0].Fields {
							if field.Name == "Old IPs" {
								oldValue = field.Value
							}
							if field.Name == "New IPs" {
								newValue = field.Value
							}
						}
						if tt.expectedOld != "" && oldValue != tt.expectedOld {
							t.Errorf("Expected Old IPs %q, got %q", tt.expectedOld, oldValue)
						}
						if tt.expectedNew != "" && newValue != tt.expectedNew {
							t.Errorf("Expected New IPs %q, got %q", tt.expectedNew, newValue)
						}
					}
				}

				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			notifier := NewDiscordNotifier(server.URL)

			err := notifier.Notify(context.Background(), tt.event)

			if (err != nil) != tt.wantError {
				t.Errorf("Notify() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestDiscordNotifier_GetEventType(t *testing.T) {
	notifier := &DiscordNotifier{}

	tests := []struct {
		name     string
		event    FailoverEvent
		expected string
	}{
		{
			name: "return to priority",
			event: FailoverEvent{
				IsPriorityIP:     true,
				ReturnToPriority: true,
			},
			expected: "✅ Recovery (Return to Priority IP)",
		},
		{
			name: "failover to priority",
			event: FailoverEvent{
				IsPriorityIP: true,
			},
			expected: "⚠️ Failover to Priority IP",
		},
		{
			name: "failover to backup",
			event: FailoverEvent{
				IsFailoverIP: true,
			},
			expected: "❌ Failover to Backup IP",
		},
		{
			name:     "generic failover",
			event:    FailoverEvent{},
			expected: "⚠️ Failover",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := notifier.getEventType(tt.event)
			if result != tt.expected {
				t.Errorf("getEventType() = %s, expected %s", result, tt.expected)
			}
		})
	}
}
