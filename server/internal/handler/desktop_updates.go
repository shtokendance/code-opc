package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

type PlatformReleaseInfo struct {
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	URL      string `json:"url,omitempty"`
}

type DesktopLatestManifest struct {
	Version      string                         `json:"version"`
	MinVersion   string                         `json:"min_version,omitempty"`
	ReleaseNotes string                         `json:"release_notes,omitempty"`
	PubDate      string                         `json:"pub_date,omitempty"`
	Platforms    map[string]PlatformReleaseInfo `json:"platforms"`
}

type DesktopUpdateCheckResponse struct {
	Available      bool   `json:"available"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	ReleaseNotes   string `json:"release_notes,omitempty"`
	PubDate        string `json:"pub_date,omitempty"`
	DownloadURL    string `json:"download_url,omitempty"`
	Filename       string `json:"filename,omitempty"`
	SHA256         string `json:"sha256,omitempty"`
	Size           int64  `json:"size,omitempty"`
}

func (h *Handler) getReleasesDir() string {
	if dir := os.Getenv("MULTICA_RELEASES_DIR"); dir != "" {
		return dir
	}
	candidates := []string{
		"/data/releases",
		"/app/releases",
		"/app/data/releases",
		"./releases",
	}
	for _, candidate := range candidates {
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			return candidate
		}
	}
	return "./releases"
}

// normalizePlatformKey generates canonical candidate keys for platform & arch matching.
func normalizePlatformKeys(platform, arch string) []string {
	p := strings.ToLower(strings.TrimSpace(platform))
	a := strings.ToLower(strings.TrimSpace(arch))

	var pAliases []string
	switch p {
	case "windows", "win", "win32":
		pAliases = []string{"windows", "win"}
	case "darwin", "mac", "macos", "osx":
		pAliases = []string{"darwin", "mac", "macos"}
	case "linux":
		pAliases = []string{"linux"}
	default:
		pAliases = []string{p}
	}

	var aAliases []string
	switch a {
	case "amd64", "x64", "x86_64":
		aAliases = []string{"amd64", "x64", "x86_64"}
	case "arm64", "aarch64":
		aAliases = []string{"arm64", "aarch64"}
	default:
		aAliases = []string{a}
	}

	var keys []string
	for _, pa := range pAliases {
		for _, aa := range aAliases {
			keys = append(keys, pa+"-"+aa, pa+"_"+aa)
		}
	}
	return keys
}

// compareSemver compares two semantic versions (e.g. "0.11.3" vs "0.11.4").
// Returns 1 if v1 > v2, -1 if v1 < v2, and 0 if v1 == v2.
func compareSemver(v1, v2 string) int {
	clean1 := strings.TrimLeft(strings.TrimSpace(v1), "vV")
	clean2 := strings.TrimLeft(strings.TrimSpace(v2), "vV")

	parts1 := strings.Split(clean1, ".")
	parts2 := strings.Split(clean2, ".")

	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	for i := 0; i < maxLen; i++ {
		num1 := 0
		if i < len(parts1) {
			num1, _ = strconv.Atoi(parts1[i])
		}
		num2 := 0
		if i < len(parts2) {
			num2, _ = strconv.Atoi(parts2[i])
		}

		if num1 > num2 {
			return 1
		}
		if num1 < num2 {
			return -1
		}
	}
	return 0
}

// CheckDesktopUpdate handles GET /api/desktop/updates/check.
// Query params:
// - platform: windows | darwin | linux
// - arch: amd64 | arm64
// - version: current client version (e.g. 0.11.3)
// - product: optional product name (e.g. CodeOPC or Multica)
func (h *Handler) CheckDesktopUpdate(w http.ResponseWriter, r *http.Request) {
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	arch := strings.TrimSpace(r.URL.Query().Get("arch"))
	currentVersion := strings.TrimSpace(r.URL.Query().Get("version"))
	if currentVersion == "" {
		currentVersion = strings.TrimSpace(r.URL.Query().Get("current_version"))
	}
	product := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("product")))

	releasesDir := h.getReleasesDir()

	manifestPath := filepath.Join(releasesDir, "latest.json")
	if product != "" {
		prodManifest := filepath.Join(releasesDir, "latest-"+product+".json")
		if _, err := os.Stat(prodManifest); err == nil {
			manifestPath = prodManifest
		}
	}

	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		// No manifest found on server, respond with up-to-date
		writeJSON(w, http.StatusOK, DesktopUpdateCheckResponse{
			Available:      false,
			CurrentVersion: currentVersion,
			LatestVersion:  currentVersion,
		})
		return
	}

	var manifest DesktopLatestManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to parse update manifest")
		return
	}

	resp := DesktopUpdateCheckResponse{
		Available:      false,
		CurrentVersion: currentVersion,
		LatestVersion:  manifest.Version,
		ReleaseNotes:   manifest.ReleaseNotes,
		PubDate:        manifest.PubDate,
	}

	// Compare versions
	if currentVersion != "" && compareSemver(manifest.Version, currentVersion) <= 0 {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// Match platform
	var releaseInfo *PlatformReleaseInfo
	candidates := normalizePlatformKeys(platform, arch)
	for _, key := range candidates {
		if info, ok := manifest.Platforms[key]; ok {
			releaseInfo = &info
			break
		}
	}

	if releaseInfo == nil || releaseInfo.Filename == "" {
		// No package for this architecture, cannot update
		writeJSON(w, http.StatusOK, resp)
		return
	}

	resp.Available = true
	resp.Filename = releaseInfo.Filename
	resp.SHA256 = releaseInfo.SHA256
	resp.Size = releaseInfo.Size

	downloadURL := releaseInfo.URL
	if downloadURL == "" {
		downloadURL = "/api/desktop/updates/download/" + releaseInfo.Filename
	}
	resp.DownloadURL = downloadURL

	writeJSON(w, http.StatusOK, resp)
}

// DownloadDesktopUpdate handles GET /api/desktop/updates/download/{filename}.
func (h *Handler) DownloadDesktopUpdate(w http.ResponseWriter, r *http.Request) {
	rawFilename := chi.URLParam(r, "filename")
	filename := filepath.Base(rawFilename)
	if filename == "." || filename == "/" || filename == "" {
		writeError(w, http.StatusBadRequest, "invalid filename")
		return
	}

	releasesDir := h.getReleasesDir()
	absReleasesDir, err := filepath.Abs(releasesDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid releases directory")
		return
	}

	targetPath := filepath.Join(absReleasesDir, filename)
	absTargetPath, err := filepath.Abs(targetPath)
	if err != nil || !strings.HasPrefix(absTargetPath, absReleasesDir) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	fileInfo, err := os.Stat(absTargetPath)
	if err != nil || fileInfo.IsDir() {
		writeError(w, http.StatusNotFound, "release file not found")
		return
	}

	// Provide download attachment header
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	http.ServeFile(w, r, absTargetPath)
}
