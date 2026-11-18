package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestIsInternalMikaTask(t *testing.T) {
	d := &Daemon{}

	tests := []struct {
		name string
		task Task
		want bool
	}{
		{
			name: "Mika agent task",
			task: Task{
				Agent: &AgentData{Name: "Mika"},
			},
			want: true,
		},
		{
			name: "Mika agent lowercase",
			task: Task{
				Agent: &AgentData{Name: "mika"},
			},
			want: true,
		},
		{
			name: "Chat session task targeting default",
			task: Task{
				ChatSessionID: "session-123",
			},
			want: true,
		},
		{
			name: "Quick create task",
			task: Task{
				QuickCreatePrompt: "Create a new homepage issue",
			},
			want: true,
		},
		{
			name: "Regular coding agent issue task",
			task: Task{
				IssueID: "issue-123",
				Agent:   &AgentData{Name: "developer"},
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := d.isInternalMikaTask(tc.task)
			if got != tc.want {
				t.Errorf("isInternalMikaTask(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestMikaThreadQuickCreateExecution(t *testing.T) {
	createdIssue := cli.Issue{
		ID:         "issue-abc",
		Identifier: "THEB-99",
		Title:      "优化工作区首页加载速度",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/daemon/tasks/task-1/start":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "running"})
		case "/api/daemon/tasks/task-1/progress":
			w.WriteHeader(http.StatusOK)
		case "/api/projects":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]cli.Project{
				{ID: "proj-1", Title: "前端优化"},
			})
		case "/api/issues":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(createdIssue)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	d := &Daemon{
		cfg: Config{
			ServerBaseURL: server.URL,
		},
		client: NewClient(server.URL),
	}

	task := Task{
		ID:                "task-1",
		WorkspaceID:       "ws-1",
		QuickCreatePrompt: "创建任务: 优化工作区首页加载速度",
	}

	res, err := d.runMikaInternalThread(context.Background(), task, slog.Default())
	if err != nil {
		t.Fatalf("runMikaInternalThread failed: %v", err)
	}

	if res.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", res.Status)
	}

	if !containsString(res.Comment, "THEB-99") {
		t.Errorf("expected comment to mention THEB-99, got %q", res.Comment)
	}
}

func TestMikaThreadChatMessageExecution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/daemon/tasks/task-2/start":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "running"})
		case "/api/daemon/tasks/task-2/progress":
			w.WriteHeader(http.StatusOK)
		case "/api/projects":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]cli.Project{
				{ID: "proj-1", Title: "Web前端工程", IssueCount: 5, DoneCount: 2},
			})
		case "/api/issues":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issues": []cli.Issue{
					{ID: "iss-1", Identifier: "THEB-1", Title: "集成测试", Status: "in_progress"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	d := &Daemon{
		cfg: Config{
			ServerBaseURL: server.URL,
		},
		client: NewClient(server.URL),
	}

	task := Task{
		ID:            "task-2",
		WorkspaceID:   "ws-1",
		ChatSessionID: "session-abc",
		ChatMessage:   "有哪些项目？",
		Agent:         &AgentData{Name: "Mika"},
	}

	res, err := d.runMikaInternalThread(context.Background(), task, slog.Default())
	if err != nil {
		t.Fatalf("runMikaInternalThread failed: %v", err)
	}

	if res.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", res.Status)
	}

	if !containsString(res.Comment, "Web前端工程") {
		t.Errorf("expected response to contain project name 'Web前端工程', got %q", res.Comment)
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
