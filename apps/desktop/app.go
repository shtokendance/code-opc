package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/pkg/embeddeddaemon"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// AppInfo represents the application version and running platform OS.
type AppInfo struct {
	Version string `json:"version"`
	OS      string `json:"os"`
}

// RuntimeConfig represents the API and WebSocket endpoints configuration.
type RuntimeConfig struct {
	SchemaVersion int    `json:"schemaVersion"`
	ApiURL        string `json:"apiUrl"`
	WsURL         string `json:"wsUrl"`
	AppURL        string `json:"appUrl"`
	PlatformURL   string `json:"platformUrl,omitempty"`
}

type ConfigError struct {
	Message string `json:"message"`
}

// RuntimeConfigResult returns ok and config, or an error.
type RuntimeConfigResult struct {
	OK     bool           `json:"ok"`
	Config *RuntimeConfig `json:"config,omitempty"`
	Error  *ConfigError   `json:"error,omitempty"`
}

// DaemonStatus represents the local agent runtime daemon status.
type DaemonStatus struct {
	State          string   `json:"state"`
	PID            int      `json:"pid,omitempty"`
	Uptime         string   `json:"uptime,omitempty"`
	DaemonID       string   `json:"daemonId,omitempty"`
	DeviceName     string   `json:"deviceName,omitempty"`
	Agents         []string `json:"agents,omitempty"`
	WorkspaceCount int      `json:"workspaceCount,omitempty"`
	Profile        string   `json:"profile,omitempty"`
	ServerURL      string   `json:"serverUrl,omitempty"`
}

type IssueWindowRequest struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

type ProxyFetchRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	BodyB64 string            `json:"bodyB64,omitempty"`
}

type ProxyFetchResponse struct {
	Status     int               `json:"status"`
	StatusText string            `json:"statusText"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body,omitempty"`
	BodyB64    string            `json:"bodyB64,omitempty"`
}

//go:embed config/endpoints.prod.json
var embeddedDefaultEndpoints []byte

var (
	defaultAPIURL      string
	defaultAppURL      string
	defaultPlatformURL string
)

type endpointTemplate struct {
	ApiURL      string `json:"apiUrl"`
	AppURL      string `json:"appUrl"`
	PlatformURL string `json:"platformUrl"`
}

func resolveTemplateEndpoints() (string, string, string) {
	var tpl endpointTemplate
	_ = json.Unmarshal(embeddedDefaultEndpoints, &tpl)
	api := strings.TrimSpace(defaultAPIURL)
	if api == "" {
		api = strings.TrimSpace(tpl.ApiURL)
	}
	app := strings.TrimSpace(defaultAppURL)
	if app == "" {
		app = strings.TrimSpace(tpl.AppURL)
	}
	platform := strings.TrimSpace(defaultPlatformURL)
	if platform == "" {
		platform = strings.TrimSpace(tpl.PlatformURL)
	}
	return api, app, platform
}

func resolveDefaultRuntimeEndpoints() (string, string, string) {
	tplAPI, tplApp, tplPlatform := resolveTemplateEndpoints()

	// 1. Env vars override
	apiURL := strings.TrimSpace(os.Getenv("MULTICA_API_URL"))
	appURL := strings.TrimSpace(os.Getenv("MULTICA_APP_URL"))
	platformURL := strings.TrimSpace(os.Getenv("MULTICA_PLATFORM_URL"))
	if apiURL != "" {
		if appURL == "" {
			appURL = tplApp
		}
		if platformURL == "" {
			platformURL = tplPlatform
		}
		return apiURL, appURL, platformURL
	}

	// 2. ~/.codeopc/desktop.json (auto-migrated from ~/.multica if needed)
	if cfgDir := getAppConfigDir(); cfgDir != "" {
		desktopPath := filepath.Join(cfgDir, "desktop.json")
		if data, err := os.ReadFile(desktopPath); err == nil {
			var rec endpointTemplate
			if json.Unmarshal(data, &rec) == nil && rec.ApiURL != "" {
				if rec.PlatformURL == "" {
					rec.PlatformURL = tplPlatform
				}
				return rec.ApiURL, rec.AppURL, rec.PlatformURL
			}
		}
	}
	// Fallback: check legacy ~/.multica/desktop.json if ~/.codeopc/desktop.json wasn't present
	home, _ := os.UserHomeDir()
	if home != "" {
		legacyPath := filepath.Join(home, legacyAppConfigDirName, "desktop.json")
		if data, err := os.ReadFile(legacyPath); err == nil {
			var rec endpointTemplate
			if json.Unmarshal(data, &rec) == nil && rec.ApiURL != "" {
				if rec.PlatformURL == "" {
					rec.PlatformURL = tplPlatform
				}
				return rec.ApiURL, rec.AppURL, rec.PlatformURL
			}
		}
	}

	// 3. Default to build-time / embedded template endpoints
	return tplAPI, tplApp, tplPlatform
}

type AppService struct {
	app               *application.App
	daemonService     *DaemonService
	mu                sync.RWMutex
	targetAPIURL      string
	targetAppURL      string
	targetPlatformURL string
	daemonToken       string
	daemonUserID      string
	openWindows       map[string]*application.WebviewWindow
	proxyClient       *http.Client
	wsProxyServer     *http.Server
	wsProxyPort       int
	wsProxyTarget     string
	mainWindow        *application.WebviewWindow
	loginOriginURL    string
	oauthServer       *http.Server
	oauthPort         int
	oauthPendingToken string
	oauthCancel       context.CancelFunc
	oauthMu           sync.Mutex

	updaterMu     sync.RWMutex
	downloadState UpdaterDownloadState
	downloadPkg   *downloadedPackageInfo
	lastCheckInfo *downloadedPackageInfo
}

func NewAppService() *AppService {
	defaultAPI, defaultApp, defaultPlatform := resolveDefaultRuntimeEndpoints()
	return &AppService{
		daemonService:     NewDaemonService(),
		targetAPIURL:      defaultAPI,
		targetAppURL:      defaultApp,
		targetPlatformURL: defaultPlatform,
		openWindows:   make(map[string]*application.WebviewWindow),
		proxyClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

func (s *AppService) Shutdown() {
	s.StopLoginOAuthServer()
	if s.daemonService != nil && s.daemonService.manager != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		s.daemonService.manager.StopAll(ctx)
		cancel()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.wsProxyServer != nil {
		_ = s.wsProxyServer.Close()
		s.wsProxyServer = nil
	}
}

// StartLoginOAuthServer starts an ephemeral loopback HTTP server on 127.0.0.1
// waiting for OAuth callback with token, and closes the port once token is received and verified.
func (s *AppService) StartLoginOAuthServer() (int, error) {
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()

	// If already running, return current port
	if s.oauthServer != nil && s.oauthPort > 0 {
		return s.oauthPort, nil
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("failed to listen on loopback port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	fmt.Printf("[OAuth Loopback] Server started on 127.0.0.1:%d\n", port)

	ctx, cancel := context.WithCancel(context.Background())
	s.oauthCancel = cancel
	s.oauthPort = port
	s.oauthPendingToken = ""

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(r.URL.Query().Get("token"))
		fmt.Printf("[OAuth Loopback] /callback called with token len: %d\n", len(token))
		if token != "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <title>登录成功 - CodeOPC</title>
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <style>
    * { box-sizing: border-box; }
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; background: #f8fafc; color: #0f172a; }
    .card { background: #ffffff; padding: 48px 40px; border-radius: 16px; box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.05), 0 8px 10px -6px rgba(0, 0, 0, 0.01); text-align: center; max-width: 420px; width: 90%; border: 1px solid #e2e8f0; }
    .icon { width: 56px; height: 56px; background: #ecfdf5; border-radius: 50%; display: flex; align-items: center; justify-content: center; margin: 0 auto 20px; color: #10b981; font-size: 28px; line-height: 1; }
    h2 { margin: 0 0 12px; font-size: 20px; font-weight: 600; letter-spacing: -0.02em; }
    p { margin: 0; color: #64748b; font-size: 14px; line-height: 1.6; }
  </style>
</head>
<body>
  <div class="card">
    <div class="icon">✓</div>
    <h2>登录成功</h2>
    <p>正在进入 CodeOPC 主界面...</p>
  </div>
</body>
</html>`))

			// Store pending token for fallback retrieval
			s.oauthMu.Lock()
			s.oauthPendingToken = token
			s.oauthMu.Unlock()

			// Emit token to frontend via Wails event
			if s.app != nil {
				s.app.EmitEvent("auth:token", token)
			}
			if s.mainWindow != nil {
				s.mainWindow.EmitEvent("auth:token", token)
			}

			// Navigate the main window back to the Wails app root after the HTTP
			// response commits in the WebView. SetURL("") resolves the exact
			// assetserver start URL and dispatches via InvokeSync internally,
			// so calling it from a goroutine avoids nested InvokeAsync->InvokeSync
			// and avoids WKWebView cancelling an in-flight navigation.
			go func() {
				time.Sleep(150 * time.Millisecond)
				s.mu.Lock()
				s.loginOriginURL = ""
				s.mu.Unlock()
				if s.mainWindow != nil {
					s.mainWindow.SetURL("")
					s.mainWindow.Focus()
				}
			}()

			// Close the port once verification passes, only if it is still this server instance!
			go func(srv *http.Server) {
				time.Sleep(3 * time.Second)
				s.oauthMu.Lock()
				isSame := s.oauthServer == srv
				s.oauthMu.Unlock()
				if isSame {
					s.StopLoginOAuthServer()
				}
			}(s.oauthServer)
			return
		}

		http.Error(w, "Bad Request: missing token", http.StatusBadRequest)
	})

	server := &http.Server{
		Handler: mux,
	}
	s.oauthServer = server

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[OAuth Loopback] server error: %v\n", err)
		}
	}()

	// Auto cleanup after 5 minutes timeout if abandoned
	go func() {
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Minute):
			s.StopLoginOAuthServer()
		}
	}()

	return port, nil
}

// StopLoginOAuthServer stops the ephemeral loopback server and frees the port.
func (s *AppService) StopLoginOAuthServer() {
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()

	if s.oauthCancel != nil {
		s.oauthCancel()
		s.oauthCancel = nil
	}
	if s.oauthServer != nil {
		_ = s.oauthServer.Close()
		s.oauthServer = nil
		s.oauthPort = 0
	}
}

// GetPendingOAuthToken returns and clears any received OAuth token
func (s *AppService) GetPendingOAuthToken() string {
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()
	tok := s.oauthPendingToken
	s.oauthPendingToken = ""
	return tok
}

func (s *AppService) SetMainWindow(win *application.WebviewWindow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mainWindow = win
}

// ensureWSProxy starts or reuses a local loopback WebSocket proxy that connects
// upstream with identical Origin headers, eliminating Origin whitelist rejections on remote servers.
func (s *AppService) ensureWSProxy(targetWSURL string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.wsProxyServer != nil && s.wsProxyTarget == targetWSURL && s.wsProxyPort > 0 {
		return fmt.Sprintf("ws://127.0.0.1:%d/ws", s.wsProxyPort), nil
	}

	if s.wsProxyServer != nil {
		_ = s.wsProxyServer.Close()
		s.wsProxyServer = nil
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen for local ws proxy: %w", err)
	}

	port := l.Addr().(*net.TCPAddr).Port
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		clientConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer clientConn.Close()

		targetParsed, err := url.Parse(targetWSURL)
		if err != nil {
			return
		}

		upstreamURL := *targetParsed
		upstreamURL.RawQuery = r.URL.RawQuery

		headers := http.Header{}
		originScheme := "http"
		if strings.HasPrefix(targetWSURL, "wss://") {
			originScheme = "https"
		}
		headers.Set("Origin", fmt.Sprintf("%s://%s", originScheme, targetParsed.Host))

		upstreamConn, _, err := websocket.DefaultDialer.Dial(upstreamURL.String(), headers)
		if err != nil {
			return
		}
		defer upstreamConn.Close()

		errChan := make(chan error, 2)
		go func() {
			for {
				msgType, msg, err := clientConn.ReadMessage()
				if err != nil {
					errChan <- err
					return
				}
				if err := upstreamConn.WriteMessage(msgType, msg); err != nil {
					errChan <- err
					return
				}
			}
		}()
		go func() {
			for {
				msgType, msg, err := upstreamConn.ReadMessage()
				if err != nil {
					errChan <- err
					return
				}
				if err := clientConn.WriteMessage(msgType, msg); err != nil {
					errChan <- err
					return
				}
			}
		}()

		<-errChan
	})

	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(l)
	}()

	s.wsProxyServer = server
	s.wsProxyPort = port
	s.wsProxyTarget = targetWSURL

	return fmt.Sprintf("ws://127.0.0.1:%d/ws", port), nil
}

func (s *AppService) SetApp(app *application.App) {
	s.app = app
	if app != nil {
		initNativeNotifications(func(payload map[string]interface{}) {
			if win := app.CurrentWindow(); win != nil {
				win.Show()
				win.Focus()
			}
			if len(payload) > 0 {
				app.EmitEvent("inbox:open", payload)
			}
		})
	}
}

var (
	appVersion     = "0.13.2"
	appProductName = "CodeOPC"
)

// GetAppInfo returns app version and normalized OS for the renderer.
func (s *AppService) GetAppInfo() AppInfo {
	osName := "unknown"
	switch runtime.GOOS {
	case "darwin":
		osName = "macos"
	case "windows":
		osName = "windows"
	case "linux":
		osName = "linux"
	}

	return AppInfo{
		Version: appVersion,
		OS:      osName,
	}
}

// GetRuntimeConfig returns the authentic API endpoints configuration.
func (s *AppService) GetRuntimeConfig() RuntimeConfigResult {
	s.mu.RLock()
	apiURL := s.targetAPIURL
	appURL := s.targetAppURL
	platformURL := s.targetPlatformURL
	s.mu.RUnlock()

	if apiURL == "" {
		apiURL, appURL, platformURL = resolveDefaultRuntimeEndpoints()
	}
	wsURL := os.Getenv("MULTICA_WS_URL")
	if wsURL == "" {
		wsURL = strings.Replace(apiURL, "http", "ws", 1) + "/ws"
	}
	if appURL == "" {
		appURL = "http://localhost:3000"
	}

	// Route WebSocket connection through local loopback proxy to avoid CORS/Origin rejection on remote server
	proxiedWS, err := s.ensureWSProxy(wsURL)
	if err == nil && proxiedWS != "" {
		wsURL = proxiedWS
	}

	return RuntimeConfigResult{
		OK: true,
		Config: &RuntimeConfig{
			SchemaVersion: 1,
			ApiURL:        apiURL,
			WsURL:         wsURL,
			AppURL:        appURL,
			PlatformURL:   platformURL,
		},
	}
}

// ProxyFetch tunnels frontend HTTP requests through native Go networking, completely
// bypassing WebKit CORS, ATS, and mixed-content restrictions without needing an insecure Electron flag.
func (s *AppService) ProxyFetch(req ProxyFetchRequest) (*ProxyFetchResponse, error) {
	var bodyReader io.Reader
	if req.BodyB64 != "" {
		raw, err := base64.StdEncoding.DecodeString(req.BodyB64)
		if err != nil {
			return nil, fmt.Errorf("decode proxy bodyB64: %w", err)
		}
		bodyReader = bytes.NewReader(raw)
	} else if req.Body != "" {
		bodyReader = strings.NewReader(req.Body)
	}
	httpReq, err := http.NewRequest(req.Method, req.URL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create proxy request: %w", err)
	}

	for k, v := range req.Headers {
		// Do not forward browser host-managed connection/origin headers to avoid tripping server proxy checks
		lower := strings.ToLower(k)
		if lower == "origin" || lower == "host" || lower == "content-length" {
			continue
		}
		httpReq.Header.Set(k, v)
	}

	resp, err := s.proxyClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute proxy request to %s: %w", req.URL, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read proxy response body: %w", err)
	}

	respHeaders := make(map[string]string)
	for k := range resp.Header {
		respHeaders[k] = resp.Header.Get(k)
	}

	contentType := resp.Header.Get("Content-Type")
	isBinary := !utf8.Valid(respBody) ||
		strings.HasPrefix(contentType, "image/") ||
		strings.HasPrefix(contentType, "application/octet-stream") ||
		strings.HasPrefix(contentType, "application/pdf") ||
		strings.HasPrefix(contentType, "video/") ||
		strings.HasPrefix(contentType, "audio/")

	var bodyStr, bodyB64 string
	if isBinary {
		bodyB64 = base64.StdEncoding.EncodeToString(respBody)
	} else {
		bodyStr = string(respBody)
	}

	return &ProxyFetchResponse{
		Status:     resp.StatusCode,
		StatusText: resp.Status,
		Headers:    respHeaders,
		Body:       bodyStr,
		BodyB64:    bodyB64,
	}, nil
}

// GetSystemLocale returns the primary system language locale.
func (s *AppService) GetSystemLocale() string {
	lang := os.Getenv("LANG")
	if lang == "" {
		return "en"
	}
	if strings.HasPrefix(lang, "zh") {
		if strings.Contains(lang, "TW") || strings.Contains(lang, "HK") {
			return "zh-Hant"
		}
		return "zh-Hans"
	}
	if strings.HasPrefix(lang, "ja") {
		return "ja"
	}
	if strings.HasPrefix(lang, "ko") {
		return "ko"
	}
	if strings.HasPrefix(lang, "fr") {
		return "fr"
	}
	return "en"
}

// GetHostName returns the local machine hostname.
func (s *AppService) GetHostName() (string, error) {
	name, err := os.Hostname()
	if err != nil {
		return "localhost", nil
	}
	return name, nil
}

// OpenExternal opens a URL in the user's default browser.
func (s *AppService) OpenExternal(rawURL string) error {
	if s.app != nil {
		return s.app.BrowserOpenURL(rawURL)
	}
	return nil
}

// OpenLoginWindow navigates the main application window in-place to the OAuth login URL.
func (s *AppService) OpenLoginWindow(rawURL string, returnURL string) error {
	if s.app == nil || s.mainWindow == nil {
		return fmt.Errorf("application is not initialized")
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("invalid login URL")
	}
	cleanReturn := strings.TrimSpace(returnURL)
	if cleanReturn == "" {
		if runtime.GOOS == "windows" {
			cleanReturn = "http://wails.localhost/"
		} else {
			cleanReturn = "wails://wails.localhost/"
		}
	}

	go func() {
		if s.mainWindow == nil {
			return
		}
		s.mu.Lock()
		s.loginOriginURL = cleanReturn
		s.mu.Unlock()

		s.mainWindow.SetURL(rawURL)
		s.mainWindow.Focus()
	}()
	return nil
}

// CloseLoginWindow restores the main window back to the app root if it was navigated to OAuth login.
func (s *AppService) CloseLoginWindow() error {
	go func() {
		s.mu.Lock()
		originURL := s.loginOriginURL
		s.loginOriginURL = ""
		s.mu.Unlock()
		if originURL != "" && s.mainWindow != nil {
			s.mainWindow.SetURL("")
			s.mainWindow.Focus()
		}
	}()
	return nil
}

// OpenIssueWindow opens a dedicated native Webview window in Wails v3.
func (s *AppService) OpenIssueWindow(req IssueWindowRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.openWindows[req.Path]; ok && existing != nil {
		existing.Focus()
		return nil
	}

	winTitle := req.Title
	if winTitle == "" {
		winTitle = "Issue Details"
	}

	win := s.app.NewWebviewWindowWithOptions(application.WebviewWindowOptions{
		Title:  winTitle,
		Width:  1050,
		Height: 850,
		URL:    req.Path,
		Mac: application.MacWindow{
			TitleBar:                application.MacTitleBarHiddenInset,
			InvisibleTitleBarHeight: 50,
		},
	})
	win.OnWindowEvent(events.Common.WindowShow, func(e *application.WindowEvent) {
		attachWebviewUIDelegate(win)
	})
	win.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, func(e *application.WindowEvent) {
		attachWebviewUIDelegate(win)
	})
	win.OnWindowEvent(events.Common.WindowClosing, func(e *application.WindowEvent) {
		s.mu.Lock()
		delete(s.openWindows, req.Path)
		s.mu.Unlock()
	})
	win.Show()
	attachWebviewUIDelegate(win)
	s.openWindows[req.Path] = win
	return nil
}

// OpenWorkspaceWindow opens or focuses a dedicated window for a workspace.
func (s *AppService) OpenWorkspaceWindow(slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.openWindows[slug]; ok && existing != nil {
		existing.Focus()
		return nil
	}

	win := s.app.NewWebviewWindowWithOptions(application.WebviewWindowOptions{
		Title:  slug,
		Width:  1280,
		Height: 900,
		URL:    "/" + slug,
		Mac: application.MacWindow{
			TitleBar:                application.MacTitleBarHiddenInset,
			InvisibleTitleBarHeight: 50,
		},
	})
	win.OnWindowEvent(events.Common.WindowShow, func(e *application.WindowEvent) {
		attachWebviewUIDelegate(win)
	})
	win.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, func(e *application.WindowEvent) {
		attachWebviewUIDelegate(win)
	})
	win.OnWindowEvent(events.Common.WindowClosing, func(e *application.WindowEvent) {
		s.mu.Lock()
		delete(s.openWindows, slug)
		s.mu.Unlock()
	})
	win.Show()
	attachWebviewUIDelegate(win)
	s.openWindows[slug] = win
	return nil
}

// PickDirectory shows an OS directory picker dialog.
func (s *AppService) PickDirectory(defaultPath string) (string, error) {
	dialog := application.OpenFileDialog().CanChooseDirectories(true).CanChooseFiles(false)
	if s.app != nil {
		if win := s.app.CurrentWindow(); win != nil {
			dialog.AttachToWindow(win)
		}
	}
	if defaultPath != "" {
		dialog.SetDirectory(defaultPath)
	}
	return dialog.PromptForSingleSelection()
}

// PickedFile represents a local file picked via the native OS open panel.
type PickedFile struct {
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	DataB64  string `json:"dataB64"`
}

// PickFiles opens a native OS file picker dialog and returns the selected files.
func (s *AppService) PickFiles(multiple bool, accept string) ([]PickedFile, error) {
	dialog := application.OpenFileDialog().CanChooseFiles(true).CanChooseDirectories(false)
	if strings.Contains(accept, "image") {
		dialog.AddFilter("Images", "*.png;*.jpg;*.jpeg;*.gif;*.webp;*.svg;*.bmp;*.ico")
	}

	var paths []string
	if multiple {
		selected, err := dialog.PromptForMultipleSelection()
		if err != nil {
			return nil, err
		}
		paths = selected
	} else {
		selected, err := dialog.PromptForSingleSelection()
		if err != nil {
			return nil, err
		}
		if selected != "" {
			paths = []string{selected}
		}
	}

	if len(paths) == 0 {
		return []PickedFile{}, nil
	}

	result := make([]PickedFile, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read picked file %q: %w", p, err)
		}
		ext := strings.ToLower(filepath.Ext(p))
		mimeType := mime.TypeByExtension(ext)
		if mimeType == "" {
			mimeType = http.DetectContentType(data)
		}
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		result = append(result, PickedFile{
			Name:     filepath.Base(p),
			MimeType: mimeType,
			DataB64:  base64.StdEncoding.EncodeToString(data),
		})
	}
	return result, nil
}

// ValidateLocalDirectoryResult matches the frontend ValidateLocalDirectoryResult contract.
type ValidateLocalDirectoryResult struct {
	OK        bool   `json:"ok"`
	Reason    string `json:"reason,omitempty"`
	Error     string `json:"error,omitempty"`
	IsGitRepo bool   `json:"is_git_repo"`
}

// ValidateLocalDirectory checks if path exists, is a directory, and whether it sits inside a git repo.
func (s *AppService) ValidateLocalDirectory(dirPath string) ValidateLocalDirectoryResult {
	if !filepath.IsAbs(dirPath) {
		return ValidateLocalDirectoryResult{OK: false, Reason: "not_absolute"}
	}
	info, err := os.Stat(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ValidateLocalDirectoryResult{OK: false, Reason: "not_found", Error: err.Error()}
		}
		return ValidateLocalDirectoryResult{OK: false, Reason: "error", Error: err.Error()}
	}
	if !info.IsDir() {
		return ValidateLocalDirectoryResult{OK: false, Reason: "not_a_directory"}
	}
	isGit := false
	cur := dirPath
	for {
		if _, statErr := os.Stat(filepath.Join(cur, ".git")); statErr == nil {
			isGit = true
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return ValidateLocalDirectoryResult{OK: true, IsGitRepo: isGit}
}

// SetUnreadBadge updates the OS dock badge on macOS.
func (s *AppService) SetUnreadBadge(count int) {
	if runtime.GOOS == "darwin" {
		label := ""
		if count > 0 {
			if count > 99 {
				label = "99+"
			} else {
				label = fmt.Sprintf("%d", count)
			}
		}
		script := fmt.Sprintf(`tell application "System Events" to set badge label of file (path to me as text) to "%s"`, label)
		_ = exec.Command("osascript", "-e", script).Run()
	}
}

// ShowNotification emits an OS notification.
func (s *AppService) ShowNotification(payload map[string]interface{}) error {
	title, _ := payload["title"].(string)
	body, _ := payload["body"].(string)
	if title == "" {
		title = "CodeOPC"
	}

	if body != "" {
		showNativeNotification(title, body, payload)
	}

	if s.app != nil {
		s.app.EmitEvent("notification:received", payload)
	}
	return nil
}

// CloseCurrentWindow closes the calling window.
func (s *AppService) CloseCurrentWindow() {
	if s.app != nil {
		if win := s.app.CurrentWindow(); win != nil {
			win.Close()
		}
	}
}

// ToggleMaximizeCurrentWindow toggles maximize/restore on the calling window.
func (s *AppService) ToggleMaximizeCurrentWindow() {
	if s.app != nil {
		if win := s.app.CurrentWindow(); win != nil {
			win.ToggleMaximise()
		}
	}
}

// ReadThemeFolder reads real custom theme directories from ~/.multica/themes.
func (s *AppService) ReadThemeFolder() ([]string, error) {
	return s.daemonService.ReadThemeFiles()
}

// --- Real Daemon Management ---

func (s *AppService) SetDaemonTargetApiUrl(apiUrl string) error {
	s.mu.Lock()
	s.targetAPIURL = apiUrl
	token := s.daemonToken
	userID := s.daemonUserID
	s.mu.Unlock()

	if token != "" {
		return s.daemonService.SyncToken(apiUrl, token, userID)
	}
	return nil
}

func (s *AppService) SyncDaemonToken(token, userId string) error {
	s.mu.Lock()
	s.daemonToken = token
	s.daemonUserID = userId
	targetURL := s.targetAPIURL
	s.mu.Unlock()

	return s.daemonService.SyncToken(targetURL, token, userId)
}

func (s *AppService) GetDaemonStatus() DaemonStatus {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()

	return s.daemonService.GetStatus(targetURL)
}

func (s *AppService) StartDaemon() error {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()

	err := s.daemonService.StartDaemon(targetURL)
	if err == nil && s.app != nil {
		status := s.daemonService.GetStatus(targetURL)
		s.app.EmitEvent("daemon:status-changed", status)
	}
	return err
}

func (s *AppService) StopDaemon() error {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()

	err := s.daemonService.StopDaemon(targetURL)
	if err == nil && s.app != nil {
		s.app.EmitEvent("daemon:status-changed", DaemonStatus{State: "stopped", ServerURL: targetURL})
	}
	return err
}

func (s *AppService) RestartDaemon() error {
	if err := s.StopDaemon(); err != nil {
		return err
	}
	return s.StartDaemon()
}

func (s *AppService) ProbeLocalRuntimes() (*embeddeddaemon.ProbeResult, error) {
	return s.daemonService.ProbeLocalRuntimes()
}

// CheckServerAuth verifies authentication with the remote server using the native client.
func (s *AppService) CheckServerAuth() (*embeddeddaemon.UserInfo, error) {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.CheckServerAuth(targetURL)
}

// ListRemoteWorkspaces fetches the workspace list from the server via Go client.
func (s *AppService) ListRemoteWorkspaces() ([]embeddeddaemon.WorkspaceInfo, error) {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.ListRemoteWorkspaces(targetURL)
}

// UploadAttachment uploads a local file directly from disk to the server as an issue attachment.
func (s *AppService) UploadAttachment(filePath, issueID string) (*embeddeddaemon.AttachmentInfo, error) {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.UploadAttachment(targetURL, filePath, issueID)
}

func (s *AppService) ListProjects() ([]embeddeddaemon.Project, error) {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.ListProjects(targetURL)
}

func (s *AppService) ListIssues(opts embeddeddaemon.ListIssueOptions) ([]embeddeddaemon.Issue, error) {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.ListIssues(targetURL, opts)
}

func (s *AppService) GetIssue(idOrIdentifier string) (*embeddeddaemon.Issue, error) {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.GetIssue(targetURL, idOrIdentifier)
}

func (s *AppService) CreateIssue(req embeddeddaemon.CreateIssueRequest) (*embeddeddaemon.Issue, error) {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.CreateIssue(targetURL, req)
}

func (s *AppService) UpdateIssue(issueID string, req embeddeddaemon.UpdateIssueRequest) (*embeddeddaemon.Issue, error) {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.UpdateIssue(targetURL, issueID, req)
}

func (s *AppService) ListIssueComments(issueID string) ([]embeddeddaemon.IssueComment, error) {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.ListIssueComments(targetURL, issueID)
}

func (s *AppService) AddIssueComment(issueID, content string) (*embeddeddaemon.IssueComment, error) {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.AddIssueComment(targetURL, issueID, content)
}

func (s *AppService) SetIssueStatus(issueID, status string) error {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.SetIssueStatus(targetURL, issueID, status)
}

func (s *AppService) AssignIssue(issueID string, assigneeType, assigneeID *string) error {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.AssignIssue(targetURL, issueID, assigneeType, assigneeID)
}

func (s *AppService) DeleteIssue(issueID string) error {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.DeleteIssue(targetURL, issueID)
}

func (s *AppService) ListIssueChildren(issueID string) ([]embeddeddaemon.Issue, error) {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.ListIssueChildren(targetURL, issueID)
}

func (s *AppService) SearchIssues(query string) ([]embeddeddaemon.Issue, error) {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.SearchIssues(targetURL, query)
}

func (s *AppService) ArchiveProject(id string) error {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.ArchiveProject(targetURL, id)
}

func (s *AppService) RestoreProject(id string) error {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.RestoreProject(targetURL, id)
}

func (s *AppService) DeleteProject(id string) error {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.DeleteProject(targetURL, id)
}

func (s *AppService) ListProjectMembers(id string) ([]embeddeddaemon.ProjectMember, error) {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.ListProjectMembers(targetURL, id)
}

func (s *AppService) AddProjectMembers(id string, userIDs []string) error {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.AddProjectMembers(targetURL, id, userIDs)
}

func (s *AppService) ListAgents() ([]embeddeddaemon.WorkspaceAgent, error) {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.ListAgents(targetURL)
}

func (s *AppService) ListWorkspaceMembers() ([]embeddeddaemon.WorkspaceMember, error) {
	s.mu.RLock()
	targetURL := s.targetAPIURL
	s.mu.RUnlock()
	return s.daemonService.ListWorkspaceMembers(targetURL)
}
