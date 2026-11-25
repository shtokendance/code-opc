package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/platformclient"
)

type ProjectKnowledgeBaseResponse struct {
	ID                string `json:"id"`
	WorkspaceID       string `json:"workspace_id"`
	ProjectID         string `json:"project_id"`
	TenantID          string `json:"tenant_id"`
	KnowledgeBaseID   string `json:"knowledge_base_id"`
	KnowledgeBaseName string `json:"knowledge_base_name"`
	CreatedBy         string `json:"created_by"`
	CreatedAt         string `json:"created_at"`
}

func projectKnowledgeBaseToResponse(row db.ProjectKnowledgeBase) ProjectKnowledgeBaseResponse {
	return ProjectKnowledgeBaseResponse{
		ID:                uuidToString(row.ID),
		WorkspaceID:       uuidToString(row.WorkspaceID),
		ProjectID:         uuidToString(row.ProjectID),
		TenantID:          row.TenantID,
		KnowledgeBaseID:   row.KnowledgeBaseID,
		KnowledgeBaseName: row.KnowledgeBaseName,
		CreatedBy:         uuidToString(row.CreatedBy),
		CreatedAt:         timestampToString(row.CreatedAt),
	}
}

// ListAvailableKnowledgeBases proxies GET /api/knowledge/bases to platform-opc
// using the caller's bearer token so only entitled spaces/KBs are returned.
func (h *Handler) ListAvailableKnowledgeBases(w http.ResponseWriter, r *http.Request) {
	userToken := extractUserBearerToken(r)
	if userToken == "" {
		writeError(w, http.StatusUnauthorized, "missing user token")
		return
	}
	client := h.getPlatformClient()
	if client == nil {
		writeError(w, http.StatusServiceUnavailable, "platform gateway is not configured")
		return
	}

	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	res, err := client.ListKnowledgeBases(r.Context(), userToken, tenantID, q, offset, limit)
	if err != nil {
		writePlatformAPIError(w, err, "failed to list knowledge bases")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ListProjectKnowledgeBases lists all knowledge bases bound to a project.
func (h *Handler) ListProjectKnowledgeBases(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListProjectKnowledgeBases(r.Context(), db.ListProjectKnowledgeBasesParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project knowledge bases")
		return
	}
	resp := make([]ProjectKnowledgeBaseResponse, len(rows))
	for i, row := range rows {
		resp[i] = projectKnowledgeBaseToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"knowledge_bases": resp, "total": len(resp)})
}

type bindProjectKnowledgeBaseRequest struct {
	TenantID          string `json:"tenant_id"`
	KnowledgeBaseID   string `json:"knowledge_base_id"`
	KnowledgeBaseName string `json:"knowledge_base_name"`
}

// BindProjectKnowledgeBase binds an enterprise knowledge base to a project.
func (h *Handler) BindProjectKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req bindProjectKnowledgeBaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.TenantID = strings.TrimSpace(req.TenantID)
	req.KnowledgeBaseID = strings.TrimSpace(req.KnowledgeBaseID)
	req.KnowledgeBaseName = strings.TrimSpace(req.KnowledgeBaseName)
	if req.TenantID == "" || req.KnowledgeBaseID == "" {
		writeError(w, http.StatusBadRequest, "tenant_id and knowledge_base_id are required")
		return
	}

	row, err := h.Queries.CreateProjectKnowledgeBase(r.Context(), db.CreateProjectKnowledgeBaseParams{
		WorkspaceID:       project.WorkspaceID,
		ProjectID:         project.ID,
		TenantID:          req.TenantID,
		KnowledgeBaseID:   req.KnowledgeBaseID,
		KnowledgeBaseName: req.KnowledgeBaseName,
		CreatedBy:         parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to bind knowledge base to project")
		return
	}
	writeJSON(w, http.StatusCreated, projectKnowledgeBaseToResponse(row))
}

// UnbindProjectKnowledgeBase removes a knowledge base binding from a project.
func (h *Handler) UnbindProjectKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	bindingID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "bindingId"), "bindingId")
	if !ok {
		return
	}
	rows, err := h.Queries.DeleteProjectKnowledgeBase(r.Context(), db.DeleteProjectKnowledgeBaseParams{
		ID:          bindingID,
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unbind knowledge base")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "knowledge base binding not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type taskKnowledgeSearchRequest struct {
	TenantID        string `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	Query           string `json:"query"`
	MatchCount      int    `json:"match_count,omitempty"`
}

func (h *Handler) resolveTaskProjectID(ctx context.Context, task db.AgentTaskQueue) pgtype.UUID {
	if task.IssueID.Valid {
		if issue, err := h.Queries.GetIssue(ctx, task.IssueID); err == nil {
			return issue.ProjectID
		}
	}
	if task.ChatSessionID.Valid {
		if cs, err := h.Queries.GetChatSession(ctx, task.ChatSessionID); err == nil {
			return cs.ProjectID
		}
	}
	if task.AutopilotRunID.Valid {
		if run, err := h.Queries.GetAutopilotRun(ctx, task.AutopilotRunID); err == nil {
			if ap, err := h.Queries.GetAutopilot(ctx, run.AutopilotID); err == nil {
				return ap.ProjectID
			}
		}
	}
	return pgtype.UUID{}
}

func (h *Handler) resolveTaskSubject(ctx context.Context, task db.AgentTaskQueue) string {
	if task.InitiatorUserID.Valid {
		return uuidToString(task.InitiatorUserID)
	}
	if task.OriginatorUserID.Valid {
		return uuidToString(task.OriginatorUserID)
	}
	if task.RuntimeID.Valid {
		if rt, err := h.Queries.GetAgentRuntime(ctx, task.RuntimeID); err == nil && rt.OwnerID.Valid {
			return uuidToString(rt.OwnerID)
		}
	}
	return ""
}

// SearchTaskKnowledge — POST /api/daemon/tasks/{id}/knowledge/search
// Proxies a daemon task's knowledge-base search to platform-opc, scoped strictly
// to the knowledge bases bound to the task's project.
func (h *Handler) SearchTaskKnowledge(w http.ResponseWriter, r *http.Request) {
	task, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	var req taskKnowledgeSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.TenantID = strings.TrimSpace(req.TenantID)
	req.KnowledgeBaseID = strings.TrimSpace(req.KnowledgeBaseID)
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	projectID := h.resolveTaskProjectID(r.Context(), task)
	if !projectID.Valid {
		writeError(w, http.StatusForbidden, "task has no project knowledge bases")
		return
	}
	boundKBs, err := h.Queries.ListProjectKnowledgeBases(r.Context(), db.ListProjectKnowledgeBasesParams{
		ProjectID:   projectID,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project knowledge bases")
		return
	}
	if len(boundKBs) == 0 {
		writeError(w, http.StatusForbidden, "project has no bound knowledge bases")
		return
	}

	var matched *db.ProjectKnowledgeBase
	for i := range boundKBs {
		kb := &boundKBs[i]
		if req.KnowledgeBaseID == "" && len(boundKBs) == 1 {
			matched = kb
			break
		}
		if req.KnowledgeBaseID != "" && kb.KnowledgeBaseID == req.KnowledgeBaseID {
			if req.TenantID == "" || kb.TenantID == req.TenantID {
				matched = kb
				break
			}
		}
	}
	if matched == nil {
		writeError(w, http.StatusForbidden, "knowledge base is not bound to this project")
		return
	}

	client := h.getPlatformClient()
	if client == nil {
		writeError(w, http.StatusServiceUnavailable, "platform gateway is not configured")
		return
	}
	subject := h.resolveTaskSubject(r.Context(), task)
	if subject == "" {
		writeError(w, http.StatusForbidden, "task has no user subject for knowledge search")
		return
	}

	res, err := client.SearchTaskKnowledge(r.Context(), platformclient.TaskKnowledgeSearchRequest{
		Subject:         subject,
		TenantID:        matched.TenantID,
		KnowledgeBaseID: matched.KnowledgeBaseID,
		Query:           req.Query,
		MatchCount:      req.MatchCount,
	})
	if err != nil {
		writePlatformAPIError(w, err, "failed to search knowledge base")
		return
	}
	writeJSON(w, http.StatusOK, res)
}
