package rules

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/timeplus"
)

// ── Test doubles ─────────────────────────────────────────────────────────────────

// recorder is a shared, ordered op log written by both the fake store and the
// fake MV surface, so tests can assert the full validate→replace→persist sequence
// (including where "append" falls relative to the MV ops) in one place.
type recorder struct {
	mu  sync.Mutex
	log []string
}

func (r *recorder) add(s string) {
	r.mu.Lock()
	r.log = append(r.log, s)
	r.mu.Unlock()
}

func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.log...)
}

type fakeStore struct {
	rec       *recorder
	mu        sync.Mutex
	latest    map[string]RuleInstance
	appended  []RuleInstance
	appendErr func(inst RuleInstance) error
	latestErr error
}

func newFakeStore(rec *recorder) *fakeStore {
	return &fakeStore{rec: rec, latest: map[string]RuleInstance{}}
}

func (s *fakeStore) LatestSnapshot(id string) (RuleInstance, bool, error) {
	if s.latestErr != nil {
		return RuleInstance{}, false, s.latestErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.latest[id]
	return inst, ok, nil
}

func (s *fakeStore) ListCurrent() ([]RuleInstance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]RuleInstance, 0, len(s.latest))
	for _, v := range s.latest {
		if !v.Deleted {
			out = append(out, v)
		}
	}
	return out, nil
}

func (s *fakeStore) Append(inst RuleInstance) error {
	s.rec.add("append")
	if s.appendErr != nil {
		if e := s.appendErr(inst); e != nil {
			return e
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appended = append(s.appended, inst)
	s.latest[inst.RuleID] = inst
	return nil
}

type fakeMV struct {
	rec     *recorder
	mu      sync.Mutex
	counts  map[string]int
	creates []string // rendered SQL passed to each CreateMVStrict, in order
	errOn   func(op string, n int) error
	exists  map[string]bool
	failed  map[string]timeplus.FailedMV
}

func newFakeMV(rec *recorder) *fakeMV {
	return &fakeMV{rec: rec, counts: map[string]int{}, exists: map[string]bool{}}
}

func (f *fakeMV) hit(op string) error {
	f.mu.Lock()
	f.counts[op]++
	n := f.counts[op]
	f.mu.Unlock()
	f.rec.add(op)
	if f.errOn != nil {
		return f.errOn(op, n)
	}
	return nil
}

func (f *fakeMV) Preflight(ruleID, renderedSQL string) error { return f.hit("preflight") }
func (f *fakeMV) ApplyUDFs(udfs []string) error              { return f.hit("applyUDFs") }

func (f *fakeMV) DropMVNoDelay(ruleID string) error {
	err := f.hit("drop")
	if err == nil {
		f.mu.Lock()
		f.exists[ruleID] = false
		f.mu.Unlock()
	}
	return err
}

func (f *fakeMV) CreateMVStrict(ruleID, renderedSQL string) error {
	f.mu.Lock()
	f.creates = append(f.creates, renderedSQL)
	f.mu.Unlock()
	err := f.hit("create")
	if err == nil {
		f.mu.Lock()
		f.exists[ruleID] = true
		f.mu.Unlock()
	}
	return err
}

func (f *fakeMV) MVExists(ruleID string) (bool, error) {
	if err := f.hit("mvexists"); err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exists[ruleID], nil
}

func (f *fakeMV) FailedMVs() (map[string]timeplus.FailedMV, error) {
	if err := f.hit("failedmvs"); err != nil {
		return nil, err
	}
	return f.failed, nil
}

type fakeDefs struct {
	defs   map[string]catalog.Definition
	ownErr error
}

func (d fakeDefs) Lookup(id string) (catalog.Definition, bool) {
	def, ok := d.defs[id]
	return def, ok
}
func (d fakeDefs) ValidateUDFOwnership(def catalog.Definition) error { return d.ownErr }

func seqIDGen() func() (string, error) {
	var mu sync.Mutex
	var n int
	return func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		return fmt.Sprintf("snap-%d", n), nil
	}
}

func newTestService(defs *fakeDefs) (*Service, *fakeStore, *fakeMV, *recorder) {
	rec := &recorder{}
	st := newFakeStore(rec)
	mv := newFakeMV(rec)
	svc := &Service{
		store:   st,
		catalog: defs,
		mv:      mv,
		locks:   newKeyedMutex(),
		genID:   seqIDGen(),
	}
	return svc, st, mv, rec
}

// ── Definition / instance builders ───────────────────────────────────────────────

func f64(v float64) *float64 { return &v }

func testDef(id, version string, params ...catalog.Parameter) catalog.Definition {
	var b strings.Builder
	b.WriteString("SELECT rule_id FROM agentguard_hook_events WHERE true")
	for _, p := range params {
		b.WriteString(" AND v = {{ .Config." + p.Key + " }}")
	}
	return catalog.Definition{
		ID:                id,
		Name:              "Name " + id,
		Version:           version,
		Description:       "desc " + id,
		Severity:          "warning",
		Category:          "test",
		FPRate:            "low",
		HookSources:       "PreToolUse",
		RequiredTelemetry: "hooks",
		SQLTemplate:       b.String(),
		Parameters:        params,
		Origin:            catalog.OriginBuiltin,
	}
}

func intParam(id string, def int64, min, max *float64) catalog.Parameter {
	return catalog.Parameter{Key: id, Label: id, Type: "integer", Default: def, Min: min, Max: max}
}

// installedInst fabricates a current snapshot as the store would return it
// (definition_hash bound to def; overrides as json.Number, mirroring a store
// read decoded with UseNumber).
func installedInst(def catalog.Definition, enabled bool, rev uint64, overrides map[string]any, udfs []string) RuleInstance {
	return RuleInstance{
		RuleID:            def.ID,
		SnapshotID:        "s0",
		Revision:          rev,
		Name:              def.Name,
		DefinitionVersion: def.Version,
		DefinitionHash:    def.Hash(),
		Description:       def.Description,
		RenderedSQL:       "RENDERED-" + def.ID,
		DeployedUDFs:      udfs,
		Overrides:         overrides,
		Enabled:           enabled,
		Severity:          def.Severity,
		Category:          def.Category,
		RequiredTelemetry: def.RequiredTelemetry,
		FPRate:            def.FPRate,
		HookSources:       def.HookSources,
		BlockPolicy:       "log_only",
	}
}

func wantLog(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("op log mismatch:\n got: %v\nwant: %v", got, want)
	}
}

// ── Install ──────────────────────────────────────────────────────────────────────

func TestInstall_DefaultsOrderingAndSnapshot(t *testing.T) {
	def := testDef("rp-001", "1.0.0")
	svc, st, _, rec := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})

	inst, err := svc.Install("rp-001", nil, "alice", "install")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	// New install: preflight (apply candidate UDFs, validate) then deploy
	// (apply, drop stale, create), then append last.
	wantLog(t, rec.snapshot(), []string{"applyUDFs", "preflight", "applyUDFs", "drop", "create", "append"})
	if inst.Revision != 1 || !inst.Enabled || inst.BlockPolicy != "log_only" {
		t.Fatalf("unexpected install state: rev=%d enabled=%v policy=%q", inst.Revision, inst.Enabled, inst.BlockPolicy)
	}
	if inst.DefinitionHash != def.Hash() {
		t.Errorf("definition hash = %q, want %q", inst.DefinitionHash, def.Hash())
	}
	if inst.RenderedSQL != def.SQLTemplate {
		t.Errorf("rendered SQL = %q, want template verbatim %q", inst.RenderedSQL, def.SQLTemplate)
	}
	if inst.UpdatedBy != "alice" || inst.UpdateReason != "install" {
		t.Errorf("audit metadata not carried: by=%q reason=%q", inst.UpdatedBy, inst.UpdateReason)
	}
	if len(st.appended) != 1 {
		t.Fatalf("expected exactly one appended snapshot, got %d", len(st.appended))
	}
}

func TestInstall_WithOverridesRenders(t *testing.T) {
	def := testDef("rp-001", "1.0.0", intParam("t", 15, f64(1), f64(100)))
	svc, _, mv, _ := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})

	inst, err := svc.Install("rp-001", map[string]any{"t": 20}, "alice", "install")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if got, ok := inst.Overrides["t"].(int64); !ok || got != 20 {
		t.Fatalf("override not normalized to int64(20): %#v", inst.Overrides["t"])
	}
	if !strings.Contains(inst.RenderedSQL, "= 20") {
		t.Errorf("rendered SQL missing override value: %q", inst.RenderedSQL)
	}
	if len(mv.creates) != 1 || mv.creates[0] != inst.RenderedSQL {
		t.Errorf("MV created with wrong SQL: %v", mv.creates)
	}
}

func TestInstall_RejectsWhenAlreadyInstalled(t *testing.T) {
	def := testDef("rp-001", "1.0.0")
	svc, st, _, rec := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	st.latest["rp-001"] = installedInst(def, true, 3, nil, nil)

	_, err := svc.Install("rp-001", nil, "alice", "install")
	if !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("want ErrAlreadyInstalled, got %v", err)
	}
	if len(rec.snapshot()) != 0 {
		t.Errorf("no Timeplus work should run when already installed, got %v", rec.snapshot())
	}
}

func TestInstall_ReinstallOverDeletedIsMonotonic(t *testing.T) {
	def := testDef("rp-001", "1.0.0")
	svc, st, _, _ := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	deleted := installedInst(def, false, 4, nil, nil)
	deleted.Deleted = true
	st.latest["rp-001"] = deleted

	inst, err := svc.Install("rp-001", nil, "alice", "reinstall")
	if err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if inst.Revision != 5 {
		t.Fatalf("reinstall revision = %d, want 5 (monotonic over deleted)", inst.Revision)
	}
}

func TestInstall_UDFOwnershipCheckedBeforeAnyWork(t *testing.T) {
	def := testDef("rp-001", "1.0.0")
	svc, _, _, rec := newTestService(&fakeDefs{
		defs:   map[string]catalog.Definition{"rp-001": def},
		ownErr: errors.New("udf owned by rp-099"),
	})
	_, err := svc.Install("rp-001", nil, "alice", "install")
	if err == nil || !strings.Contains(err.Error(), "udf owned") {
		t.Fatalf("want UDF ownership error, got %v", err)
	}
	if len(rec.snapshot()) != 0 {
		t.Errorf("ownership failure must precede all Timeplus work, got %v", rec.snapshot())
	}
}

func TestInstall_NotInCatalog(t *testing.T) {
	svc, _, _, _ := newTestService(&fakeDefs{defs: map[string]catalog.Definition{}})
	if _, err := svc.Install("nope", nil, "a", "r"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestInstall_AppendFailureDropsMV(t *testing.T) {
	def := testDef("rp-001", "1.0.0")
	svc, st, _, rec := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	st.appendErr = func(RuleInstance) error { return errors.New("append boom") }

	_, err := svc.Install("rp-001", nil, "alice", "install")
	if err == nil || !strings.Contains(err.Error(), "append boom") {
		t.Fatalf("want append error, got %v", err)
	}
	// The just-created MV must be dropped after the failed append.
	log := rec.snapshot()
	if log[len(log)-1] != "drop" {
		t.Errorf("expected trailing rollback drop, got %v", log)
	}
	if len(st.appended) != 0 {
		t.Errorf("no snapshot should persist on append failure")
	}
}

// ── Configure ────────────────────────────────────────────────────────────────────

func TestConfigure_EnabledReplacesMVThenAppends(t *testing.T) {
	def := testDef("rp-001", "1.0.0", intParam("t", 15, f64(1), f64(100)))
	svc, st, mv, rec := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	st.latest["rp-001"] = installedInst(def, true, 2, map[string]any{"t": json.Number("15")}, []string{"udf-old"})

	inst, err := svc.Configure("rp-001", map[string]any{"t": 30}, "bob", "tune")
	if err != nil {
		t.Fatalf("configure: %v", err)
	}
	// preflight (apply cand, validate, restore prior) → replace (drop, apply, create) → append.
	wantLog(t, rec.snapshot(), []string{"applyUDFs", "preflight", "applyUDFs", "drop", "applyUDFs", "create", "append"})
	if inst.Revision != 3 {
		t.Errorf("revision = %d, want 3", inst.Revision)
	}
	if got, _ := inst.Overrides["t"].(int64); got != 30 {
		t.Errorf("override t = %#v, want int64(30)", inst.Overrides["t"])
	}
	if len(mv.creates) != 1 || mv.creates[0] != inst.RenderedSQL {
		t.Errorf("MV not created with new rendered SQL: %v", mv.creates)
	}
}

func TestConfigure_DisabledPreflightsAndAppendsNoMV(t *testing.T) {
	def := testDef("rp-001", "1.0.0", intParam("t", 15, f64(1), f64(100)))
	svc, st, _, rec := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	st.latest["rp-001"] = installedInst(def, false, 2, nil, []string{"udf-old"})

	if _, err := svc.Configure("rp-001", map[string]any{"t": 30}, "bob", "tune"); err != nil {
		t.Fatalf("configure: %v", err)
	}
	// No drop/create — disabled rules have no live MV.
	wantLog(t, rec.snapshot(), []string{"applyUDFs", "preflight", "applyUDFs", "append"})
}

func TestConfigure_InvalidOverrideWritesNothing(t *testing.T) {
	def := testDef("rp-001", "1.0.0", intParam("t", 15, f64(1), f64(100)))
	svc, st, _, rec := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	st.latest["rp-001"] = installedInst(def, true, 2, nil, nil)

	_, err := svc.Configure("rp-001", map[string]any{"t": 999}, "bob", "tune")
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ValidationError, got %v", err)
	}
	if len(rec.snapshot()) != 0 {
		t.Errorf("invalid configuration must perform no MV or snapshot write, got %v", rec.snapshot())
	}
}

func TestConfigure_DefinitionChangedConflicts(t *testing.T) {
	def := testDef("rp-001", "1.0.0", intParam("t", 15, f64(1), f64(100)))
	svc, st, _, rec := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	cur := installedInst(def, true, 2, nil, nil)
	cur.DefinitionHash = "stale-hash"
	st.latest["rp-001"] = cur

	_, err := svc.Configure("rp-001", map[string]any{"t": 30}, "bob", "tune")
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("want *ConflictError on definition drift, got %v", err)
	}
	if len(rec.snapshot()) != 0 {
		t.Errorf("definition drift must write nothing, got %v", rec.snapshot())
	}
}

func TestConfigure_NotInstalled(t *testing.T) {
	def := testDef("rp-001", "1.0.0")
	svc, _, _, _ := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	if _, err := svc.Configure("rp-001", nil, "b", "r"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestConfigure_MVCreateFailureRestoresPreviousMV(t *testing.T) {
	def := testDef("rp-001", "1.0.0", intParam("t", 15, f64(1), f64(100)))
	svc, st, mv, rec := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	cur := installedInst(def, true, 2, map[string]any{"t": json.Number("15")}, []string{"udf-old"})
	st.latest["rp-001"] = cur
	// Fail only the candidate (first) create; the rollback recreate must succeed.
	mv.errOn = func(op string, n int) error {
		if op == "create" && n == 1 {
			return errors.New("syntax boom")
		}
		return nil
	}

	_, err := svc.Configure("rp-001", map[string]any{"t": 30}, "bob", "tune")
	if err == nil || !strings.Contains(err.Error(), "syntax boom") {
		t.Fatalf("want create error surfaced, got %v", err)
	}
	var crit *CriticalError
	if errors.As(err, &crit) {
		t.Fatalf("rollback succeeded, must not be CriticalError: %v", err)
	}
	if len(st.appended) != 0 {
		t.Errorf("candidate snapshot must not be appended on MV failure")
	}
	// The previous MV is rebuilt from the prior rendered SQL.
	if len(mv.creates) != 2 || mv.creates[1] != cur.RenderedSQL {
		t.Errorf("rollback did not recreate previous MV from prior SQL: %v", mv.creates)
	}
	if strings.Contains(strings.Join(rec.snapshot(), ","), "append") {
		t.Errorf("no append should occur: %v", rec.snapshot())
	}
}

func TestConfigure_AppendFailureAfterReplacementRestoresPreviousMV(t *testing.T) {
	def := testDef("rp-001", "1.0.0", intParam("t", 15, f64(1), f64(100)))
	svc, st, mv, _ := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	cur := installedInst(def, true, 2, map[string]any{"t": json.Number("15")}, []string{"udf-old"})
	st.latest["rp-001"] = cur
	st.appendErr = func(RuleInstance) error { return errors.New("append boom") }

	_, err := svc.Configure("rp-001", map[string]any{"t": 30}, "bob", "tune")
	if err == nil || !strings.Contains(err.Error(), "append boom") {
		t.Fatalf("want append error, got %v", err)
	}
	if len(st.appended) != 0 {
		t.Errorf("no snapshot should persist")
	}
	// candidate create + rollback recreate(prior SQL).
	if len(mv.creates) != 2 || mv.creates[1] != cur.RenderedSQL {
		t.Errorf("previous MV not restored after append failure: %v", mv.creates)
	}
}

func TestConfigure_CriticalErrorWhenRollbackFails(t *testing.T) {
	def := testDef("rp-001", "1.0.0", intParam("t", 15, f64(1), f64(100)))
	svc, st, mv, _ := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	st.latest["rp-001"] = installedInst(def, true, 2, map[string]any{"t": json.Number("15")}, []string{"udf-old"})
	// Candidate create fails AND the rollback recreate also fails.
	mv.errOn = func(op string, n int) error {
		if op == "create" {
			return errors.New("create always fails")
		}
		return nil
	}

	_, err := svc.Configure("rp-001", map[string]any{"t": 30}, "bob", "tune")
	var crit *CriticalError
	if !errors.As(err, &crit) {
		t.Fatalf("want *CriticalError when rollback also fails, got %v", err)
	}
	if crit.Op != "configure" || crit.RuleID != "rp-001" {
		t.Errorf("critical error mislabeled: %+v", crit)
	}
}

// ── Preflight UDF restore consistency ─────────────────────────────────────────────

// A failed UDF restore leaves global UDFs mutated while the snapshot still points
// at the previous deployment, so it must surface as a *CriticalError — not a log
// line — whether the trigger was a candidate-apply failure or a validation failure.
func TestPreflight_RestoreFailureAfterValidationFailureIsCritical(t *testing.T) {
	def := testDef("rp-001", "1.0.0", intParam("t", 15, f64(1), f64(100)))
	svc, st, mv, _ := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	st.latest["rp-001"] = installedInst(def, true, 2, map[string]any{"t": json.Number("15")}, []string{"udf-old"})
	// candidate apply (n=1) ok; validation fails; restore apply (n=2) fails.
	mv.errOn = func(op string, n int) error {
		if op == "preflight" {
			return errors.New("validate boom")
		}
		if op == "applyUDFs" && n == 2 {
			return errors.New("restore boom")
		}
		return nil
	}
	_, err := svc.Configure("rp-001", map[string]any{"t": 30}, "bob", "tune")
	var crit *CriticalError
	if !errors.As(err, &crit) {
		t.Fatalf("want *CriticalError when UDF restore fails, got %v", err)
	}
	if crit.Op != "preflight" || crit.RollbackErr == nil {
		t.Errorf("critical error mislabeled or missing rollback cause: %+v", crit)
	}
	if len(st.appended) != 0 {
		t.Errorf("no snapshot must persist when preflight is critical")
	}
}

func TestPreflight_RestoreFailureAfterApplyFailureIsCritical(t *testing.T) {
	def := testDef("rp-001", "1.0.0", intParam("t", 15, f64(1), f64(100)))
	svc, st, mv, _ := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	st.latest["rp-001"] = installedInst(def, true, 2, map[string]any{"t": json.Number("15")}, []string{"udf-old"})
	// candidate apply (n=1) fails; restore apply (n=2) also fails.
	mv.errOn = func(op string, n int) error {
		if op == "applyUDFs" {
			return errors.New("udf boom")
		}
		return nil
	}
	_, err := svc.Configure("rp-001", map[string]any{"t": 30}, "bob", "tune")
	var crit *CriticalError
	if !errors.As(err, &crit) {
		t.Fatalf("want *CriticalError when candidate apply and restore both fail, got %v", err)
	}
	if crit.Op != "preflight" {
		t.Errorf("critical error mislabeled: %+v", crit)
	}
}

// Control: a validation failure with a SUCCESSFUL restore stays a plain error —
// the escalation is keyed on the restore failure, not on validation failing.
func TestPreflight_ValidationFailureWithCleanRestoreIsNotCritical(t *testing.T) {
	def := testDef("rp-001", "1.0.0", intParam("t", 15, f64(1), f64(100)))
	svc, st, mv, _ := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	// installedInst with deployed UDFs so the restore path is exercised.
	st.latest["rp-001"] = installedInst(def, true, 2, map[string]any{"t": json.Number("15")}, []string{"udf-old"})
	mv.errOn = func(op string, n int) error {
		if op == "preflight" {
			return errors.New("validate boom")
		}
		return nil // restore succeeds
	}
	_, err := svc.Configure("rp-001", map[string]any{"t": 30}, "bob", "tune")
	if err == nil || !strings.Contains(err.Error(), "validate boom") {
		t.Fatalf("want the validation error surfaced, got %v", err)
	}
	var crit *CriticalError
	if errors.As(err, &crit) {
		t.Fatalf("clean restore must not be CriticalError: %v", err)
	}
}

// ── Toggle ───────────────────────────────────────────────────────────────────────

func TestToggle_DisablePreservesRenderedSQLAndOverrides(t *testing.T) {
	def := testDef("rp-001", "1.0.0", intParam("t", 15, f64(1), f64(100)))
	svc, st, _, rec := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	cur := installedInst(def, true, 2, map[string]any{"t": json.Number("30")}, []string{"udf"})
	st.latest["rp-001"] = cur

	inst, err := svc.Toggle("rp-001", "carol", "disable")
	if err != nil {
		t.Fatalf("toggle: %v", err)
	}
	wantLog(t, rec.snapshot(), []string{"drop", "append"})
	if inst.Enabled {
		t.Errorf("rule should be disabled")
	}
	if inst.RenderedSQL != cur.RenderedSQL || !reflect.DeepEqual(inst.Overrides, cur.Overrides) {
		t.Errorf("toggle must preserve rendered SQL and overrides")
	}
	if inst.Revision != 3 {
		t.Errorf("revision = %d, want 3", inst.Revision)
	}
}

func TestToggle_EnableDeploysFromSnapshot(t *testing.T) {
	def := testDef("rp-001", "1.0.0")
	svc, st, mv, rec := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	cur := installedInst(def, false, 2, nil, []string{"udf"})
	st.latest["rp-001"] = cur

	inst, err := svc.Toggle("rp-001", "carol", "enable")
	if err != nil {
		t.Fatalf("toggle: %v", err)
	}
	wantLog(t, rec.snapshot(), []string{"applyUDFs", "drop", "create", "append"})
	if !inst.Enabled {
		t.Errorf("rule should be enabled")
	}
	if len(mv.creates) != 1 || mv.creates[0] != cur.RenderedSQL {
		t.Errorf("enable must deploy snapshot rendered SQL: %v", mv.creates)
	}
}

func TestToggle_AppendFailureOnDisableRestoresMV(t *testing.T) {
	def := testDef("rp-001", "1.0.0")
	svc, st, mv, _ := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	cur := installedInst(def, true, 2, nil, []string{"udf"})
	st.latest["rp-001"] = cur
	st.appendErr = func(RuleInstance) error { return errors.New("append boom") }

	_, err := svc.Toggle("rp-001", "carol", "disable")
	if err == nil || !strings.Contains(err.Error(), "append boom") {
		t.Fatalf("want append error, got %v", err)
	}
	// MV was dropped to disable, then recreated on rollback.
	if len(mv.creates) != 1 || mv.creates[0] != cur.RenderedSQL {
		t.Errorf("MV not restored after failed disable append: %v", mv.creates)
	}
}

func TestToggle_AppendFailureOnEnableDropsMV(t *testing.T) {
	def := testDef("rp-001", "1.0.0")
	svc, st, mv, rec := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	cur := installedInst(def, false, 2, nil, []string{"udf"})
	st.latest["rp-001"] = cur
	st.appendErr = func(RuleInstance) error { return errors.New("append boom") }

	_, err := svc.Toggle("rp-001", "carol", "enable")
	if err == nil || !strings.Contains(err.Error(), "append boom") {
		t.Fatalf("want append error, got %v", err)
	}
	var crit *CriticalError
	if errors.As(err, &crit) {
		t.Fatalf("rollback drop succeeded, must not be CriticalError: %v", err)
	}
	// The just-created MV must be dropped on rollback, leaving none deployed.
	if mv.exists["rp-001"] {
		t.Errorf("MV should have been dropped after failed enable append")
	}
	// Sequence: deploy (apply, drop, create) → append(fail) → rollback drop.
	log := rec.snapshot()
	if log[len(log)-1] != "drop" {
		t.Errorf("expected trailing rollback drop, got %v", log)
	}
}

// ── SetPolicy ────────────────────────────────────────────────────────────────────

func TestSetPolicy_SnapshotOnly(t *testing.T) {
	def := testDef("rp-001", "1.0.0")
	svc, st, _, rec := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	st.latest["rp-001"] = installedInst(def, true, 2, nil, nil)

	inst, err := svc.SetPolicy("rp-001", "auto_block", "dan", "raise")
	if err != nil {
		t.Fatalf("set policy: %v", err)
	}
	wantLog(t, rec.snapshot(), []string{"append"})
	if inst.BlockPolicy != "auto_block" || !inst.Enabled || inst.Revision != 3 {
		t.Errorf("unexpected: policy=%q enabled=%v rev=%d", inst.BlockPolicy, inst.Enabled, inst.Revision)
	}
}

func TestSetPolicy_InvalidRejected(t *testing.T) {
	def := testDef("rp-001", "1.0.0")
	svc, st, _, rec := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	st.latest["rp-001"] = installedInst(def, true, 2, nil, nil)

	_, err := svc.SetPolicy("rp-001", "nuke", "dan", "raise")
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ValidationError, got %v", err)
	}
	if len(rec.snapshot()) != 0 {
		t.Errorf("invalid policy must write nothing")
	}
}

// ── Upgrade ──────────────────────────────────────────────────────────────────────

func TestUpgrade_PreservesCompatibleOverrides(t *testing.T) {
	v1 := testDef("rp-001", "1.0.0", intParam("t", 15, f64(1), f64(1000)))
	v2 := testDef("rp-001", "2.0.0", intParam("t", 15, f64(1), f64(1000)))
	v2.Description = "v2 desc" // force a different hash
	svc, st, _, rec := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": v2}})
	st.latest["rp-001"] = installedInst(v1, true, 2, map[string]any{"t": json.Number("30")}, nil)

	inst, err := svc.Upgrade("rp-001", "erin", "upgrade")
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if inst.DefinitionVersion != "2.0.0" || inst.DefinitionHash != v2.Hash() {
		t.Errorf("instance not moved to v2: version=%q", inst.DefinitionVersion)
	}
	if got, _ := inst.Overrides["t"].(int64); got != 30 {
		t.Errorf("compatible override not preserved: %#v", inst.Overrides["t"])
	}
	// Replacement path (enabled): preflight → drop/apply/create → append.
	wantLog(t, rec.snapshot(), []string{"applyUDFs", "preflight", "drop", "applyUDFs", "create", "append"})
}

func TestUpgrade_BlocksIncompatibleOverrides(t *testing.T) {
	v1 := testDef("rp-001", "1.0.0", intParam("t", 15, f64(1), f64(1000)))
	v2 := testDef("rp-001", "2.0.0", intParam("t", 15, f64(1), f64(100))) // tightened max
	svc, st, _, rec := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": v2}})
	st.latest["rp-001"] = installedInst(v1, true, 2, map[string]any{"t": json.Number("800")}, nil)

	_, err := svc.Upgrade("rp-001", "erin", "upgrade")
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("want *ConflictError, got %v", err)
	}
	if len(ce.Conflicts) == 0 {
		t.Errorf("conflict detail should be populated")
	}
	if len(rec.snapshot()) != 0 {
		t.Errorf("blocked upgrade must perform no Timeplus work, got %v", rec.snapshot())
	}
}

func TestUpgrade_NoopWhenHashMatches(t *testing.T) {
	def := testDef("rp-001", "1.0.0")
	svc, st, _, rec := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	st.latest["rp-001"] = installedInst(def, true, 2, nil, nil)

	inst, err := svc.Upgrade("rp-001", "erin", "upgrade")
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if inst.Revision != 2 {
		t.Errorf("no-op upgrade should return current instance (rev 2), got %d", inst.Revision)
	}
	if len(rec.snapshot()) != 0 {
		t.Errorf("no-op upgrade must do nothing, got %v", rec.snapshot())
	}
}

// ── Uninstall ────────────────────────────────────────────────────────────────────

func TestUninstall_DropsMVAndSoftDeletes(t *testing.T) {
	def := testDef("rp-001", "1.0.0")
	svc, st, _, rec := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	st.latest["rp-001"] = installedInst(def, true, 2, nil, nil)

	inst, err := svc.Uninstall("rp-001", "frank", "remove")
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	wantLog(t, rec.snapshot(), []string{"drop", "append"})
	if !inst.Deleted || inst.Revision != 3 {
		t.Errorf("unexpected: deleted=%v rev=%d", inst.Deleted, inst.Revision)
	}
}

func TestUninstall_AppendFailureRestoresMV(t *testing.T) {
	def := testDef("rp-001", "1.0.0")
	svc, st, mv, _ := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	cur := installedInst(def, true, 2, nil, nil)
	st.latest["rp-001"] = cur
	st.appendErr = func(RuleInstance) error { return errors.New("append boom") }

	_, err := svc.Uninstall("rp-001", "frank", "remove")
	if err == nil || !strings.Contains(err.Error(), "append boom") {
		t.Fatalf("want append error, got %v", err)
	}
	if len(mv.creates) != 1 || mv.creates[0] != cur.RenderedSQL {
		t.Errorf("MV not restored after failed uninstall append: %v", mv.creates)
	}
}

// ── Sync ─────────────────────────────────────────────────────────────────────────

func TestSync_ReconcilesMissingAndOrphanedMVs(t *testing.T) {
	a := testDef("rp-a", "1.0.0")
	b := testDef("rp-b", "1.0.0")
	svc, st, mv, _ := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-a": a, "rp-b": b}})
	st.latest["rp-a"] = installedInst(a, true, 1, nil, nil)  // enabled, MV missing
	st.latest["rp-b"] = installedInst(b, false, 1, nil, nil) // disabled, MV present
	mv.exists["rp-a"] = false
	mv.exists["rp-b"] = true

	results, err := svc.Sync()
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	byID := map[string]string{}
	for _, r := range results {
		byID[r.RuleID] = r.Action
	}
	if byID["rp-a"] != "created" {
		t.Errorf("enabled rule with missing MV: action=%q, want created", byID["rp-a"])
	}
	if byID["rp-b"] != "dropped" {
		t.Errorf("disabled rule with present MV: action=%q, want dropped", byID["rp-b"])
	}
}

// TestSync_ReplacesEnabledMVToGuaranteeFreshSQL pins the force-replace contract:
// an enabled rule whose MV already exists is dropped and recreated from the
// snapshot's rendered_sql, so a stale MV running drifted SQL (the state a
// CriticalError can leave, and the one its recovery message says to fix with
// /api/rules/sync) is actually repaired. Existence-only reconciliation could not.
func TestSync_ReplacesEnabledMVToGuaranteeFreshSQL(t *testing.T) {
	a := testDef("rp-a", "1.0.0")
	svc, st, mv, _ := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-a": a}})
	st.latest["rp-a"] = installedInst(a, true, 1, nil, nil)
	mv.exists["rp-a"] = true

	results, err := svc.Sync()
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(results) != 1 || results[0].Action != "replaced" {
		t.Fatalf("enabled rule with present MV should be replaced, got %+v", results)
	}
	if mv.counts["drop"] != 1 || mv.counts["create"] != 1 {
		t.Errorf("force replace should drop once and create once, got drop=%d create=%d",
			mv.counts["drop"], mv.counts["create"])
	}
	if len(mv.creates) != 1 || mv.creates[0] != "RENDERED-rp-a" {
		t.Errorf("recreated MV must use the snapshot rendered_sql, got %+v", mv.creates)
	}
}

// TestSync_PreflightFailureLeavesExistingMVIntact pins the validate-first guard:
// if the snapshot SQL fails preflight, force-replace must NOT drop the existing
// (healthy) MV, so sync can never turn a working MV into no MV.
func TestSync_PreflightFailureLeavesExistingMVIntact(t *testing.T) {
	a := testDef("rp-a", "1.0.0")
	svc, st, mv, _ := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-a": a}})
	st.latest["rp-a"] = installedInst(a, true, 1, nil, nil)
	mv.exists["rp-a"] = true
	mv.errOn = func(op string, n int) error {
		if op == "preflight" {
			return errors.New("candidate SQL does not build")
		}
		return nil
	}

	results, err := svc.Sync()
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(results) != 1 || results[0].Action != "preflight_failed" {
		t.Fatalf("preflight failure should report preflight_failed, got %+v", results)
	}
	if mv.counts["drop"] != 0 || mv.counts["create"] != 0 {
		t.Errorf("a healthy MV must not be touched when preflight fails, got drop=%d create=%d",
			mv.counts["drop"], mv.counts["create"])
	}
}

// ── Concurrency / serialization ──────────────────────────────────────────────────

func TestService_ConcurrentMutationsSerializeWithMonotonicRevisions(t *testing.T) {
	def := testDef("rp-001", "1.0.0")
	svc, st, _, _ := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}})
	st.latest["rp-001"] = installedInst(def, true, 1, nil, nil)

	const n = 25
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			// SetPolicy is snapshot-only, so revision computation is the only
			// shared state under test.
			_, _ = svc.SetPolicy("rp-001", "auto_block", "p", "concurrent")
		}()
	}
	wg.Wait()

	if len(st.appended) != n {
		t.Fatalf("expected %d appends, got %d", n, len(st.appended))
	}
	// NOTE on scope: fakeStore gives immediate read-after-write, so this exercises
	// the SERVICE's per-rule locking (without it the LatestSnapshot→Append
	// read-modify-write would race and produce duplicate revisions). Real-Timeplus
	// revision monotonicity additionally depends on an INSERT being visible to the
	// next table() read; that is a store/engine property verified live, not here.
	seen := map[uint64]bool{}
	ids := map[string]bool{}
	var max uint64
	for _, inst := range st.appended {
		if seen[inst.Revision] {
			t.Fatalf("duplicate revision %d — mutations were not serialized", inst.Revision)
		}
		seen[inst.Revision] = true
		if inst.SnapshotID == "" || ids[inst.SnapshotID] {
			t.Fatalf("snapshot_id must be present and unique, got %q", inst.SnapshotID)
		}
		ids[inst.SnapshotID] = true
		if inst.Revision > max {
			max = inst.Revision
		}
	}
	// Started at revision 1; n serialized appends end at 1+n.
	if max != uint64(1+n) {
		t.Errorf("max revision = %d, want %d", max, 1+n)
	}
}

// laggyStore models Proton's non-linearizable table() read: a just-appended
// snapshot is NOT visible to the next LatestSnapshot (visibility lags by one
// append). Without the read-your-writes cache this reproduces the live bug —
// rapid same-rule mutations read stale state and duplicate a revision / lose an
// update.
type laggyStore struct {
	appended []RuleInstance
}

func (s *laggyStore) Append(inst RuleInstance) error {
	s.appended = append(s.appended, inst)
	return nil
}

func (s *laggyStore) LatestSnapshot(id string) (RuleInstance, bool, error) {
	// Exclude the most recent append: it has not become visible yet.
	var found *RuleInstance
	for i := 0; i < len(s.appended)-1; i++ {
		if s.appended[i].RuleID == id {
			inst := s.appended[i]
			found = &inst
		}
	}
	if found == nil {
		return RuleInstance{}, false, nil
	}
	return *found, true, nil
}

func (s *laggyStore) ListCurrent() ([]RuleInstance, error) { return nil, nil }

func TestService_ReadYourWritesSurvivesLaggyStore(t *testing.T) {
	def := testDef("rp-001", "1.0.0")
	rec := &recorder{}
	laggy := &laggyStore{}
	mv := newFakeMV(rec)
	svc := &Service{
		store:   laggy,
		catalog: &fakeDefs{defs: map[string]catalog.Definition{"rp-001": def}},
		mv:      mv,
		locks:   newKeyedMutex(),
		genID:   seqIDGen(),
		cache:   newSnapshotCache(), // the fix under test
	}

	i1, err := svc.Install("rp-001", nil, "a", "install")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	// Toggle twice. With a laggy store and no cache, the second toggle would not
	// see the first's append → it would re-read the install snapshot, duplicate a
	// revision, and toggle the wrong direction. The cache must prevent that.
	t2, err := svc.Toggle("rp-001", "a", "toggle")
	if err != nil {
		t.Fatalf("toggle 1: %v", err)
	}
	t3, err := svc.Toggle("rp-001", "a", "toggle")
	if err != nil {
		t.Fatalf("toggle 2: %v", err)
	}

	if i1.Revision != 1 || t2.Revision != 2 || t3.Revision != 3 {
		t.Fatalf("revisions not contiguous: install=%d t1=%d t2=%d", i1.Revision, t2.Revision, t3.Revision)
	}
	if !i1.Enabled || t2.Enabled || !t3.Enabled {
		t.Fatalf("toggle direction wrong: install=%v t1=%v t2=%v (want true,false,true)", i1.Enabled, t2.Enabled, t3.Enabled)
	}
	// No duplicate revisions persisted.
	seen := map[uint64]bool{}
	for _, a := range laggy.appended {
		if seen[a.Revision] {
			t.Fatalf("duplicate revision %d persisted despite the cache", a.Revision)
		}
		seen[a.Revision] = true
	}
}

func TestKeyedMutex_SerializesSameKey(t *testing.T) {
	km := newKeyedMutex()
	var active, maxActive int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := km.lock("k")
			defer unlock()
			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			mu.Unlock()
			mu.Lock()
			active--
			mu.Unlock()
		}()
	}
	wg.Wait()
	if maxActive != 1 {
		t.Fatalf("max concurrent holders = %d, want 1", maxActive)
	}
}

// ── PlanUpgradeOverrides ─────────────────────────────────────────────────────────

func TestPlanUpgradeOverrides(t *testing.T) {
	t.Run("preserve same id", func(t *testing.T) {
		def := testDef("r", "2.0.0", intParam("t", 15, f64(1), f64(100)))
		res := PlanUpgradeOverrides(def, map[string]any{"t": json.Number("30")})
		if !res.OK() || len(res.Warnings) != 0 {
			t.Fatalf("unexpected outcome: %+v", res)
		}
		if got, _ := res.Overrides["t"].(int64); got != 30 {
			t.Errorf("preserved value = %#v, want int64(30)", res.Overrides["t"])
		}
	})

	t.Run("alias migrates", func(t *testing.T) {
		p := intParam("tnew", 15, f64(1), f64(100))
		p.Aliases = []string{"told"}
		def := testDef("r", "2.0.0", p)
		res := PlanUpgradeOverrides(def, map[string]any{"told": json.Number("30")})
		if !res.OK() || len(res.Warnings) != 0 {
			t.Fatalf("unexpected: %+v", res)
		}
		if got, _ := res.Overrides["tnew"].(int64); got != 30 {
			t.Errorf("alias not migrated: %#v", res.Overrides)
		}
	})

	t.Run("removed param warns", func(t *testing.T) {
		def := testDef("r", "2.0.0", intParam("t", 15, f64(1), f64(100)))
		res := PlanUpgradeOverrides(def, map[string]any{"gone": json.Number("5")})
		if len(res.Warnings) != 1 || len(res.Overrides) != 0 {
			t.Fatalf("removed param should warn and drop: %+v", res)
		}
	})

	t.Run("invalid value conflicts", func(t *testing.T) {
		def := testDef("r", "2.0.0", intParam("t", 15, f64(1), f64(10)))
		res := PlanUpgradeOverrides(def, map[string]any{"t": json.Number("99")})
		if res.OK() || len(res.Conflicts) != 1 {
			t.Fatalf("out-of-range value should conflict: %+v", res)
		}
	})

	t.Run("ambiguous alias conflicts", func(t *testing.T) {
		p1 := intParam("a", 1, f64(0), f64(100))
		p1.Aliases = []string{"x"}
		p2 := intParam("b", 1, f64(0), f64(100))
		p2.Aliases = []string{"x"}
		def := testDef("r", "2.0.0", p1, p2)
		res := PlanUpgradeOverrides(def, map[string]any{"x": json.Number("5")})
		if res.OK() || len(res.Conflicts) != 1 {
			t.Fatalf("ambiguous alias should conflict: %+v", res)
		}
	})
}
