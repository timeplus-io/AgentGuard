package supervisor

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/config"
)

func TestUp_Smoke(t *testing.T) {
	if os.Getenv("AGENTGUARD_SMOKE") != "1" {
		t.Skip("set AGENTGUARD_SMOKE=1 with a real timeplusd to run")
	}
	bin := os.Getenv("TIMEPLUSD_BIN")
	if bin == "" {
		t.Skip("set TIMEPLUSD_BIN to the timeplusd path")
	}

	dir := t.TempDir()
	cfg := &config.Config{
		Server:     config.ServerConfig{Port: 0},
		Timeplus:   config.TimeplusConfig{Host: "localhost", Port: 3218, NativePort: 8463},
		CatalogDir: dir,
		ConfigPath: dir + "/config.yaml",
	}
	mgr, err := catalog.New(catalog.SeedFS, dir)
	if err != nil {
		t.Fatal(err)
	}
	sup := New(cfg, mgr, DefaultDevCredentials(), bin, []string{"server"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sup.Boot(ctx)

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if sup.Status().State == StateReady {
			sup.Stop(10 * time.Second)
			return
		}
		if sup.Status().State == StateFailed {
			t.Fatalf("boot failed: %s — %s", sup.Status().Reason, sup.Status().Detail)
		}
		time.Sleep(time.Second)
	}
	t.Fatal("did not reach ready within 2m")
}
