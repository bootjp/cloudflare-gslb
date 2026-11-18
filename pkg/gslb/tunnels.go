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

// checkTunnelOrigin selects a tier, never a replica within a tier.
// Replica health gates independently disconnect origins that cannot serve the application.
func (s *Service) checkTunnelOrigin(ctx context.Context, origin config.OriginConfig, checker healthcheck.Checker) error {
	s.checkMutex.Lock()
	defer s.checkMutex.Unlock()
	client := s.getDNSClientForOrigin(origin)
	groups := append([]config.TunnelGroup(nil), origin.TunnelGroups...)
	if len(groups) == 0 {
		return fmt.Errorf("no Tunnel groups configured")
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Priority > groups[j].Priority })
	state, err := s.readTunnelState(ctx, origin, client, groups)
	if err != nil {
		return err
	}
	current, currentPriority, found := state.target, state.priority, state.set
	selected, failures, err := selectTunnelGroup(ctx, origin, checker, groups, currentPriority, found)
	if err != nil {
		return err
	}
	target := config.CanonicalDNSName(selected.Target)
	if target == current {
		return nil
	}
	if s.config.DryRun {
		log.Printf("Dry run: would select Tunnel group %s priority=%d for %s", selected.Name, selected.Priority, origin.Name)
		return nil
	}
	if err := confirmTunnelState(ctx, client, origin, state); err != nil {
		return err
	}
	if _, err := client.UpdateDNSRecord(ctx, state.recordID, origin.Name, "CNAME", target); err != nil {
		return fmt.Errorf("update Tunnel CNAME: %w", err)
	}
	if err := confirmTunnelCNAME(ctx, client, origin, target); err != nil {
		return err
	}
	log.Printf("Selected Tunnel group %s priority=%d for %s", selected.Name, selected.Priority, origin.Name)
	s.sendNotifications(origin, []string{current}, []string{target}, "Switching Tunnel priority group", selected.Priority == groups[0].Priority, selected.Priority < groups[0].Priority, currentPriority, selected.Priority, groups[0].Priority, failures)
	return nil
}

func selectTunnelGroup(ctx context.Context, origin config.OriginConfig, checker healthcheck.Checker, groups []config.TunnelGroup, currentPriority int, currentSet bool) (config.TunnelGroup, []notifier.HealthCheckFailure, error) {
	var failures []notifier.HealthCheckFailure
	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return config.TunnelGroup{}, failures, err
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
		log.Printf("Tunnel group probe failed origin=%s group=%s: %v", origin.Name, group.Name, err)
		failures = append(failures, notifier.HealthCheckFailure{Priority: group.Priority, IP: config.CanonicalDNSName(group.Target), Reason: err.Error()})
	}
	return config.TunnelGroup{}, failures, fmt.Errorf("all eligible Tunnel groups unhealthy; preserving current DNS")
}

type tunnelState struct {
	recordID string
	proxied  bool
	target   string
	priority int
	set      bool
}

func (s *Service) readTunnelState(ctx context.Context, origin config.OriginConfig, client cloudflare.DNSClientInterface, groups []config.TunnelGroup) (tunnelState, error) {
	records, err := client.GetDNSRecords(ctx, origin.Name, "CNAME")
	if err != nil {
		return tunnelState{}, fmt.Errorf("read current CNAME: %w", err)
	}
	if len(records) == 0 && s.config.DryRun {
		return tunnelState{}, nil
	}
	if len(records) != 1 {
		return tunnelState{}, fmt.Errorf("expected one managed CNAME; migrate A/AAAA separately before enabling writes")
	}
	current := config.CanonicalDNSName(records[0].Content)
	if records[0].Proxied != origin.Proxied {
		return tunnelState{}, fmt.Errorf("CNAME proxy drift; refusing DNS changes")
	}
	for _, group := range groups {
		if config.CanonicalDNSName(group.Target) == current {
			return tunnelState{recordID: records[0].ID, proxied: records[0].Proxied, target: current, priority: group.Priority, set: true}, nil
		}
	}
	return tunnelState{}, fmt.Errorf("CNAME ownership drift; refusing DNS changes")
}
func confirmTunnelCNAME(ctx context.Context, client cloudflare.DNSClientInterface, origin config.OriginConfig, target string) error {
	// DNS API acknowledgement alone does not prove the selected value was applied.
	confirmed, err := client.GetDNSRecords(ctx, origin.Name, "CNAME")
	if err != nil {
		return fmt.Errorf("read back Tunnel CNAME: %w", err)
	}
	if len(confirmed) != 1 || config.CanonicalDNSName(confirmed[0].Content) != target || confirmed[0].Proxied != origin.Proxied {
		return fmt.Errorf("tunnel CNAME read-back mismatch")
	}
	return nil
}

// Recheck the snapshot after probes, then update only the original record ID.
// This is a drift guard, not an atomic compare-and-swap with the DNS provider.
func confirmTunnelState(ctx context.Context, client cloudflare.DNSClientInterface, origin config.OriginConfig, before tunnelState) error {
	records, err := client.GetDNSRecords(ctx, origin.Name, "CNAME")
	if err != nil {
		return fmt.Errorf("recheck Tunnel CNAME before update: %w", err)
	}
	if len(records) != 1 || records[0].ID != before.recordID || config.CanonicalDNSName(records[0].Content) != before.target || records[0].Proxied != before.proxied {
		return fmt.Errorf("CNAME changed during group probes; refusing DNS changes")
	}
	return nil
}
