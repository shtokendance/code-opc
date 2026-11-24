package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/pkg/platformclient"
)

type importKnowledgeSpaceSkillRequest struct {
	CatalogID      string `json:"catalog_id"`
	TenantID       string `json:"tenant_id"`
	ExpectedSHA256 string `json:"expected_sha256"`
	TenantName     string `json:"tenant_name,omitempty"`
	DetailURL      string `json:"detail_url,omitempty"`
	Name           string `json:"name,omitempty"`
	OnConflict     string `json:"on_conflict,omitempty"`
}

func extractUserBearerToken(r *http.Request) string {
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		trimmed := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		if trimmed != "" && trimmed != authHeader {
			return trimmed
		}
	}
	if cookie, err := r.Cookie(auth.AuthCookieName); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return strings.TrimSpace(cookie.Value)
	}
	return ""
}

func writePlatformAPIError(w http.ResponseWriter, err error, fallbackMsg string) {
	var apiErr *platformclient.APIError
	if errors.As(err, &apiErr) {
		status := apiErr.Status
		// Never forward upstream 401 to the frontend, as ApiClient treats any 401
		// as an expired CodeOPC session and forces a logout/redirect to /login.
		if status == http.StatusUnauthorized {
			msg := apiErr.Message
			if msg == "" {
				msg = fallbackMsg
			}
			writeError(w, http.StatusBadGateway, "platform authentication failed: "+msg)
			return
		}
		if status == http.StatusForbidden || status == http.StatusBadRequest || status == http.StatusNotFound || status == http.StatusConflict || status == http.StatusUnprocessableEntity {
			msg := apiErr.Message
			if msg == "" {
				msg = fallbackMsg
			}
			writeError(w, status, msg)
			return
		}
		writeError(w, http.StatusBadGateway, apiErr.Error())
		return
	}
	writeError(w, http.StatusBadGateway, fallbackMsg+": "+err.Error())
}

// ListKnowledgeSpaceSkills handles GET /api/skills/knowledge-space.
// Uses the user's platform access token to list skills from all accessible spaces
// (including shared spaces) via platform-opc's /integration/v1/resources/skills.
func (h *Handler) ListKnowledgeSpaceSkills(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	userToken := extractUserBearerToken(r)
	if userToken == "" {
		writeError(w, http.StatusUnauthorized, "missing user access token")
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	offset := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			offset = v
		}
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	res, err := h.getPlatformClient().ListResourceSkills(r.Context(), userToken, tenantID, q, offset, limit)
	if err != nil {
		writePlatformAPIError(w, err, "failed to list knowledge space skills")
		return
	}

	writeJSON(w, http.StatusOK, res)
}

// ImportKnowledgeSpaceSkill handles POST /api/skills/import/knowledge-space.
// Downloads the skill archive from platform-opc using the user's token and imports it
// into the current workspace via the shared finishSkillImport pipeline.
func (h *Handler) ImportKnowledgeSpaceSkill(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	creatorUUID := parseUUID(creatorID)

	userToken := extractUserBearerToken(r)
	if userToken == "" {
		writeError(w, http.StatusUnauthorized, "missing user access token")
		return
	}

	var req importKnowledgeSpaceSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.CatalogID = strings.TrimSpace(req.CatalogID)
	req.TenantID = strings.TrimSpace(req.TenantID)
	req.ExpectedSHA256 = strings.TrimSpace(req.ExpectedSHA256)
	if req.CatalogID == "" || req.TenantID == "" || req.ExpectedSHA256 == "" {
		writeError(w, http.StatusBadRequest, "catalog_id, tenant_id, and expected_sha256 are required")
		return
	}
	if !validImportOnConflict(req.OnConflict) {
		writeError(w, http.StatusBadRequest, "on_conflict must be one of: fail, overwrite, rename, skip")
		return
	}
	strategy := req.OnConflict
	if strategy == "" {
		strategy = importOnConflictFail
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	archiveBytes, err := h.getPlatformClient().DownloadResourceSkillArchive(
		ctx,
		userToken,
		req.CatalogID,
		req.TenantID,
		req.ExpectedSHA256,
		maxImportArchiveUploadSize,
	)
	if err != nil {
		writePlatformAPIError(w, err, "failed to download skill archive from knowledge space")
		return
	}

	imported, err := parseSkillArchive(archiveBytes, req.CatalogID+".zip")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if customName := strings.TrimSpace(req.Name); customName != "" {
		imported.name = customName
	}

	origin := map[string]any{
		"type":       "knowledge_space",
		"catalog_id": req.CatalogID,
		"tenant_id":  req.TenantID,
	}
	if req.TenantName != "" {
		origin["tenant_name"] = req.TenantName
	}
	if req.DetailURL != "" {
		origin["source_url"] = req.DetailURL
	}
	imported.origin = origin

	h.finishSkillImport(w, r, workspaceID, workspaceUUID, creatorUUID, creatorID, strategy, true, imported)
}
