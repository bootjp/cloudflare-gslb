package notifier

import (
	"context"
	"strings"
	"time"
)

// FailoverEvent represents a failover event
type FailoverEvent struct {
	OriginName       string
	ZoneName         string
	RecordType       string
	OldIP            string
	NewIP            string   // Primary new IP (for backward compatibility)
	NewIPs           []string // All new IPs (when multiple IPs are activated)
	Reason           string
	Timestamp        time.Time
	IsPriorityIP     bool
	IsFailoverIP     bool
	ReturnToPriority bool
}

// GetNewIPsDisplay returns a display string for new IPs
func (e FailoverEvent) GetNewIPsDisplay() string {
	if len(e.NewIPs) > 0 {
		return strings.Join(e.NewIPs, ", ")
	}
	return e.NewIP
}

// Notifier is the interface that all notifiers must implement
type Notifier interface {
	// Notify sends a notification about a failover event
	Notify(ctx context.Context, event FailoverEvent) error
}
