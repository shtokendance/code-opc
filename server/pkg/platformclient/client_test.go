package platformclient

import (
	"context"
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
