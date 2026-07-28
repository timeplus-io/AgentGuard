package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/rbac"
)

// RBAC administration endpoints (#243). All are gated by api:access:* via the
// rbac middleware, so only roles granted access-control administration reach
// them. `updated_by` records the acting session user for audit.

// ListPermissions returns the permission catalog for building the admin UI.
func ListPermissions() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"resources": rbac.Resources,
			"actions":   []string{"ui", "read", "write"},
		})
	}
}

// ListRoles returns all roles with their permission sets.
func ListRoles(store *rbac.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		roles, err := store.ListRoles()
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"roles": roles})
	}
}

type roleBody struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// CreateRole creates a custom role.
func CreateRole(store *rbac.Store, res *rbac.Resolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body roleBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
			return
		}
		name := strings.TrimSpace(body.Name)
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "role name is required"})
			return
		}
		if name == rbac.RoleAdmin || name == rbac.RoleViewer {
			c.JSON(http.StatusConflict, gin.H{"error": "that name is reserved for a built-in role"})
			return
		}
		if _, exists, err := store.GetRole(name); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		} else if exists {
			c.JSON(http.StatusConflict, gin.H{"error": "a role with that name already exists"})
			return
		}
		if err := rbac.ValidatePermissions(body.Permissions); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := store.UpsertRole(rbac.Role{
			Name: name, Description: body.Description, Permissions: body.Permissions, Builtin: false,
		}, sessionUser(c)); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		res.Invalidate()
		c.JSON(http.StatusCreated, gin.H{"status": "created"})
	}
}

// UpdateRole edits a custom role's permissions/description (built-in → 409).
func UpdateRole(store *rbac.Store, res *rbac.Resolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		existing, exists, err := store.GetRole(name)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "role not found"})
			return
		}
		if existing.Builtin {
			c.JSON(http.StatusConflict, gin.H{"error": "built-in roles cannot be edited"})
			return
		}
		var body roleBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
			return
		}
		if err := rbac.ValidatePermissions(body.Permissions); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := store.UpsertRole(rbac.Role{
			Name: name, Description: body.Description, Permissions: body.Permissions, Builtin: false,
		}, sessionUser(c)); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		res.Invalidate()
		c.JSON(http.StatusOK, gin.H{"status": "updated"})
	}
}

// DeleteRole removes a custom role (built-in → 409). Users on it fall back to
// no access (their assignment now points at a missing role).
func DeleteRole(store *rbac.Store, res *rbac.Resolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		existing, exists, err := store.GetRole(name)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "role not found"})
			return
		}
		if existing.Builtin {
			c.JSON(http.StatusConflict, gin.H{"error": "built-in roles cannot be deleted"})
			return
		}
		if err := store.DeleteRole(name, sessionUser(c)); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		res.Invalidate()
		c.JSON(http.StatusOK, gin.H{"status": "deleted"})
	}
}

// ListUsers returns current user→role assignments.
func ListUsers(store *rbac.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		assigns, err := store.ListAssignments()
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"users": assigns})
	}
}

// AssignUserRole sets (or clears, with an empty role) a user's role. Guards
// against removing the last access-control administrator (self-lockout).
func AssignUserRole(store *rbac.Store, res *rbac.Resolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := c.Param("user")
		var body struct {
			Role string `json:"role"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
			return
		}
		role := strings.TrimSpace(body.Role)

		roles, err := store.ListRoles()
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		permsByRole := map[string][]string{}
		for _, r := range roles {
			permsByRole[r.Name] = r.Permissions
		}
		if role != "" {
			if _, ok := permsByRole[role]; !ok {
				c.JSON(http.StatusBadRequest, gin.H{"error": "unknown role"})
				return
			}
		}

		// Self-lockout guard: refuse a change that would leave zero users able
		// to administer access control.
		if err := guardLastAdmin(store, permsByRole, user, role); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if role == "" {
			err = store.UnassignUser(user, sessionUser(c))
		} else {
			err = store.AssignRole(user, role, sessionUser(c))
		}
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		res.Invalidate()
		c.JSON(http.StatusOK, gin.H{"status": "assigned"})
	}
}

// guardLastAdmin returns an error if assigning `user` the role `newRole` would
// remove the final holder of the access-control admin permission.
func guardLastAdmin(store *rbac.Store, permsByRole map[string][]string, user, newRole string) error {
	has := func(perms []string) bool {
		for _, p := range perms {
			if p == rbac.AdminPermission {
				return true
			}
		}
		return false
	}
	newIsAdmin := newRole != "" && has(permsByRole[newRole])
	if newIsAdmin {
		return nil // not removing an admin
	}
	assigns, err := store.ListAssignments()
	if err != nil {
		return nil // don't block on a read error here
	}
	admins := map[string]bool{}
	for _, a := range assigns {
		if has(permsByRole[a.Role]) {
			admins[a.User] = true
		}
	}
	if admins[user] && len(admins) <= 1 {
		return errInLastAdmin
	}
	return nil
}

type rbacError string

func (e rbacError) Error() string { return string(e) }

const errInLastAdmin = rbacError("cannot remove the last access-control administrator; assign the admin role to another user first")
