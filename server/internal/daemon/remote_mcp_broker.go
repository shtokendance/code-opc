package daemon

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/multica-ai/multica/server/pkg/platformclient"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

const (
	remoteMCPMaxRequestBytes = 1 << 20
	remoteMCPMaxCalls        = 256
	remoteMCPMaxConcurrency  = 8
)

type remoteMCPBrokerSet struct {
	servers   []*http.Server
	listeners []net.Listener
	once      sync.Once
}

type remoteMCPCredentialResolver func(context.Context, string) (http.Header, error)

func (set *remoteMCPBrokerSet) Close() {
	if set == nil {
		return
	}
	set.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		for _, server := range set.servers {
			_ = server.Shutdown(ctx)
		}
		for _, listener := range set.listeners {
			_ = listener.Close()
		}
	})
}

func startTaskRemoteMCPBrokers(setupCtx, lifetimeCtx context.Context, taskID, provider string, connections []remotemcp.Connection, resolveCredential remoteMCPCredentialResolver, logger *slog.Logger) (json.RawMessage, []string, *remoteMCPBrokerSet, error) {
	if len(connections) == 0 {
		return nil, nil, nil, nil
	}
	set := &remoteMCPBrokerSet{}
	servers := map[string]any{}
	diagnostics := make([]string, 0)
	for _, connection := range connections {
		if !providerSupportsRemoteMCPBroker(provider) {
			message := fmt.Sprintf("Remote MCP %s is incompatible with provider %s", connection.ContributionKey, provider)
			if connection.FailurePolicy == "optional" {
				diagnostics = append(diagnostics, message)
				continue
			}
			set.Close()
			return nil, diagnostics, nil, errors.New(message)
		}
		if strings.HasPrefix(connection.ContributionID, remotemcp.EnterpriseContributionPrefix) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				set.Close()
				return nil, diagnostics, nil, fmt.Errorf("listen for Remote MCP broker: %w", err)
			}
			pathToken, err := randomBrokerToken()
			if err != nil {
				_ = listener.Close()
				set.Close()
				return nil, diagnostics, nil, err
			}
			endpoint, _ := url.Parse(connection.Endpoint)
			baseURL := ""
			if endpoint != nil {
				baseURL = endpoint.String()
			}
			if baseURL == "" {
				baseURL = connection.Endpoint
			}
			if baseURL == "" {
				baseURL = strings.TrimSpace(os.Getenv("PLATFORM_GATEWAY_URL"))
				if baseURL == "" {
					baseURL = platformclient.DefaultPlatformGatewayURL
				}
			}
			serviceToken := strings.TrimSpace(os.Getenv("PLATFORM_SERVICE_TOKEN"))
			if serviceToken == "" {
				serviceToken = platformclient.DefaultPlatformServiceToken
			}
			platformCli := platformclient.NewClient(baseURL, serviceToken, 30*time.Second)
			proxy := &remoteMCPProxy{
				taskID: taskID, connection: connection, endpoint: endpoint,
				platformClient: platformCli,
				path:           "/" + pathToken,
				semaphore:      make(chan struct{}, remoteMCPMaxConcurrency), logger: logger,
			}
			server := &http.Server{Handler: proxy, ReadHeaderTimeout: 5 * time.Second}
			set.servers = append(set.servers, server)
			set.listeners = append(set.listeners, listener)
			go func() {
				if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) && logger != nil {
					logger.Warn("Remote MCP broker stopped unexpectedly", "task_id", taskID, "contribution", connection.ContributionKey, "error", serveErr)
				}
			}()
			name := remoteMCPServerName(connection)
			servers[name] = map[string]any{
				"type": "http",
				"url":  "http://" + listener.Addr().String() + proxy.path,
			}
			continue
		}
		headers := make(http.Header)
		if connection.CredentialHeader != "" && resolveCredential != nil {
			resolvedHeaders, resolveErr := resolveCredential(setupCtx, connection.ContributionID)
			if resolveErr != nil {
				message := fmt.Sprintf("Remote MCP %s credential is unavailable", connection.ContributionKey)
				if connection.FailurePolicy == "optional" {
					diagnostics = append(diagnostics, message)
					continue
				}
				set.Close()
				return nil, diagnostics, nil, fmt.Errorf("%s: %w", message, resolveErr)
			}
			headers = resolvedHeaders
		} else if connection.CredentialHeader != "" {
			message := fmt.Sprintf("Remote MCP %s credential resolver is unavailable", connection.ContributionKey)
			if connection.FailurePolicy == "optional" {
				diagnostics = append(diagnostics, message)
				continue
			}
			set.Close()
			return nil, diagnostics, nil, errors.New(message)
		}
		discovered, _, err := remotemcp.Discover(setupCtx, connection.Endpoint, connection.EndpointAllowedHosts, connection.ProtocolVersions, headers)
		if err == nil {
			err = validatePinnedRemoteMCPTools(connection.ApprovedTools, discovered)
		}
		if err != nil {
			message := fmt.Sprintf("Remote MCP %s failed startup validation", connection.ContributionKey)
			if connection.FailurePolicy == "optional" {
				diagnostics = append(diagnostics, message)
				continue
			}
			set.Close()
			return nil, diagnostics, nil, fmt.Errorf("%s: %w", message, err)
		}
		endpoint, err := remotemcp.ValidatePublicHTTPSEndpoint(setupCtx, connection.Endpoint, connection.EndpointAllowedHosts, nil)
		if err != nil {
			set.Close()
			return nil, diagnostics, nil, err
		}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			set.Close()
			return nil, diagnostics, nil, fmt.Errorf("listen for Remote MCP broker: %w", err)
		}
		pathToken, err := randomBrokerToken()
		if err != nil {
			_ = listener.Close()
			set.Close()
			return nil, diagnostics, nil, err
		}
		proxy := &remoteMCPProxy{
			taskID: taskID, connection: connection, endpoint: endpoint,
			client: remotemcp.NewSecureHTTPClient(endpoint), credentialHeaders: headers,
			resolveCredential: resolveCredential, path: "/" + pathToken,
			semaphore: make(chan struct{}, remoteMCPMaxConcurrency), logger: logger,
		}
		server := &http.Server{Handler: proxy, ReadHeaderTimeout: 5 * time.Second}
		set.servers = append(set.servers, server)
		set.listeners = append(set.listeners, listener)
		go func() {
			if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) && logger != nil {
				logger.Warn("Remote MCP broker stopped unexpectedly", "task_id", taskID, "contribution", connection.ContributionKey, "error", serveErr)
			}
		}()
		name := remoteMCPServerName(connection)
		servers[name] = map[string]any{
			"type": "http",
			"url":  "http://" + listener.Addr().String() + proxy.path,
		}
	}
	if len(servers) == 0 {
		set.Close()
		return nil, diagnostics, nil, nil
	}
	go func() {
		<-lifetimeCtx.Done()
		set.Close()
	}()
	raw, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		set.Close()
		return nil, diagnostics, nil, err
	}
	return raw, diagnostics, set, nil
}

func providerSupportsRemoteMCPBroker(provider string) bool {
	switch provider {
	case "codex", "claude", "hermes", "qoder", "mcode":
		return true
	default:
		return false
	}
}

func validatePinnedRemoteMCPTools(approved, discovered []remotemcp.Tool) error {
	available := make(map[string]remotemcp.Tool, len(discovered))
	for _, tool := range discovered {
		available[tool.Name] = tool
	}
	for _, pinned := range approved {
		current, ok := available[pinned.Name]
		if !ok {
			return fmt.Errorf("approved tool %q is missing", pinned.Name)
		}
		if current.SchemaDigest != pinned.SchemaDigest {
			return fmt.Errorf("approved tool %q schema drifted", pinned.Name)
		}
	}
	return nil
}

func randomBrokerToken() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func remoteMCPServerName(connection remotemcp.Connection) string {
	name := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, strings.ToLower(connection.ContributionKey))
	// Plugin contribution IDs share a colon-delimited prefix. Hash the complete
	// identity so the provider name is safe and different installations stay distinct.
	digest := sha256.Sum256([]byte(connection.ContributionID))
	suffix := hex.EncodeToString(digest[:8])
	if strings.HasPrefix(connection.ContributionID, remotemcp.EnterpriseContributionPrefix) {
		return "enterprise-" + name + "-" + suffix
	}
	return "plugin-" + name + "-" + suffix
}

type remoteMCPProxy struct {
	taskID            string
	connection        remotemcp.Connection
	endpoint          *url.URL
	client            *http.Client
	platformClient    *platformclient.Client
	platformOnce      sync.Once
	pollInterval      time.Duration
	pollTimeout       time.Duration
	credentialHeaders http.Header
	resolveCredential remoteMCPCredentialResolver
	path              string
	semaphore         chan struct{}
	calls             atomic.Int64
	logger            *slog.Logger
}

type remoteMCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (proxy *remoteMCPProxy) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	started := time.Now()
	toolName := ""
	resultClass := "rejected"
	defer func() {
		if proxy.logger != nil {
			proxy.logger.Info("Remote MCP broker call",
				"task_id", proxy.taskID,
				"installation_id", proxy.connection.InstallationID,
				"contribution", proxy.connection.ContributionKey,
				"tool", toolName,
				"duration_ms", time.Since(started).Milliseconds(),
				"result_class", resultClass,
			)
		}
	}()
	if request.URL.Path != proxy.path || request.Method != http.MethodPost {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if proxy.calls.Add(1) > remoteMCPMaxCalls {
		writeRemoteMCPError(w, nil, -32002, "Remote MCP task call limit exceeded")
		return
	}
	select {
	case proxy.semaphore <- struct{}{}:
		defer func() { <-proxy.semaphore }()
	default:
		writeRemoteMCPError(w, nil, -32003, "Remote MCP concurrency limit exceeded")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(request.Body, remoteMCPMaxRequestBytes+1))
	if err != nil || len(raw) > remoteMCPMaxRequestBytes {
		writeRemoteMCPError(w, nil, -32600, "Remote MCP request is invalid")
		return
	}
	var rpcRequest remoteMCPRequest
	if err := json.Unmarshal(raw, &rpcRequest); err != nil || rpcRequest.JSONRPC != "2.0" {
		writeRemoteMCPError(w, rpcRequest.ID, -32600, "Remote MCP request is invalid")
		return
	}
	if !allowedRemoteMCPMethod(rpcRequest.Method) {
		writeRemoteMCPError(w, rpcRequest.ID, -32601, "Only approved Remote MCP tools are available")
		return
	}
	if strings.HasPrefix(proxy.connection.ContributionID, remotemcp.EnterpriseContributionPrefix) {
		proxy.serveEnterpriseHTTP(w, request, rpcRequest, raw, &resultClass, &toolName)
		return
	}
	if rpcRequest.Method == "tools/call" {
		var params struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(rpcRequest.Params, &params); err != nil || !proxy.toolApproved(params.Name) {
			writeRemoteMCPError(w, rpcRequest.ID, -32602, "Remote MCP tool is not approved")
			return
		}
		toolName = params.Name
	}
	upstream, err := http.NewRequestWithContext(request.Context(), http.MethodPost, proxy.endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		writeRemoteMCPError(w, rpcRequest.ID, -32603, "Remote MCP request failed")
		return
	}
	upstream.Header.Set("Content-Type", "application/json")
	upstream.Header.Set("Accept", "application/json, text/event-stream")
	for _, header := range []string{"Mcp-Session-Id", "Mcp-Protocol-Version", "Last-Event-ID"} {
		if value := request.Header.Get(header); value != "" {
			upstream.Header.Set(header, value)
		}
	}
	credentialHeaders := proxy.credentialHeaders
	if proxy.connection.CredentialHeader != "" && proxy.resolveCredential != nil {
		credentialHeaders, err = proxy.resolveCredential(request.Context(), proxy.connection.ContributionID)
		if err != nil {
			resultClass = "credential_revoked"
			writeRemoteMCPError(w, rpcRequest.ID, -32005, "Remote MCP credential is revoked or unavailable")
			return
		}
	}
	for key, values := range credentialHeaders {
		for _, value := range values {
			upstream.Header.Add(key, value)
		}
	}
	response, err := proxy.client.Do(upstream)
	if err != nil {
		resultClass = "remote_error"
		writeRemoteMCPError(w, rpcRequest.ID, -32000, "Remote MCP service is unavailable")
		return
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, remotemcp.MaxResponseBytes+1))
	if err != nil || len(responseBody) > remotemcp.MaxResponseBytes {
		resultClass = "remote_error"
		writeRemoteMCPError(w, rpcRequest.ID, -32001, "Remote MCP response exceeded the allowed limit")
		return
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		resultClass = "remote_error"
		writeRemoteMCPError(w, rpcRequest.ID, -32000, "Remote MCP service returned an error")
		return
	}
	if rpcRequest.Method == "tools/list" {
		responseBody, err = decodeRemoteMCPSSEData(response.Header.Get("Content-Type"), responseBody)
		if err != nil {
			resultClass = "remote_error"
			writeRemoteMCPError(w, rpcRequest.ID, -32000, "Remote MCP service returned an invalid response")
			return
		}
		responseBody, err = proxy.filterToolsListResponse(responseBody)
		if err != nil {
			resultClass = "schema_drift"
			writeRemoteMCPError(w, rpcRequest.ID, -32004, "Remote MCP tool schema changed and requires review")
			return
		}
		w.Header().Set("Content-Type", "application/json")
	} else if contentType := response.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	for _, header := range []string{"Mcp-Session-Id", "Mcp-Protocol-Version"} {
		if value := response.Header.Get(header); value != "" {
			w.Header().Set(header, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(responseBody)
	resultClass = "success"
}

func (proxy *remoteMCPProxy) getPlatformClient() *platformclient.Client {
	proxy.platformOnce.Do(func() {
		if proxy.platformClient != nil {
			return
		}
		baseURL := ""
		if proxy.endpoint != nil {
			baseURL = proxy.endpoint.String()
		}
		if baseURL == "" {
			baseURL = proxy.connection.Endpoint
		}
		if baseURL == "" {
			baseURL = strings.TrimSpace(os.Getenv("PLATFORM_GATEWAY_URL"))
			if baseURL == "" {
				baseURL = platformclient.DefaultPlatformGatewayURL
			}
		}
		serviceToken := strings.TrimSpace(os.Getenv("PLATFORM_SERVICE_TOKEN"))
		if serviceToken == "" {
			serviceToken = platformclient.DefaultPlatformServiceToken
		}
		proxy.platformClient = platformclient.NewClient(baseURL, serviceToken, 30*time.Second)
	})
	return proxy.platformClient
}

func writeMCPToolCallResult(w http.ResponseWriter, id json.RawMessage, text string, isError bool) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"content": []map[string]any{
				{
					"type": "text",
					"text": text,
				},
			},
			"isError": isError,
		},
	})
}

func (proxy *remoteMCPProxy) serveEnterpriseHTTP(w http.ResponseWriter, request *http.Request, rpcRequest remoteMCPRequest, raw []byte, resultClass *string, toolName *string) {
	switch rpcRequest.Method {
	case "initialize":
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      rpcRequest.ID,
			"result": map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    "enterprise-mcp",
					"version": "1.0.0",
				},
			},
		})
		*resultClass = "success"

	case "notifications/initialized", "notifications/cancelled":
		w.WriteHeader(http.StatusOK)
		*resultClass = "success"

	case "ping":
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      rpcRequest.ID,
			"result":  map[string]any{},
		})
		*resultClass = "success"

	case "tools/list":
		tools := make([]map[string]any, 0, len(proxy.connection.ApprovedTools))
		for _, tool := range proxy.connection.ApprovedTools {
			item := map[string]any{
				"name": tool.Name,
			}
			if tool.Description != "" {
				item["description"] = tool.Description
			}
			if len(tool.InputSchema) > 0 {
				var schema any
				if err := json.Unmarshal(tool.InputSchema, &schema); err == nil {
					item["inputSchema"] = schema
				} else {
					item["inputSchema"] = tool.InputSchema
				}
			} else {
				item["inputSchema"] = map[string]any{
					"type": "object",
				}
			}
			tools = append(tools, item)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      rpcRequest.ID,
			"result": map[string]any{
				"tools": tools,
			},
		})
		*resultClass = "success"

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(rpcRequest.Params, &params); err != nil || !proxy.toolApproved(params.Name) {
			writeRemoteMCPError(w, rpcRequest.ID, -32602, "Remote MCP tool is not approved")
			return
		}
		*toolName = params.Name

		var publicConfig struct {
			Subject   string `json:"subject"`
			TenantID  string `json:"tenant_id"`
			DeviceID  string `json:"device_id"`
			ServiceID string `json:"service_id"`
			TaskID    string `json:"task_id"`
		}
		if len(proxy.connection.PublicConfig) > 0 {
			_ = json.Unmarshal(proxy.connection.PublicConfig, &publicConfig)
		}
		taskID := publicConfig.TaskID
		if taskID == "" {
			taskID = proxy.taskID
		}
		serviceID := publicConfig.ServiceID
		if serviceID == "" {
			serviceID = strings.TrimPrefix(proxy.connection.ContributionID, remotemcp.EnterpriseContributionPrefix)
		}

		client := proxy.getPlatformClient()
		invokeReq := platformclient.TaskInvocationRequest{
			Subject:   publicConfig.Subject,
			TenantID:  publicConfig.TenantID,
			ServiceID: serviceID,
			ToolName:  params.Name,
			Arguments: params.Arguments,
			TaskID:    taskID,
			Device:    publicConfig.DeviceID,
		}

		invokeResp, err := client.InvokeTask(request.Context(), invokeReq)
		if err != nil {
			*resultClass = "remote_error"
			writeMCPToolCallResult(w, rpcRequest.ID, err.Error(), true)
			return
		}
		if invokeResp.Error != nil {
			*resultClass = "remote_error"
			writeMCPToolCallResult(w, rpcRequest.ID, fmt.Sprintf("[%s] %s", invokeResp.Error.Code, invokeResp.Error.Message), true)
			return
		}

		invocationID := invokeResp.Data.InvocationID
		status := invokeResp.Data.Status
		var finalStatus *platformclient.InvocationStatusResponse

		if status == "created" || status == "executing" {
			interval := proxy.pollInterval
			if interval <= 0 {
				interval = 500 * time.Millisecond
			}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			timeoutDur := proxy.pollTimeout
			if timeoutDur <= 0 {
				timeoutDur = 60 * time.Second
			}
			timer := time.NewTimer(timeoutDur)
			defer timer.Stop()

			for {
				select {
				case <-request.Context().Done():
					*resultClass = "remote_error"
					writeMCPToolCallResult(w, rpcRequest.ID, request.Context().Err().Error(), true)
					return
				case <-timer.C:
					*resultClass = "remote_error"
					writeMCPToolCallResult(w, rpcRequest.ID, "task invocation timed out waiting for completion", true)
					return
				case <-ticker.C:
					statusResp, err := client.GetInvocation(request.Context(), invocationID, publicConfig.Subject, publicConfig.TenantID, taskID, publicConfig.DeviceID)
					if err != nil {
						*resultClass = "remote_error"
						writeMCPToolCallResult(w, rpcRequest.ID, fmt.Sprintf("failed to get invocation status: %v", err), true)
						return
					}
					if statusResp.Error != nil || (statusResp.Data.Status != "created" && statusResp.Data.Status != "executing") {
						finalStatus = statusResp
						break
					}
				}
				if finalStatus != nil {
					break
				}
			}
		} else {
			statusResp, err := client.GetInvocation(request.Context(), invocationID, publicConfig.Subject, publicConfig.TenantID, taskID, publicConfig.DeviceID)
			if err != nil {
				*resultClass = "remote_error"
				writeMCPToolCallResult(w, rpcRequest.ID, fmt.Sprintf("failed to get invocation status: %v", err), true)
				return
			}
			finalStatus = statusResp
		}

		if finalStatus == nil {
			*resultClass = "remote_error"
			writeMCPToolCallResult(w, rpcRequest.ID, "invocation finished without status", true)
			return
		}

		if finalStatus.Error != nil {
			*resultClass = "remote_error"
			writeMCPToolCallResult(w, rpcRequest.ID, fmt.Sprintf("[%s] %s", finalStatus.Error.Code, finalStatus.Error.Message), true)
			return
		}

		if finalStatus.Data.Status != "completed" {
			*resultClass = "remote_error"
			errMsg := finalStatus.Data.Error
			if errMsg == "" {
				errMsg = fmt.Sprintf("invocation ended with status %q", finalStatus.Data.Status)
			}
			writeMCPToolCallResult(w, rpcRequest.ID, errMsg, true)
			return
		}

		if finalStatus.Data.Error != "" {
			*resultClass = "remote_error"
			writeMCPToolCallResult(w, rpcRequest.ID, finalStatus.Data.Error, true)
			return
		}

		resultText := ""
		if len(finalStatus.Data.ResultRef) > 0 {
			var strVal string
			if err := json.Unmarshal(finalStatus.Data.ResultRef, &strVal); err == nil {
				resultText = strVal
			} else {
				resultText = string(finalStatus.Data.ResultRef)
			}
		}
		*resultClass = "success"
		writeMCPToolCallResult(w, rpcRequest.ID, resultText, false)

	default:
		writeRemoteMCPError(w, rpcRequest.ID, -32601, "Only approved Remote MCP tools are available")
	}
}

func decodeRemoteMCPSSEData(contentType string, raw []byte) ([]byte, error) {
	if !strings.HasPrefix(strings.ToLower(contentType), "text/event-stream") {
		return raw, nil
	}
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("data:")) {
			data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			if len(data) > 0 {
				return data, nil
			}
		}
	}
	return nil, errors.New("Remote MCP SSE response contained no data")
}

func allowedRemoteMCPMethod(method string) bool {
	switch method {
	case "initialize", "notifications/initialized", "notifications/cancelled", "ping", "tools/list", "tools/call":
		return true
	default:
		return false
	}
}

func (proxy *remoteMCPProxy) toolApproved(name string) bool {
	for _, tool := range proxy.connection.ApprovedTools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func (proxy *remoteMCPProxy) filterToolsListResponse(raw []byte) ([]byte, error) {
	var response map[string]json.RawMessage
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(response["result"], &result); err != nil {
		return nil, err
	}
	var tools []struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	if err := json.Unmarshal(result["tools"], &tools); err != nil {
		return nil, err
	}
	byName := make(map[string]json.RawMessage, len(tools))
	for _, tool := range tools {
		canonical, err := canonicalRemoteMCPJSON(tool.InputSchema)
		if err != nil {
			return nil, err
		}
		byName[tool.Name] = canonical
	}
	pinned := append([]remotemcp.Tool(nil), proxy.connection.ApprovedTools...)
	sort.Slice(pinned, func(i, j int) bool { return pinned[i].Name < pinned[j].Name })
	filtered := make([]map[string]any, 0, len(pinned))
	for _, tool := range pinned {
		current, ok := byName[tool.Name]
		if !ok || remotemcp.DigestBytes(current) != tool.SchemaDigest {
			return nil, errors.New("tool schema drift")
		}
		filtered = append(filtered, map[string]any{
			"name": tool.Name, "description": tool.Description, "inputSchema": json.RawMessage(current),
		})
	}
	result["tools"], _ = json.Marshal(filtered)
	response["result"], _ = json.Marshal(result)
	return json.Marshal(response)
}

func canonicalRemoteMCPJSON(raw json.RawMessage) (json.RawMessage, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(value)
	return json.RawMessage(canonical), err
}

func writeRemoteMCPError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": message},
	})
}

func mergeTaskRemoteMCPConfig(base, overlay json.RawMessage) (json.RawMessage, error) {
	if len(overlay) == 0 {
		return base, nil
	}
	var overlayDocument struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(overlay, &overlayDocument); err != nil {
		return nil, err
	}
	baseDocument := struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}{MCPServers: map[string]json.RawMessage{}}
	trimmed := bytes.TrimSpace(base)
	if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
		if err := json.Unmarshal(trimmed, &baseDocument); err != nil {
			return nil, err
		}
		if baseDocument.MCPServers == nil {
			baseDocument.MCPServers = map[string]json.RawMessage{}
		}
	}
	for name, server := range overlayDocument.MCPServers {
		baseDocument.MCPServers[name] = server
	}
	return json.Marshal(baseDocument)
}
