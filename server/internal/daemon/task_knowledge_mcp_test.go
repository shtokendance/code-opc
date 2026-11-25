package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestExtractProjectKnowledgeBases(t *testing.T) {
	t.Parallel()
	resources := []ProjectResourceData{
		{
			ID:           "r1",
			ResourceType: "github_repo",
			ResourceRef:  json.RawMessage(`{"url":"https://github.com/example/repo"}`),
		},
		{
			ID:           "r2",
			ResourceType: "knowledge_base",
			Label:        "Product Docs",
			ResourceRef:  json.RawMessage(`{"tenant_id":"100","knowledge_base_id":"kb-1","knowledge_base_name":"Product Docs"}`),
		},
		{
			ID:           "r3",
			ResourceType: "knowledge_base",
			Label:        "Product Docs Duplicate",
			ResourceRef:  json.RawMessage(`{"tenant_id":"100","knowledge_base_id":"kb-1"}`),
		},
		{
			ID:           "r4",
			ResourceType: "knowledge_base",
			Label:        "Runbooks",
			ResourceRef:  json.RawMessage(`{"tenant_id":"100","knowledge_base_id":"kb-2"}`),
		},
	}

	got := extractProjectKnowledgeBases(resources)
	if len(got) != 2 {
		t.Fatalf("expected 2 unique knowledge bases, got %d: %+v", len(got), got)
	}
	if got[0].KnowledgeBaseID != "kb-1" || got[0].KnowledgeBaseName != "Product Docs" {
		t.Fatalf("unexpected first KB: %+v", got[0])
	}
	if got[1].KnowledgeBaseID != "kb-2" || got[1].KnowledgeBaseName != "Runbooks" {
		t.Fatalf("unexpected second KB: %+v", got[1])
	}
}

func TestStartTaskKnowledgeMCP_ListAndCall(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var searchedKBs []string
	cfgRaw, set, err := startTaskKnowledgeMCP(
		ctx,
		"task-1",
		[]projectKnowledgeBaseRef{
			{TenantID: "100", KnowledgeBaseID: "kb-1", KnowledgeBaseName: "Docs"},
			{TenantID: "100", KnowledgeBaseID: "kb-2", KnowledgeBaseName: "FAQ"},
		},
		func(_ context.Context, taskID, tenantID, kbID, query string, matchCount int) (json.RawMessage, error) {
			searchedKBs = append(searchedKBs, kbID)
			if taskID != "task-1" || tenantID != "100" || query != "deploy" || matchCount != 5 {
				t.Fatalf("unexpected search args: task=%s tenant=%s kb=%s query=%s count=%d", taskID, tenantID, kbID, query, matchCount)
			}
			return json.RawMessage(`{"results":[{"title":"Deploy Guide","content":"Run make up","score":0.91}]}`), nil
		},
		nil,
	)
	if err != nil {
		t.Fatalf("startTaskKnowledgeMCP: %v", err)
	}
	defer set.Close()

	var cfg struct {
		McpServers map[string]struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(cfgRaw, &cfg); err != nil {
		t.Fatalf("unmarshal mcp config: %v", err)
	}
	entry, ok := cfg.McpServers[knowledgeMCPServerName]
	if !ok || entry.Type != "http" || entry.URL == "" {
		t.Fatalf("unexpected mcpServers entry: %+v", cfg.McpServers)
	}

	// 1. tools/list
	listReq := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	resp, err := http.Post(entry.URL, "application/json", bytes.NewBufferString(listReq))
	if err != nil {
		t.Fatalf("tools/list POST: %v", err)
	}
	defer resp.Body.Close()
	var listRes struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listRes); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}
	if len(listRes.Result.Tools) != 1 || listRes.Result.Tools[0].Name != knowledgeMCPToolName {
		t.Fatalf("unexpected tools/list result: %+v", listRes.Result.Tools)
	}
	if !strings.Contains(listRes.Result.Tools[0].Description, "kb-1") || !strings.Contains(listRes.Result.Tools[0].Description, "kb-2") {
		t.Fatalf("tool description missing bound KBs: %s", listRes.Result.Tools[0].Description)
	}

	// 2. tools/call without knowledge_base_id searches all bound KBs
	callReq := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search_knowledge_base","arguments":{"query":"deploy"}}}`
	callResp, err := http.Post(entry.URL, "application/json", bytes.NewBufferString(callReq))
	if err != nil {
		t.Fatalf("tools/call POST: %v", err)
	}
	defer callResp.Body.Close()
	var callRes struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.NewDecoder(callResp.Body).Decode(&callRes); err != nil {
		t.Fatalf("decode tools/call: %v", err)
	}
	if callRes.Result.IsError || len(callRes.Result.Content) == 0 || !strings.Contains(callRes.Result.Content[0].Text, "Deploy Guide") {
		t.Fatalf("unexpected tools/call result: %+v", callRes.Result)
	}
	if len(searchedKBs) != 2 {
		t.Fatalf("expected both bound KBs searched, got %v", searchedKBs)
	}

	// 3. tools/call with unbound knowledge_base_id returns tool error
	unboundReq := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search_knowledge_base","arguments":{"query":"deploy","knowledge_base_id":"kb-other"}}}`
	unboundResp, err := http.Post(entry.URL, "application/json", bytes.NewBufferString(unboundReq))
	if err != nil {
		t.Fatalf("tools/call unbound POST: %v", err)
	}
	defer unboundResp.Body.Close()
	var unboundRes struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.NewDecoder(unboundResp.Body).Decode(&unboundRes); err != nil {
		t.Fatalf("decode unbound tools/call: %v", err)
	}
	if !unboundRes.Result.IsError {
		t.Fatalf("expected isError=true for unbound knowledge_base_id, got %+v", unboundRes.Result)
	}
}
