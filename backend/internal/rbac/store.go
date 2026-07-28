package rbac

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/timeplus/agentguard/internal/config"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// Built-in role names. These are seeded on provision and cannot be deleted;
// their permission sets are refreshed from the catalog on every seed so they
// stay current as resources are added.
const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

// Two mutable streams keyed on their primary key (re-insert = upsert; a
// deleted=true row tombstones). Mirrors agentguard_sentry_config.
const ddlRoles = `
CREATE MUTABLE STREAM IF NOT EXISTS agentguard_roles
(
  role        string,
  description string,
  permissions string,
  builtin     bool          DEFAULT false,
  deleted     bool          DEFAULT false,
  updated_at  datetime64(3) DEFAULT now64(3),
  updated_by  string
)
PRIMARY KEY (role)`

const ddlUserRoles = `
CREATE MUTABLE STREAM IF NOT EXISTS agentguard_user_roles
(
  user       string,
  role       string,
  deleted    bool          DEFAULT false,
  updated_at datetime64(3) DEFAULT now64(3),
  updated_by string
)
PRIMARY KEY (user)`

// Role is a named permission set.
type Role struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	Builtin     bool     `json:"builtin"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
	UpdatedBy   string   `json:"updated_by,omitempty"`
}

// Assignment maps a user to exactly one role.
type Assignment struct {
	User      string `json:"user"`
	Role      string `json:"role"`
	UpdatedAt string `json:"updated_at,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

// Store reads/writes the RBAC streams for a given Timeplus endpoint.
type Store struct{ TP *config.TimeplusConfig }

func asStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1"
	case float64:
		return t != 0
	}
	return false
}

// EnsureStreams creates the RBAC streams if absent.
func EnsureStreams(cfg *config.TimeplusConfig) error {
	for _, ddl := range []string{ddlRoles, ddlUserRoles} {
		if err := tp.ExecHTTP(cfg, ddl); err != nil {
			return fmt.Errorf("rbac: create stream: %w", err)
		}
	}
	return nil
}

// ── Roles ──────────────────────────────────────────────────────────────────

func (s *Store) ListRoles() ([]Role, error) {
	rows, err := tp.Query(s.TP, `SELECT role, description, permissions, builtin,
  to_string(updated_at) AS updated_at, updated_by
FROM table(agentguard_roles) WHERE NOT deleted ORDER BY role`)
	if err != nil {
		return nil, err
	}
	out := make([]Role, 0, len(rows))
	for _, r := range rows {
		var perms []string
		_ = json.Unmarshal([]byte(asStr(r["permissions"])), &perms)
		out = append(out, Role{
			Name: asStr(r["role"]), Description: asStr(r["description"]),
			Permissions: perms, Builtin: asBool(r["builtin"]),
			UpdatedAt: asStr(r["updated_at"]), UpdatedBy: asStr(r["updated_by"]),
		})
	}
	return out, nil
}

func (s *Store) GetRole(name string) (Role, bool, error) {
	roles, err := s.ListRoles()
	if err != nil {
		return Role{}, false, err
	}
	for _, r := range roles {
		if r.Name == name {
			return r, true, nil
		}
	}
	return Role{}, false, nil
}

// UpsertRole writes (inserts/replaces) a role.
func (s *Store) UpsertRole(r Role, updatedBy string) error {
	permJSON, err := json.Marshal(r.Permissions)
	if err != nil {
		return fmt.Errorf("rbac: marshal permissions: %w", err)
	}
	return s.writeRole(r.Name, r.Description, string(permJSON), r.Builtin, false, updatedBy)
}

// DeleteRole tombstones a custom role.
func (s *Store) DeleteRole(name, updatedBy string) error {
	return s.writeRole(name, "", "[]", false, true, updatedBy)
}

func (s *Store) writeRole(role, desc, permJSON string, builtin, deleted bool, updatedBy string) error {
	conn, err := tp.Connect(s.TP)
	if err != nil {
		return fmt.Errorf("rbac: connect: %w", err)
	}
	defer conn.Close()
	batch, err := conn.PrepareBatch(context.Background(),
		"INSERT INTO agentguard_roles (role, description, permissions, builtin, deleted, updated_by)")
	if err != nil {
		return fmt.Errorf("rbac: prepare role: %w", err)
	}
	defer func() { _ = batch.Abort() }()
	if err := batch.Append(role, desc, permJSON, builtin, deleted, updatedBy); err != nil {
		return fmt.Errorf("rbac: append role: %w", err)
	}
	return batch.Send()
}

// ── Assignments ────────────────────────────────────────────────────────────

func (s *Store) ListAssignments() ([]Assignment, error) {
	rows, err := tp.Query(s.TP, `SELECT user, role,
  to_string(updated_at) AS updated_at, updated_by
FROM table(agentguard_user_roles) WHERE NOT deleted ORDER BY user`)
	if err != nil {
		return nil, err
	}
	out := make([]Assignment, 0, len(rows))
	for _, r := range rows {
		out = append(out, Assignment{
			User: asStr(r["user"]), Role: asStr(r["role"]),
			UpdatedAt: asStr(r["updated_at"]), UpdatedBy: asStr(r["updated_by"]),
		})
	}
	return out, nil
}

// RoleForUser returns the user's assigned role name, or ("", false) if none.
func (s *Store) RoleForUser(user string) (string, bool, error) {
	assigns, err := s.ListAssignments()
	if err != nil {
		return "", false, err
	}
	for _, a := range assigns {
		if a.User == user {
			return a.Role, true, nil
		}
	}
	return "", false, nil
}

// AssignRole sets a user's role (upsert).
func (s *Store) AssignRole(user, role, updatedBy string) error {
	return s.writeAssignment(user, role, false, updatedBy)
}

// UnassignUser removes a user's role (back to no access).
func (s *Store) UnassignUser(user, updatedBy string) error {
	return s.writeAssignment(user, "", true, updatedBy)
}

func (s *Store) writeAssignment(user, role string, deleted bool, updatedBy string) error {
	conn, err := tp.Connect(s.TP)
	if err != nil {
		return fmt.Errorf("rbac: connect: %w", err)
	}
	defer conn.Close()
	batch, err := conn.PrepareBatch(context.Background(),
		"INSERT INTO agentguard_user_roles (user, role, deleted, updated_by)")
	if err != nil {
		return fmt.Errorf("rbac: prepare assignment: %w", err)
	}
	defer func() { _ = batch.Abort() }()
	if err := batch.Append(user, role, deleted, updatedBy); err != nil {
		return fmt.Errorf("rbac: append assignment: %w", err)
	}
	return batch.Send()
}

// SeedBuiltins upserts the admin and viewer roles, refreshing their permission
// sets from the catalog (idempotent; safe to run on every provision).
func (s *Store) SeedBuiltins(updatedBy string) error {
	if err := s.UpsertRole(Role{
		Name: RoleAdmin, Description: "Full access, including access control.",
		Permissions: AllPermissions(), Builtin: true,
	}, updatedBy); err != nil {
		return err
	}
	return s.UpsertRole(Role{
		Name: RoleViewer, Description: "View-only: read every page, no changes.",
		Permissions: ViewerPermissions(), Builtin: true,
	}, updatedBy)
}
