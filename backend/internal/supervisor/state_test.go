package supervisor

import "testing"

func TestTracker_DefaultIsStarting(t *testing.T) {
	tr := NewTracker()
	if got := tr.Get().State; got != StateStarting {
		t.Errorf("default state: want %q, got %q", StateStarting, got)
	}
}

func TestTracker_SetUpdatesStateAndSince(t *testing.T) {
	tr := NewTracker()
	before := tr.Get().Since
	tr.Set(StateHealthy)
	s := tr.Get()
	if s.State != StateHealthy {
		t.Errorf("state: want %q, got %q", StateHealthy, s.State)
	}
	if !s.Since.After(before) && !s.Since.Equal(before) {
		t.Error("Since should advance on state change")
	}
}

func TestTracker_FailRecordsReasonAndDetail(t *testing.T) {
	tr := NewTracker()
	tr.Fail(ReasonProvisionFailed, "stream create error")
	s := tr.Get()
	if s.State != StateFailed {
		t.Errorf("state: want %q, got %q", StateFailed, s.State)
	}
	if s.Reason != ReasonProvisionFailed || s.Detail != "stream create error" {
		t.Errorf("reason/detail not recorded: %+v", s)
	}
}

func TestTracker_SetEngineAndProvisioned(t *testing.T) {
	tr := NewTracker()
	tr.SetEngine(4321, true)
	tr.SetProvisioned(true)
	s := tr.Get()
	if s.Engine.PID != 4321 || !s.Engine.Healthy {
		t.Errorf("engine not recorded: %+v", s.Engine)
	}
	if !s.Provisioned {
		t.Error("provisioned not recorded")
	}
}

func TestTracker_ClaimFromFailedIsExclusive(t *testing.T) {
	tr := NewTracker()
	tr.Fail(ReasonProvisionFailed, "x")
	if !tr.ClaimFromFailed() {
		t.Fatal("first claim from failed should win")
	}
	if tr.ClaimFromFailed() {
		t.Error("second claim should fail (already transitioned out of failed)")
	}
	if got := tr.Get().State; got != StateStarting {
		t.Errorf("state after claim: want %q, got %q", StateStarting, got)
	}
}
