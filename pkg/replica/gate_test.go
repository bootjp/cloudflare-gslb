package replica

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type fakeConnector struct {
	active bool
	starts int
	stops  int
}

func (f *fakeConnector) Start(context.Context) error {
	if !f.active {
		f.starts++
		f.active = true
	}
	return nil
}
func (f *fakeConnector) Stop(context.Context) error {
	if f.active {
		f.stops++
		f.active = false
	}
	return nil
}
func (f *fakeConnector) Running() bool { return f.active }

func TestReplicaStartsOnlyWhenReadyAndDisconnectsOnFailure(t *testing.T) {
	c := &fakeConnector{}
	healthy := false
	g := &Gate{Connector: c, Rise: 2, Fall: 2, Interval: time.Millisecond, Probe: func(context.Context) error {
		if !healthy {
			return fmt.Errorf("app unavailable")
		}
		return nil
	}}
	tick := func() {
		t.Helper()
		if err := g.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	tick()
	if c.active {
		t.Fatal("started before initial readiness")
	}
	healthy = true
	tick()
	if c.active {
		t.Fatal("started before rise threshold")
	}
	tick()
	if !c.active || c.starts != 1 {
		t.Fatal("healthy connector did not start")
	}
	healthy = false
	tick()
	if !c.active {
		t.Fatal("stopped before fall threshold")
	}
	tick()
	if c.active || c.stops != 1 {
		t.Fatal("unhealthy replica remains connected")
	}
	healthy = true
	tick()
	tick()
	if !c.active || c.starts != 2 {
		t.Fatal("independent probe did not recover stopped connector")
	}
	c.active = false
	tick()
	if c.starts != 3 {
		t.Fatal("healthy but crashed connector not restarted")
	}
}

func TestGateShutdownDisconnects(t *testing.T) {
	c := &fakeConnector{}
	ctx, cancel := context.WithCancel(context.Background())
	g := &Gate{Connector: c, Rise: 1, Fall: 1, Interval: time.Millisecond, Probe: func(context.Context) error { cancel(); return nil }}
	if err := g.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if c.active {
		t.Fatal("connector left connected after gate shutdown")
	}
}
