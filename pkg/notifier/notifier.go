package notifier

import (
	"context"
	"time"
)

// FailoverEvent represents a failover event
type FailoverEvent struct {
	OriginName       string
	ZoneName         string
	RecordType       string
	OldIP            string
	NewIP            string
	OldIPs           []string
	NewIPs           []string
	Reason           string
	Timestamp        time.Time
	IsPriorityIP     bool
	IsFailoverIP     bool
	ReturnToPriority bool
	OldPriority      int
	NewPriority      int
	MaxPriority      int
}

// Notifier is the interface that all notifiers must implement
type Notifier interface {
	// Notify sends a notification about a failover event
	Notify(ctx context.Context, event FailoverEvent) error
}
