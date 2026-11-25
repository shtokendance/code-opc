package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type UpdaterPreferences struct {
	AutomaticUpdates bool `json:"automaticUpdates"`
}

type UpdaterDownloadState struct {
	Status  string `json:"status"`           // "idle" | "downloading" | "downloaded" | "error"
	Version string `json:"version,omitempty"`
	Reason  string `json:"reason,omitempty"`  // "unavailable" | "network" | "unknown"
}

type ManualUpdateCheckResult struct {
	OK             bool                 `json:"ok"`
	CurrentVersion string               `json:"currentVersion,omitempty"`
	LatestVersion  string               `json:"latestVersion,omitempty"`
	Available      bool                 `json:"available"`
	DownloadState  UpdaterDownloadState `json:"downloadState"`
	Error          string               `json:"error,omitempty"`
	Reason         string               `json:"reason,omitempty"`
}

type downloadedPackageInfo struct {
	version      string
	zipPath      string
	sha256       string
	releaseNotes string
}

func getUpdaterPreferencesPath() string {
	dir := getAppConfigDir()
	if dir == "" {
		return ""
	}
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "updater-preferences.json")
}

// GetUpdatePreferences returns the user's updater preferences.
func (s *AppService) GetUpdatePreferences() UpdaterPreferences {
	prefPath := getUpdaterPreferencesPath()
	if prefPath != "" {
		if data, err := os.ReadFile(prefPath); err == nil {
			var p UpdaterPreferences
			if json.Unmarshal(data, &p) == nil {
				return p
			}
		}
	}
	return UpdaterPreferences{AutomaticUpdates: true}
}

// SetUpdatePreferences updates and persists the user's updater preferences.
func (s *AppService) SetUpdatePreferences(enabled bool) UpdaterPreferences {
	p := UpdaterPreferences{AutomaticUpdates: enabled}
	prefPath := getUpdaterPreferencesPath()
	if prefPath != "" {
		if data, err := json.MarshalIndent(p, "", "  "); err == nil {
			_ = os.WriteFile(prefPath, data, 0644)
		}
	}
	return p
}

// GetDownloadState returns the current download state.
func (s *AppService) GetDownloadState() UpdaterDownloadState {
	s.updaterMu.RLock()
	defer s.updaterMu.RUnlock()
	if s.downloadState.Status == "" {
		return UpdaterDownloadState{Status: "idle"}
	}
	return s.downloadState
}

func (s *AppService) setDownloadState(st UpdaterDownloadState) {
	s.updaterMu.Lock()
	s.downloadState = st
	s.updaterMu.Unlock()

	if s.app != nil {
		s.app.EmitEvent("updater:download-state", st)
	}
}

// CheckForUpdates queries the API server for desktop updates.
func (s *AppService) CheckForUpdates() ManualUpdateCheckResult {
	s.mu.RLock()
	apiURL := s.targetAPIURL
	s.mu.RUnlock()
	if apiURL == "" {
		apiURL, _ = resolveDefaultRuntimeEndpoints()
	}

	currentVersion := s.GetAppInfo().Version
	platform := runtime.GOOS
	arch := runtime.GOARCH

	q := url.Values{}
	q.Set("platform", platform)
	q.Set("arch", arch)
	q.Set("version", currentVersion)
	q.Set("product", appProductName)

	checkEndpoint := fmt.Sprintf("%s/api/desktop/updates/check?%s", strings.TrimRight(apiURL, "/"), q.Encode())

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(checkEndpoint)
	if err != nil {
		return ManualUpdateCheckResult{
			OK:     false,
			Reason: "network",
			Error:  err.Error(),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ManualUpdateCheckResult{
			OK:     false,
			Reason: "unavailable",
			Error:  fmt.Sprintf("server returned HTTP %d", resp.StatusCode),
		}
	}

	var checkResp struct {
		Available      bool   `json:"available"`
		CurrentVersion string `json:"current_version"`
		LatestVersion  string `json:"latest_version"`
		ReleaseNotes   string `json:"release_notes"`
		PubDate        string `json:"pub_date"`
		DownloadURL    string `json:"download_url"`
		Filename       string `json:"filename"`
		SHA256         string `json:"sha256"`
		Size           int64  `json:"size"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&checkResp); err != nil {
		return ManualUpdateCheckResult{
			OK:     false,
			Reason: "unknown",
			Error:  "invalid server response",
		}
	}

	s.updaterMu.Lock()
	s.lastCheckInfo = &downloadedPackageInfo{
		version:      checkResp.LatestVersion,
		releaseNotes: checkResp.ReleaseNotes,
		sha256:       checkResp.SHA256,
		zipPath:      checkResp.DownloadURL,
	}
	s.updaterMu.Unlock()

	currentState := s.GetDownloadState()

	if !checkResp.Available {
		return ManualUpdateCheckResult{
			OK:             true,
			CurrentVersion: currentVersion,
			LatestVersion:  checkResp.LatestVersion,
			Available:      false,
			DownloadState:  currentState,
		}
	}

	// If already downloaded for this latest version, report downloaded
	if currentState.Status == "downloaded" && currentState.Version == checkResp.LatestVersion {
		return ManualUpdateCheckResult{
			OK:             true,
			CurrentVersion: currentVersion,
			LatestVersion:  checkResp.LatestVersion,
			Available:      true,
			DownloadState:  currentState,
		}
	}

	// Trigger automatic background download if preferences allow and not currently downloading
	prefs := s.GetUpdatePreferences()
	if prefs.AutomaticUpdates && currentState.Status != "downloading" {
		go func() {
			_ = s.DownloadUpdate()
		}()
	}

	return ManualUpdateCheckResult{
		OK:             true,
		CurrentVersion: currentVersion,
		LatestVersion:  checkResp.LatestVersion,
		Available:      true,
		DownloadState:  s.GetDownloadState(),
	}
}

// DownloadUpdate downloads the update package from the API server.
func (s *AppService) DownloadUpdate() error {
	s.updaterMu.RLock()
	info := s.lastCheckInfo
	s.updaterMu.RUnlock()

	if info == nil || info.version == "" || info.zipPath == "" {
		// Need to check first
		checkRes := s.CheckForUpdates()
		if !checkRes.OK || !checkRes.Available {
			return errors.New("no update available to download")
		}
		s.updaterMu.RLock()
		info = s.lastCheckInfo
		s.updaterMu.RUnlock()
	}

	currentState := s.GetDownloadState()
	if currentState.Status == "downloading" {
		return nil
	}
	if currentState.Status == "downloaded" && currentState.Version == info.version {
		return nil
	}

	s.setDownloadState(UpdaterDownloadState{
		Status:  "downloading",
		Version: info.version,
	})

	s.mu.RLock()
	apiURL := s.targetAPIURL
	s.mu.RUnlock()
	if apiURL == "" {
		apiURL, _ = resolveDefaultRuntimeEndpoints()
	}

	downloadURL := info.zipPath
	if !strings.HasPrefix(downloadURL, "http://") && !strings.HasPrefix(downloadURL, "https://") {
		downloadURL = strings.TrimRight(apiURL, "/") + "/" + strings.TrimLeft(downloadURL, "/")
	}

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Get(downloadURL)
	if err != nil {
		s.setDownloadState(UpdaterDownloadState{
			Status:  "error",
			Version: info.version,
			Reason:  "network",
		})
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.setDownloadState(UpdaterDownloadState{
			Status:  "error",
			Version: info.version,
			Reason:  "unavailable",
		})
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Prepare temp file
	tempFile, err := os.CreateTemp("", fmt.Sprintf("multica-update-%s-*.zip", info.version))
	if err != nil {
		s.setDownloadState(UpdaterDownloadState{Status: "error", Reason: "unknown"})
		return err
	}
	tempZipPath := tempFile.Name()
	defer tempFile.Close()

	hasher := sha256.New()
	contentLength := resp.ContentLength

	// Stream with progress
	buf := make([]byte, 64*1024)
	var downloaded int64
	lastReport := time.Now()

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := tempFile.Write(buf[:n]); wErr != nil {
				s.setDownloadState(UpdaterDownloadState{Status: "error", Reason: "unknown"})
				return wErr
			}
			hasher.Write(buf[:n])
			downloaded += int64(n)

			if contentLength > 0 && time.Since(lastReport) > 200*time.Millisecond {
				lastReport = time.Now()
				percent := int(float64(downloaded) / float64(contentLength) * 100)
				if s.app != nil {
					s.app.EmitEvent("updater:download-progress", map[string]any{
						"percent": percent,
					})
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			s.setDownloadState(UpdaterDownloadState{Status: "error", Reason: "network"})
			return readErr
		}
	}

	// Verify SHA256 if expected
	actualSHA := hex.EncodeToString(hasher.Sum(nil))
	if info.sha256 != "" && !strings.EqualFold(actualSHA, info.sha256) {
		_ = os.Remove(tempZipPath)
		s.setDownloadState(UpdaterDownloadState{
			Status:  "error",
			Version: info.version,
			Reason:  "network",
		})
		return fmt.Errorf("sha256 mismatch: expected %s, got %s", info.sha256, actualSHA)
	}

	s.updaterMu.Lock()
	s.downloadPkg = &downloadedPackageInfo{
		version:      info.version,
		zipPath:      tempZipPath,
		sha256:       actualSHA,
		releaseNotes: info.releaseNotes,
	}
	s.updaterMu.Unlock()

	s.setDownloadState(UpdaterDownloadState{
		Status:  "downloaded",
		Version: info.version,
	})

	if s.app != nil {
		s.app.EmitEvent("updater:update-downloaded", map[string]any{
			"version":      info.version,
			"releaseNotes": info.releaseNotes,
		})
	}

	return nil
}

// InstallUpdate extracts the downloaded update and replaces the running application.
func (s *AppService) InstallUpdate() error {
	s.updaterMu.RLock()
	pkg := s.downloadPkg
	s.updaterMu.RUnlock()

	if pkg == nil || pkg.zipPath == "" {
		return errors.New("no update downloaded ready to install")
	}

	extractDir, err := os.MkdirTemp("", "multica-extracted-*")
	if err != nil {
		return fmt.Errorf("create temp extract dir: %w", err)
	}
	defer os.RemoveAll(extractDir)

	if err := unzipArchive(pkg.zipPath, extractDir); err != nil {
		return fmt.Errorf("unzip update package: %w", err)
	}

	// Perform platform-specific in-place replacement and restart
	if err := installAndRestart(extractDir); err != nil {
		return fmt.Errorf("install and restart: %w", err)
	}

	// Graceful shutdown after launching new binary
	if s.app != nil {
		go func() {
			time.Sleep(300 * time.Millisecond)
			s.app.Quit()
		}()
	}

	return nil
}

func unzipArchive(srcZip, destDir string) error {
	r, err := zip.OpenReader(srcZip)
	if err != nil {
		return err
	}
	defer r.Close()

	cleanDest := filepath.Clean(destDir)

	for _, f := range r.File {
		targetPath := filepath.Join(cleanDest, f.Name)
		if !strings.HasPrefix(filepath.Clean(targetPath), cleanDest) {
			return fmt.Errorf("illegal file path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
		if err != nil {
			return err
		}
	}

	return nil
}
