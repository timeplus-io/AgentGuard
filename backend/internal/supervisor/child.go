package supervisor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Child is the supervised engine process, abstracted so Boot() is testable
// with a fake while execChild carries the real OS behavior.
type Child interface {
	PID() int
	Wait() error
	Stop(timeout time.Duration) error
}

type execChild struct {
	cmd      *exec.Cmd
	waitOnce sync.Once
	waitErr  error
	waitDone chan struct{}
}

// startExecChild launches the binary with the watchdog disabled (so AgentGuard
// is the sole supervisor) in its own process group, mirroring stdout/stderr to
// os.Stderr (prefixed) and the log buffer.
func startExecChild(ctx context.Context, bin string, args []string, logs *LogBuffer) (Child, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), "TIMEPLUSD_WATCHDOG_ENABLE=0")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	w := io.MultiWriter(&prefixWriter{w: os.Stderr, prefix: []byte("[timeplusd] ")}, logs)
	cmd.Stdout = w
	cmd.Stderr = w

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start timeplusd: %w", err)
	}
	return &execChild{cmd: cmd, waitDone: make(chan struct{})}, nil
}

func (c *execChild) PID() int { return c.cmd.Process.Pid }

// reap calls cmd.Wait exactly once, regardless of how many goroutines call it.
func (c *execChild) reap() {
	c.waitOnce.Do(func() {
		c.waitErr = c.cmd.Wait()
		close(c.waitDone)
	})
}

func (c *execChild) Wait() error {
	c.reap()
	return c.waitErr
}

func (c *execChild) Stop(timeout time.Duration) error {
	go c.reap() // ensure the child is reaped even if nobody else called Wait
	pgid := c.cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGTERM) // signal the whole process group
	select {
	case <-c.waitDone:
		return nil
	case <-time.After(timeout):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-c.waitDone
		return nil
	}
}

// prefixWriter prepends a tag to each write so combined logs stay readable.
type prefixWriter struct {
	w      io.Writer
	prefix []byte
}

func (p *prefixWriter) Write(b []byte) (int, error) {
	if _, err := p.w.Write(p.prefix); err != nil {
		return 0, err
	}
	return p.w.Write(b)
}
