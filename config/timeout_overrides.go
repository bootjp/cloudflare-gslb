package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ApplyHealthTimeoutOverrides applies non-secret, per-origin timeout settings
// after the main configuration has been loaded from a Secret.
func (c *Config) ApplyHealthTimeoutOverrides(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	var overrides map[string]int
	if err := json.Unmarshal([]byte(raw), &overrides); err != nil {
		return fmt.Errorf("parse health timeout overrides: %w", err)
	}
	if overrides == nil {
		return fmt.Errorf("health timeout overrides must be a JSON object")
	}
	knownOrigins := make(map[string]struct{}, len(c.Origins))
	for _, origin := range c.Origins {
		knownOrigins[origin.Name] = struct{}{}
	}

	for name, timeout := range overrides {
		if !validHealthTimeout(timeout) {
			return fmt.Errorf("health timeout for %q must be between 1 and 300 seconds", name)
		}
		if _, ok := knownOrigins[name]; !ok {
			return fmt.Errorf("health timeout override refers to unknown origin %q", name)
		}
	}

	for i := range c.Origins {
		if timeout, ok := overrides[c.Origins[i].Name]; ok {
			c.Origins[i].HealthCheck.Timeout = timeout
		}
	}
	return nil
}

func validHealthTimeout(timeout int) bool {
	return timeout >= 1 && timeout <= 300
}
