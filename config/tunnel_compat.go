package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// TunnelGroup is retained for legacy configuration compatibility.
//
// Deprecated: use CNAMETarget and cname_targets.
type TunnelGroup = CNAMETarget

var tunnelName = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.cfargotunnel\.com$`)

// Legacy names and constraints are isolated from the generic CNAME selector.
func normalizeLegacyTunnelGroups(origin *OriginConfig) error {
	if len(origin.TunnelGroups) == 0 {
		return nil
	}
	if len(origin.CNAMETargets) != 0 {
		return fmt.Errorf("use cname_targets or legacy tunnel_groups, not both")
	}
	if origin.RecordType != "CNAME" || !origin.Proxied {
		return fmt.Errorf("legacy tunnel_groups require CNAME and proxied=true")
	}
	for _, group := range origin.TunnelGroups {
		if !tunnelName.MatchString(CanonicalDNSName(group.Target)) {
			return fmt.Errorf("legacy tunnel_groups require UUID.cfargotunnel.com destinations; use cname_targets for other DNS targets")
		}
		u, err := url.Parse(group.ProbeURL)
		if err == nil && strings.HasSuffix(CanonicalDNSName(u.Hostname()), ".cfargotunnel.com") {
			return fmt.Errorf("legacy Tunnel probes require a published hostname, not a Tunnel UUID")
		}
	}
	origin.CNAMETargets = append([]CNAMETarget(nil), origin.TunnelGroups...)
	origin.TunnelGroups = nil
	return nil
}
