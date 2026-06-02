package supervisor

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitHealthy_SucceedsAfterRetries(t *testing.T) {
	calls := 0
	probe := func() error {
		calls++
		if calls < 3 {
			return errors.New("not up yet")
		}
		return nil
	}
	err := waitHealthy(context.Background(), probe, time.Millisecond, time.Second)
	if err != nil {
		t.Fatalf("want success, got %v (calls=%d)", err, calls)
	}
	if calls < 3 {
		t.Errorf("want >=3 probe calls, got %d", calls)
	}
}

func TestWaitHealthy_TimesOut(t *testing.T) {
	probe := func() error { return errors.New("never up") }
	err := waitHealthy(context.Background(), probe, time.Millisecond, 20*time.Millisecond)
	if err == nil {
		t.Fatal("want timeout error, got nil")
	}
}

func TestWaitHealthy_CancelledByContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitHealthy(ctx, func() error { return errors.New("x") }, time.Millisecond, time.Second)
	if err == nil {
		t.Fatal("want context error, got nil")
	}
}
