// Package replica gates one service's cloudflared process on independent origin readiness.
package replica

import (
	"context"
	"fmt"
	"log"
	"time"
)

type Connector interface {
	Start(context.Context) error
	Stop(context.Context) error
	Running() bool
}

type Gate struct {
	Probe     func(context.Context) error
	Connector Connector
	Interval  time.Duration
	Fall      int
	Rise      int
	failures  int
	successes int
}

// Tick does not use the public tunnel: the origin remains probeable while its connector is stopped.
func (g *Gate) Tick(ctx context.Context) error {
	if err := g.Probe(ctx); err != nil {
		g.successes = 0
		if g.failures < g.Fall {
			g.failures++
		}
		log.Printf("Replica readiness failed (%d/%d): %v", g.failures, g.Fall, err)
		if g.failures >= g.Fall {
			return g.Connector.Stop(ctx)
		}
		return nil
	}
	g.failures = 0
	if g.successes < g.Rise {
		g.successes++
	}
	if g.successes >= g.Rise && !g.Connector.Running() {
		return g.Connector.Start(ctx)
	}
	return nil
}

func (g *Gate) Run(ctx context.Context) (err error) {
	if err := g.validate(); err != nil {
		return err
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if stopErr := g.Connector.Stop(stopCtx); stopErr != nil {
			err = fmt.Errorf("stop connector: %w", stopErr)
		}
	}()
	ticker := time.NewTicker(g.Interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := g.Tick(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (g *Gate) validate() error {
	if g.Probe == nil || g.Connector == nil || g.Interval <= 0 || g.Fall <= 0 || g.Rise <= 0 {
		return fmt.Errorf("invalid replica gate configuration")
	}
	return nil
}
