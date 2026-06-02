package supervisor

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/config"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

const (
	healthInterval       = time.Second
	healthTimeout        = 90 * time.Second
	provisionDetailBytes = 2000
)

// Supervisor owns the child timeplusd lifecycle and the boot state machine.
type Supervisor struct {
	cfg   *config.Config
	mgr   *catalog.Manager
	creds TimeplusCredentials

	tracker *Tracker
	logs    *LogBuffer

	// Injectable phases (real impls set by New; overridden in unit tests).
	startChild    func(ctx context.Context) (Child, error)
	waitHealthyFn func(ctx context.Context) error
	provisionFn   func(emit func(status, msg string)) error
	setupComplete func() bool

	bootCtx context.Context
	fatal   chan error

	mu    sync.Mutex
	child Child
}

// New builds a Supervisor wired to real implementations. binPath/args describe
// how to launch timeplusd; creds supplies the credentials seam.
func New(cfg *config.Config, mgr *catalog.Manager, creds TimeplusCredentials, binPath string, args []string) *Supervisor {
	s := &Supervisor{
		cfg:     cfg,
		mgr:     mgr,
		creds:   creds,
		tracker: NewTracker(),
		logs:    NewLogBuffer(500),
		fatal:   make(chan error, 1),
	}
	s.startChild = func(ctx context.Context) (Child, error) {
		return startExecChild(ctx, binPath, args, s.logs)
	}
	s.waitHealthyFn = func(ctx context.Context) error {
		return waitHealthy(ctx, s.probe, healthInterval, healthTimeout)
	}
	s.provisionFn = s.provision
	s.setupComplete = func() bool {
		config.Mu.RLock()
		defer config.Mu.RUnlock()
		return s.cfg.SetupComplete
	}
	return s
}

// localCfg returns a TimeplusConfig pointed at the co-located child with the
// seam credentials.
func (s *Supervisor) localCfg() config.TimeplusConfig {
	return config.TimeplusConfig{
		Host:       "localhost",
		Port:       s.cfg.Timeplus.Port,
		NativePort: s.cfg.Timeplus.NativePort,
		User:       s.creds.User(),
		Password:   s.creds.Password(),
	}
}

func (s *Supervisor) probe() error {
	c := s.localCfg()
	_, err := tp.Query(&c, "SELECT 1 AS ok")
	return err
}

// provision mirrors handlers.Provision: run Init, persist config, update live cfg.
func (s *Supervisor) provision(emit func(status, msg string)) error {
	c := s.localCfg()
	if err := tp.Init(c, s.cfg.AgentGuardURL, s.mgr.All(), emit); err != nil {
		return err
	}
	newCfg := *s.cfg
	newCfg.SetupComplete = true
	if err := config.Save(&newCfg, s.cfg.ConfigPath); err != nil {
		return err
	}
	config.Mu.Lock()
	s.cfg.SetupComplete = true
	config.Mu.Unlock()
	return nil
}

// Boot runs the state machine in the calling goroutine. The HTTP server is
// already serving (started by runUp) before Boot is invoked.
func (s *Supervisor) Boot(ctx context.Context) {
	s.mu.Lock()
	s.bootCtx = ctx
	s.mu.Unlock()
	s.tracker.Set(StateStarting)

	child, err := s.startChild(ctx)
	if err != nil {
		s.tracker.Fail(ReasonEngineExited, err.Error())
		return
	}
	s.mu.Lock()
	s.child = child
	s.mu.Unlock()
	s.tracker.SetEngine(child.PID(), false)

	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()

	healthDone := make(chan error, 1)
	hctx, hcancel := context.WithCancel(ctx)
	go func() { healthDone <- s.waitHealthyFn(hctx) }()

	select {
	case err := <-exited:
		hcancel()
		s.tracker.Fail(ReasonEngineExited, errString(err, "timeplusd exited during startup"))
		return
	case err := <-healthDone:
		hcancel()
		if err != nil {
			s.stopChild()
			s.tracker.Fail(ReasonEngineUnhealthy, err.Error())
			return
		}
	}

	s.tracker.SetEngine(child.PID(), true)
	s.tracker.Set(StateHealthy)

	if !s.setupComplete() {
		s.tracker.Set(StateProvisioning)
		emit := func(status, msg string) {
			_, _ = s.logs.Write([]byte("[provision:" + status + "] " + msg + "\n"))
		}
		if err := s.provisionFn(emit); err != nil {
			detail := s.logs.Tail(provisionDetailBytes)
			if detail == "" {
				detail = err.Error()
			}
			s.stopChild()
			s.tracker.Fail(ReasonProvisionFailed, detail)
			return
		}
	}
	s.tracker.SetProvisioned(true)
	s.tracker.Set(StateReady)

	// Post-ready: a child exit is fatal (fail-fast; runUp exits non-zero).
	go func() {
		err := <-exited
		select {
		case s.fatal <- errString2(err):
		default:
		}
	}()
}

// Status returns the current status with redacted detail.
func (s *Supervisor) Status() Status { return s.tracker.Get() }

// Fatal emits once when a post-ready child exit occurs (fail-fast signal).
func (s *Supervisor) Fatal() <-chan error { return s.fatal }

// Retry re-runs Boot from a failed state (spec §5.7).
func (s *Supervisor) Retry() error {
	if !s.tracker.ClaimFromFailed() {
		return errors.New("retry is only valid from a failed state")
	}
	s.mu.Lock()
	ctx := s.bootCtx
	s.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	go s.Boot(ctx)
	return nil
}

// Stop terminates the child (used by runUp shutdown).
func (s *Supervisor) Stop(timeout time.Duration) {
	s.mu.Lock()
	child := s.child
	s.mu.Unlock()
	if child != nil {
		_ = child.Stop(timeout)
	}
}

// stopChild terminates the child if one is running (used on failure paths so a
// later Retry starts a clean engine rather than colliding on ports).
func (s *Supervisor) stopChild() {
	s.mu.Lock()
	child := s.child
	s.mu.Unlock()
	if child != nil {
		_ = child.Stop(5 * time.Second)
	}
}

func errString(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	return err.Error()
}

func errString2(err error) error {
	if err == nil {
		return errors.New("timeplusd exited")
	}
	return err
}
