package replica

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Process owns only the child it launches. It never searches or signals other connectors.
type Process struct {
	Path    string
	Args    []string
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	done    chan struct{}
	waitErr error
}

func (p *Process) Running() bool {
	if p.done == nil {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (p *Process) Start(ctx context.Context) error {
	if p.Running() {
		return nil
	}
	if p.cancel != nil {
		p.cancel()
	}
	childCtx, cancel := context.WithCancel(ctx)
	// Path and Args are operator-owned configuration; no shell is involved.
	cmd := exec.CommandContext(childCtx, p.Path, p.Args...) // #nosec G204 -- executable and arguments come from operator-owned configuration; no shell is used.
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start connector: %w", err)
	}
	p.cmd, p.cancel, p.done, p.waitErr = cmd, cancel, make(chan struct{}), nil
	done := p.done
	go func() { p.waitErr = cmd.Wait(); close(done) }()
	log.Print("Started service Tunnel connector")
	return nil
}

func (p *Process) Stop(ctx context.Context) error {
	if p.done == nil {
		return nil
	}
	p.cancel()
	select {
	case <-ctx.Done():
		if p.Running() {
			_ = p.cmd.Process.Kill()
		}
		return ctx.Err()
	case <-p.done:
	}
	// Exit status after an intentional disconnect is expected. A subsequent healthy tick can restart it.
	var exitErr *exec.ExitError
	if p.waitErr != nil && !errors.As(p.waitErr, &exitErr) && !errors.Is(p.waitErr, context.Canceled) {
		return p.waitErr
	}
	return nil
}
