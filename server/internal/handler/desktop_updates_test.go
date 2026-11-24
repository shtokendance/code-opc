package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDesktopUpdates(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("MULTICA_RELEASES_DIR", tempDir)

	manifest := DesktopLatestManifest{
		Version:      "0.11.4",
		ReleaseNotes: "New features and bug fixes",
		PubDate:      "2026-11-24T12:00:00Z",
		Platforms: map[string]PlatformReleaseInfo{
			"windows-amd64": {
				Filename: "Multica-0.11.4-win-x64.zip",
				SHA256:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
				Size:     1024,
			},
			"darwin-arm64": {
				Filename: "Multica-0.11.4-arm64.zip",
				SHA256:   "abcd1234ef",
				Size:     2048,
			},
		},
	}
	manifestBytes, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "latest.json"), manifestBytes, 0644))

	// Create dummy zip file
	dummyWinZip := filepath.Join(tempDir, "Multica-0.11.4-win-x64.zip")
	require.NoError(t, os.WriteFile(dummyWinZip, []byte("fake-win-zip-content"), 0644))

	h := &Handler{}
	r := chi.NewRouter()
	r.Get("/api/desktop/updates/check", h.CheckDesktopUpdate)
	r.Get("/api/desktop/updates/download/{filename}", h.DownloadDesktopUpdate)

	t.Run("Update available for older client", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/desktop/updates/check?platform=windows&arch=x64&version=0.11.3", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp DesktopUpdateCheckResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

		assert.True(t, resp.Available)
		assert.Equal(t, "0.11.3", resp.CurrentVersion)
		assert.Equal(t, "0.11.4", resp.LatestVersion)
		assert.Equal(t, "Multica-0.11.4-win-x64.zip", resp.Filename)
		assert.Equal(t, "/api/desktop/updates/download/Multica-0.11.4-win-x64.zip", resp.DownloadURL)
		assert.Equal(t, int64(1024), resp.Size)
		assert.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", resp.SHA256)
	})

	t.Run("Already up to date", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/desktop/updates/check?platform=windows&arch=amd64&version=0.11.4", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp DesktopUpdateCheckResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

		assert.False(t, resp.Available)
		assert.Equal(t, "0.11.4", resp.LatestVersion)
	})

	t.Run("Newer client than server", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/desktop/updates/check?platform=windows&arch=amd64&version=0.12.0", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp DesktopUpdateCheckResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

		assert.False(t, resp.Available)
	})

	t.Run("Platform matching with aliases (mac/arm64)", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/desktop/updates/check?platform=mac&arch=arm64&version=0.11.0", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp DesktopUpdateCheckResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

		assert.True(t, resp.Available)
		assert.Equal(t, "Multica-0.11.4-arm64.zip", resp.Filename)
	})

	t.Run("Download existing file", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/desktop/updates/download/Multica-0.11.4-win-x64.zip", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "fake-win-zip-content", rec.Body.String())
	})

	t.Run("Download non-existent file", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/desktop/updates/download/non-existent.zip", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("Download path traversal attempt", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/desktop/updates/download/..%2F..%2Fetc%2Fpasswd", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		// filepath.Base strips the path components to passwd which doesn't exist in tempDir
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}
