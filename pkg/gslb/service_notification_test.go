package gslb

import (
	"context"
	"testing"
	"time"

	"github.com/bootjp/cloudflare-gslb/config"
	"github.com/bootjp/cloudflare-gslb/pkg/notifier"
)

// MockNotifier is a mock implementation of the Notifier interface for testing
type MockNotifier struct {
	NotifyCalled    bool
	LastEvent       notifier.FailoverEvent
	NotifyError     error
	NotifyCallCount int
}

func (m *MockNotifier) Notify(ctx context.Context, event notifier.FailoverEvent) error {
	m.NotifyCalled = true
	m.LastEvent = event
	m.NotifyCallCount++
	return m.NotifyError
}

func TestService_sendNotifications(t *testing.T) {
	tests := []struct {
		name             string
		origin           config.OriginConfig
		oldIPs           []string
		newIPs           []string
		oldPriority      int
		newPriority      int
		maxPriority      int
		reason           string
		isPriorityIP     bool
		isFailoverIP     bool
		expectNotifyCall bool
	}{
		{
			name: "send notification on failover",
			origin: config.OriginConfig{
				Name:       "www",
				ZoneName:   "example.com",
				RecordType: "A",
			},
			oldIPs:           []string{"192.168.1.1"},
			newIPs:           []string{"192.168.1.2"},
			oldPriority:      100,
			newPriority:      50,
			maxPriority:      100,
			reason:           "Health check failed",
			isPriorityIP:     false,
			isFailoverIP:     true,
			expectNotifyCall: true,
		},
		{
			name: "send notification on return to priority",
			origin: config.OriginConfig{
				Name:             "www",
				ZoneName:         "example.com",
				RecordType:       "A",
				ReturnToPriority: true,
			},
			oldIPs:           []string{"192.168.1.2"},
			newIPs:           []string{"192.168.1.1", "192.168.1.3"},
			oldPriority:      50,
			newPriority:      100,
			maxPriority:      100,
			reason:           "Priority IP is healthy again",
			isPriorityIP:     true,
			isFailoverIP:     false,
			expectNotifyCall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock notifier
			mockNotifier := &MockNotifier{}

			// Create a service with the mock notifier
			service := &Service{
				config:    &config.Config{},
				notifiers: []notifier.Notifier{mockNotifier},
			}

			// Call sendNotifications
			service.sendNotifications(
				tt.origin,
				tt.oldIPs,
				tt.newIPs,
				tt.reason,
				tt.isPriorityIP,
				tt.isFailoverIP,
				tt.oldPriority,
				tt.newPriority,
				tt.maxPriority,
			)

			// Wait a bit for the goroutine to execute
			time.Sleep(100 * time.Millisecond)

			// Verify the notification was sent
			if tt.expectNotifyCall && !mockNotifier.NotifyCalled {
				t.Error("Expected notification to be called, but it was not")
			}

			if mockNotifier.NotifyCalled {
				// Verify event details
				if mockNotifier.LastEvent.OriginName != tt.origin.Name {
					t.Errorf("Expected origin name %s, got %s", tt.origin.Name, mockNotifier.LastEvent.OriginName)
				}
				if mockNotifier.LastEvent.ZoneName != tt.origin.ZoneName {
					t.Errorf("Expected zone name %s, got %s", tt.origin.ZoneName, mockNotifier.LastEvent.ZoneName)
				}
				if mockNotifier.LastEvent.OldIP != firstIP(tt.oldIPs) {
					t.Errorf("Expected old IP %s, got %s", firstIP(tt.oldIPs), mockNotifier.LastEvent.OldIP)
				}
				if mockNotifier.LastEvent.NewIP != firstIP(tt.newIPs) {
					t.Errorf("Expected new IP %s, got %s", firstIP(tt.newIPs), mockNotifier.LastEvent.NewIP)
				}
				if !sameStringSet(mockNotifier.LastEvent.OldIPs, tt.oldIPs) {
					t.Errorf("Expected old IPs %v, got %v", tt.oldIPs, mockNotifier.LastEvent.OldIPs)
				}
				if !sameStringSet(mockNotifier.LastEvent.NewIPs, tt.newIPs) {
					t.Errorf("Expected new IPs %v, got %v", tt.newIPs, mockNotifier.LastEvent.NewIPs)
				}
				if mockNotifier.LastEvent.Reason != tt.reason {
					t.Errorf("Expected reason %s, got %s", tt.reason, mockNotifier.LastEvent.Reason)
				}
				if mockNotifier.LastEvent.IsPriorityIP != tt.isPriorityIP {
					t.Errorf("Expected IsPriorityIP %v, got %v", tt.isPriorityIP, mockNotifier.LastEvent.IsPriorityIP)
				}
				if mockNotifier.LastEvent.IsFailoverIP != tt.isFailoverIP {
					t.Errorf("Expected IsFailoverIP %v, got %v", tt.isFailoverIP, mockNotifier.LastEvent.IsFailoverIP)
				}
				if mockNotifier.LastEvent.OldPriority != tt.oldPriority {
					t.Errorf("Expected OldPriority %d, got %d", tt.oldPriority, mockNotifier.LastEvent.OldPriority)
				}
				if mockNotifier.LastEvent.NewPriority != tt.newPriority {
					t.Errorf("Expected NewPriority %d, got %d", tt.newPriority, mockNotifier.LastEvent.NewPriority)
				}
			}
		})
	}
}

func TestService_sendNotifications_noNotifiers(t *testing.T) {
	// Create a service without notifiers
	service := &Service{
		config:    &config.Config{},
		notifiers: []notifier.Notifier{},
	}

	origin := config.OriginConfig{
		Name:       "www",
		ZoneName:   "example.com",
		RecordType: "A",
	}

	// This should not panic even without notifiers
	service.sendNotifications(
		origin,
		[]string{"192.168.1.1"},
		[]string{"192.168.1.2"},
		"Health check failed",
		false,
		true,
		100,
		50,
		100,
	)

	// If we got here without panic, the test passes
}

func TestService_sendNotifications_multipleNotifiers(t *testing.T) {
	// Create multiple mock notifiers
	mockNotifier1 := &MockNotifier{}
	mockNotifier2 := &MockNotifier{}

	// Create a service with multiple notifiers
	service := &Service{
		config:    &config.Config{},
		notifiers: []notifier.Notifier{mockNotifier1, mockNotifier2},
	}

	origin := config.OriginConfig{
		Name:       "www",
		ZoneName:   "example.com",
		RecordType: "A",
	}

	// Call sendNotifications
	service.sendNotifications(
		origin,
		[]string{"192.168.1.1"},
		[]string{"192.168.1.2"},
		"Health check failed",
		false,
		true,
		100,
		50,
		100,
	)

	// Wait for goroutines to execute
	time.Sleep(100 * time.Millisecond)

	// Verify both notifiers were called
	if !mockNotifier1.NotifyCalled {
		t.Error("Expected first notifier to be called, but it was not")
	}
	if !mockNotifier2.NotifyCalled {
		t.Error("Expected second notifier to be called, but it was not")
	}
}
