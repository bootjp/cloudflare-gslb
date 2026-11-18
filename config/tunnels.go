package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/idna"
)

// TunnelGroup is one priority tier. All healthy replicas in this tier share a UUID.
type TunnelGroup struct {
	Name     string `json:"name" yaml:"name"`
	Priority int    `json:"priority" yaml:"priority"`
	Target   string `json:"target" yaml:"target"`
	ProbeURL string `json:"probe_url" yaml:"probe_url"`
}

var tunnelName = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.cfargotunnel\.com$`)

func CanonicalDNSName(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, "."))
}

func validateOriginMode(origin OriginConfig) error {
	if origin.RecordType != "CNAME" {
		if len(origin.TunnelGroups) != 0 {
			return fmt.Errorf("tunnel_groups require CNAME records")
		}
		return validateRecordType(origin.RecordType)
	}
	return validateTunnelGroups(origin)
}

func validateTunnelGroups(origin OriginConfig) error {
	if !origin.Proxied || len(origin.TunnelGroups) == 0 {
		return fmt.Errorf("tunnel origins require proxied=true and tunnel_groups")
	}
	if len(origin.PriorityLevels)+len(origin.PriorityFailoverIPs)+len(origin.FailoverIPs) != 0 {
		return fmt.Errorf("tunnel groups cannot be mixed with IP priority levels")
	}
	if err := validateTunnelHealth(origin.HealthCheck); err != nil {
		return err
	}
	names, targets, priorities := map[string]bool{}, map[string]bool{}, map[int]bool{}
	for _, group := range origin.TunnelGroups {
		target := CanonicalDNSName(group.Target)
		if !validTunnelIdentity(group, names, targets, priorities) {
			return fmt.Errorf("tunnel groups require unique names, targets and priorities and a UUID.cfargotunnel.com target")
		}
		if err := validateTunnelProbe(origin.Name, group); err != nil {
			return err
		}
		names[group.Name], targets[target], priorities[group.Priority] = true, true, true
	}
	return nil
}

func validateTunnelHealth(hc HealthCheck) error {
	if hc.Type != "url" || hc.Host != "" || hc.Endpoint != "" || hc.InsecureSkipVerify || hc.Timeout < 1 || hc.Timeout > 300 {
		return fmt.Errorf("tunnel health checks require type=url, timeout 1..300, URL Host/SNI and TLS verification")
	}
	return nil
}
func validateTunnelProbe(originName string, group TunnelGroup) error {
	u, err := url.Parse(group.ProbeURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return fmt.Errorf("tunnel group %s requires an HTTPS probe URL without credentials, query or fragment", group.Name)
	}
	return validateTunnelProbeNames(originName, u.Hostname(), group.Name)
}

func validateTunnelProbeNames(originName, hostname, groupName string) error {
	probeName, err := canonicalProbeDNSName(hostname)
	if err != nil {
		return fmt.Errorf("tunnel group %s has an invalid probe hostname: %w", groupName, err)
	}
	publicName, err := canonicalProbeDNSName(originName)
	if err != nil {
		return fmt.Errorf("invalid origin hostname: %w", err)
	}
	if probeName == publicName || strings.HasSuffix(probeName, ".cfargotunnel.com") {
		return fmt.Errorf("tunnel group %s must probe a fixed published hostname, not the selected public name or tunnel UUID", groupName)
	}
	return nil
}

func validTunnelIdentity(group TunnelGroup, names, targets map[string]bool, priorities map[int]bool) bool {
	target := CanonicalDNSName(group.Target)
	return group.Name != "" && !names[group.Name] && !targets[target] && !priorities[group.Priority] && tunnelName.MatchString(target)
}

func canonicalProbeDNSName(name string) (string, error) {
	ascii, err := idna.Lookup.ToASCII(name)
	if err != nil {
		return "", err
	}
	return CanonicalDNSName(ascii), nil
}
