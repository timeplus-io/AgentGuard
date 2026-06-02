package supervisor

import (
	"sync"
	"time"
)

// State is the supervisor boot/runtime phase reflected by the UI.
type State string

const (
	StateStarting     State = "starting"
	StateHealthy      State = "healthy"
	StateProvisioning State = "provisioning"
	StateReady        State = "ready"
	StateFailed       State = "failed"
)

// Reason classifies a StateFailed transition.
const (
	ReasonEngineUnhealthy = "engine_unhealthy"
	ReasonEngineExited    = "engine_exited"
	ReasonProvisionFailed = "provision_failed"
)

// EngineStatus is the child timeplusd's observable status.
type EngineStatus struct {
	PID     int  `json:"pid,omitempty"`
	Healthy bool `json:"healthy"`
}

// Status is the full supervisor status surfaced via /api/supervisor/status.
type Status struct {
	State       State        `json:"state"`
	Reason      string       `json:"reason,omitempty"`
	Detail      string       `json:"detail,omitempty"`
	Since       time.Time    `json:"since"`
	Engine      EngineStatus `json:"engine"`
	Provisioned bool         `json:"provisioned"`
}

// Tracker is a concurrency-safe holder of the current Status.
type Tracker struct {
	mu     sync.RWMutex
	status Status
}

func NewTracker() *Tracker {
	return &Tracker{status: Status{State: StateStarting, Since: time.Now()}}
}

func (t *Tracker) Get() Status {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.status
}

func (t *Tracker) Set(s State) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.status.State != s {
		t.status.Since = time.Now()
	}
	t.status.State = s
	t.status.Reason = ""
	t.status.Detail = ""
}

func (t *Tracker) Fail(reason, detail string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.State = StateFailed
	t.status.Reason = reason
	t.status.Detail = detail
	t.status.Since = time.Now()
}

// ClaimFromFailed atomically transitions failed→starting, returning true only
// for the caller that won the transition. This lets concurrent Retry() calls
// race safely without both relaunching the engine.
func (t *Tracker) ClaimFromFailed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.status.State != StateFailed {
		return false
	}
	t.status.State = StateStarting
	t.status.Reason = ""
	t.status.Detail = ""
	t.status.Since = time.Now()
	return true
}

func (t *Tracker) SetEngine(pid int, healthy bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.Engine = EngineStatus{PID: pid, Healthy: healthy}
}

func (t *Tracker) SetProvisioned(v bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.Provisioned = v
}
