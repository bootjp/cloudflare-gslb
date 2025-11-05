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
				Reason:       "Health check failed",
				Timestamp:    time.Now(),
				IsFailoverIP: true,
			},
			expectedColor: 15158332, // Red
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
				Reason:           "Priority IP is healthy again",
				Timestamp:        time.Now(),
				IsPriorityIP:     true,
				ReturnToPriority: true,
			},
			expectedColor: 5763719, // Green
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
			// Create a test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify the request
				if r.Method != "POST" {
					t.Errorf("Expected POST request, got %s", r.Method)
				}

				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("Expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
				}

				// Read and parse the body
				body, _ := io.ReadAll(r.Body)
				var msg discordMessage
				if err := json.Unmarshal(body, &msg); err != nil {
					t.Errorf("Failed to unmarshal request body: %v", err)
				}

				// Verify message structure
				if !tt.wantError && tt.statusCode == http.StatusOK {
					if len(msg.Embeds) == 0 {
						t.Error("Expected embeds in message")
					} else if msg.Embeds[0].Color != tt.expectedColor {
						t.Errorf("Expected color %d, got %d", tt.expectedColor, msg.Embeds[0].Color)
					}
				}

				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			// Create notifier with test server URL
			notifier := NewDiscordNotifier(server.URL)

			// Call Notify
			err := notifier.Notify(context.Background(), tt.event)

			// Check error
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
