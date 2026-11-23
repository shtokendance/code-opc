package embeddeddaemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestServerClientInteractions(t *testing.T) {
	// Setup test server that handles all standard CLI endpoints
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer valid_token" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/me":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    "user_123",
				"name":  "Test Engineer",
				"email": "engineer@multica.ai",
			})
		case "/api/workspaces":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":   "ws_1",
					"name": "Primary Workspace",
					"slug": "primary",
					"role": "admin",
				},
			})
		case "/api/upload-file":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           "att_999",
				"url":          "http://example.com/att_999",
				"download_url": "http://example.com/att_999/download",
				"markdown_url": "http://example.com/att_999/md",
				"filename":     "test.png",
				"content_type": "image/png",
				"size_bytes":   1024,
			})
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer srv.Close()

	client := NewServerClient(srv.URL, "ws_1", "valid_token")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Test Whoami
	user, err := client.Whoami(ctx)
	if err != nil {
		t.Fatalf("Whoami failed: %v", err)
	}
	if user.Email != "engineer@multica.ai" || user.Name != "Test Engineer" {
		t.Errorf("unexpected user: %+v", user)
	}
	t.Logf("ServerClient.Whoami succeeded: %+v", user)

	// 2. Test ListWorkspaces
	workspaces, err := client.ListWorkspaces(ctx)
	if err != nil {
		t.Fatalf("ListWorkspaces failed: %v", err)
	}
	if len(workspaces) != 1 || workspaces[0].Slug != "primary" {
		t.Errorf("unexpected workspaces: %+v", workspaces)
	}
	t.Logf("ServerClient.ListWorkspaces succeeded: %+v", workspaces)

	// 3. Test Unauthorized request
	badClient := NewServerClient(srv.URL, "ws_1", "invalid_token")
	_, err = badClient.Whoami(ctx)
	if err == nil {
		t.Fatalf("expected unauthorized error, got nil")
	}
	t.Logf("Expected auth rejection succeeded: %v", err)

	// 4. Test Generic JSON
	var genericResp map[string]any
	if err := client.GetJSON(ctx, "/api/custom", &genericResp); err != nil {
		t.Fatalf("GetJSON failed: %v", err)
	}
	if genericResp["status"] != "ok" {
		t.Errorf("unexpected custom status: %+v", genericResp)
	}
}
