package rbac

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// routePermissions maps "METHOD /api/<full route pattern>" → the permission key
// required to call it. Extra backend route-groups (metrics, holds, activity,
// notifications, onboarding, proton, config) fold into the ten catalog
// resources so the admin only ever toggles those. GET → :read; mutating verbs
// → :write, except a few read-only POSTs (test/query/compile) mapped to :read.
//
// Keep this in sync with the protected route registrations in cmd/server/main.go.
// Any protected route NOT listed here is denied by default (fail-closed) once
// RBAC is active.
var routePermissions = map[string]string{
	// dashboard (+ live activity / notification feeds)
	"GET /api/dashboard":            ReadKey("dashboard"),
	"GET /api/activity":             ReadKey("dashboard"),
	"GET /api/notifications/stream": ReadKey("dashboard"),

	// agents (+ metrics)
	"GET /api/agents":                                                        ReadKey("agents"),
	"GET /api/agents/:id":                                                    ReadKey("agents"),
	"GET /api/agents/:id/sessions":                                           ReadKey("agents"),
	"GET /api/agents/:id/sessions/:session_id":                              ReadKey("agents"),
	"GET /api/agents/:id/sessions/:session_id/runs/:run_id/trace":           ReadKey("agents"),
	"GET /api/agents/:id/sessions/:session_id/timeline":                     ReadKey("agents"),
	"GET /api/agents/:id/events":                                            ReadKey("agents"),
	"GET /api/metrics/llm":                                                   ReadKey("agents"),
	"GET /api/metrics/tools":                                                 ReadKey("agents"),

	// threats (+ holds — human approvals for security responses)
	"GET /api/threats":                    ReadKey("threats"),
	"GET /api/threats/:id":                ReadKey("threats"),
	"POST /api/threats/:id/acknowledge":   WriteKey("threats"),
	"POST /api/threats/:id/clear":         WriteKey("threats"),
	"GET /api/holds":                      ReadKey("threats"),
	"GET /api/holds/:hold_id":             ReadKey("threats"),
	"POST /api/holds/:hold_id/decide":     WriteKey("threats"),
	"POST /api/holds/_drain":              WriteKey("threats"),

	// cost
	"GET /api/cost":                                ReadKey("cost"),
	"GET /api/cost/pricing":                        ReadKey("cost"),
	"POST /api/cost/pricing":                       WriteKey("cost"),
	"DELETE /api/cost/pricing/:provider/:model":    WriteKey("cost"),
	"GET /api/cost/budget":                         ReadKey("cost"),
	"POST /api/cost/budget":                        WriteKey("cost"),

	// rules
	"GET /api/rules":                       ReadKey("rules"),
	"GET /api/rules/catalog":               ReadKey("rules"),
	"POST /api/rules/catalog":              WriteKey("rules"),
	"POST /api/rules/catalog/import":       WriteKey("rules"),
	"GET /api/rules/catalog/export":        ReadKey("rules"),
	"POST /api/rules/catalog/compile":      ReadKey("rules"), // pure preview, no write
	"GET /api/rules/catalog/:id":           ReadKey("rules"),
	"PUT /api/rules/catalog/:id":           WriteKey("rules"),
	"DELETE /api/rules/catalog/:id":        WriteKey("rules"),
	"POST /api/rules/catalog/:id/clone":    WriteKey("rules"),
	"GET /api/rules/mv-status":             ReadKey("rules"),
	"POST /api/rules/sync":                 WriteKey("rules"),
	"POST /api/rules":                      WriteKey("rules"),
	"GET /api/rules/:id":                   ReadKey("rules"),
	"POST /api/rules/:id/install":          WriteKey("rules"),
	"PUT /api/rules/:id/configuration":     WriteKey("rules"),
	"POST /api/rules/:id/toggle":           WriteKey("rules"),
	"POST /api/rules/:id/upgrade":          WriteKey("rules"),
	"POST /api/rules/:id/policy":           WriteKey("rules"),
	"DELETE /api/rules/:id":                WriteKey("rules"),

	// dlp
	"GET /api/dlp/status":        ReadKey("dlp"),
	"POST /api/dlp/query":        ReadKey("dlp"), // read-only forensic query despite POST
	"GET /api/dlp/messages":      ReadKey("dlp"),
	"GET /api/dlp/messages/:id":  ReadKey("dlp"),
	"GET /api/dlp/profiles":      ReadKey("dlp"),
	"GET /api/dlp/config":        ReadKey("dlp"),
	"PUT /api/dlp/config/:key":   WriteKey("dlp"),

	// sentry
	"POST /api/sentry/chat":                             WriteKey("sentry"),
	"POST /api/sentry/approvals/:proposal_id/decide":    WriteKey("sentry"),
	"GET /api/sentry/conversations":                     ReadKey("sentry"),
	"GET /api/sentry/conversations/:id":                 ReadKey("sentry"),
	"PATCH /api/sentry/conversations/:id":               WriteKey("sentry"),
	"DELETE /api/sentry/conversations/:id":              WriteKey("sentry"),
	"GET /api/sentry/status":                            ReadKey("sentry"),
	"GET /api/sentry/config":                            ReadKey("sentry"),
	"PUT /api/sentry/config/:key":                       WriteKey("sentry"),
	"POST /api/sentry/config/test":                      ReadKey("sentry"), // test-only

	// investigate (+ raw proton query backing the SQL console)
	"GET /api/investigate/tables": ReadKey("investigate"),
	"POST /api/proton/query":      ReadKey("investigate"),

	// settings (config + onboarding)
	"GET /api/config":                    ReadKey("settings"),
	"PUT /api/config":                    WriteKey("settings"),
	"POST /api/config/test-timeplus":     ReadKey("settings"), // test-only
	"POST /api/config/test-dlp":          ReadKey("settings"), // test-only
	"POST /api/onboarding/generate":      WriteKey("settings"),
	"GET /api/onboarding/install-script": ReadKey("settings"),

	// access control (RBAC administration)
	"GET /api/permissions":          ReadKey("access"),
	"GET /api/roles":                ReadKey("access"),
	"POST /api/roles":               WriteKey("access"),
	"PUT /api/roles/:name":          WriteKey("access"),
	"DELETE /api/roles/:name":       WriteKey("access"),
	"GET /api/users":                ReadKey("access"),
	"PUT /api/users/:user/role":     WriteKey("access"),
}

// RequiredPermission returns the permission key for a method+route pattern, and
// whether the route is mapped.
func RequiredPermission(method, fullPath string) (string, bool) {
	p, ok := routePermissions[method+" "+fullPath]
	return p, ok
}

// Middleware enforces the permission required for the matched protected route.
// It must be chained AFTER auth.AuthRequired (which sets "timeplus_user").
// When RBAC is not yet active (unprovisioned) it allows everything (legacy).
func Middleware(res *Resolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		perm, mapped := RequiredPermission(c.Request.Method, c.FullPath())

		if !res.Active() {
			// RBAC not provisioned yet — allow everything (legacy behavior)
			// EXCEPT access-control administration: there is no admin to grant
			// it, so it must be bootstrapped via provisioning, never self-serve.
			if mapped && (perm == ReadKey("access") || perm == WriteKey("access")) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "access control is not enabled"})
				return
			}
			c.Next()
			return
		}

		userVal, _ := c.Get("timeplus_user")
		user, _ := userVal.(string)

		if !mapped {
			// Unknown protected route — fail closed.
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}

		perms, _, err := res.PermissionsFor(user)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "access control unavailable"})
			return
		}
		if !perms[perm] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":    "insufficient permissions",
				"required": perm,
			})
			return
		}
		c.Next()
	}
}
