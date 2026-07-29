package handlers

import "github.com/gin-gonic/gin"

// supportedLocales mirrors the ui.default_language enum in
// internal/config/registry.go. Keep the two in sync.
var supportedLocales = map[string]bool{
	"en":    true,
	"zh-CN": true,
}

// resolveLocale returns the request's UI locale from the ?locale= query param,
// defaulting to "en" for an absent or unsupported value. The UI sends its
// active i18n locale explicitly (not Accept-Language).
func resolveLocale(c *gin.Context) string {
	loc := c.Query("locale")
	if supportedLocales[loc] {
		return loc
	}
	return "en"
}
