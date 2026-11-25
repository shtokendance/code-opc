package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	knowledgeMCPServerName  = "codeopc-knowledge"
	knowledgeMCPToolName    = "search_knowledge_base"
	knowledgeMCPCallTimeout = 30 * time.Second
)

type projectKnowledgeBaseRef struct {
	TenantID          string `json:"tenant_id"`
	KnowledgeBaseID   string `json:"knowledge_base_id"`
	KnowledgeBaseName string `json:"knowledge_base_name,omitempty"`
}

// knowledgeSearcher performs one knowledge-base search via the code-opc server.
type knowledgeSearcher func(ctx context.Context, taskID, tenantID, knowledgeBaseID, query string, matchCount int) (json.RawMessage, error)

func extractProjectKnowledgeBases(resources []ProjectResourceData) []projectKnowledgeBaseRef {
	if len(resources) == 0 {
		return nil
	}
	var out []projectKnowledgeBaseRef
	seen := make(map[string]struct{})
	for _, r := range resources {
		if r.ResourceType != "knowledge_base" {
			continue
		}
		var ref projectKnowledgeBaseRef
		if len(r.ResourceRef) > 0 {
			_ = json.Unmarshal(r.ResourceRef, &ref)
		}
		ref.TenantID = strings.TrimSpace(ref.TenantID)
		ref.KnowledgeBaseID = strings.TrimSpace(ref.KnowledgeBaseID)
		ref.KnowledgeBaseName = strings.TrimSpace(ref.KnowledgeBaseName)
		if ref.KnowledgeBaseName == "" {
			ref.KnowledgeBaseName = strings.TrimSpace(r.Label)
		}
		if ref.TenantID == "" || ref.KnowledgeBaseID == "" {
			continue
		}
		key := ref.TenantID + ":" + ref.KnowledgeBaseID
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

type knowledgeMCPServer struct {
	taskID string
	kbs    []projectKnowledgeBaseRef
	byKBID map[string]projectKnowledgeBaseRef
	search knowledgeSearcher
	path   string
	logger *slog.Logger
}

// startTaskKnowledgeMCP starts a local HTTP MCP server exposing
// search_knowledge_base when the task's project has bound knowledge bases.
func startTaskKnowledgeMCP(lifetimeCtx context.Context, taskID string, kbs []projectKnowledgeBaseRef, search knowledgeSearcher, logger *slog.Logger) (json.RawMessage, *pluginHookMCPSet, error) {
	if len(kbs) == 0 || search == nil {
		return nil, nil, nil
	}

	byKBID := make(map[string]projectKnowledgeBaseRef, len(kbs))
	for _, kb := range kbs {
		byKBID[kb.KnowledgeBaseID] = kb
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, fmt.Errorf("listen for knowledge MCP server: %w", err)
	}
	pathToken, err := randomBrokerToken()
	if err != nil {
		_ = listener.Close()
		return nil, nil, err
	}

	handler := &knowledgeMCPServer{
		taskID: taskID,
		kbs:    kbs,
		byKBID: byKBID,
		search: search,
		path:   "/" + pathToken,
		logger: logger,
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	set := &pluginHookMCPSet{server: server, listener: listener}

	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) && logger != nil {
			logger.Warn("knowledge MCP server stopped unexpectedly", "task_id", taskID, "error", serveErr)
		}
	}()
	go func() {
		<-lifetimeCtx.Done()
		set.Close()
	}()

	raw, err := json.Marshal(map[string]any{"mcpServers": map[string]any{
		knowledgeMCPServerName: map[string]any{
			"type": "http",
			"url":  "http://" + listener.Addr().String() + handler.path,
		},
	}})
	if err != nil {
		set.Close()
		return nil, nil, err
	}
	return raw, set, nil
}

func (s *knowledgeMCPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != s.path || r.Method != http.MethodPost {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, pluginHookMCPMaxRequestBytes))
	if err != nil {
		writePluginHookMCPError(w, nil, -32700, "could not read the request")
		return
	}
	var request pluginHookMCPRequest
	if err := json.Unmarshal(body, &request); err != nil {
		writePluginHookMCPError(w, nil, -32700, "request is not valid JSON-RPC")
		return
	}

	switch request.Method {
	case "initialize":
		writePluginHookMCPResult(w, request.ID, map[string]any{
			"protocolVersion": pluginHookMCPProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": knowledgeMCPServerName, "version": "1"},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		writePluginHookMCPResult(w, request.ID, map[string]any{"tools": s.toolDescriptors()})
	case "tools/call":
		s.handleCall(w, r, request)
	default:
		writePluginHookMCPError(w, request.ID, -32601, "unsupported method "+request.Method)
	}
}

func (s *knowledgeMCPServer) toolDescriptors() []map[string]any {
	names := make([]string, 0, len(s.kbs))
	for _, kb := range s.kbs {
		label := kb.KnowledgeBaseName
		if label == "" {
			label = kb.KnowledgeBaseID
		}
		names = append(names, fmt.Sprintf("%s (knowledge_base_id: %s)", label, kb.KnowledgeBaseID))
	}
	desc := "Search the knowledge bases bound to this project. Available knowledge bases: " + strings.Join(names, "; ") + "."
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query text.",
			},
			"knowledge_base_id": map[string]any{
				"type":        "string",
				"description": "Optional knowledge_base_id to restrict search to a single bound knowledge base. Omit to search all bound knowledge bases.",
			},
			"match_count": map[string]any{
				"type":        "integer",
				"description": "Maximum number of hits to return per knowledge base (default 5, max 20).",
			},
		},
		"required": []string{"query"},
	}
	return []map[string]any{{
		"name":        knowledgeMCPToolName,
		"description": desc,
		"inputSchema": schema,
	}}
}

type knowledgeSearchHitPayload struct {
	KnowledgeID       string  `json:"knowledge_id"`
	ChunkID           string  `json:"chunk_id"`
	Title             string  `json:"title"`
	KnowledgeBaseID   string  `json:"knowledge_base_id"`
	KnowledgeBaseName string  `json:"knowledge_base_name"`
	Content           string  `json:"content"`
	Score             float64 `json:"score"`
}

func (s *knowledgeMCPServer) handleCall(w http.ResponseWriter, r *http.Request, request pluginHookMCPRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	}
	if err := json.Unmarshal(request.Params, &params); err != nil {
		writePluginHookMCPError(w, request.ID, -32602, "invalid tool call parameters")
		return
	}
	if params.Name != knowledgeMCPToolName {
		writePluginHookMCPError(w, request.ID, -32602, "unknown tool "+params.Name)
		return
	}

	var args struct {
		Query           string `json:"query"`
		KnowledgeBaseID string `json:"knowledge_base_id"`
		MatchCount      int    `json:"match_count"`
	}
	if err := json.Unmarshal(params.Arguments, &args); err != nil {
		writePluginHookMCPResult(w, request.ID, map[string]any{
			"isError": true,
			"content": []map[string]any{{"type": "text", "text": "invalid arguments: " + err.Error()}},
		})
		return
	}
	args.Query = strings.TrimSpace(args.Query)
	args.KnowledgeBaseID = strings.TrimSpace(args.KnowledgeBaseID)
	if args.Query == "" {
		writePluginHookMCPResult(w, request.ID, map[string]any{
			"isError": true,
			"content": []map[string]any{{"type": "text", "text": "query is required"}},
		})
		return
	}
	if args.MatchCount <= 0 {
		args.MatchCount = 5
	} else if args.MatchCount > 20 {
		args.MatchCount = 20
	}

	targets := s.kbs
	if args.KnowledgeBaseID != "" {
		kb, ok := s.byKBID[args.KnowledgeBaseID]
		if !ok {
			writePluginHookMCPResult(w, request.ID, map[string]any{
				"isError": true,
				"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("knowledge_base_id %q is not bound to this project", args.KnowledgeBaseID)}},
			})
			return
		}
		targets = []projectKnowledgeBaseRef{kb}
	}

	ctx, cancel := context.WithTimeout(r.Context(), knowledgeMCPCallTimeout)
	defer cancel()

	var allHits []knowledgeSearchHitPayload
	var searchErrs []string
	for _, kb := range targets {
		raw, err := s.search(ctx, s.taskID, kb.TenantID, kb.KnowledgeBaseID, args.Query, args.MatchCount)
		if err != nil {
			if s.logger != nil {
				s.logger.Info("knowledge MCP search failed", "task_id", s.taskID, "knowledge_base_id", kb.KnowledgeBaseID, "error", err)
			}
			searchErrs = append(searchErrs, fmt.Sprintf("%s (%s): %v", kb.KnowledgeBaseName, kb.KnowledgeBaseID, err))
			continue
		}
		var parsed struct {
			Results []knowledgeSearchHitPayload `json:"results"`
		}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			searchErrs = append(searchErrs, fmt.Sprintf("%s (%s): invalid response", kb.KnowledgeBaseName, kb.KnowledgeBaseID))
			continue
		}
		for i := range parsed.Results {
			if parsed.Results[i].KnowledgeBaseName == "" {
				parsed.Results[i].KnowledgeBaseName = kb.KnowledgeBaseName
			}
			if parsed.Results[i].KnowledgeBaseID == "" {
				parsed.Results[i].KnowledgeBaseID = kb.KnowledgeBaseID
			}
		}
		allHits = append(allHits, parsed.Results...)
	}

	if len(allHits) == 0 && len(searchErrs) > 0 {
		writePluginHookMCPResult(w, request.ID, map[string]any{
			"isError": true,
			"content": []map[string]any{{"type": "text", "text": "knowledge search failed: " + strings.Join(searchErrs, "; ")}},
		})
		return
	}

	sort.SliceStable(allHits, func(i, j int) bool {
		return allHits[i].Score > allHits[j].Score
	})
	if len(allHits) == 0 {
		writePluginHookMCPResult(w, request.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": "No matching knowledge base entries found."}},
		})
		return
	}

	var b strings.Builder
	for i, hit := range allHits {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		kbLabel := hit.KnowledgeBaseName
		if kbLabel == "" {
			kbLabel = hit.KnowledgeBaseID
		}
		fmt.Fprintf(&b, "### [%d] %s (KB: %s, score: %.4f)\n%s\n", i+1, hit.Title, kbLabel, hit.Score, strings.TrimSpace(hit.Content))
	}
	writePluginHookMCPResult(w, request.ID, map[string]any{
		"content": []map[string]any{{"type": "text", "text": b.String()}},
	})
}
