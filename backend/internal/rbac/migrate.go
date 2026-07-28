package rbac

import "github.com/timeplus/agentguard/internal/config"

// Migrate activates RBAC on a deployment that completed setup BEFORE RBAC
// existed (its streams were never seeded by the setup wizard). It ensures the
// streams, seeds/refreshes the built-in roles, and — only when no user is
// assigned any role yet (a genuinely fresh migration) — makes the service
// account the initial administrator so there is always a way in. It is
// idempotent and a no-op once any assignment exists, so a deliberately
// configured deployment is never overridden.
//
// New installs don't need this: the setup wizard seeds RBAC at provision time,
// so by the time SetupComplete is true RBAC is already active and Migrate finds
// an assignment and does nothing.
func Migrate(cfg *config.TimeplusConfig, serviceUser string) error {
	if err := EnsureStreams(cfg); err != nil {
		return err
	}
	store := &Store{TP: cfg}
	if err := store.SeedBuiltins(serviceUser); err != nil {
		return err
	}
	assigns, err := store.ListAssignments()
	if err != nil {
		return err
	}
	if len(assigns) > 0 {
		return nil // already configured — leave assignments alone
	}
	if serviceUser == "" {
		serviceUser = "default"
	}
	return store.AssignRole(serviceUser, RoleAdmin, serviceUser)
}
