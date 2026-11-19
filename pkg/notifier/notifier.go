package notifier

import (
	"context"
	"time"
)

// FailoverEvent represents a failover event
type FailoverEvent struct {
	OriginName             string
	ZoneName               string
	RecordType             string
	OldTargetName          string
	NewTargetName          string
	OldIP                  string
	NewIP                  string
	OldIPs                 []string
	NewIPs                 []string
	Reason                 string
	HealthCheckFailures    []HealthCheckFailure
	CheckerHostname        string
	Timestamp              time.Time
	IsPriorityIP           bool
	IsFailoverIP           bool
	IsSamePriorityFailover bool
	IsSamePriorityRecovery bool
	ReturnToPriority       bool
	OldPriority            int
	NewPriority            int
	MaxPriority            int
}

// HealthCheckFailure identifies an unhealthy destination at a priority level.
type HealthCheckFailure struct {
	TargetName string
	Priority   int
	IP         string
	Reason     string
}

// Notifier is the interface that all notifiers must implement
type Notifier interface {
	// Notify sends a notification about a failover event
	Notify(ctx context.Context, event FailoverEvent) error
}
