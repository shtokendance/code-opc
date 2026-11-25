package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestHandleMikaQuickCreateSetsAssigneeAndOrigin(t *testing.T) {
	t.Parallel()

	var captured cli.CreateIssueRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/issues" {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if err := json.Unmarshal(body, &captured); err != nil {
				t.Fatalf("unmarshal CreateIssueRequest: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"issue-1","identifier":"DP-39","title":"Fix bug"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	d := &Daemon{}
	sdkClient := cli.NewAPIClient(srv.URL, "ws-1", "tok-1")
	task := Task{
		ID:                       "qc-task-1",
		AgentID:                  "agent-1",
		ProjectID:                "proj-1",
		ParentIssueID:            "parent-1",
		QuickCreatePrompt:        "Fix bug\nDetails here",
		QuickCreatePriority:      "high",
		QuickCreateDueDate:       "2026-12-01",
		QuickCreateAttachmentIDs: []string{"att-1"},
	}

	res, err := d.handleMikaQuickCreate(context.Background(), task, sdkClient, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("handleMikaQuickCreate: %v", err)
	}
	if res.Status != "completed" {
		t.Fatalf("status = %q, want completed", res.Status)
	}
	if captured.AssigneeType == nil || *captured.AssigneeType != "agent" {
		t.Fatalf("assignee_type = %v, want agent", captured.AssigneeType)
	}
	if captured.AssigneeID == nil || *captured.AssigneeID != "agent-1" {
		t.Fatalf("assignee_id = %v, want agent-1", captured.AssigneeID)
	}
	if captured.OriginType == nil || *captured.OriginType != "quick_create" {
		t.Fatalf("origin_type = %v, want quick_create", captured.OriginType)
	}
	if captured.OriginID == nil || *captured.OriginID != "qc-task-1" {
		t.Fatalf("origin_id = %v, want qc-task-1", captured.OriginID)
	}
	if captured.ProjectID == nil || *captured.ProjectID != "proj-1" {
		t.Fatalf("project_id = %v, want proj-1", captured.ProjectID)
	}
	if captured.ParentIssueID == nil || *captured.ParentIssueID != "parent-1" {
		t.Fatalf("parent_issue_id = %v, want parent-1", captured.ParentIssueID)
	}
	if captured.DueDate == nil || *captured.DueDate != "2026-12-01" {
		t.Fatalf("due_date = %v, want 2026-12-01", captured.DueDate)
	}
	if len(captured.AttachmentIDs) != 1 || captured.AttachmentIDs[0] != "att-1" {
		t.Fatalf("attachment_ids = %v, want [att-1]", captured.AttachmentIDs)
	}
}
