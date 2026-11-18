package replica

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestProcessOwnsAndReapsItsChild(t *testing.T) {
	path, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not installed")
	}
	p := &Process{Path: path, Args: []string{"60"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	defer func() { _ = p.Stop(stopCtx) }()
	if err := p.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !p.Running() {
		t.Fatal("child did not start")
	}
	if err := p.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	if p.Running() {
		t.Fatal("child remained alive after stop")
	}
	if err := p.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !p.Running() {
		t.Fatal("child did not restart")
	}
	cancel()
	if err := p.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	if p.Running() {
		t.Fatal("child remained alive after cancellation")
	}
}
