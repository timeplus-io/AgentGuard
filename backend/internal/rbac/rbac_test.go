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
