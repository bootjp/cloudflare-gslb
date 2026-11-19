package gslb

import (
	"context"
	"fmt"
	"log"
	"sort"

	"github.com/bootjp/cloudflare-gslb/config"
	"github.com/bootjp/cloudflare-gslb/pkg/cloudflare"
	"github.com/bootjp/cloudflare-gslb/pkg/healthcheck"
	"github.com/bootjp/cloudflare-gslb/pkg/notifier"
)

// checkCNAMEOrigin selects one healthy DNS target according to its priority.
func (s *Service) checkCNAMEOrigin(ctx context.Context, origin config.OriginConfig, checker healthcheck.Checker) error {
	s.checkMutex.Lock()
	defer s.checkMutex.Unlock()
	client := s.getDNSClientForOrigin(origin)
	candidates := append([]config.CNAMETarget(nil), origin.CNAMETargets...)
	if len(candidates) == 0 {
		return fmt.Errorf("no CNAME candidates configured")
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Priority > candidates[j].Priority })
	state, err := s.readCNAMEState(ctx, origin, client, candidates)
	if err != nil {
		return err
	}
	current, currentPriority, found := state.target, state.priority, state.set
	selected, failures, err := selectCNAMETarget(ctx, origin, checker, candidates, currentPriority, found)
	if err != nil {
		return err
	}
	target := config.CanonicalDNSName(selected.Target)
	if target == current {
		return nil
	}
	if s.config.DryRun {
		log.Printf("Dry run: would select CNAME target %s priority=%d for %s", selected.Name, selected.Priority, origin.Name)
		return nil
	}
	if err := confirmCNAMEState(ctx, client, origin, state); err != nil {
		return err
	}
	if _, err := client.UpdateDNSRecord(ctx, state.recordID, origin.Name, "CNAME", target); err != nil {
		return fmt.Errorf("update CNAME: %w", err)
	}
	if err := confirmCNAMERecord(ctx, client, origin, target); err != nil {
		return err
	}
	log.Printf("Selected CNAME target %s priority=%d for %s", selected.Name, selected.Priority, origin.Name)
	s.sendNotifications(origin, []string{current}, []string{target}, "Switching CNAME target", selected.Priority == candidates[0].Priority, selected.Priority < candidates[0].Priority, currentPriority, selected.Priority, candidates[0].Priority, failures)
	return nil
}

func selectCNAMETarget(ctx context.Context, origin config.OriginConfig, checker healthcheck.Checker, candidates []config.CNAMETarget, currentPriority int, currentSet bool) (config.CNAMETarget, []notifier.HealthCheckFailure, error) {
	var failures []notifier.HealthCheckFailure
	for _, group := range candidates {
		if err := ctx.Err(); err != nil {
			return config.CNAMETarget{}, failures, err
		}
		if !origin.ReturnToPriority && currentSet && group.Priority > currentPriority {
			continue
		}
		var err error
		if contextual, ok := checker.(interface {
			CheckContext(context.Context, string) error
		}); ok {
			err = contextual.CheckContext(ctx, group.ProbeURL)
		} else {
			err = checker.Check(group.ProbeURL)
		}
		if err == nil {
			return group, failures, nil
		}
		log.Printf("CNAME target probe failed origin=%s group=%s: %v", origin.Name, group.Name, err)
		failures = append(failures, notifier.HealthCheckFailure{Priority: group.Priority, IP: config.CanonicalDNSName(group.Target), Reason: err.Error()})
	}
	return config.CNAMETarget{}, failures, fmt.Errorf("all eligible CNAME candidates unhealthy; preserving current DNS")
}

type cnameState struct {
	recordID string
	proxied  bool
	target   string
	priority int
	set      bool
}

func (s *Service) readCNAMEState(ctx context.Context, origin config.OriginConfig, client cloudflare.DNSClientInterface, candidates []config.CNAMETarget) (cnameState, error) {
	records, err := client.GetDNSRecords(ctx, origin.Name, "CNAME")
	if err != nil {
		return cnameState{}, fmt.Errorf("read current CNAME: %w", err)
	}
	if len(records) == 0 && s.config.DryRun {
		return cnameState{}, nil
	}
	if len(records) != 1 {
		return cnameState{}, fmt.Errorf("expected one managed CNAME; migrate A/AAAA separately before enabling writes")
	}
	current := config.CanonicalDNSName(records[0].Content)
	if records[0].Proxied != origin.Proxied {
		return cnameState{}, fmt.Errorf("CNAME proxy drift; refusing DNS changes")
	}
	for _, group := range candidates {
		if config.CanonicalDNSName(group.Target) == current {
			return cnameState{recordID: records[0].ID, proxied: records[0].Proxied, target: current, priority: group.Priority, set: true}, nil
		}
	}
	return cnameState{}, fmt.Errorf("CNAME ownership drift; refusing DNS changes")
}
func confirmCNAMERecord(ctx context.Context, client cloudflare.DNSClientInterface, origin config.OriginConfig, target string) error {
	// DNS API acknowledgement alone does not prove the selected value was applied.
	confirmed, err := client.GetDNSRecords(ctx, origin.Name, "CNAME")
	if err != nil {
		return fmt.Errorf("read back CNAME: %w", err)
	}
	if len(confirmed) != 1 || config.CanonicalDNSName(confirmed[0].Content) != target || confirmed[0].Proxied != origin.Proxied {
		return fmt.Errorf("cname CNAME read-back mismatch")
	}
	return nil
}

// Recheck the snapshot after probes, then update only the original record ID.
// This is a drift guard, not an atomic compare-and-swap with the DNS provider.
func confirmCNAMEState(ctx context.Context, client cloudflare.DNSClientInterface, origin config.OriginConfig, before cnameState) error {
	records, err := client.GetDNSRecords(ctx, origin.Name, "CNAME")
	if err != nil {
		return fmt.Errorf("recheck CNAME before update: %w", err)
	}
	if len(records) != 1 || records[0].ID != before.recordID || config.CanonicalDNSName(records[0].Content) != before.target || records[0].Proxied != before.proxied {
		return fmt.Errorf("CNAME changed during candidate probes; refusing DNS changes")
	}
	return nil
}
