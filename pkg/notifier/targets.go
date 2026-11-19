package notifier

import (
	"fmt"
	"strings"
)

type targetField struct{ Title, Value string }

func notificationTargets(event FailoverEvent) (targetField, targetField) {
	old := targetField{Title: "Old IPs", Value: formatIPList(event.OldIPs, event.OldIP)}
	next := targetField{Title: "New IPs", Value: formatIPList(event.NewIPs, event.NewIP)}
	if event.RecordType == "CNAME" {
		old.Title, next.Title = "Old Target", "New Target"
		old.Value = formatNamedTarget(event.OldTargetName, old.Value, event.OldPriority)
		next.Value = formatNamedTarget(event.NewTargetName, next.Value, event.NewPriority)
	}
	return old, next
}

func formatNamedTarget(name, address string, priority int) string {
	if name == "" {
		return address
	}
	return fmt.Sprintf("%s (priority %d)\n%s", name, priority, address)
}

func namedFailureTarget(failure HealthCheckFailure) string {
	if failure.TargetName == "" {
		return failure.IP
	}
	if failure.IP == "" {
		return failure.TargetName
	}
	return fmt.Sprintf("%s (%s)", failure.TargetName, failure.IP)
}

func destinationKind(event FailoverEvent) string {
	if event.RecordType == "CNAME" {
		return "Target"
	}
	return "IP"
}

func formatHealthCheckFailures(failures []HealthCheckFailure) string {
	lines := make([]string, 0, len(failures))
	for _, failure := range failures {
		if namedFailureTarget(failure) == "" {
			lines = append(lines, fmt.Sprintf("Priority %d: %s", failure.Priority, failure.Reason))
		} else {
			lines = append(lines, fmt.Sprintf("Priority %d, %s: %s", failure.Priority, namedFailureTarget(failure), failure.Reason))
		}
	}
	value := []rune(strings.Join(lines, "\n"))
	if len(value) > 1024 {
		return string(value[:1021]) + "..."
	}
	return string(value)
}

func formatIPList(ips []string, fallback string) string {
	if len(ips) == 0 {
		if fallback == "" {
			return "-"
		}
		return fallback
	}
	return strings.Join(ips, "\n")
}
