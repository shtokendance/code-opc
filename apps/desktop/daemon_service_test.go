package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/pkg/embeddeddaemon"
)

func TestDeriveProfileName(t *testing.T) {
	service := NewDaemonService()
	p1 := service.DeriveProfileName("http://localhost:8080")
	if p1 != "desktop-localhost-8080" {
		t.Errorf("expected desktop-localhost-8080, got %s", p1)
	}

	p2 := service.DeriveProfileName("https://api.multica.ai")
	if p2 != "desktop-api.multica.ai" {
		t.Errorf("expected desktop-api.multica.ai, got %s", p2)
	}
}

func TestProbeLocalRuntimesReal(t *testing.T) {
	service := NewDaemonService()
	result, err := service.ProbeLocalRuntimes()
	if err != nil {
		t.Fatalf("expected real probe to succeed, got error: %v", err)
	}
	if result.ProbeResult != "success" {
		t.Fatalf("expected success probe_result, got %s", result.ProbeResult)
	}
	t.Logf("Real in-process runtimes found without CLI: count=%d, providers=%+v", result.RuntimeCount, result.ProviderSummary)
}

func TestGetStatusReal(t *testing.T) {
	service := NewDaemonService()
	status := service.GetStatus("http://localhost:8080")
	t.Logf("Real daemon status: state=%s, pid=%d, agents=%v", status.State, status.PID, status.Agents)
}

func TestSyncTokenReal(t *testing.T) {
	service := NewDaemonService()
	targetURL := "http://localhost:8080"
	testToken := "test_token_in_process_123"
	profile := service.DeriveProfileName(targetURL)

	defer func() {
		if home, err := os.UserHomeDir(); err == nil {
			_ = os.RemoveAll(home + "/.codeopc/profiles/" + profile)
		}
	}()

	err := service.SyncToken(targetURL, testToken, "user_1")
	if err != nil {
		t.Fatalf("SyncToken failed: %v", err)
	}

	savedToken, err := embeddeddaemon.DefaultManager.LoadToken(profile)
	if err != nil {
		t.Fatalf("LoadToken failed: %v", err)
	}
	if savedToken != testToken {
		t.Errorf("expected token %s, got %s", testToken, savedToken)
	}
}

func TestServerClientInDesktop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/me":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    "desktop_user_1",
				"name":  "Desktop Tester",
				"email": "desktop@multica.ai",
			})
		case "/api/workspaces":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "ws_alpha", "name": "Alpha", "slug": "alpha"},
			})
		case "/api/upload-file":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "att_1",
				"filename":    "hello.txt",
				"markdown_url": "http://example.com/hello.txt",
			})
		case "/api/projects":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "proj_1", "title": "桌面端重构", "workspace_id": "ws_alpha"},
			})
		case "/api/issues":
			if r.Method == http.MethodPost {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":         "iss_new",
					"identifier": "ALPHA-1",
					"title":      "测试新任务",
					"status":     "todo",
				})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"issues": []map[string]any{
						{"id": "iss_1", "identifier": "ALPHA-1", "title": "测试新任务", "status": "todo"},
					},
				})
			}
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	service := NewDaemonService()
	profile := service.DeriveProfileName(srv.URL)
	defer func() {
		if home, err := os.UserHomeDir(); err == nil {
			_ = os.RemoveAll(home + "/.codeopc/profiles/" + profile)
		}
	}()

	// 1. Sync Token to profile
	_ = service.SyncToken(srv.URL, "sample_desktop_token", "user_1")

	// 2. CheckServerAuth
	me, err := service.CheckServerAuth(srv.URL)
	if err != nil {
		t.Fatalf("CheckServerAuth failed: %v", err)
	}
	if me.Email != "desktop@multica.ai" {
		t.Errorf("unexpected user email: %s", me.Email)
	}
	t.Logf("CheckServerAuth verified: %+v", me)

	// 3. ListRemoteWorkspaces
	workspaces, err := service.ListRemoteWorkspaces(srv.URL)
	if err != nil {
		t.Fatalf("ListRemoteWorkspaces failed: %v", err)
	}
	if len(workspaces) != 1 || workspaces[0].Slug != "alpha" {
		t.Errorf("unexpected workspaces: %+v", workspaces)
	}
	t.Logf("ListRemoteWorkspaces verified: %+v", workspaces)

	// 4. UploadAttachment
	tmpFile := filepath.Join(t.TempDir(), "hello.txt")
	_ = os.WriteFile(tmpFile, []byte("hello multica desktop"), 0o644)
	att, err := service.UploadAttachment(srv.URL, tmpFile, "issue_123")
	if err != nil {
		t.Fatalf("UploadAttachment failed: %v", err)
	}
	if att.Filename != "hello.txt" {
		t.Errorf("unexpected attachment filename: %s", att.Filename)
	}
	t.Logf("UploadAttachment verified: %+v", att)

	// 5. ListProjects
	projects, err := service.ListProjects(srv.URL)
	if err != nil || len(projects) != 1 || projects[0].Title != "桌面端重构" {
		t.Fatalf("ListProjects failed: %v, projects: %+v", err, projects)
	}

	// 6. CreateIssue
	issue, err := service.CreateIssue(srv.URL, embeddeddaemon.CreateIssueRequest{
		Title: "测试新任务",
	})
	if err != nil || issue.Identifier != "ALPHA-1" {
		t.Fatalf("CreateIssue failed: %v, issue: %+v", err, issue)
	}

	// 7. ListIssues
	issues, err := service.ListIssues(srv.URL, embeddeddaemon.ListIssueOptions{})
	if err != nil || len(issues) != 1 || issues[0].Title != "测试新任务" {
		t.Fatalf("ListIssues failed: %v, issues: %+v", err, issues)
	}
}

func TestProxyFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom-Echo", r.Header.Get("X-Custom-Test"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"proxied":true,"path":"` + r.URL.Path + `"}`))
	}))
	defer srv.Close()

	appService := NewAppService()
	resp, err := appService.ProxyFetch(ProxyFetchRequest{
		Method:  "POST",
		URL:     srv.URL + "/auth/send-code",
		Headers: map[string]string{"X-Custom-Test": "hello-proxy"},
		Body:    `{"email":"test@multica.ai"}`,
	})
	if err != nil {
		t.Fatalf("ProxyFetch failed: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("expected 200, got %d", resp.Status)
	}
	if resp.Headers["X-Custom-Echo"] != "hello-proxy" {
		t.Errorf("expected header echo, got %s", resp.Headers["X-Custom-Echo"])
	}
	t.Logf("ProxyFetch verified: status=%d, body=%s", resp.Status, resp.Body)
}

func TestProxyFetchMultipart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, "invalid multipart form: "+err.Error(), http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "missing file: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()
		content, _ := io.ReadAll(file)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"filename":"` + header.Filename + `","size":` + string(rune('0'+len(content))) + `}`))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "test.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte{0x50, 0x4b, 0x03, 0x04, 0x00, 0xff}) // binary xlsx header bytes
	_ = writer.Close()

	appSvc := NewAppService()
	resp, err := appSvc.ProxyFetch(ProxyFetchRequest{
		Method:  "POST",
		URL:     srv.URL + "/api/upload",
		Headers: map[string]string{"Content-Type": writer.FormDataContentType()},
		BodyB64: base64.StdEncoding.EncodeToString(buf.Bytes()),
	})
	if err != nil {
		t.Fatalf("ProxyFetch multipart failed: %v", err)
	}
	if resp.Status != 200 {
		t.Fatalf("expected 200, got %d: %s", resp.Status, resp.Body)
	}
	if !strings.Contains(resp.Body, `"filename":"test.xlsx"`) {
		t.Errorf("unexpected response body: %s", resp.Body)
	}
	t.Logf("ProxyFetchMultipart verified: status=%d, body=%s", resp.Status, resp.Body)
}

func TestWSProxy(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			// Simulate server checking that Origin matches Host
			origin := r.Header.Get("Origin")
			return origin == "http://"+r.Host
		},
	}

	mockRemoteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			// Echo back
			if err := conn.WriteMessage(mt, append([]byte("echo:"), msg...)); err != nil {
				return
			}
		}
	}))
	defer mockRemoteServer.Close()

	remoteWS := strings.Replace(mockRemoteServer.URL, "http://", "ws://", 1) + "/ws"

	appService := NewAppService()
	defer appService.Shutdown()

	localWS, err := appService.ensureWSProxy(remoteWS)
	if err != nil {
		t.Fatalf("ensureWSProxy failed: %v", err)
	}

	// Connect client to local proxy with arbitrary webview origin
	clientHeader := http.Header{}
	clientHeader.Set("Origin", "http://wails.localhost")

	clientConn, _, err := websocket.DefaultDialer.Dial(localWS+"?workspace_slug=test-ws", clientHeader)
	if err != nil {
		t.Fatalf("dial local ws proxy failed: %v", err)
	}
	defer clientConn.Close()

	// Send message
	testMsg := []byte("ping-realtime")
	if err := clientConn.WriteMessage(websocket.TextMessage, testMsg); err != nil {
		t.Fatalf("write message failed: %v", err)
	}

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, resp, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("read message failed: %v", err)
	}
	if mt != websocket.TextMessage || string(resp) != "echo:ping-realtime" {
		t.Fatalf("unexpected ws response: %s", string(resp))
	}
	t.Logf("WSProxy verified successfully: got %s", string(resp))
}
