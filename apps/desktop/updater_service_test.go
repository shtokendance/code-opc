package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunAutoUpdateLoopChecksOnStartupAndInterval(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/desktop/updates/check" {
			hits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"available":      false,
				"latest_version": appVersion,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	svc := NewAppService()
	svc.targetAPIURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go svc.runAutoUpdateLoop(ctx, 10*time.Millisecond, 25*time.Millisecond)

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if hits.Load() >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	if got := hits.Load(); got < 2 {
		t.Fatalf("expected at least 2 auto-update checks (initial + ticker), got %d", got)
	}
}

func TestRunAutoUpdateLoopSkipsWhenDisabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	svc := NewAppService()
	svc.targetAPIURL = srv.URL
	svc.SetUpdatePreferences(false)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()

	svc.runAutoUpdateLoop(ctx, 5*time.Millisecond, 15*time.Millisecond)

	if got := hits.Load(); got != 0 {
		t.Fatalf("expected 0 auto-update checks when disabled, got %d", got)
	}
}
