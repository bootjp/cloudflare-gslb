package config

import "testing"

func TestApplyHealthTimeoutOverrides(t *testing.T) {
	newConfig := func() *Config {
		return &Config{Origins: []OriginConfig{
			{Name: "misskey.bootjp.me", HealthCheck: HealthCheck{Timeout: 5}},
			{Name: "resize.gg", HealthCheck: HealthCheck{Timeout: 10}},
		}}
	}

	t.Run("updates only named origin", func(t *testing.T) {
		cfg := newConfig()
		if err := cfg.ApplyHealthTimeoutOverrides(`{"misskey.bootjp.me":10}`); err != nil {
			t.Fatal(err)
		}
		if cfg.Origins[0].HealthCheck.Timeout != 10 || cfg.Origins[1].HealthCheck.Timeout != 10 {
			t.Fatalf("unexpected timeouts: %d, %d", cfg.Origins[0].HealthCheck.Timeout, cfg.Origins[1].HealthCheck.Timeout)
		}
	})

	for _, raw := range []string{`{"missing.example":10}`, `{"misskey.bootjp.me":0}`, `{"misskey.bootjp.me":301}`, `not-json`, `null`} {
		t.Run(raw, func(t *testing.T) {
			cfg := newConfig()
			if err := cfg.ApplyHealthTimeoutOverrides(raw); err == nil {
				t.Fatal("expected invalid override to fail")
			}
			if cfg.Origins[0].HealthCheck.Timeout != 5 || cfg.Origins[1].HealthCheck.Timeout != 10 {
				t.Fatal("invalid override changed configuration")
			}
		})
	}
}
