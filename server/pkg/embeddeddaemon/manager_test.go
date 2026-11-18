package embeddeddaemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestProbeLocalRuntimesReal(t *testing.T) {
	mgr := NewManager(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := mgr.ProbeLocalRuntimes(ctx, "test-probe")
	if err != nil {
		t.Fatalf("ProbeLocalRuntimes failed: %v", err)
	}

	if res.ProbeResult != "success" {
		t.Errorf("expected success, got %s", res.ProbeResult)
	}

	t.Logf("Real probe detected %d local runtimes: %+v", res.RuntimeCount, res.ProviderSummary)
	if res.RuntimeCount == 0 {
		t.Logf("No runtimes detected on this host (normal if no agents are installed)")
	}
}

func TestSyncTokenReal(t *testing.T) {
	mgr := NewManager(nil)
	profile := "test-desktop-profile-" + time.Now().Format("150405")
	defer func() {
		if pDir, err := ProfileDir(profile); err == nil {
			_ = os.RemoveAll(pDir)
		}
	}()

	testToken := "multica_test_token_123456"
	testURL := "http://localhost:8080"

	if err := mgr.SyncToken(profile, testToken, testURL); err != nil {
		t.Fatalf("SyncToken failed: %v", err)
	}

	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		t.Fatalf("LoadCLIConfigForProfile failed: %v", err)
	}

	if cfg.Token != testToken {
		t.Errorf("expected token %s, got %s", testToken, cfg.Token)
	}
	if cfg.ServerURL != testURL {
		t.Errorf("expected server url %s, got %s", testURL, cfg.ServerURL)
	}
}

func TestLifecycleReal(t *testing.T) {
	mgr := NewManager(nil)
	profile := "test-desktop-lifecycle-" + time.Now().Format("150405")
	defer func() {
		if pDir, err := ProfileDir(profile); err == nil {
			_ = os.RemoveAll(pDir)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Server endpoint to allow daemon to connect successfully
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/daemon/workspaces" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	opts := DaemonOptions{
		Profile:        profile,
		ServerURL:      srv.URL,
		AuthToken:      "test_auth_token_for_daemon_lifecycle",
		WorkspacesRoot: t.TempDir(),
	}

	err := mgr.Start(ctx, opts)
	if err != nil {
		t.Fatalf("mgr.Start failed: %v", err)
	}

	// Wait briefly for health port to become active
	time.Sleep(300 * time.Millisecond)

	status, err := mgr.GetStatus(ctx, profile)
	if err != nil {
		t.Fatalf("mgr.GetStatus failed: %v", err)
	}

	if !status.Running {
		t.Fatalf("expected daemon to be running, got status: %+v", status)
	}
	t.Logf("Embedded daemon running successfully on port %d, pid %d", status.Port, status.PID)

	// Test Stop
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := mgr.Stop(stopCtx, profile); err != nil {
		t.Fatalf("mgr.Stop failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	statusAfter, _ := mgr.GetStatus(context.Background(), profile)
	if statusAfter.Running {
		t.Errorf("expected daemon to be stopped, but still running")
	}
}
