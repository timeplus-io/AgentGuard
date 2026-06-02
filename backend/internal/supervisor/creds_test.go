package supervisor

import "testing"

func TestStaticCredentials_ReturnsConfigured(t *testing.T) {
	var c TimeplusCredentials = NewStaticCredentials("proton", "timeplus@t+")
	if c.User() != "proton" {
		t.Errorf("user: want proton, got %q", c.User())
	}
	if c.Password() != "timeplus@t+" {
		t.Errorf("password: want timeplus@t+, got %q", c.Password())
	}
}

func TestDefaultDevCredentials(t *testing.T) {
	c := DefaultDevCredentials()
	// A directly-launched timeplusd accepts the built-in `default` user (no password).
	if c.User() != "default" || c.Password() != "" {
		t.Errorf("unexpected dev creds: %q/%q", c.User(), c.Password())
	}
}
