package platformclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestListServices_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected authorization header: %s", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/integration/v1/mcp/services" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("tenant") != "20001" {
			t.Errorf("unexpected tenant: %s", r.URL.Query().Get("tenant"))
		}
		if r.URL.Query().Get("include_schema") != "true" {
			t.Errorf("expected include_schema=true")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"services": [
				{
					"service_id": "weknora-kb",
					"tools": [
						{
							"name": "kb_search",
							"description": "搜索企业知识库",
							"input_schema": {"type": "object", "properties": {"query": {"type": "string"}}}
						}
					]
				}
			]
		}`))
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "test-token", 5*time.Second)
	services, err := client.ListServices(context.Background(), "20001", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].ServiceID != "weknora-kb" {
		t.Errorf("expected service_id=weknora-kb, got %s", services[0].ServiceID)
	}
	if len(services[0].Tools) != 1 || services[0].Tools[0].Name != "kb_search" {
		t.Errorf("expected tool kb_search, got %+v", services[0].Tools)
	}
}

func TestListServices_TimeoutOrError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error": "upstream unavailable"}`))
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "test-token", 1*time.Second)
	_, err := client.ListServices(context.Background(), "20001", true)
	if err == nil {
		t.Fatal("expected error on 502, got nil")
	}
}

func TestInvokeTask_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected authorization header: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected content-type: %s", r.Header.Get("Content-Type"))
		}
		if r.URL.Path != "/integration/v1/mcp/invoke-task" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}

		var req TaskInvocationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request body failed: %v", err)
		}
		if req.Subject != "user-1" || req.TenantID != "20001" || req.ServiceID != "weknora-kb" ||
			req.ToolName != "kb_search" || req.TaskID != "task-100" || req.Device != "dev-1" {
			t.Errorf("unexpected request content: %+v", req)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": {
				"invocation_id": "inv-001",
				"status": "created"
			}
		}`))
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "test-token", 5*time.Second)
	if client.BaseURL() != ts.URL {
		t.Errorf("expected BaseURL %s, got %s", ts.URL, client.BaseURL())
	}
	if client.ServiceToken() != "test-token" {
		t.Errorf("expected ServiceToken test-token, got %s", client.ServiceToken())
	}

	resp, err := client.InvokeTask(context.Background(), TaskInvocationRequest{
		Subject:   "user-1",
		TenantID:  "20001",
		ServiceID: "weknora-kb",
		ToolName:  "kb_search",
		Arguments: json.RawMessage(`{"query":"test"}`),
		TaskID:    "task-100",
		Device:    "dev-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success true, got false")
	}
	if resp.Data.InvocationID != "inv-001" || resp.Data.Status != "created" {
		t.Errorf("unexpected data: %+v", resp.Data)
	}
}

func TestInvokeTask_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{
			"success": false,
			"error": {
				"code": "SPACE_FORBIDDEN",
				"message": "tenant not in active entitlement set"
			}
		}`))
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "test-token", 5*time.Second)
	_, err := client.InvokeTask(context.Background(), TaskInvocationRequest{
		Subject:   "user-1",
		TenantID:  "20001",
		ServiceID: "weknora-kb",
		ToolName:  "kb_search",
		TaskID:    "task-100",
		Device:    "dev-1",
	})
	if err == nil {
		t.Fatal("expected error on 403, got nil")
	}
}

func TestGetInvocation_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected authorization header: %s", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/integration/v1/mcp/invocations/inv-001" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		q := r.URL.Query()
		if q.Get("subject") != "user-1" || q.Get("tenant") != "20001" ||
			q.Get("task_id") != "task-100" || q.Get("device") != "dev-1" {
			t.Errorf("unexpected query params: %s", r.URL.RawQuery)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": {
				"invocation_id": "inv-001",
				"status": "completed",
				"result_ref": {"answer": "knowledge result"}
			}
		}`))
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "test-token", 5*time.Second)
	resp, err := client.GetInvocation(context.Background(), "inv-001", "user-1", "20001", "task-100", "dev-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success true, got false")
	}
	if resp.Data.InvocationID != "inv-001" || resp.Data.Status != "completed" {
		t.Errorf("unexpected data: %+v", resp.Data)
	}
	if string(resp.Data.ResultRef) != `{"answer": "knowledge result"}` {
		t.Errorf("unexpected result_ref: %s", string(resp.Data.ResultRef))
	}
}

func TestGetInvocation_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "invocation not found"}`))
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "test-token", 5*time.Second)
	_, err := client.GetInvocation(context.Background(), "inv-missing", "user-1", "20001", "task-100", "dev-1")
	if err == nil {
		t.Fatal("expected error on 404, got nil")
	}
}
