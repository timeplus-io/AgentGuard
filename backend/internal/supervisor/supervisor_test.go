package supervisor

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fakeChild implements Child for Boot tests.
type fakeChild struct {
	pid   int
	exit  chan error // send to simulate process exit
	stops int32
}

func newFakeChild(pid int) *fakeChild { return &fakeChild{pid: pid, exit: make(chan error, 1)} }
func (f *fakeChild) PID() int         { return f.pid }
func (f *fakeChild) Wait() error      { return <-f.exit }
func (f *fakeChild) Stop(time.Duration) error {
	atomic.AddInt32(&f.stops, 1)
	select {
	case f.exit <- nil:
	default:
	}
	return nil
}
func (f *fakeChild) stopCount() int { return int(atomic.LoadInt32(&f.stops)) }

func newTestSupervisor() (*Supervisor, *fakeChild) {
	fc := newFakeChild(1234)
	s := &Supervisor{
		tracker:       NewTracker(),
		logs:          NewLogBuffer(10),
		setupComplete: func() bool { return false },
		startChild:    func(context.Context) (Child, error) { return fc, nil },
		waitHealthyFn: func(context.Context) error { return nil },
		provisionFn:   func(func(string, string)) error { return nil },
		fatal:         make(chan error, 1),
	}
	return s, fc
}

func waitForState(t *testing.T, s *Supervisor, want State) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if s.Status().State == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("state never reached %q (last=%q, reason=%q)", want, s.Status().State, s.Status().Reason)
}

func TestBoot_HappyPathReachesReady(t *testing.T) {
	s, _ := newTestSupervisor()
	go s.Boot(context.Background())
	waitForState(t, s, StateReady)
	if !s.Status().Provisioned {
		t.Error("want provisioned=true at ready")
	}
}

func TestBoot_HealthFailureFailsEngineUnhealthy(t *testing.T) {
	s, fc := newTestSupervisor()
	s.waitHealthyFn = func(context.Context) error { return errors.New("no engine") }
	go s.Boot(context.Background())
	waitForState(t, s, StateFailed)
	if s.Status().Reason != ReasonEngineUnhealthy {
		t.Errorf("reason: want %q, got %q", ReasonEngineUnhealthy, s.Status().Reason)
	}
	if fc.stopCount() < 1 {
		t.Error("child should be stopped on health failure")
	}
}

func TestBoot_ProvisionFailureFailsProvision(t *testing.T) {
	s, fc := newTestSupervisor()
	s.provisionFn = func(func(string, string)) error { return errors.New("ddl failed") }
	go s.Boot(context.Background())
	waitForState(t, s, StateFailed)
	if s.Status().Reason != ReasonProvisionFailed {
		t.Errorf("reason: want %q, got %q", ReasonProvisionFailed, s.Status().Reason)
	}
	if fc.stopCount() < 1 {
		t.Error("child should be stopped on provision failure")
	}
}

func TestBoot_ChildExitDuringStartupFailsEngineExited(t *testing.T) {
	s, fc := newTestSupervisor()
	s.waitHealthyFn = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() } // never healthy
	go s.Boot(context.Background())
	fc.exit <- errors.New("boom") // simulate early crash
	waitForState(t, s, StateFailed)
	if s.Status().Reason != ReasonEngineExited {
		t.Errorf("reason: want %q, got %q", ReasonEngineExited, s.Status().Reason)
	}
}

func TestBoot_SkipsProvisionWhenAlreadyComplete(t *testing.T) {
	s, _ := newTestSupervisor()
	provisionCalled := false
	s.setupComplete = func() bool { return true }
	s.provisionFn = func(func(string, string)) error { provisionCalled = true; return nil }
	go s.Boot(context.Background())
	waitForState(t, s, StateReady)
	if provisionCalled {
		t.Error("provision should be skipped when setup already complete")
	}
}

func TestRetry_OnlyValidFromFailed(t *testing.T) {
	s, _ := newTestSupervisor()
	if err := s.Retry(); err == nil {
		t.Error("retry from starting should error")
	}
	s.tracker.Fail(ReasonProvisionFailed, "x")
	if err := s.Retry(); err != nil {
		t.Errorf("retry from failed should succeed, got %v", err)
	}
}
