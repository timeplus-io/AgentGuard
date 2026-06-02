package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
)

func init() { gin.SetMode(gin.TestMode) }

func TestAuthRequired_NoCookie(t *testing.T) {
	store := auth.NewSessionStore()
	r := gin.New()
	r.Use(auth.AuthRequired(store))
	r.GET("/test", func(c *gin.Context) { c.JSON(200, nil) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestAuthRequired_InvalidCookie(t *testing.T) {
	store := auth.NewSessionStore()
	r := gin.New()
	r.Use(auth.AuthRequired(store))
	r.GET("/test", func(c *gin.Context) { c.JSON(200, nil) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: "agentguard_session", Value: "bogus-id"})
	r.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestAuthRequired_ValidSession_InjectsCredentials(t *testing.T) {
	store := auth.NewSessionStore()
	sess := store.Create("alice", "s3cret")

	r := gin.New()
	r.Use(auth.AuthRequired(store))
	r.GET("/test", func(c *gin.Context) {
		user, _ := c.Get("timeplus_user")
		pass, _ := c.Get("timeplus_password")
		c.JSON(200, gin.H{"user": user, "pass": pass})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: "agentguard_session", Value: sess.ID})
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("want 200, got %d", w.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["user"] != "alice" {
		t.Errorf("injected user: want alice, got %s", body["user"])
	}
	if body["pass"] != "s3cret" {
		t.Errorf("injected pass: want s3cret, got %s", body["pass"])
	}
}

func TestRequestCfg_AbsentKeys_PreservesBaseCredentials(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	// No keys set in context

	base := &config.Config{
		Timeplus: config.TimeplusConfig{Host: "myhost", Port: 3218},
	}
	got := auth.RequestCfg(c, base)

	if got.Timeplus.User != "" {
		t.Errorf("want empty user, got %s", got.Timeplus.User)
	}
	if got.Timeplus.Password != "" {
		t.Errorf("want empty password, got %s", got.Timeplus.Password)
	}
	if got.Timeplus.Host != "myhost" {
		t.Error("host should be preserved")
	}
}

func TestRequestCfg_InjectsFromContext(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("timeplus_user", "alice")
	c.Set("timeplus_password", "s3cret")

	base := &config.Config{
		Timeplus: config.TimeplusConfig{Host: "localhost", Port: 3218, NativePort: 8463},
	}
	got := auth.RequestCfg(c, base)

	if got.Timeplus.User != "alice" {
		t.Errorf("user: want alice, got %s", got.Timeplus.User)
	}
	if got.Timeplus.Password != "s3cret" {
		t.Errorf("password: want s3cret, got %s", got.Timeplus.Password)
	}
	if got.Timeplus.Host != "localhost" {
		t.Error("host should be preserved from base config")
	}
	// Verify base config is not mutated
	if base.Timeplus.User != "" {
		t.Error("base config should not be mutated")
	}
}

func TestHasValidSession(t *testing.T) {
	store := auth.NewSessionStore()
	sess := store.Create("bob", "pw")

	// Valid cookie -> true
	c1, _ := gin.CreateTestContext(httptest.NewRecorder())
	c1.Request = httptest.NewRequest("POST", "/x", nil)
	c1.Request.AddCookie(&http.Cookie{Name: "agentguard_session", Value: sess.ID})
	if !auth.HasValidSession(c1, store) {
		t.Error("valid cookie should yield true")
	}

	// No cookie -> false
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Request = httptest.NewRequest("POST", "/x", nil)
	if auth.HasValidSession(c2, store) {
		t.Error("missing cookie should yield false")
	}
}
