package config

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/idna"
)

// CNAMETarget is one candidate DNS destination and its independent readiness URL.
type CNAMETarget struct {
	Name     string `json:"name" yaml:"name"`
	Priority int    `json:"priority" yaml:"priority"`
	Target   string `json:"target" yaml:"target"`
	ProbeURL string `json:"probe_url" yaml:"probe_url"`
}

var dnsLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func CanonicalDNSName(name string) string {
	ascii, err := idna.Lookup.ToASCII(name)
	if err == nil {
		name = ascii
	}
	return strings.ToLower(strings.TrimSuffix(name, "."))
}

func normalizeOriginMode(origin *OriginConfig) error {
	if origin.RecordType != "CNAME" {
		if len(origin.CNAMETargets) != 0 {
			return fmt.Errorf("cname_targets require CNAME records")
		}
		return validateRecordType(origin.RecordType)
	}
	return normalizeCNAMETargets(origin)
}

func normalizeCNAMETargets(origin *OriginConfig) error {
	if len(origin.CNAMETargets) == 0 {
		return fmt.Errorf("CNAME origins require cname_targets")
	}
	if len(origin.PriorityLevels)+len(origin.PriorityFailoverIPs)+len(origin.FailoverIPs) != 0 {
		return fmt.Errorf("CNAME targets cannot be mixed with IP priority levels")
	}
	if err := validateCNAMEHealth(origin.HealthCheck); err != nil {
		return err
	}
	name, err := canonicalCNAMEHost(origin.Name)
	if err != nil {
		return fmt.Errorf("invalid CNAME origin hostname: %w", err)
	}
	origin.Name = name
	names, targets, priorities := map[string]bool{}, map[string]bool{}, map[int]bool{}
	for i := range origin.CNAMETargets {
		candidate := &origin.CNAMETargets[i]
		if err := normalizeCNAMECandidate(name, candidate); err != nil {
			return err
		}
		if !uniqueCNAMETarget(*candidate, names, targets, priorities) {
			return fmt.Errorf("CNAME targets require unique nonempty names, destinations and priorities")
		}
		names[candidate.Name], targets[candidate.Target], priorities[candidate.Priority] = true, true, true
	}
	return nil
}

func canonicalCNAMEHost(name string) (string, error) {
	ascii, err := canonicalProbeDNSName(name)
	if err != nil {
		return "", err
	}
	if ascii == "" || len(ascii) > 253 || net.ParseIP(ascii) != nil {
		return "", fmt.Errorf("expected a DNS hostname")
	}
	for _, label := range strings.Split(ascii, ".") {
		if !dnsLabel.MatchString(label) {
			return "", fmt.Errorf("invalid DNS hostname label")
		}
	}
	return ascii, nil
}

func validateCNAMEHealth(hc HealthCheck) error {
	if hc.Type != "url" || hc.Host != "" || hc.Endpoint != "" || hc.InsecureSkipVerify || hc.Timeout < 1 || hc.Timeout > 300 {
		return fmt.Errorf("CNAME health checks require type=url, timeout 1..300, URL Host/SNI and TLS verification")
	}
	return nil
}

func validateCNAMEProbe(originName string, candidate CNAMETarget) error {
	u, err := url.Parse(candidate.ProbeURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return fmt.Errorf("CNAME target %s requires an HTTPS probe URL without credentials, query or fragment", candidate.Name)
	}
	return validateCNAMEProbeNames(originName, u.Hostname(), candidate.Name)
}

func validateCNAMEProbeNames(originName, hostname, candidateName string) error {
	probeName, err := canonicalProbeDNSName(hostname)
	if err != nil {
		return fmt.Errorf("CNAME target %s has an invalid probe hostname: %w", candidateName, err)
	}
	publicName, err := canonicalProbeDNSName(originName)
	if err != nil {
		return fmt.Errorf("invalid origin hostname: %w", err)
	}
	if probeName == publicName {
		return fmt.Errorf("CNAME target %s must probe a fixed hostname, not the selected public name", candidateName)
	}
	return nil
}

func canonicalProbeDNSName(name string) (string, error) {
	ascii, err := idna.Lookup.ToASCII(name)
	if err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimSuffix(ascii, ".")), nil
}

func normalizeCNAMECandidate(originName string, candidate *CNAMETarget) error {
	target, err := canonicalCNAMEHost(candidate.Target)
	if err != nil {
		return fmt.Errorf("invalid CNAME target %s: %w", candidate.Name, err)
	}
	if target == originName {
		return fmt.Errorf("CNAME target %s points to its own public hostname", candidate.Name)
	}
	candidate.Target = target
	return validateCNAMEProbe(originName, *candidate)
}

func uniqueCNAMETarget(candidate CNAMETarget, names, targets map[string]bool, priorities map[int]bool) bool {
	return candidate.Name != "" && !names[candidate.Name] && !targets[candidate.Target] && !priorities[candidate.Priority]
}
