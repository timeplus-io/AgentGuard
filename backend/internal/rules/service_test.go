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
	defs    map[string]catalog.Definition
	ownErr  error
	saveErr error    // when set, Save fails (publish failure)
	saved   []string // ordered IDs passed to Save (publish + rollback republish)
}

func (d *fakeDefs) Lookup(id string) (catalog.Definition, bool) {
	def, ok := d.defs[id]
	return def, ok
}
func (d *fakeDefs) ValidateUDFOwnership(def catalog.Definition) error { return d.ownErr }

// Save mirrors *catalog.Manager.Save faithfully enough for the rollback tests:
// it structurally validates the definition and, on success, stores it in the
// in-memory map (overwriting any previous revision under the same ID). saveErr
// models a publish failure. The ordered saved log lets tests assert that a
// failed update republishes the previous definition.
func (d *fakeDefs) Save(def catalog.Definition) error {
	if d.saveErr != nil {
		return d.saveErr
	}
	if err := def.Validate(); err != nil {
		return err
	}
	if d.defs == nil {
		d.defs = map[string]catalog.Definition{}
	}
	d.defs[def.ID] = def
	d.saved = append(d.saved, def.ID)
	return nil
}

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

// TestInstall_RejectsUDFOwnedByInstalledSnapshot verifies that Install rejects
// a new rule whose UDF name is already deployed by a DIFFERENT installed
// instance's DeployedUDFs, even when the catalog-level ownership check passes
// (because the prior rule's definition is absent from or changed in the catalog).
// This exercises the snapshot-ledger path added in Phase B (checkUDFOwnership).
func TestInstall_RejectsUDFOwnedByInstalledSnapshot(t *testing.T) {
	const udfDDL = "CREATE OR REPLACE FUNCTION ag_shared_fn(x string) RETURNS string LANGUAGE PYTHON AS $$...$$;"
	defs := &fakeDefs{defs: map[string]catalog.Definition{
		"rp-b": {
			ID: "rp-b", Name: "B", Version: "1",
			SQLTemplate: "SELECT rule_id FROM agentguard_hook_events WHERE true",
			UDFs:        []string{udfDDL},
		},
	}}
	svc, st, _, _ := newTestService(defs)
	// Seed rp-a as an installed, enabled instance that deployed ag_shared_fn.
	// The catalog has no entry for rp-a — simulating a rule whose definition was
	// removed/changed but whose live MV still depends on the UDF.
	st.latest["rp-a"] = RuleInstance{
		RuleID:       "rp-a",
		SnapshotID:   "s0",
		Revision:     1,
		Enabled:      true,
		DeployedUDFs: []string{udfDDL},
	}

	_, err := svc.Install("rp-b", nil, "tester", "t")
	if err == nil || !strings.Contains(err.Error(), "ag_shared_fn") {
		t.Fatalf("expected UDF-ownership rejection mentioning ag_shared_fn, got %v", err)
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

// ── ApplyUDFs / preflight non-destructive guarantee ──────────────────────────────

// TestConfigure_ApplyFailLeavesLiveMVAndSnapshotIntact locks the guarantee that
// if ApplyUDFs(candidate) fails during preflight, the live MV is never dropped
// and the persisted snapshot remains unchanged. Phase A's Configure preflights
// before replaceAndPersist, so this is a characterization/guard test that passes
// immediately (no RED phase per Task 8 decision 3).
func TestConfigure_ApplyFailLeavesLiveMVAndSnapshotIntact(t *testing.T) {
	def := testDef("rp-x", "1.0.0", intParam("threshold", 5, f64(1), f64(100)))
	svc, st, mv, _ := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-x": def}})
	cur := installedInst(def, true, 1, nil, nil)
	st.latest["rp-x"] = cur
	mv.exists["rp-x"] = true

	// Fail the first ApplyUDFs call (candidate apply in preflight).
	mv.errOn = func(op string, n int) error {
		if op == "applyUDFs" && n == 1 {
			return errors.New("udf apply boom")
		}
		return nil
	}

	appendsBefore := len(st.appended)
	_, err := svc.Configure("rp-x", map[string]any{"threshold": 9}, "t", "r")
	if err == nil {
		t.Fatal("expected error")
	}
	if mv.counts["drop"] != 0 {
		t.Fatalf("live MV was dropped before validation (%d drops); preflight must run before any destructive op", mv.counts["drop"])
	}
	if len(st.appended) != appendsBefore {
		t.Fatal("snapshot must be unchanged when preflight fails")
	}
}

// TestConfigure_CreateFailRestoresPreviousMV locks the rollback guarantee: if
// CreateMVStrict fails after the live MV has been dropped, rollback must
// recreate the previous MV from cur.RenderedSQL and leave the snapshot
// unchanged. This covers the replaceAndPersist→rollback→recreateMV path.
func TestConfigure_CreateFailRestoresPreviousMV(t *testing.T) {
	def := testDef("rp-x", "1.0.0", intParam("threshold", 5, f64(1), f64(100)))
	svc, st, mv, _ := newTestService(&fakeDefs{defs: map[string]catalog.Definition{"rp-x": def}})
	cur := installedInst(def, true, 1, nil, nil)
	prevRenderedSQL := cur.RenderedSQL
	st.latest["rp-x"] = cur
	mv.exists["rp-x"] = true

	// Fail only the first CreateMVStrict call (the candidate); the rollback
	// recreate (second create) must succeed with the previous rendered SQL.
	mv.errOn = func(op string, n int) error {
		if op == "create" && n == 1 {
			return errors.New("create boom")
		}
		return nil
	}

	_, err := svc.Configure("rp-x", map[string]any{"threshold": 9}, "t", "r")
	if err == nil {
		t.Fatal("expected error")
	}
	if len(st.appended) != 0 {
		t.Fatalf("candidate snapshot must not be persisted on create failure; got %d appends", len(st.appended))
	}
	// rollback→recreateMV must create the previous MV from last-known-good SQL.
	if len(mv.creates) < 2 || mv.creates[len(mv.creates)-1] != prevRenderedSQL {
		t.Fatalf("previous MV not restored from last-known-good rendered_sql:\n creates=%v\n want last=%q", mv.creates, prevRenderedSQL)
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

// ── UpdateDefinition (atomic publish + rollback) ─────────────────────────────────

func customDef(id, version string, params ...catalog.Parameter) catalog.Definition {
	d := testDef(id, version, params...)
	d.Origin = catalog.OriginCustom
	return d
}

// TestUpdateDefinition_RejectsBuiltin: editing a builtin/imported definition is
// refused before anything is published or deployed — operators must clone first.
func TestUpdateDefinition_RejectsBuiltin(t *testing.T) {
	def := testDef("rp-003", "1.0.0") // testDef sets Origin = builtin
	defs := &fakeDefs{defs: map[string]catalog.Definition{"rp-003": def}}
	svc, _, _, rec := newTestService(defs)

	_, err := svc.UpdateDefinition(def, "t", "r")
	if err == nil || !strings.Contains(err.Error(), "custom") {
		t.Fatalf("expected builtin-edit rejection mentioning custom, got %v", err)
	}
	if len(rec.snapshot()) != 0 {
		t.Errorf("rejected edit must perform no Timeplus work, got %v", rec.snapshot())
	}
	if len(defs.saved) != 0 {
		t.Errorf("rejected edit must not publish, got saves %v", defs.saved)
	}
}

// TestUpdateDefinition_NotFound: editing an unknown definition is a 404-class
// catalog.ErrNotFound, before any publish.
func TestUpdateDefinition_NotFound(t *testing.T) {
	defs := &fakeDefs{defs: map[string]catalog.Definition{}}
	svc, _, _, _ := newTestService(defs)
	if _, err := svc.UpdateDefinition(customDef("nope", "1.0.0"), "t", "r"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("want catalog.ErrNotFound, got %v", err)
	}
}

// TestUpdateDefinition_NotInstalledSavesOnly: when no instance is installed the
// publish IS the whole operation — no MV work, no snapshot, zero instance back.
func TestUpdateDefinition_NotInstalledSavesOnly(t *testing.T) {
	prev := customDef("rp-c", "1.0.0")
	defs := &fakeDefs{defs: map[string]catalog.Definition{"rp-c": prev}}
	svc, st, _, rec := newTestService(defs)

	mutated := prev
	mutated.Description = "edited desc"

	inst, err := svc.UpdateDefinition(mutated, "alice", "edit")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if inst.RuleID != "" {
		t.Errorf("expected zero instance when not installed, got %+v", inst)
	}
	if defs.defs["rp-c"].Description != "edited desc" {
		t.Errorf("definition not published")
	}
	if len(rec.snapshot()) != 0 {
		t.Errorf("no MV/append work when not installed, got %v", rec.snapshot())
	}
	if len(st.appended) != 0 {
		t.Errorf("no snapshot append when not installed, got %d", len(st.appended))
	}
}

// TestUpdateDefinition_EnabledReplacesAndAppends: the happy path for an installed
// enabled rule — publish, recompile against stored overrides, replace the MV, and
// append a snapshot whose hash has moved to the edited definition.
func TestUpdateDefinition_EnabledReplacesAndAppends(t *testing.T) {
	prev := customDef("rp-c", "1.0.0", intParam("t", 15, f64(1), f64(100)))
	defs := &fakeDefs{defs: map[string]catalog.Definition{"rp-c": prev}}
	svc, st, mv, rec := newTestService(defs)
	cur := installedInst(prev, true, 2, map[string]any{"t": json.Number("20")}, nil)
	st.latest["rp-c"] = cur

	mutated := prev
	mutated.Description = "edited desc"

	inst, err := svc.UpdateDefinition(mutated, "alice", "edit")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	// publish → recompile/preflight (apply cand, validate; no restore: udfs nil) →
	// replace (drop, apply, create) → append.
	wantLog(t, rec.snapshot(), []string{"applyUDFs", "preflight", "drop", "applyUDFs", "create", "append"})
	if inst.Revision != 3 {
		t.Errorf("revision = %d, want 3", inst.Revision)
	}
	if inst.DefinitionHash != mutated.Hash() {
		t.Errorf("snapshot hash not moved to edited definition")
	}
	if got, _ := inst.Overrides["t"].(int64); got != 20 {
		t.Errorf("stored override not preserved: %#v", inst.Overrides["t"])
	}
	if defs.defs["rp-c"].Description != "edited desc" {
		t.Errorf("definition not published")
	}
	if len(mv.creates) != 1 || mv.creates[0] != inst.RenderedSQL {
		t.Errorf("MV not created with new rendered SQL: %v", mv.creates)
	}
}

// TestUpdateDefinition_RedeployFailRestoresDefinitionAndMV is the load-bearing
// atomicity test: when the candidate MV create fails after publish, the previous
// definition must be republished AND the previous MV recreated AND no snapshot
// appended. The MV recreation is the existing replaceAndPersist→rollback path;
// the definition restoration is UpdateDefinition's added compensation.
func TestUpdateDefinition_RedeployFailRestoresDefinitionAndMV(t *testing.T) {
	prev := customDef("rp-c", "1.0.0", intParam("t", 15, f64(1), f64(100)))
	defs := &fakeDefs{defs: map[string]catalog.Definition{"rp-c": prev}}
	svc, st, mv, _ := newTestService(defs)
	cur := installedInst(prev, true, 2, map[string]any{"t": json.Number("15")}, []string{"udf-old"})
	st.latest["rp-c"] = cur

	mutated := prev
	mutated.Description = "edited desc" // different hash, same schema

	// Fail only the candidate (first) create; the rollback recreate must succeed.
	mv.errOn = func(op string, n int) error {
		if op == "create" && n == 1 {
			return errors.New("create boom")
		}
		return nil
	}

	_, err := svc.UpdateDefinition(mutated, "t", "r")
	if err == nil {
		t.Fatal("expected redeploy failure")
	}
	var crit *CriticalError
	if errors.As(err, &crit) {
		t.Fatalf("MV rollback and definition restore both succeeded, must not be CriticalError: %v", err)
	}
	// Definition rolled back to prev (hash unchanged from the pre-edit definition).
	if got := defs.defs["rp-c"].Hash(); got != prev.Hash() {
		t.Fatalf("definition not rolled back: hash=%s want=%s", got, prev.Hash())
	}
	// Previous definition was republished (publish then rollback-republish).
	if len(defs.saved) != 2 || defs.saved[1] != "rp-c" {
		t.Errorf("expected publish then rollback-republish, got saves %v", defs.saved)
	}
	// Previous MV recreated from the instance's last-known-good rendered SQL.
	if len(mv.creates) < 2 || mv.creates[len(mv.creates)-1] != cur.RenderedSQL {
		t.Fatalf("previous MV not restored from last-known-good rendered_sql: %v", mv.creates)
	}
	// Candidate snapshot must not persist.
	if len(st.appended) != 0 {
		t.Fatalf("candidate snapshot must not persist on redeploy failure, got %d", len(st.appended))
	}
}

// TestUpdateDefinition_PublishFailureDeploysNothing: if the catalog publish fails
// (e.g. a defective edited definition), nothing is recompiled or deployed and the
// publish error is returned verbatim.
func TestUpdateDefinition_PublishFailureDeploysNothing(t *testing.T) {
	prev := customDef("rp-c", "1.0.0")
	defs := &fakeDefs{defs: map[string]catalog.Definition{"rp-c": prev}, saveErr: errors.New("publish boom")}
	svc, st, _, rec := newTestService(defs)
	st.latest["rp-c"] = installedInst(prev, true, 2, nil, nil)

	_, err := svc.UpdateDefinition(prev, "t", "r")
	if err == nil || !strings.Contains(err.Error(), "publish boom") {
		t.Fatalf("want publish error, got %v", err)
	}
	if len(rec.snapshot()) != 0 {
		t.Errorf("failed publish must deploy nothing, got %v", rec.snapshot())
	}
}

// TestUpdateDefinition_RedeployFailWithBrokenRestoreIsCritical: when both the
// candidate create AND its MV rollback fail, replaceAndPersist already surfaces a
// CriticalError; UpdateDefinition must propagate it (still restoring the
// definition for catalog↔snapshot consistency).
func TestUpdateDefinition_RedeployFailWithBrokenRestoreIsCritical(t *testing.T) {
	prev := customDef("rp-c", "1.0.0", intParam("t", 15, f64(1), f64(100)))
	defs := &fakeDefs{defs: map[string]catalog.Definition{"rp-c": prev}}
	svc, _, mv, _ := newTestService(defs)
	st := svc.store.(*fakeStore)
	st.latest["rp-c"] = installedInst(prev, true, 2, map[string]any{"t": json.Number("15")}, nil)

	mutated := prev
	mutated.Description = "edited desc"
	// Every create fails: candidate create AND rollback recreate both fail.
	mv.errOn = func(op string, n int) error {
		if op == "create" {
			return errors.New("create always fails")
		}
		return nil
	}

	_, err := svc.UpdateDefinition(mutated, "t", "r")
	var crit *CriticalError
	if !errors.As(err, &crit) {
		t.Fatalf("want *CriticalError when MV rollback also fails, got %v", err)
	}
	// Even in the critical case the definition is restored so the catalog matches
	// the still-current (un-appended) snapshot.
	if got := defs.defs["rp-c"].Hash(); got != prev.Hash() {
		t.Errorf("definition not restored in critical case: hash=%s want=%s", got, prev.Hash())
	}
}

// TestUpdateDefinition_DropParamMigratesOverrides verifies that editing a custom
// rule to REMOVE a parameter migrates the stored overrides (drops the override
// for the removed parameter, preserves the rest) instead of failing on a now-
// "unknown parameter" — the same compatibility handling Upgrade applies. Removing
// a parameter is the core authoring use case, so it must not roll back the edit.
func TestUpdateDefinition_DropParamMigratesOverrides(t *testing.T) {
	prev := customDef("rp-c", "1.0.0",
		intParam("a", 5, f64(1), f64(100)),
		intParam("b", 10, f64(1), f64(100)))
	defs := &fakeDefs{defs: map[string]catalog.Definition{"rp-c": prev}}
	svc, _, mv, rec := newTestService(defs)
	st := svc.store.(*fakeStore)
	st.latest["rp-c"] = installedInst(prev, true, 2,
		map[string]any{"a": json.Number("20"), "b": json.Number("30")}, nil)

	// Edit: keep only "a" (customDef regenerates the template from its params, so
	// the new SQL references only {{ .Config.a }} — a valid edit).
	mutated := customDef("rp-c", "1.1.0", intParam("a", 5, f64(1), f64(100)))

	inst, err := svc.UpdateDefinition(mutated, "alice", "drop param b")
	if err != nil {
		t.Fatalf("update dropping a parameter must succeed, got %v", err)
	}
	// Enabled replace path runs to completion (no rollback).
	wantLog(t, rec.snapshot(), []string{"applyUDFs", "preflight", "drop", "applyUDFs", "create", "append"})
	if got, _ := inst.Overrides["a"].(int64); got != 20 {
		t.Errorf("kept override not preserved: %#v", inst.Overrides["a"])
	}
	if _, present := inst.Overrides["b"]; present {
		t.Errorf("override for the dropped parameter must be removed, got %#v", inst.Overrides["b"])
	}
	if inst.DefinitionHash != mutated.Hash() {
		t.Errorf("snapshot hash not moved to edited definition")
	}
	if len(mv.creates) != 1 || mv.creates[0] != inst.RenderedSQL {
		t.Errorf("MV not created with new rendered SQL: %v", mv.creates)
	}
}

// TestUpdateDefinition_IncompatibleOverrideConflicts verifies that an edit whose
// stored override no longer fits the edited schema (here: a tightened max below
// the stored value) blocks with a *ConflictError, performs no Timeplus work, and
// rolls the definition back to the previous version — unlike a dropped parameter,
// which migrates.
func TestUpdateDefinition_IncompatibleOverrideConflicts(t *testing.T) {
	prev := customDef("rp-c", "1.0.0", intParam("t", 15, f64(1), f64(1000)))
	defs := &fakeDefs{defs: map[string]catalog.Definition{"rp-c": prev}}
	svc, _, _, rec := newTestService(defs)
	st := svc.store.(*fakeStore)
	st.latest["rp-c"] = installedInst(prev, true, 2, map[string]any{"t": json.Number("800")}, nil)

	mutated := customDef("rp-c", "1.1.0", intParam("t", 15, f64(1), f64(100))) // 800 > new max

	_, err := svc.UpdateDefinition(mutated, "alice", "tighten max")
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("incompatible override: want *ConflictError, got %v", err)
	}
	if len(ce.Conflicts) == 0 {
		t.Errorf("conflict detail should be populated")
	}
	// Definition rolled back: the catalog still holds the previous version.
	if got := defs.defs["rp-c"].Hash(); got != prev.Hash() {
		t.Errorf("definition not rolled back: hash=%s want=%s", got, prev.Hash())
	}
	// A blocked edit deploys nothing and appends no snapshot. The only catalog
	// writes are the publish + rollback-republish; no MV/preflight ops run.
	if len(rec.snapshot()) != 0 {
		t.Errorf("blocked edit must perform no Timeplus work, got %v", rec.snapshot())
	}
	if len(st.appended) != 0 {
		t.Errorf("blocked edit must append no snapshot, got %d", len(st.appended))
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
