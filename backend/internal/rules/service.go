package rules

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/timeplus"
)

// Service is the single orchestrator for rule-instance lifecycle mutations:
// install, configure, toggle, set-policy, upgrade, uninstall, and MV sync. It
// composes the snapshot store (store.go), the parameter compiler
// (validate.go/render.go/compatibility.go), the catalog of definitions, and the
// Timeplus MV primitives (internal/timeplus/rules.go) behind one boundary so the
// validate-first / replace-second / persist-last ordering and rollback live in
// exactly one place. Handlers must delegate here and never hand-write
// agentguard_rules INSERTs or rule MV DDL.
//
// Every mutation is serialized per rule_id (see keyedMutex): the per-rule lock is
// acquired BEFORE the current snapshot is read, so the next revision
// (current.Revision + 1) is computed atomically with the snapshot append. This
// makes a single backend process a safe single writer. Multi-replica deployments
// would interleave revisions and MV replacements across processes; until a
// deployment-wide lock exists, rule-management mutations are single-writer only.
type Service struct {
	store   snapshotStore
	catalog defStore
	mv      mvOps
	locks   *keyedMutex
	// genID produces a unique snapshot_id per appended snapshot. Injectable so
	// tests get deterministic IDs; production uses crypto/rand.
	genID func() (string, error)
	// cache is the process-wide read-your-writes accelerator (see snapshotCache).
	cache *snapshotCache
}

// currentForMutation reads a rule's current snapshot for a mutation, overlaying
// the read-your-writes cache so a just-committed-but-not-yet-visible append is
// never missed. It MUST be called under the per-rule lock (so the read and the
// subsequent append are atomic for this writer).
func (s *Service) currentForMutation(ruleID string) (RuleInstance, bool, error) {
	inst, found, err := s.store.LatestSnapshot(ruleID)
	if err != nil {
		return RuleInstance{}, false, err
	}
	if s.cache != nil {
		inst, found = s.cache.latest(ruleID, inst, found)
	}
	return inst, found, nil
}

// appendAndCache persists a snapshot and, on success, records it as the rule's
// latest in the read-your-writes cache.
func (s *Service) appendAndCache(inst RuleInstance) error {
	if err := s.store.Append(inst); err != nil {
		return err
	}
	if s.cache != nil {
		s.cache.record(inst)
	}
	return nil
}

// installEnabled / installPolicy are the universal-state defaults a fresh
// install starts from, matching AgentGuard's historical behavior: a newly
// installed rule is enabled and only logs (it does not block) until an operator
// raises its policy.
const (
	installEnabled = true
	installPolicy  = "log_only"
)

// snapshotStore is the persistence surface the service needs. *Store satisfies
// it; tests fake it to assert append ordering and revision computation.
type snapshotStore interface {
	LatestSnapshot(ruleID string) (RuleInstance, bool, error)
	ListCurrent() ([]RuleInstance, error)
	Append(inst RuleInstance) error
}

// defStore is the catalog surface the service needs. *catalog.Manager satisfies
// it; tests fake it so install/configure/upgrade run without a filesystem.
//
// Save validates and publishes a definition (the create-custom and
// edit-definition paths). UpdateDefinition relies on Save being atomic enough
// that a failed publish leaves nothing deployed, and that re-Saving the previous
// definition restores it byte-for-byte for rollback.
type defStore interface {
	Lookup(id string) (catalog.Definition, bool)
	ValidateUDFOwnership(d catalog.Definition) error
	Save(d catalog.Definition) error
}

// mvOps is the Timeplus materialized-view surface the service drives. The
// production implementation (protonMVOps) delegates to internal/timeplus; tests
// use a fake to assert MV operation ordering and rollback without a live Proton.
//
// Preflight validates rendered SQL against a disposable sink; it does NOT manage
// UDFs — the service applies candidate UDFs before Preflight and restores the
// previously deployed set after (UDFs are global in Timeplus). The service uses
// drop-then-create (not pause/resume) for the MV: a disabled rule has no live
// MV, so re-enabling always deploys the snapshot's last-known-good rendered_sql
// rather than resuming a view that may carry stale SQL.
type mvOps interface {
	Preflight(ruleID, renderedSQL string) error
	ApplyUDFs(udfs []string) error
	DropMVNoDelay(ruleID string) error
	CreateMVStrict(ruleID, renderedSQL string) error
	MVExists(ruleID string) (bool, error)
	FailedMVs() (map[string]timeplus.FailedMV, error)
}

// protonMVOps binds the package-level internal/timeplus primitives to one
// config so they satisfy mvOps.
type protonMVOps struct{ cfg *config.TimeplusConfig }

func (p protonMVOps) Preflight(ruleID, renderedSQL string) error {
	return timeplus.PreflightCandidate(p.cfg, ruleID, renderedSQL)
}
func (p protonMVOps) ApplyUDFs(udfs []string) error { return timeplus.ApplyUDFs(p.cfg, udfs) }
func (p protonMVOps) DropMVNoDelay(ruleID string) error {
	return timeplus.DropMVNoDelay(p.cfg, ruleID)
}
func (p protonMVOps) CreateMVStrict(ruleID, renderedSQL string) error {
	return timeplus.CreateRuleMVStrict(p.cfg, ruleID, renderedSQL)
}
func (p protonMVOps) MVExists(ruleID string) (bool, error) {
	return timeplus.MVExists(p.cfg, ruleID)
}
func (p protonMVOps) FailedMVs() (map[string]timeplus.FailedMV, error) {
	return timeplus.FailedMVs(p.cfg)
}

// Lifecycle is the process-wide owner of the per-rule mutation locks, the
// catalog, the snapshot-id generator, and the read-your-writes snapshot cache.
// Timeplus credentials vary per request (auth.RequestCfg overrides them from the
// session), so a Service is bound to one request's config via For; all of them
// share this owner's locks and cache, making per-rule serialization and
// read-your-writes process-wide rather than per-request. Construct one Lifecycle
// at startup and call For(cfg) per request.
type Lifecycle struct {
	catalog defStore
	locks   *keyedMutex
	genID   func() (string, error)
	cache   *snapshotCache
}

// NewLifecycle builds the process-wide lifecycle owner around the catalog manager.
func NewLifecycle(cat *catalog.Manager) *Lifecycle {
	return &Lifecycle{
		catalog: cat,
		locks:   newKeyedMutex(),
		genID:   genSnapshotID,
		cache:   newSnapshotCache(),
	}
}

// For returns a Service bound to one request's Timeplus config, sharing this
// Lifecycle's process-wide per-rule locks and read-your-writes cache.
func (l *Lifecycle) For(cfg *config.TimeplusConfig) *Service {
	return &Service{
		store:   NewStore(cfg),
		catalog: l.catalog,
		mv:      protonMVOps{cfg: cfg},
		locks:   l.locks,
		genID:   l.genID,
		cache:   l.cache,
	}
}

// snapshotCache is a process-wide read-your-writes accelerator: it holds the
// last successfully-appended complete snapshot per rule_id. Proton's
// table(agentguard_rules) read is NOT linearizable — a just-committed INSERT is
// not immediately visible to the next table() read (verified live: two rapid
// same-rule mutations otherwise read stale state and both append the same
// revision, a lost update). Under the documented single-writer model, this
// in-process cache is the authoritative latest snapshot for the mutation path;
// the stream remains the durable store and eventually-consistent reads (List /
// Describe) read it directly. On process restart the cache starts empty and the
// stream has long since flushed, so reads fall back to the DB safely.
//
// It assumes single-writer (the same assumption as the per-rule lock): a second
// mutating process would have a stale cache. Multi-replica mutation requires a
// deployment-wide lock before it is safe — same caveat as the lock.
type snapshotCache struct {
	mu sync.Mutex
	m  map[string]RuleInstance
}

func newSnapshotCache() *snapshotCache { return &snapshotCache{m: make(map[string]RuleInstance)} }

// latest returns the newer of the DB read and the cached snapshot for one rule.
// The cache wins on a revision tie or when the DB hasn't yet surfaced the row.
func (c *snapshotCache) latest(ruleID string, dbInst RuleInstance, dbFound bool) (RuleInstance, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cached, ok := c.m[ruleID]
	if ok && (!dbFound || cached.Revision >= dbInst.Revision) {
		return cached, true
	}
	return dbInst, dbFound
}

// record stores a freshly-appended snapshot as the new latest for its rule.
func (c *snapshotCache) record(inst RuleInstance) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cur, ok := c.m[inst.RuleID]; !ok || inst.Revision >= cur.Revision {
		c.m[inst.RuleID] = inst
	}
}

// ── Errors mapped to HTTP statuses by the handlers ───────────────────────────────

// ErrNotFound means no installed (non-deleted) instance exists for the rule
// (handlers map it to 404). The compiler's *ValidationError (400) and
// *DefinitionError (500) flow through unchanged.
var ErrNotFound = errors.New("rule not installed")

// ErrAlreadyInstalled means an install was requested for a rule that already has
// a non-deleted instance (handlers map it to 409).
var ErrAlreadyInstalled = errors.New("rule already installed")

// ConflictError is a blocking, non-retryable mutation refusal that is the
// operator's to resolve (handlers map it to 409): an upgrade with incompatible
// overrides, or a configure against a definition that has changed since install.
// Conflicts/Warnings carry per-parameter upgrade detail when relevant.
type ConflictError struct {
	Message   string               `json:"message"`
	Conflicts []CompatibilityIssue `json:"conflicts,omitempty"`
	Warnings  []CompatibilityIssue `json:"warnings,omitempty"`
}

func (e *ConflictError) Error() string { return e.Message }

// CriticalError means a mutation failed AND the compensating rollback also
// failed, so the deployed MV may not match the persisted snapshot. The service
// guarantees the persisted snapshot is left at the last consistent revision (the
// candidate snapshot is NOT appended), so /rules/sync can reconcile the MV.
// Handlers surface it as a 500 that names the rule and the manual recourse.
type CriticalError struct {
	Op          string
	RuleID      string
	Err         error
	RollbackErr error
}

func (e *CriticalError) Error() string {
	return fmt.Sprintf("rule %q: %s failed (%v) AND rollback failed (%v); materialized view %s may be out of sync with the persisted snapshot — run /api/rules/sync, and if the rule has no current snapshot (a failed install) drop %s manually",
		e.RuleID, e.Op, e.Err, e.RollbackErr, timeplus.MVName(e.RuleID), timeplus.MVName(e.RuleID))
}

func (e *CriticalError) Unwrap() error { return e.Err }

// genSnapshotID returns a random 128-bit hex snapshot identifier.
func genSnapshotID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate snapshot id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// ── UDF ownership ────────────────────────────────────────────────────────────────

// checkUDFOwnership rejects def if any of its UDF function names is already
// deployed by a different, non-deleted installed instance. Global CREATE OR
// REPLACE FUNCTION would otherwise overwrite a UDF a live MV depends on.
// This consults the real ownership ledger (installed snapshots' DeployedUDFs),
// not just catalog definitions. The catalog-level check is still run first to
// catch in-catalog conflicts before the more expensive store read.
func (s *Service) checkUDFOwnership(def catalog.Definition) error {
	if err := s.catalog.ValidateUDFOwnership(def); err != nil {
		return err
	}
	mine, err := catalog.ExtractUDFNames(def.UDFs)
	if err != nil {
		return err
	}
	if len(mine) == 0 {
		return nil
	}
	owned := make(map[string]bool, len(mine))
	for _, n := range mine {
		owned[n] = true
	}
	current, err := s.store.ListCurrent()
	if err != nil {
		return err
	}
	for _, inst := range current {
		if inst.RuleID == def.ID || inst.Deleted {
			continue
		}
		names, err := catalog.ExtractUDFNames(inst.DeployedUDFs)
		if err != nil {
			continue // an unparseable deployed UDF cannot assert ownership
		}
		for _, n := range names {
			if owned[n] {
				return fmt.Errorf("%w: UDF function %q is already deployed by installed rule %q", catalog.ErrUDFConflict, n, inst.RuleID)
			}
		}
	}
	return nil
}

// ── Read path ────────────────────────────────────────────────────────────────────

// MVHealth is the deployed-MV diagnostic for one rule. A disabled rule is
// expected to have Exists=false; callers cross-reference Enabled before treating
// a missing or failed MV as unhealthy.
type MVHealth struct {
	Exists       bool      `json:"exists"`
	FailedState  string    `json:"failed_state,omitempty"`
	LastFailedAt time.Time `json:"last_failed_at,omitempty"`
}

// InstanceView is the complete read model for GET /api/rules/:id: the persisted
// instance plus the current catalog definition's parameter schema, normalized
// defaults, effective values (defaults overlaid with the instance's overrides),
// and — when the catalog has moved past the installed definition — the upgrade
// plan (preserved overrides, warnings, conflicts). MV exposes deployed-MV health.
type InstanceView struct {
	Instance   RuleInstance         `json:"instance"`
	InCatalog  bool                 `json:"in_catalog"`
	Definition catalog.Definition   `json:"definition"`
	Schema     []catalog.Parameter  `json:"schema"`
	Defaults   map[string]any       `json:"defaults"`
	Effective  map[string]any       `json:"effective"`
	Upgrade    *CompatibilityResult `json:"upgrade,omitempty"`
	MV         MVHealth             `json:"mv"`
}

// List returns the current snapshot of every installed (non-deleted) rule,
// ordered by revision. It is the read backing for GET /api/rules and the
// installed-status overlay on the catalog. It takes no lock.
func (s *Service) List() ([]RuleInstance, error) {
	return s.store.ListCurrent()
}

// Describe returns the full read model for one installed rule. It does not mutate
// and takes no lock. Schema/defaults/effective are computed against the CURRENT
// catalog definition; when that definition differs from the installed one
// (Upgrade != nil), the schema shown is what an upgrade would apply, and Upgrade
// carries the per-parameter outcome of moving the instance's overrides forward.
func (s *Service) Describe(ruleID string) (InstanceView, error) {
	inst, found, err := s.currentForMutation(ruleID)
	if err != nil {
		return InstanceView{}, err
	}
	if !found || inst.Deleted {
		return InstanceView{}, ErrNotFound
	}

	view := InstanceView{Instance: inst}

	def, ok := s.catalog.Lookup(ruleID)
	if ok {
		view.InCatalog = true
		view.Definition = def
		view.Schema = def.Parameters
		// Defaults are best-effort: a defective definition (bad default) leaves
		// them nil rather than failing the whole read.
		if defaults, derr := normalizedDefaults(def); derr == nil {
			view.Defaults = defaults
			plan := PlanUpgradeOverrides(def, inst.Overrides)
			view.Effective = effectiveValues(defaults, plan.Overrides)
			if def.Hash() != inst.DefinitionHash {
				view.Upgrade = &plan
			}
		}
	}

	// MV health (best-effort; a Timeplus hiccup here must not fail the read).
	if exists, mvErr := s.mv.MVExists(ruleID); mvErr == nil {
		view.MV.Exists = exists
	}
	if failed, fErr := s.mv.FailedMVs(); fErr == nil {
		if fm, bad := failed[timeplus.MVName(ruleID)]; bad {
			view.MV.FailedState = fm.State
			view.MV.LastFailedAt = fm.SeenAt
		}
	}
	return view, nil
}

// ── Public mutations ─────────────────────────────────────────────────────────────

// Install creates the first instance of a catalog rule (or reinstalls a
// previously uninstalled one), rendering its definition with the supplied typed
// overrides. The rule starts enabled with a log-only policy. It fails with
// ErrAlreadyInstalled if a live instance already exists, *ValidationError for
// bad overrides, and *DefinitionError for a defective definition. The MV is
// created only after the candidate SQL passes preflight, and the snapshot is
// appended last; if the append fails the freshly-created MV is dropped.
func (s *Service) Install(ruleID string, overrides map[string]any, actor, reason string) (RuleInstance, error) {
	unlock := s.locks.lock(ruleID)
	defer unlock()

	def, ok := s.catalog.Lookup(ruleID)
	if !ok {
		return RuleInstance{}, ErrNotFound
	}

	prev, found, err := s.currentForMutation(ruleID)
	if err != nil {
		return RuleInstance{}, err
	}
	if found && !prev.Deleted {
		return RuleInstance{}, ErrAlreadyInstalled
	}

	// New installs and custom definitions must not claim a UDF name already owned
	// by another installed definition, since CREATE OR REPLACE FUNCTION mutates
	// global state. This runs before any preflight or deployment.
	if err := s.checkUDFOwnership(def); err != nil {
		return RuleInstance{}, err
	}

	compiled, err := Compile(def, overrides)
	if err != nil {
		return RuleInstance{}, err
	}
	udfs := def.UDFs

	// Preflight with no prior UDFs to restore (nothing is deployed yet).
	if err := s.preflight(ruleID, compiled.SQL, udfs, nil); err != nil {
		return RuleInstance{}, err
	}

	if installEnabled {
		if err := s.deployMV(ruleID, compiled.SQL, udfs); err != nil {
			return RuleInstance{}, err
		}
	}

	rev := uint64(1)
	if found {
		rev = prev.Revision + 1
	}
	inst, err := s.buildSnapshot(def, compiled, udfs, installEnabled, installPolicy, false, rev, actor, reason)
	if err != nil {
		if installEnabled {
			if dropErr := s.mv.DropMVNoDelay(ruleID); dropErr != nil {
				return RuleInstance{}, &CriticalError{Op: "install", RuleID: ruleID, Err: err, RollbackErr: dropErr}
			}
		}
		return RuleInstance{}, err
	}
	if err := s.appendAndCache(inst); err != nil {
		if installEnabled {
			if dropErr := s.mv.DropMVNoDelay(ruleID); dropErr != nil {
				return RuleInstance{}, &CriticalError{Op: "install", RuleID: ruleID, Err: err, RollbackErr: dropErr}
			}
		}
		return RuleInstance{}, err
	}
	return inst, nil
}

// Configure replaces an installed rule's full parameter override map (omitted
// keys reset to their default) against the SAME definition the instance is on.
// Because Phase A does not persist the prior schema, the catalog definition must
// still match the instance's definition_hash; if the catalog has moved on, the
// caller must Upgrade first (returned as a *ConflictError). For an enabled rule
// the MV is replaced (drop NO DELAY → apply UDFs → create) and restored on any
// failure; for a disabled rule only the candidate is preflighted and the new
// snapshot appended.
func (s *Service) Configure(ruleID string, overrides map[string]any, actor, reason string) (RuleInstance, error) {
	unlock := s.locks.lock(ruleID)
	defer unlock()

	cur, found, err := s.currentForMutation(ruleID)
	if err != nil {
		return RuleInstance{}, err
	}
	if !found || cur.Deleted {
		return RuleInstance{}, ErrNotFound
	}

	def, ok := s.catalog.Lookup(ruleID)
	if !ok {
		return RuleInstance{}, &ConflictError{Message: "rule definition is no longer in the catalog; cannot reconfigure an orphaned rule"}
	}
	if def.Hash() != cur.DefinitionHash {
		return RuleInstance{}, &ConflictError{Message: "rule definition has changed since install; upgrade the rule before configuring it"}
	}

	compiled, err := Compile(def, overrides)
	if err != nil {
		return RuleInstance{}, err
	}
	candUDFs := def.UDFs

	// Preflight the candidate, restoring the instance's currently deployed UDFs
	// afterward so the still-running current MV keeps its dependencies.
	if err := s.preflight(ruleID, compiled.SQL, candUDFs, cur.DeployedUDFs); err != nil {
		return RuleInstance{}, err
	}

	rev := cur.Revision + 1
	next, err := s.buildSnapshot(def, compiled, candUDFs, cur.Enabled, cur.BlockPolicy, false, rev, actor, reason)
	if err != nil {
		return RuleInstance{}, err
	}

	return s.replaceAndPersist("configure", cur, next, candUDFs)
}

// UpdateDefinition edits a custom catalog definition and, when an instance is
// installed, atomically republishes the definition and redeploys the rule's MV
// against the instance's stored overrides — rolling BOTH back on any failure.
//
// Ordering (all under the per-rule lock):
//
//	a. Lookup the current definition; reject a missing one (catalog.ErrNotFound)
//	   or a non-custom one (a *ConflictError — builtin/imported must be cloned
//	   first). Capture it as the rollback target.
//	b. catalog.Save(def) — validates and publishes. A failure here deployed
//	   nothing, so the error is returned verbatim.
//	c. Read the current installed snapshot. If none is installed, the publish was
//	   the whole operation: return a zero instance.
//	d. Otherwise recompile against the instance's CURRENT overrides, check UDF
//	   ownership, preflight, and (via replaceAndPersist) replace the MV for an
//	   enabled rule and append the new snapshot — exactly as Configure does.
//
// On ANY failure in step d the previous definition is republished
// (restoreDefinition); the previous MV is already restored by the same seams
// Configure relies on (preflight never drops the live MV; replaceAndPersist's own
// rollback recreates the previous MV from the instance's last-known-good
// artifacts), so this method adds no parallel MV-rollback mechanism. If the
// republish itself fails — or replaceAndPersist already returned a CriticalError
// because its MV rollback failed — the result is a *CriticalError.
func (s *Service) UpdateDefinition(def catalog.Definition, actor, reason string) (RuleInstance, error) {
	const op = "update-definition"
	unlock := s.locks.lock(def.ID)
	defer unlock()

	// a. The definition must already exist and be custom-owned.
	cur, found := s.catalog.Lookup(def.ID)
	if !found {
		return RuleInstance{}, catalog.ErrNotFound
	}
	if cur.Origin != catalog.OriginCustom {
		return RuleInstance{}, &ConflictError{Message: fmt.Sprintf(
			"rule %q is a %s definition and cannot be edited in place; clone it to a custom rule first", def.ID, cur.Origin)}
	}
	prevDef := cur // rollback target

	// b. Publish. Nothing is deployed yet, so a failure needs no compensation.
	if err := s.catalog.Save(def); err != nil {
		return RuleInstance{}, err
	}

	// c. Is there an installed instance to redeploy?
	inst, installed, err := s.currentForMutation(def.ID)
	if err != nil {
		return s.restoreDefinition(op, prevDef, err)
	}
	if !installed || inst.Deleted {
		return RuleInstance{}, nil // publish was the whole operation
	}

	// d. Recompile against the instance's stored overrides and redeploy, mirroring
	// Configure. Any failure here restores the previous definition; the MV is
	// restored by preflight/replaceAndPersist themselves.
	compiled, err := Compile(def, inst.Overrides)
	if err != nil {
		return s.restoreDefinition(op, prevDef, err)
	}
	if err := s.checkUDFOwnership(def); err != nil {
		return s.restoreDefinition(op, prevDef, err)
	}
	candUDFs := def.UDFs

	if err := s.preflight(def.ID, compiled.SQL, candUDFs, inst.DeployedUDFs); err != nil {
		return s.restoreDefinition(op, prevDef, err)
	}

	next, err := s.buildSnapshot(def, compiled, candUDFs, inst.Enabled, inst.BlockPolicy, false, inst.Revision+1, actor, reason)
	if err != nil {
		return s.restoreDefinition(op, prevDef, err)
	}

	out, err := s.replaceAndPersist(op, inst, next, candUDFs)
	if err != nil {
		return s.restoreDefinition(op, prevDef, err)
	}
	return out, nil
}

// restoreDefinition compensates a failed installed-definition update by
// republishing the previous definition (step b's publish is the only state this
// method itself created). The live MV is already restored to the instance's
// last-known-good by the failing seam, so there is no MV work here. If
// republishing fails, the catalog no longer matches the persisted snapshot — a
// *CriticalError. When the cause is already a *CriticalError (replaceAndPersist's
// own MV rollback failed) it is propagated unchanged after a successful
// republish, preserving its richer recovery message.
func (s *Service) restoreDefinition(op string, prevDef catalog.Definition, cause error) (RuleInstance, error) {
	if rbErr := s.catalog.Save(prevDef); rbErr != nil {
		return RuleInstance{}, &CriticalError{Op: op, RuleID: prevDef.ID, Err: cause, RollbackErr: rbErr}
	}
	return RuleInstance{}, cause
}

// Toggle flips a rule's enabled flag. Enabling deploys the MV from the last-
// known-good snapshot (its rendered_sql + deployed UDFs); disabling drops the
// MV. rendered_sql, overrides, UDFs, and policy are carried forward unchanged.
// Disabling a rule should also abandon its pending holds — that is a
// post-persistence side effect owned by the caller, which inspects the returned
// instance's Enabled flag.
func (s *Service) Toggle(ruleID, actor, reason string) (RuleInstance, error) {
	unlock := s.locks.lock(ruleID)
	defer unlock()

	cur, found, err := s.currentForMutation(ruleID)
	if err != nil {
		return RuleInstance{}, err
	}
	if !found || cur.Deleted {
		return RuleInstance{}, ErrNotFound
	}

	newEnabled := !cur.Enabled
	next, err := s.cloneSnapshot(cur, actor, reason)
	if err != nil {
		return RuleInstance{}, err
	}
	next.Enabled = newEnabled

	if newEnabled {
		if err := s.deployMV(ruleID, cur.RenderedSQL, cur.DeployedUDFs); err != nil {
			return RuleInstance{}, err
		}
	} else {
		if err := s.mv.DropMVNoDelay(ruleID); err != nil {
			return RuleInstance{}, err
		}
	}

	if err := s.appendAndCache(next); err != nil {
		// Restore the MV to match the still-current (pre-toggle) snapshot.
		var rbErr error
		if newEnabled {
			rbErr = s.mv.DropMVNoDelay(ruleID)
		} else {
			rbErr = s.recreateMV(cur)
		}
		if rbErr != nil {
			return RuleInstance{}, &CriticalError{Op: "toggle", RuleID: ruleID, Err: err, RollbackErr: rbErr}
		}
		return RuleInstance{}, err
	}
	return next, nil
}

// SetPolicy changes a rule's block policy (log_only | auto_block | hold) without
// touching its MV — the policy is read by the hold/threat flow, not the MV. As
// with Toggle, switching to log_only should abandon pending holds; that side
// effect is the caller's, keyed off the returned instance's BlockPolicy.
func (s *Service) SetPolicy(ruleID, policy, actor, reason string) (RuleInstance, error) {
	switch policy {
	case "log_only", "auto_block", "hold":
	default:
		return RuleInstance{}, &ValidationError{Fields: []FieldError{{
			Parameter: "block_policy",
			Message:   "must be one of: log_only, auto_block, hold",
		}}}
	}

	unlock := s.locks.lock(ruleID)
	defer unlock()

	cur, found, err := s.currentForMutation(ruleID)
	if err != nil {
		return RuleInstance{}, err
	}
	if !found || cur.Deleted {
		return RuleInstance{}, ErrNotFound
	}

	next, err := s.cloneSnapshot(cur, actor, reason)
	if err != nil {
		return RuleInstance{}, err
	}
	next.BlockPolicy = policy

	if err := s.appendAndCache(next); err != nil {
		return RuleInstance{}, err
	}
	return next, nil
}

// Upgrade re-renders an installed rule against the current catalog definition,
// preserving compatible overrides (including alias migrations) and blocking with
// a *ConflictError when an override no longer fits the new schema. It is a no-op
// (returns the current instance) when the catalog hash already matches. On
// success it deploys like Configure: replace the MV for an enabled rule, restore
// on failure, append last.
func (s *Service) Upgrade(ruleID, actor, reason string) (RuleInstance, error) {
	unlock := s.locks.lock(ruleID)
	defer unlock()

	cur, found, err := s.currentForMutation(ruleID)
	if err != nil {
		return RuleInstance{}, err
	}
	if !found || cur.Deleted {
		return RuleInstance{}, ErrNotFound
	}

	def, ok := s.catalog.Lookup(ruleID)
	if !ok {
		return RuleInstance{}, &ConflictError{Message: "rule definition is no longer in the catalog; nothing to upgrade to"}
	}
	if def.Hash() == cur.DefinitionHash {
		return cur, nil // already at the catalog version
	}

	if err := s.checkUDFOwnership(def); err != nil {
		return RuleInstance{}, err
	}

	plan := PlanUpgradeOverrides(def, cur.Overrides)
	if !plan.OK() {
		return RuleInstance{}, &ConflictError{
			Message:   "upgrade blocked: existing overrides are incompatible with the new definition",
			Conflicts: plan.Conflicts,
			Warnings:  plan.Warnings,
		}
	}

	compiled, err := Compile(def, plan.Overrides)
	if err != nil {
		return RuleInstance{}, err
	}
	candUDFs := def.UDFs

	if err := s.preflight(ruleID, compiled.SQL, candUDFs, cur.DeployedUDFs); err != nil {
		return RuleInstance{}, err
	}

	rev := cur.Revision + 1
	next, err := s.buildSnapshot(def, compiled, candUDFs, cur.Enabled, cur.BlockPolicy, false, rev, actor, reason)
	if err != nil {
		return RuleInstance{}, err
	}

	return s.replaceAndPersist("upgrade", cur, next, candUDFs)
}

// Uninstall soft-deletes an installed rule: it drops the MV, then appends a
// deleted=true snapshot that preserves every other field for audit. If the
// snapshot append fails after the drop, the MV is recreated to match the still-
// current snapshot. Pending holds should be abandoned by the caller afterward.
func (s *Service) Uninstall(ruleID, actor, reason string) (RuleInstance, error) {
	unlock := s.locks.lock(ruleID)
	defer unlock()

	cur, found, err := s.currentForMutation(ruleID)
	if err != nil {
		return RuleInstance{}, err
	}
	if !found || cur.Deleted {
		return RuleInstance{}, ErrNotFound
	}

	// Build the next snapshot (including its generated ID) BEFORE the destructive
	// drop, so a genID failure cannot leave an enabled rule's MV dropped while its
	// persisted snapshot still says enabled (matching Toggle's ordering).
	next, err := s.cloneSnapshot(cur, actor, reason)
	if err != nil {
		return RuleInstance{}, err
	}
	next.Deleted = true

	if err := s.mv.DropMVNoDelay(ruleID); err != nil {
		return RuleInstance{}, err
	}

	if err := s.appendAndCache(next); err != nil {
		if cur.Enabled {
			if rbErr := s.recreateMV(cur); rbErr != nil {
				return RuleInstance{}, &CriticalError{Op: "uninstall", RuleID: ruleID, Err: err, RollbackErr: rbErr}
			}
		}
		return RuleInstance{}, err
	}
	return next, nil
}

// SyncResult reports the reconciliation outcome for one rule's MV.
type SyncResult struct {
	RuleID string `json:"rule_id"`
	Action string `json:"action"`
	Error  string `json:"error,omitempty"`
}

// Sync reconciles deployed MVs with the current (non-deleted) snapshots without
// appending anything: an enabled rule whose MV is missing is recreated from its
// snapshot SQL + UDFs; a disabled rule whose MV still exists has it dropped. It
// does not depend on the current catalog definition — only on the last-known-
// good artifacts in the snapshot — so restart/sync works even when the catalog
// is newer or the definition was removed. Each rule is reconciled under its own
// per-rule lock so a concurrent mutation cannot interleave.
func (s *Service) Sync() ([]SyncResult, error) {
	instances, err := s.store.ListCurrent()
	if err != nil {
		return nil, err
	}
	results := make([]SyncResult, 0, len(instances))
	for _, inst := range instances {
		results = append(results, s.syncOne(inst))
	}
	return results, nil
}

func (s *Service) syncOne(inst RuleInstance) SyncResult {
	unlock := s.locks.lock(inst.RuleID)
	defer unlock()

	// Overlay the read-your-writes cache: the ListCurrent snapshot may predate a
	// just-committed mutation that the DB read hasn't surfaced yet.
	if s.cache != nil {
		inst, _ = s.cache.latest(inst.RuleID, inst, true)
	}
	res := SyncResult{RuleID: inst.RuleID}
	if inst.Deleted { // cache shows it was uninstalled since the list read
		if err := s.mv.DropMVNoDelay(inst.RuleID); err != nil {
			res.Action = "drop_failed"
			res.Error = err.Error()
		} else {
			res.Action = "ok"
		}
		return res
	}
	exists, err := s.mv.MVExists(inst.RuleID)
	if err != nil {
		res.Action = "check_failed"
		res.Error = err.Error()
		return res
	}
	switch {
	case inst.Enabled:
		// Force replace, even when an MV already exists, so the live MV is
		// guaranteed to run this snapshot's rendered_sql — existence-only
		// reconciliation could not repair an MV that exists but runs drifted SQL
		// (the state a CriticalError leaves, and the one its recovery message
		// tells operators to fix with /api/rules/sync). But validate FIRST: a
		// candidate preflight against a disposable sink proves the snapshot SQL
		// (and UDFs) build before the destructive drop, so a failed recreate can
		// never turn a healthy MV into no MV. restoreUDFs is the running MV's UDF
		// set when one exists (so preflight leaves it intact), nil otherwise.
		var restoreUDFs []string
		if exists {
			restoreUDFs = inst.DeployedUDFs
		}
		if err := s.preflight(inst.RuleID, inst.RenderedSQL, inst.DeployedUDFs, restoreUDFs); err != nil {
			res.Action = "preflight_failed"
			res.Error = err.Error()
			return res
		}
		// Proton MVs are forward-only, so recreating a healthy MV loses no data
		// and emits no duplicates; the brief per-rule gap is acceptable here.
		if err := s.deployMV(inst.RuleID, inst.RenderedSQL, inst.DeployedUDFs); err != nil {
			res.Action = "create_failed"
			res.Error = err.Error()
		} else if exists {
			res.Action = "replaced"
		} else {
			res.Action = "created"
		}
	case !inst.Enabled && exists:
		if err := s.mv.DropMVNoDelay(inst.RuleID); err != nil {
			res.Action = "drop_failed"
			res.Error = err.Error()
		} else {
			res.Action = "dropped"
		}
	default:
		res.Action = "ok"
	}
	return res
}

// ── Shared mutation helpers ──────────────────────────────────────────────────────

// preflight applies the candidate's UDFs, validates the rendered SQL against a
// disposable sink, and (for an existing instance) always restores the previously
// deployed UDFs before returning — whether validation passed or failed — because
// Timeplus UDFs are global and the still-running current MV depends on them. A
// new install passes restoreUDFs=nil.
//
// A restore failure is a consistency failure, not a soft warning: global UDF
// definitions are left as the candidate's while the persisted snapshot still
// points at the previous deployment, so it is surfaced as a *CriticalError
// (mapped to 500 with the manual-recourse message) instead of only being logged.
// It wraps the candidate apply / validation error when there is one.
func (s *Service) preflight(ruleID, renderedSQL string, candidateUDFs, restoreUDFs []string) (err error) {
	if applyErr := s.mv.ApplyUDFs(candidateUDFs); applyErr != nil {
		if len(restoreUDFs) > 0 {
			if rErr := s.mv.ApplyUDFs(restoreUDFs); rErr != nil {
				log.Printf("rules: preflight restore UDFs for %s after apply failure: %v", ruleID, rErr)
				return &CriticalError{Op: "preflight", RuleID: ruleID, Err: applyErr, RollbackErr: rErr}
			}
		}
		return fmt.Errorf("apply candidate UDFs: %w", applyErr)
	}
	if len(restoreUDFs) > 0 {
		defer func() {
			if rErr := s.mv.ApplyUDFs(restoreUDFs); rErr != nil {
				log.Printf("rules: preflight restore UDFs for %s: %v", ruleID, rErr)
				cause := err
				if cause == nil {
					cause = errors.New("preflight succeeded but candidate UDFs were left applied")
				}
				err = &CriticalError{Op: "preflight", RuleID: ruleID, Err: cause, RollbackErr: rErr}
			}
		}()
	}
	if vErr := s.mv.Preflight(ruleID, renderedSQL); vErr != nil {
		return vErr
	}
	return nil
}

// deployMV applies UDFs and creates a rule's MV, dropping any stale same-named MV
// first (NO DELAY) so the non-IF-NOT-EXISTS create can never silently reuse one.
func (s *Service) deployMV(ruleID, renderedSQL string, udfs []string) error {
	if err := s.mv.ApplyUDFs(udfs); err != nil {
		return err
	}
	if err := s.mv.DropMVNoDelay(ruleID); err != nil {
		return err
	}
	return s.mv.CreateMVStrict(ruleID, renderedSQL)
}

// recreateMV restores a rule's MV from a snapshot's last-known-good artifacts
// (its deployed UDFs + rendered SQL). Used by rollback paths.
func (s *Service) recreateMV(inst RuleInstance) error {
	return s.deployMV(inst.RuleID, inst.RenderedSQL, inst.DeployedUDFs)
}

// replaceAndPersist performs the enabled-rule replacement (drop current MV →
// apply candidate UDFs → create candidate MV) then appends the next snapshot,
// restoring the previous MV on any failure. For a disabled rule it skips the MV
// work and just appends. cur is the still-current snapshot (the rollback target);
// next is the candidate to persist.
func (s *Service) replaceAndPersist(op string, cur, next RuleInstance, candidateUDFs []string) (RuleInstance, error) {
	ruleID := cur.RuleID
	if cur.Enabled {
		// Drop the current MV first. If this fails the current MV is untouched
		// and still consistent with the persisted snapshot — no rollback needed.
		if err := s.mv.DropMVNoDelay(ruleID); err != nil {
			return RuleInstance{}, err
		}
		// From here the current MV is gone; any failure must restore it.
		if err := s.mv.ApplyUDFs(candidateUDFs); err != nil {
			return s.rollback(op, cur, err)
		}
		if err := s.mv.CreateMVStrict(ruleID, next.RenderedSQL); err != nil {
			return s.rollback(op, cur, err)
		}
	}
	if err := s.appendAndCache(next); err != nil {
		if cur.Enabled {
			return s.rollback(op, cur, err)
		}
		return RuleInstance{}, err
	}
	return next, nil
}

// rollback restores the previous MV after a failed replacement and returns the
// original error (or a CriticalError if the restore itself fails). The candidate
// snapshot is never appended, so the persisted snapshot stays at cur.
func (s *Service) rollback(op string, cur RuleInstance, cause error) (RuleInstance, error) {
	// Drop any partial candidate MV, then rebuild the previous one from its
	// last-known-good artifacts.
	if dropErr := s.mv.DropMVNoDelay(cur.RuleID); dropErr != nil {
		return RuleInstance{}, &CriticalError{Op: op, RuleID: cur.RuleID, Err: cause, RollbackErr: dropErr}
	}
	if rbErr := s.recreateMV(cur); rbErr != nil {
		return RuleInstance{}, &CriticalError{Op: op, RuleID: cur.RuleID, Err: cause, RollbackErr: rbErr}
	}
	return RuleInstance{}, cause
}

// buildSnapshot assembles a complete instance snapshot from a compiled
// definition. Definition metadata is copied in so instance reads stay complete
// and auditable independent of the catalog. The snapshot_id is freshly
// generated; updated_at is stamped by the store on append.
func (s *Service) buildSnapshot(def catalog.Definition, compiled Compiled, udfs []string, enabled bool, policy string, deleted bool, revision uint64, actor, reason string) (RuleInstance, error) {
	id, err := s.genID()
	if err != nil {
		return RuleInstance{}, err
	}
	return RuleInstance{
		RuleID:            def.ID,
		SnapshotID:        id,
		Revision:          revision,
		Name:              def.Name,
		DefinitionVersion: def.Version,
		DefinitionHash:    compiled.DefinitionHash,
		Description:       def.Description,
		RenderedSQL:       compiled.SQL,
		DeployedUDFs:      udfs,
		Overrides:         compiled.Overrides,
		Enabled:           enabled,
		Deleted:           deleted,
		Severity:          def.Severity,
		Category:          def.Category,
		RequiredTelemetry: def.RequiredTelemetry,
		FPRate:            def.FPRate,
		HookSources:       def.HookSources,
		BlockPolicy:       policy,
		UpdateReason:      reason,
		UpdatedBy:         actor,
	}, nil
}

// cloneSnapshot copies a current instance into the next revision, assigning a
// fresh snapshot_id and stamping actor/reason. Used by mutations that preserve
// the rendered SQL and overrides (toggle, policy, uninstall): the caller mutates
// only the one field that changed. The shared DeployedUDFs/Overrides slices and
// maps are read-only on the append path, so sharing them is safe.
func (s *Service) cloneSnapshot(cur RuleInstance, actor, reason string) (RuleInstance, error) {
	id, err := s.genID()
	if err != nil {
		return RuleInstance{}, err
	}
	next := cur
	next.SnapshotID = id
	next.Revision = cur.Revision + 1
	next.UpdateReason = reason
	next.UpdatedBy = actor
	next.UpdatedAt = time.Time{} // stamped by the store on append
	return next, nil
}

// ── Per-rule serialization ───────────────────────────────────────────────────────

// keyedMutex serializes work by string key (rule_id). Entries are created on
// demand and never removed — rule IDs are bounded, so the map cannot grow
// unboundedly in practice, and never deleting avoids the unlock-after-delete
// race a refcounted variant would need.
type keyedMutex struct {
	mu sync.Mutex
	m  map[string]*sync.Mutex
}

func newKeyedMutex() *keyedMutex {
	return &keyedMutex{m: make(map[string]*sync.Mutex)}
}

// lock acquires the per-key mutex and returns its unlock function.
func (k *keyedMutex) lock(key string) func() {
	k.mu.Lock()
	mu, ok := k.m[key]
	if !ok {
		mu = &sync.Mutex{}
		k.m[key] = mu
	}
	k.mu.Unlock()
	mu.Lock()
	return mu.Unlock
}
