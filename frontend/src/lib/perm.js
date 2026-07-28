// Permission-key helpers — must match backend internal/rbac/catalog.go.
export const uiKey = (resource) => `ui:${resource}`
export const readKey = (resource) => `api:${resource}:read`
export const writeKey = (resource) => `api:${resource}:write`

// Top nav resources in display order (key → route), used for nav filtering and
// the default landing redirect.
export const NAV_RESOURCES = [
  ['dashboard', '/dashboard'],
  ['agents', '/agents'],
  ['threats', '/threats'],
  ['cost', '/cost'],
  ['rules', '/rules'],
  ['dlp', '/dlp'],
  ['investigate', '/investigate'],
  ['sentry', '/sentry'],
  ['settings', '/settings'],
]
