package handlers

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestResolveLocale(t *testing.T) {
	cases := map[string]string{
		"zh-CN": "zh-CN",
		"en":    "en",
		"":      "en",
		"fr":    "en",
		"zh":    "en",
		"ZH-CN": "en", // case-sensitive, matches the config enum exactly
	}
	for in, want := range cases {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/x?locale="+in, nil)
		if got := resolveLocale(c); got != want {
			t.Errorf("resolveLocale(locale=%q) = %q, want %q", in, got, want)
		}
	}
}
