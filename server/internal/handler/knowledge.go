package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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
