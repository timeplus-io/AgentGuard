package rbac

import "testing"

func hasPerm(perms []string, want string) bool {
	for _, p := range perms {
		if p == want {
			return true
		}
	}
	return false
}

func TestAdminHasEveryPermission(t *testing.T) {
	all := AllPermissions()
	// Admin includes access:write; a write key exists for every writable resource.
	if !hasPerm(all, WriteKey("access")) {
		t.Fatal("admin missing access:write")
	}
	for _, r := range Resources {
		if !hasPerm(all, UIKey(r.Key)) || !hasPerm(all, ReadKey(r.Key)) {
			t.Fatalf("admin missing ui/read for %s", r.Key)
		}
		if r.HasWrite && !hasPerm(all, WriteKey(r.Key)) {
			t.Fatalf("admin missing write for %s", r.Key)
		}
	}
}

func TestViewerIsReadOnlyAndNoAccessAdmin(t *testing.T) {
	v := ViewerPermissions()
	for _, p := range v {
		if p == WriteKey("threats") || p == WriteKey("rules") || p == WriteKey("cost") {
			t.Fatalf("viewer must not hold write permission %q", p)
		}
	}
	if hasPerm(v, UIKey("access")) || hasPerm(v, ReadKey("access")) {
		t.Fatal("viewer must not see the access-control surface")
	}
	// But viewer can read the normal resources.
	if !hasPerm(v, ReadKey("threats")) || !hasPerm(v, UIKey("dashboard")) {
		t.Fatal("viewer should read threats and see dashboard")
	}
}

func TestValidatePermissions(t *testing.T) {
	if err := ValidatePermissions([]string{ReadKey("rules"), WriteKey("cost")}); err != nil {
		t.Fatalf("valid perms rejected: %v", err)
	}
	if err := ValidatePermissions([]string{"ui:nonexistent"}); err == nil {
		t.Fatal("unknown permission accepted")
	}
	// A write key for a read-only resource is not a real permission.
	if err := ValidatePermissions([]string{WriteKey("dashboard")}); err == nil {
		t.Fatal("write key for read-only resource accepted")
	}
}

func TestRoutePermissionsAreValidKeys(t *testing.T) {
	for route, perm := range routePermissions {
		if !validKeys[perm] {
			t.Fatalf("route %q maps to invalid permission %q", route, perm)
		}
	}
}

// Proton (raw SQL) is intentionally not a gated resource: it is cross-cutting
// plumbing every authenticated user needs, exposed as a baseline route instead.
func TestProtonIsNotAResource(t *testing.T) {
	for _, r := range Resources {
		if r.Key == "proton" || r.Key == "investigate" {
			t.Fatalf("unexpected resource %q still in catalog", r.Key)
		}
	}
	all := AllPermissions()
	if hasPerm(all, ReadKey("proton")) || hasPerm(all, UIKey("proton")) {
		t.Fatal("proton permission keys must not be minted")
	}
}

// /api/proton/query must be reachable by any authenticated user: present in
// baselineRoutes and absent from the per-resource routePermissions map.
func TestProtonQueryIsBaselineNotGated(t *testing.T) {
	const key = "POST /api/proton/query"
	if !baselineRoutes[key] {
		t.Fatalf("%q must be a baseline route", key)
	}
	if _, mapped := routePermissions[key]; mapped {
		t.Fatalf("%q must not also be in routePermissions", key)
	}
}

// A route is either resource-gated or baseline, never both (avoids ambiguity).
func TestBaselineAndGatedAreDisjoint(t *testing.T) {
	for route := range baselineRoutes {
		if _, dup := routePermissions[route]; dup {
			t.Fatalf("route %q is both baseline and resource-gated", route)
		}
	}
}
