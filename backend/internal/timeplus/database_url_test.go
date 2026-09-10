package timeplus

import (
	"testing"

	"github.com/timeplus/agentguard/internal/config"
)

func TestHTTPURL(t *testing.T) {
	cfg := &config.TimeplusConfig{Host: "h", Port: 3218}
	if got := HTTPURL(cfg, ""); got != "http://h:3218/" {
		t.Errorf("got %q", got)
	}
	cfg.Database = "default"
	if got := HTTPURL(cfg, ""); got != "http://h:3218/" {
		t.Errorf("default db must not be appended, got %q", got)
	}
	cfg.Database = "ag"
	if got := HTTPURL(cfg, ""); got != "http://h:3218/?database=ag" {
		t.Errorf("got %q", got)
	}
	if got := HTTPURL(cfg, "default_format=JSONEachRow"); got != "http://h:3218/?default_format=JSONEachRow&database=ag" {
		t.Errorf("got %q", got)
	}
	cfg.Database = ""
	if got := HTTPURL(cfg, "default_format=JSONEachRow"); got != "http://h:3218/?default_format=JSONEachRow" {
		t.Errorf("got %q", got)
	}
}
