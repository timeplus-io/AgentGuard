package supervisor

import (
	"context"
	"fmt"
	"time"
)

// waitHealthy polls probe every interval until it returns nil, the timeout
// elapses, or ctx is cancelled.
func waitHealthy(ctx context.Context, probe func() error, interval, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for {
		if last = probe(); last == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeplusd not healthy within %s: %w", timeout, last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}
