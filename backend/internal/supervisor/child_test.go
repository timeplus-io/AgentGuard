package supervisor

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestHelperProcess is not a real test — it's a stand-in child binary that the
// child tests re-exec. It prints its watchdog env, then blocks until killed.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	os.Stdout.WriteString("WATCHDOG=" + os.Getenv("TIMEPLUSD_WATCHDOG_ENABLE") + "\n")
	time.Sleep(30 * time.Second) // wait to be signalled; Stop() should cut this short
	os.Exit(0)
}

func TestExecChild_StartDisablesWatchdogAndStops(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	lb := NewLogBuffer(10)

	ch, err := startExecChild(context.Background(), os.Args[0],
		[]string{"-test.run=TestHelperProcess"}, lb)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if ch.PID() <= 0 {
		t.Errorf("want a real PID, got %d", ch.PID())
	}

	// Give it a moment to print, then assert the watchdog env was passed as "0".
	time.Sleep(300 * time.Millisecond)
	found := false
	for _, l := range lb.Lines() {
		if l == "WATCHDOG=0" {
			found = true
		}
	}
	if !found {
		t.Errorf("watchdog env not disabled; captured lines: %v", lb.Lines())
	}

	start := time.Now()
	if err := ch.Stop(2 * time.Second); err != nil {
		t.Errorf("stop: %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Error("Stop did not terminate the child promptly")
	}
}

func TestExecChild_ConcurrentStopAndWaitSafe(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	lb := NewLogBuffer(10)
	ch, err := startExecChild(context.Background(), os.Args[0], []string{"-test.run=TestHelperProcess"}, lb)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- ch.Wait() }() // one goroutine waits
	time.Sleep(50 * time.Millisecond)
	if err := ch.Stop(2 * time.Second); err != nil { // main stops concurrently
		t.Errorf("stop: %v", err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Wait did not return after Stop")
	}
}
