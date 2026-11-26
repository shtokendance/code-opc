package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/platformclient"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
	"github.com/multica-ai/multica/server/pkg/remotemcp/remotemcptest"
)

func TestRemoteMCPServerNameUsesSafeDistinctContributionIdentities(t *testing.T) {
	connections := []remotemcp.Connection{
		{ContributionKey: "example__toolbox", ContributionID: "plugin:12345678-1234-4234-8234-1234567890ab:toolbox"},
		{ContributionKey: "example__toolbox", ContributionID: "plugin:12345678-1234-4234-8234-1234567890ac:toolbox"},
		{ContributionKey: "example__toolbox", ContributionID: "plugin:12345678-1234-4234-8234-1234567890ab:other_hook"},
		{ContributionKey: "Mixed Case/☃", ContributionID: "00000000-0000-0000-0000-000000000001"},
	}
	safeName := regexp.MustCompile(`^[a-z0-9_-]+$`)
	seen := map[string]bool{}
	for _, connection := range connections {
		name := remoteMCPServerName(connection)
		if !safeName.MatchString(name) {
			t.Errorf("Codex-incompatible server name %q for %q", name, connection.ContributionID)
		}
		if seen[name] {
			t.Errorf("distinct contribution identities alias to %q", name)
		}
		seen[name] = true
		if again := remoteMCPServerName(connection); again != name {
			t.Fatalf("server identity changed: %q != %q", again, name)
		}
	}
}

func TestRemoteMCPProxyFiltersToolsAndInjectsCredential(t *testing.T) {
	fixture := remotemcptest.NewServer()
	defer fixture.Close()
	schema := json.RawMessage(`{"type":"object","properties":{}}`)
	canonical, err := canonicalRemoteMCPJSON(schema)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, _ := url.Parse(fixture.URL)
	proxy := &remoteMCPProxy{
		taskID: "task", endpoint: endpoint, client: fixture.Client(), path: "/capability",
		credentialHeaders: http.Header{"Authorization": []string{"Bearer " + remotemcptest.Credential}},
		semaphore:         make(chan struct{}, remoteMCPMaxConcurrency), logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		connection: remotemcp.Connection{
			InstallationID: "installation", ContributionKey: "fixture",
			ApprovedTools: []remotemcp.Tool{{
				Name: "fixture.read", Description: "pinned", InputSchema: canonical,
				SchemaDigest: remotemcp.DigestBytes(canonical), Risk: "read",
			}},
		},
	}

	request := httptest.NewRequest(http.MethodPost, "/capability", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "fixture.write") || !strings.Contains(response.Body.String(), "fixture.read") {
		t.Fatalf("filtered tools/list = %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "pinned") {
		t.Fatalf("tools/list did not preserve reviewed description: %s", response.Body.String())
	}

	writeSchema, err := canonicalRemoteMCPJSON(json.RawMessage(`{
		"type":"object",
		"required":["value"],
		"properties":{"value":{"type":"string"}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	proxy.connection.ApprovedTools = append(proxy.connection.ApprovedTools, remotemcp.Tool{
		Name: "fixture.write", InputSchema: writeSchema, SchemaDigest: remotemcp.DigestBytes(writeSchema), Risk: "write",
	})
	writeRequest := httptest.NewRequest(http.MethodPost, "/capability", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"fixture.write","arguments":{"value":"broker-write"}}}`))
	writeResponse := httptest.NewRecorder()
	proxy.ServeHTTP(writeResponse, writeRequest)
	if writes := fixture.Writes(); len(writes) != 1 || writes[0] != "broker-write" {
		t.Fatalf("fixture writes = %#v, response = %s", writes, writeResponse.Body.String())
	}
}

func TestRemoteMCPProxyRejectsUnapprovedToolWithoutCallingUpstream(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer upstream.Close()
	endpoint, _ := url.Parse(upstream.URL)
	proxy := &remoteMCPProxy{
		endpoint: endpoint, client: upstream.Client(), path: "/capability",
		semaphore:  make(chan struct{}, remoteMCPMaxConcurrency),
		connection: remotemcp.Connection{ApprovedTools: []remotemcp.Tool{{Name: "allowed"}}},
	}
	request := httptest.NewRequest(http.MethodPost, "/capability", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"denied","arguments":{"secret":"not logged"}}}`))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if called || !strings.Contains(response.Body.String(), "not approved") {
		t.Fatalf("called=%v response=%s", called, response.Body.String())
	}
}

func TestRemoteMCPProxyPreservesAcceptedNotificationStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Fatalf("upstream method = %s", request.Method)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer upstream.Close()
	endpoint, _ := url.Parse(upstream.URL)
	proxy := &remoteMCPProxy{
		endpoint: endpoint, client: upstream.Client(), path: "/capability",
		semaphore: make(chan struct{}, remoteMCPMaxConcurrency),
	}

	request := httptest.NewRequest(http.MethodPost, "/capability", bytes.NewBufferString(
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
	))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusAccepted)
	}
	if response.Body.Len() != 0 {
		t.Fatalf("notification response body = %q, want empty", response.Body.String())
	}
}

func TestRemoteMCPProxyRechecksCredentialBeforeUpstreamCall(t *testing.T) {
	const contributionID = "plugin:12345678-1234-4234-8234-1234567890ab:toolbox"
	called := false
	credentialChecked := false
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer upstream.Close()
	endpoint, _ := url.Parse(upstream.URL)
	proxy := &remoteMCPProxy{
		endpoint: endpoint, client: upstream.Client(), path: "/capability",
		semaphore: make(chan struct{}, remoteMCPMaxConcurrency),
		connection: remotemcp.Connection{
			ContributionID: contributionID, CredentialHeader: "Authorization",
			ApprovedTools: []remotemcp.Tool{{Name: "allowed"}},
		},
		resolveCredential: func(_ context.Context, id string) (http.Header, error) {
			credentialChecked = true
			if id != contributionID {
				t.Errorf("credential lookup identity = %q, want original %q", id, contributionID)
			}
			return nil, errors.New("revoked")
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/capability", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"allowed","arguments":{}}}`))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if !credentialChecked || called || !strings.Contains(response.Body.String(), "revoked or unavailable") {
		t.Fatalf("credentialChecked=%v called=%v response=%s", credentialChecked, called, response.Body.String())
	}
}

func TestRemoteMCPProviderMatrixAndConfigMerge(t *testing.T) {
	for _, provider := range []string{"codex", "claude", "hermes", "qoder", "mcode"} {
		if !providerSupportsRemoteMCPBroker(provider) {
			t.Fatalf("provider %s must support Remote MCP", provider)
		}
	}
	if providerSupportsRemoteMCPBroker("deveco") {
		t.Fatal("unsupported provider was accepted")
	}
	merged, err := mergeTaskRemoteMCPConfig(
		json.RawMessage(`{"mcpServers":{"agent":{"command":"agent"}}}`),
		json.RawMessage(`{"mcpServers":{"plugin":{"type":"http","url":"http://127.0.0.1/mcp"}}}`),
	)
	if err != nil || !strings.Contains(string(merged), `"agent"`) || !strings.Contains(string(merged), `"plugin"`) {
		t.Fatalf("merge = %s, %v", merged, err)
	}
}

func TestRemoteMCPProxyFiltersMultiLineAndNotificationPrefixedSSEToolsList(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{}}`)
	canonical, err := canonicalRemoteMCPJSON(schema)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		_, _ = io.WriteString(w,
			"event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progress\":1}}\n\n"+
				"event: message\n"+
				"data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[\n"+
				"data: {\"name\":\"fixture.read\",\"description\":\"Read\",\"inputSchema\":{\"type\":\"object\",\"properties\":{}}},\n"+
				"data: {\"name\":\"fixture.write\",\"description\":\"Write\",\"inputSchema\":{\"type\":\"object\",\"properties\":{}}}\n"+
				"data: ]}}\n\n")
		w.(http.Flusher).Flush()
		// Keep the stream open, as Streamable HTTP servers may.
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	defer close(release)
	endpoint, _ := url.Parse(upstream.URL)
	proxy := &remoteMCPProxy{
		taskID: "task", endpoint: endpoint, client: upstream.Client(), path: "/capability",
		semaphore: make(chan struct{}, remoteMCPMaxConcurrency),
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		connection: remotemcp.Connection{
			InstallationID: "installation", ContributionKey: "fixture",
			ApprovedTools: []remotemcp.Tool{{
				Name: "fixture.read", Description: "pinned", InputSchema: canonical,
				SchemaDigest: remotemcp.DigestBytes(canonical), Risk: "read",
			}},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/capability", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if strings.Contains(body, `"error"`) || strings.Contains(body, "fixture.write") || !strings.Contains(body, "fixture.read") {
		t.Fatalf("filtered SSE tools/list = %s", body)
	}
}

func TestRemoteMCPServerName_EnterprisePrefix(t *testing.T) {
	conn := remotemcp.Connection{
		ContributionKey: "weknora-kb",
		ContributionID:  remotemcp.EnterpriseContributionPrefix + "weknora-kb",
	}
	name := remoteMCPServerName(conn)
	if !strings.HasPrefix(name, "enterprise-weknora-kb-") {
		t.Errorf("expected server name to start with enterprise-weknora-kb-, got %q", name)
	}
}

func TestEnterpriseRemoteMCPProxy_InitializeAndPing(t *testing.T) {
	proxy := &remoteMCPProxy{
		taskID: "task-1",
		path:   "/enterprise",
		connection: remotemcp.Connection{
			ContributionID:  remotemcp.EnterpriseContributionPrefix + "service-1",
			ContributionKey: "enterprise-service-1",
		},
		semaphore: make(chan struct{}, remoteMCPMaxConcurrency),
	}

	// initialize
	req := httptest.NewRequest(http.MethodPost, "/enterprise", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("initialize status = %d: %s", w.Code, w.Body.String())
	}
	var initResp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &initResp); err != nil {
		t.Fatalf("unmarshal init response: %v", err)
	}
	if initResp.Result.ProtocolVersion != "2024-11-05" {
		t.Errorf("unexpected protocolVersion: %s", initResp.Result.ProtocolVersion)
	}

	// notifications/initialized
	req = httptest.NewRequest(http.MethodPost, "/enterprise", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`))
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("notifications/initialized status = %d", w.Code)
	}

	// notifications/cancelled
	req = httptest.NewRequest(http.MethodPost, "/enterprise", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{}}`))
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("notifications/cancelled status = %d", w.Code)
	}

	// ping
	req = httptest.NewRequest(http.MethodPost, "/enterprise", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"ping","params":{}}`))
	w = httptest.NewRecorder()
	proxy.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ping status = %d: %s", w.Code, w.Body.String())
	}
}

func TestEnterpriseRemoteMCPProxy_ToolsList(t *testing.T) {
	proxy := &remoteMCPProxy{
		taskID: "task-1",
		path:   "/enterprise",
		connection: remotemcp.Connection{
			ContributionID:  remotemcp.EnterpriseContributionPrefix + "service-1",
			ContributionKey: "enterprise-service-1",
			ApprovedTools: []remotemcp.Tool{
				{
					Name:        "kb_search",
					Description: "Search KB",
					InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
				},
			},
		},
		semaphore: make(chan struct{}, remoteMCPMaxConcurrency),
	}

	req := httptest.NewRequest(http.MethodPost, "/enterprise", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tools/list status = %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal tools/list response: %v", err)
	}
	if len(resp.Result.Tools) != 1 || resp.Result.Tools[0].Name != "kb_search" {
		t.Fatalf("unexpected tools: %+v", resp.Result.Tools)
	}
}

func TestEnterpriseRemoteMCPProxy_ToolsCall_SuccessWithPolling(t *testing.T) {
	pollCount := 0
	platformServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/integration/v1/mcp/invoke-task" {
			var invokeReq platformclient.TaskInvocationRequest
			_ = json.NewDecoder(r.Body).Decode(&invokeReq)
			if invokeReq.ToolName != "kb_search" || invokeReq.Subject != "user-123" {
				t.Errorf("unexpected invokeReq: %+v", invokeReq)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"invocation_id":"inv-999","status":"executing"}}`))
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/integration/v1/mcp/invocations/inv-999" {
			pollCount++
			if pollCount == 1 {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"success":true,"data":{"invocation_id":"inv-999","status":"executing"}}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"invocation_id":"inv-999","status":"completed","result_ref":"found 3 articles"}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer platformServer.Close()

	client := platformclient.NewClient(platformServer.URL, "test-token", 5*time.Second)

	publicConfig, _ := json.Marshal(map[string]string{
		"subject":    "user-123",
		"tenant_id":  "20001",
		"device_id":  "dev-123",
		"service_id": "service-1",
		"task_id":    "task-999",
	})

	proxy := &remoteMCPProxy{
		taskID: "task-999",
		path:   "/enterprise",
		connection: remotemcp.Connection{
			ContributionID:  remotemcp.EnterpriseContributionPrefix + "service-1",
			ContributionKey: "enterprise-service-1",
			Endpoint:        platformServer.URL,
			PublicConfig:    publicConfig,
			ApprovedTools: []remotemcp.Tool{
				{
					Name: "kb_search",
				},
			},
		},
		platformClient: client,
		pollInterval:   10 * time.Millisecond,
		semaphore:      make(chan struct{}, remoteMCPMaxConcurrency),
	}

	req := httptest.NewRequest(http.MethodPost, "/enterprise", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kb_search","arguments":{"q":"docs"}}}`))
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tools/call status = %d: %s", w.Code, w.Body.String())
	}

	var callResp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &callResp); err != nil {
		t.Fatalf("unmarshal tools/call response: %v", err)
	}
	if callResp.Result.IsError {
		t.Fatalf("expected isError=false, got true: %s", w.Body.String())
	}
	if len(callResp.Result.Content) != 1 || callResp.Result.Content[0].Text != "found 3 articles" {
		t.Fatalf("unexpected content: %+v", callResp.Result.Content)
	}
	if pollCount < 2 {
		t.Fatalf("expected at least 2 polls, got %d", pollCount)
	}
}

func TestEnterpriseRemoteMCPProxy_ToolsCall_Unapproved(t *testing.T) {
	proxy := &remoteMCPProxy{
		taskID: "task-1",
		path:   "/enterprise",
		connection: remotemcp.Connection{
			ContributionID:  remotemcp.EnterpriseContributionPrefix + "service-1",
			ContributionKey: "enterprise-service-1",
			ApprovedTools: []remotemcp.Tool{
				{Name: "allowed_tool"},
			},
		},
		semaphore: make(chan struct{}, remoteMCPMaxConcurrency),
	}

	req := httptest.NewRequest(http.MethodPost, "/enterprise", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"denied_tool","arguments":{}}}`))
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "not approved") {
		t.Fatalf("expected not approved error, got: %s", w.Body.String())
	}
}

func TestEnterpriseRemoteMCPProxy_ToolsCall_ExecutionFailure(t *testing.T) {
	platformServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/integration/v1/mcp/invoke-task" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"invocation_id":"inv-fail","status":"executing"}}`))
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/integration/v1/mcp/invocations/inv-fail" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"invocation_id":"inv-fail","status":"failed","error":"knowledge index offline"}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer platformServer.Close()

	client := platformclient.NewClient(platformServer.URL, "test-token", 5*time.Second)

	proxy := &remoteMCPProxy{
		taskID: "task-fail",
		path:   "/enterprise",
		connection: remotemcp.Connection{
			ContributionID:  remotemcp.EnterpriseContributionPrefix + "service-1",
			ContributionKey: "enterprise-service-1",
			ApprovedTools:   []remotemcp.Tool{{Name: "kb_search"}},
		},
		platformClient: client,
		pollInterval:   10 * time.Millisecond,
		semaphore:      make(chan struct{}, remoteMCPMaxConcurrency),
	}

	req := httptest.NewRequest(http.MethodPost, "/enterprise", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kb_search","arguments":{}}}`))
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	var callResp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &callResp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !callResp.Result.IsError {
		t.Fatalf("expected isError=true, got false")
	}
	if !strings.Contains(callResp.Result.Content[0].Text, "knowledge index offline") {
		t.Errorf("unexpected error text: %s", callResp.Result.Content[0].Text)
	}
}

func TestStartTaskRemoteMCPBrokers_EnterpriseBypassesDiscovery(t *testing.T) {
	connections := []remotemcp.Connection{
		{
			InstallationID:  "enterprise",
			ContributionID:  remotemcp.EnterpriseContributionPrefix + "service-1",
			ContributionKey: "enterprise-service-1",
			Endpoint:        "http://10.101.102.116:3002",
			Transport:       "remote_bridge",
			ApprovedTools:   []remotemcp.Tool{{Name: "kb_search"}},
		},
	}
	cfg, diags, set, err := startTaskRemoteMCPBrokers(context.Background(), context.Background(), "task-1", "claude", connections, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer set.Close()
	if len(diags) != 0 {
		t.Errorf("unexpected diagnostics: %v", diags)
	}
	if len(cfg) == 0 {
		t.Fatal("expected non-empty config")
	}
	if !strings.Contains(string(cfg), "enterprise-service-1") {
		t.Errorf("expected config to contain enterprise-service-1, got %s", string(cfg))
	}
}

func TestEnterpriseRemoteMCPProxy_ConcurrentGetPlatformClient_NoDataRace(t *testing.T) {
	proxy := &remoteMCPProxy{
		connection: remotemcp.Connection{
			Endpoint: "http://127.0.0.1:9999",
		},
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cli := proxy.getPlatformClient()
			if cli == nil {
				t.Error("expected non-nil platform client")
			}
		}()
	}
	wg.Wait()
}

func TestEnterpriseRemoteMCPProxy_ToolsCall_TerminalStatuses(t *testing.T) {
	for _, terminalStatus := range []string{"cancelled", "rejected", "failed"} {
		t.Run(terminalStatus, func(t *testing.T) {
			pollCount := 0
			platformServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost && r.URL.Path == "/integration/v1/mcp/invoke-task" {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"success":true,"data":{"invocation_id":"inv-term","status":"executing"}}`))
					return
				}
				if r.Method == http.MethodGet && r.URL.Path == "/integration/v1/mcp/invocations/inv-term" {
					pollCount++
					w.WriteHeader(http.StatusOK)
					_, _ = fmt.Fprintf(w, `{"success":true,"data":{"invocation_id":"inv-term","status":%q}}`, terminalStatus)
					return
				}
				http.NotFound(w, r)
			}))
			defer platformServer.Close()

			client := platformclient.NewClient(platformServer.URL, "test-token", 5*time.Second)
			proxy := &remoteMCPProxy{
				taskID: "task-term",
				path:   "/enterprise",
				connection: remotemcp.Connection{
					ContributionID:  remotemcp.EnterpriseContributionPrefix + "service-1",
					ContributionKey: "enterprise-service-1",
					ApprovedTools:   []remotemcp.Tool{{Name: "kb_search"}},
				},
				platformClient: client,
				pollInterval:   10 * time.Millisecond,
				pollTimeout:    2 * time.Second,
				semaphore:      make(chan struct{}, remoteMCPMaxConcurrency),
			}

			req := httptest.NewRequest(http.MethodPost, "/enterprise", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kb_search","arguments":{}}}`))
			w := httptest.NewRecorder()
			proxy.ServeHTTP(w, req)

			var callResp struct {
				Result struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
					IsError bool `json:"isError"`
				} `json:"result"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &callResp); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			if !callResp.Result.IsError {
				t.Fatalf("expected isError=true for terminal status %s", terminalStatus)
			}
			expectedMsg := fmt.Sprintf("invocation ended with status %q", terminalStatus)
			if !strings.Contains(callResp.Result.Content[0].Text, expectedMsg) {
				t.Errorf("status=%s: expected error message %q, got: %s", terminalStatus, expectedMsg, callResp.Result.Content[0].Text)
			}
			if pollCount != 1 {
				t.Errorf("status=%s: expected exactly 1 poll, got %d (should stop polling immediately on terminal status)", terminalStatus, pollCount)
			}
		})
	}
}

func TestEnterpriseRemoteMCPProxy_ToolsCall_StatusRespError(t *testing.T) {
	platformServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/integration/v1/mcp/invoke-task" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true,"data":{"invocation_id":"inv-err","status":"executing"}}`))
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/integration/v1/mcp/invocations/inv-err" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":false,"data":{"invocation_id":"inv-err","status":"error"},"error":{"code":"UPSTREAM_UNAVAILABLE","message":"upstream cluster unreachable"}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer platformServer.Close()

	client := platformclient.NewClient(platformServer.URL, "test-token", 5*time.Second)
	proxy := &remoteMCPProxy{
		taskID: "task-err",
		path:   "/enterprise",
		connection: remotemcp.Connection{
			ContributionID:  remotemcp.EnterpriseContributionPrefix + "service-1",
			ContributionKey: "enterprise-service-1",
			ApprovedTools:   []remotemcp.Tool{{Name: "kb_search"}},
		},
		platformClient: client,
		pollInterval:   10 * time.Millisecond,
		semaphore:      make(chan struct{}, remoteMCPMaxConcurrency),
	}

	req := httptest.NewRequest(http.MethodPost, "/enterprise", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kb_search","arguments":{}}}`))
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	var callResp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &callResp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !callResp.Result.IsError {
		t.Fatalf("expected isError=true")
	}
	if !strings.Contains(callResp.Result.Content[0].Text, "[UPSTREAM_UNAVAILABLE] upstream cluster unreachable") {
		t.Errorf("unexpected error text: %s", callResp.Result.Content[0].Text)
	}
}
