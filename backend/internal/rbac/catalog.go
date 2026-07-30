// Package rbac implements AgentGuard's application-layer role-based access
// control (#243). Users authenticate with Timeplus credentials (there is no
// local user registry); rbac maps a username → one role → a set of permission
// keys, and gates the HTTP surface and the UI on those keys. It is a layer on
// top of Timeplus-native grants, not a replacement for them.
//
// Permission keys have two surfaces per resource:
//
//	ui:<resource>          — may open the page / see the nav item
//	api:<resource>:read    — may call the resource's read (GET) endpoints
//	api:<resource>:write   — may call the resource's mutating endpoints
//
// A user with no role assignment resolves to the empty set: no access.
package rbac

import "fmt"

// Action is a permission surface for a resource.
type Action string

const (
	ActionUI    Action = "ui"
	ActionRead  Action = "read"
	ActionWrite Action = "write"
)

// Resource is one gated area of the product. HasWrite is false for read-only
// resources (dashboard, agents, …) — they expose no mutating endpoints, so
// their write key is never minted.
type Resource struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Group    string `json:"group"`
	HasWrite bool   `json:"has_write"`
}

// Resources is the ordered catalog. The `access` resource gates role/user
// management itself (the Access Control admin surface); only roles granted
// api:access:write may administer RBAC.
var Resources = []Resource{
	{Key: "dashboard", Label: "Dashboard", Group: "Monitoring", HasWrite: false},
	{Key: "agents", Label: "Agents", Group: "Monitoring", HasWrite: false},
	{Key: "threats", Label: "Threats", Group: "Security", HasWrite: true},
	{Key: "rules", Label: "Rules", Group: "Security", HasWrite: true},
	{Key: "cost", Label: "Cost", Group: "Governance", HasWrite: true},
	{Key: "dlp", Label: "Semantic DLP", Group: "Security", HasWrite: true},
	{Key: "sentry", Label: "Sentry", Group: "Security", HasWrite: true},
	{Key: "settings", Label: "Settings", Group: "Administration", HasWrite: true},
	{Key: "access", Label: "Access Control", Group: "Administration", HasWrite: true},
}

// Permission-key builders. Keep these the single source of truth for the wire
// format so the middleware, the catalog, and the frontend never drift.
func UIKey(resource string) string    { return "ui:" + resource }
func ReadKey(resource string) string  { return "api:" + resource + ":read" }
func WriteKey(resource string) string { return "api:" + resource + ":write" }

// AllPermissions returns every valid permission key, in catalog order. This is
// exactly the admin role's permission set.
func AllPermissions() []string {
	out := make([]string, 0, len(Resources)*3)
	for _, r := range Resources {
		out = append(out, UIKey(r.Key), ReadKey(r.Key))
		if r.HasWrite {
			out = append(out, WriteKey(r.Key))
		}
	}
	return out
}

// ViewerPermissions is the built-in view-only set: every UI + every read, no
// writes, and no access-control administration.
func ViewerPermissions() []string {
	out := make([]string, 0, len(Resources)*2)
	for _, r := range Resources {
		if r.Key == "access" {
			continue // viewers cannot see or read the admin surface
		}
		out = append(out, UIKey(r.Key), ReadKey(r.Key))
	}
	return out
}

// validKeys is the set of every mintable permission key, for validation.
var validKeys = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range AllPermissions() {
		m[k] = true
	}
	return m
}()

// ValidatePermissions returns an error if any key is not a known permission.
func ValidatePermissions(perms []string) error {
	for _, p := range perms {
		if !validKeys[p] {
			return fmt.Errorf("unknown permission %q", p)
		}
	}
	return nil
}

// AdminPermission is the key required to administer roles and users.
var AdminPermission = WriteKey("access")
