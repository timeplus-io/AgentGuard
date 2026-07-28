package rbac

import (
	"sync"
	"time"
)

// Resolver resolves a username → permission set, with a short cache. RBAC
// metadata is read with the service account (the Store's TimeplusConfig), not
// the caller's credentials, so access decisions don't depend on per-user grants.
//
// Activation: RBAC is "active" once the roles stream is seeded (post-provision).
// Before activation (streams missing / no roles — e.g. a deployment that
// upgraded but hasn't re-provisioned) enforcement fails OPEN so nobody is
// locked out. Activation is sticky: once we've seen roles, a later read error
// fails CLOSED rather than silently disabling access control.
type Resolver struct {
	store *Store
	ttl   time.Duration

	mu        sync.Mutex
	cache     map[string]cacheEntry
	activated bool
	actExp    time.Time
}

type cacheEntry struct {
	perms map[string]bool
	role  string
	exp   time.Time
}

// NewResolver builds a resolver over the given store (service-account bound).
func NewResolver(store *Store) *Resolver {
	return &Resolver{store: store, ttl: 15 * time.Second, cache: map[string]cacheEntry{}}
}

// Invalidate clears the caches — call after any role/assignment mutation.
func (r *Resolver) Invalidate() {
	r.mu.Lock()
	r.cache = map[string]cacheEntry{}
	r.actExp = time.Time{}
	r.mu.Unlock()
}

// Active reports whether RBAC enforcement is in effect. True once the roles
// stream has at least one role (seeded at provision). Sticky: stays true after
// first activation even if a later read fails.
func (r *Resolver) Active() bool {
	r.mu.Lock()
	if r.activated {
		r.mu.Unlock()
		return true
	}
	if time.Now().Before(r.actExp) {
		act := r.activated
		r.mu.Unlock()
		return act
	}
	r.mu.Unlock()

	roles, err := r.store.ListRoles()
	active := err == nil && len(roles) > 0

	r.mu.Lock()
	if active {
		r.activated = true
	}
	r.actExp = time.Now().Add(r.ttl)
	r.mu.Unlock()
	return active
}

// PermissionsFor returns the user's permission set and role name. An unassigned
// user resolves to the empty set (no access).
func (r *Resolver) PermissionsFor(user string) (map[string]bool, string, error) {
	r.mu.Lock()
	if e, ok := r.cache[user]; ok && time.Now().Before(e.exp) {
		perms, role := e.perms, e.role
		r.mu.Unlock()
		return perms, role, nil
	}
	r.mu.Unlock()

	perms := map[string]bool{}
	roleName := ""
	role, ok, err := r.store.RoleForUser(user)
	if err != nil {
		return nil, "", err
	}
	if ok {
		roleName = role
		rl, found, err := r.store.GetRole(role)
		if err != nil {
			return nil, "", err
		}
		if found {
			for _, p := range rl.Permissions {
				perms[p] = true
			}
		}
	}

	r.mu.Lock()
	r.cache[user] = cacheEntry{perms: perms, role: roleName, exp: time.Now().Add(r.ttl)}
	r.mu.Unlock()
	return perms, roleName, nil
}

// Has reports whether the user holds a specific permission.
func (r *Resolver) Has(user, perm string) (bool, error) {
	perms, _, err := r.PermissionsFor(user)
	if err != nil {
		return false, err
	}
	return perms[perm], nil
}
