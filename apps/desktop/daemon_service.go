package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/embeddeddaemon"
)

// DaemonService provides full native daemon control in-process without CLI dependencies.
type DaemonService struct {
	manager *embeddeddaemon.Manager
}

func NewDaemonService() *DaemonService {
	var writer io.Writer = os.Stderr
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		logDir := filepath.Join(home, ".multica")
		if os.MkdirAll(logDir, 0o755) == nil {
			logPath := filepath.Join(logDir, "daemon.log")
			if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
				writer = io.MultiWriter(os.Stderr, f)
			}
		}
	}
	logger := slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return &DaemonService{
		manager: embeddeddaemon.NewManager(logger),
	}
}

// DeriveProfileName calculates the deterministic desktop profile name for a server URL.
func (d *DaemonService) DeriveProfileName(targetURL string) string {
	if targetURL == "" {
		return "desktop"
	}
	parsed, err := url.Parse(targetURL)
	if err != nil || parsed.Host == "" {
		h := sha256.Sum256([]byte(targetURL))
		return fmt.Sprintf("desktop-%s", hex.EncodeToString(h[:4]))
	}

	host := strings.ToLower(parsed.Host)
	host = strings.ReplaceAll(host, ":", "-")
	return fmt.Sprintf("desktop-%s", host)
}

// GetStatus checks real in-process or profile health of the daemon for targetURL.
func (d *DaemonService) GetStatus(targetURL string) DaemonStatus {
	profile := d.DeriveProfileName(targetURL)
	hostname, _ := os.Hostname()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	status, err := d.manager.GetStatus(ctx, profile)
	if err != nil || status == nil || !status.Running {
		return DaemonStatus{
			State:      "stopped",
			Profile:    profile,
			ServerURL:  targetURL,
			DeviceName: hostname,
			Agents:     []string{},
		}
	}

	uptimeStr := ""
	if status.RawHealth != nil {
		if ut, ok := status.RawHealth["uptime"].(string); ok {
			uptimeStr = ut
		}
	}

	daemonID := ""
	if status.RawHealth != nil {
		if did, ok := status.RawHealth["daemon_id"].(string); ok {
			daemonID = did
		}
	}

	deviceName := hostname
	if status.RawHealth != nil {
		if dn, ok := status.RawHealth["device_name"].(string); ok && dn != "" {
			deviceName = dn
		}
	}

	agents := status.Providers
	if len(agents) == 0 && status.RawHealth != nil {
		if ags, ok := status.RawHealth["agents"].([]any); ok {
			for _, a := range ags {
				if as, ok := a.(string); ok {
					agents = append(agents, as)
				}
			}
		}
	}

	return DaemonStatus{
		State:          "running",
		PID:            status.PID,
		Uptime:         uptimeStr,
		DaemonID:       daemonID,
		DeviceName:     deviceName,
		Agents:         agents,
		WorkspaceCount: len(status.Workspaces),
		Profile:        profile,
		ServerURL:      status.ServerURL,
	}
}

// StartDaemon launches the in-process daemon without shelling out to external multica CLI.
func (d *DaemonService) StartDaemon(targetURL string) error {
	profile := d.DeriveProfileName(targetURL)

	// Check if already running
	status := d.GetStatus(targetURL)
	if status.State == "running" {
		return nil
	}

	opts := embeddeddaemon.DaemonOptions{
		Profile:    profile,
		ServerURL:  targetURL,
		LaunchedBy: "desktop",
		CLIVersion: "0.11.0",
		CLICommit:  "wails-desktop",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := d.manager.Start(ctx, opts); err != nil {
		return fmt.Errorf("start in-process daemon: %w", err)
	}

	// Poll briefly until ready
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st := d.GetStatus(targetURL)
		if st.State == "running" {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

// StopDaemon stops the embedded daemon cleanly.
func (d *DaemonService) StopDaemon(targetURL string) error {
	profile := d.DeriveProfileName(targetURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return d.manager.Stop(ctx, profile)
}

// RestartDaemon restarts the embedded daemon.
func (d *DaemonService) RestartDaemon(targetURL string) error {
	profile := d.DeriveProfileName(targetURL)
	opts := embeddeddaemon.DaemonOptions{
		Profile:    profile,
		ServerURL:  targetURL,
		LaunchedBy: "desktop",
		CLIVersion: "0.11.0",
		CLICommit:  "wails-desktop",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	return d.manager.Restart(ctx, opts)
}

// SyncToken writes the token and server URL into the profile configuration directly.
func (d *DaemonService) SyncToken(targetURL string, token string, userId string) error {
	profile := d.DeriveProfileName(targetURL)
	_ = d.manager.SyncToken("", token, targetURL) // Also keep default profile synced for CLI!
	return d.manager.SyncToken(profile, token, targetURL)
}

// ReadThemeFiles reads real custom theme directories from ~/.multica/themes.
func (d *DaemonService) ReadThemeFiles() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	themeDir := filepath.Join(home, ".multica", "themes")
	entries, err := os.ReadDir(themeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	var themes []string
	for _, e := range entries {
		if e.IsDir() {
			themes = append(themes, e.Name())
		}
	}
	return themes, nil
}

// ProbeLocalRuntimes performs in-process detection of installed AI agent CLIs.
func (d *DaemonService) ProbeLocalRuntimes() (*embeddeddaemon.ProbeResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	profile := d.DeriveProfileName("")
	return d.manager.ProbeLocalRuntimes(ctx, profile)
}

// GetServerClient returns an authenticated ServerClient for the target server and profile.
func (d *DaemonService) GetServerClient(targetURL string) (*embeddeddaemon.ServerClient, error) {
	profile := d.DeriveProfileName(targetURL)
	return d.manager.ClientForProfile(profile)
}

// CheckServerAuth verifies the saved token against the remote server /api/me.
func (d *DaemonService) CheckServerAuth(targetURL string) (*embeddeddaemon.UserInfo, error) {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return client.Whoami(ctx)
}

// ListRemoteWorkspaces fetches the user's workspaces from the remote server.
func (d *DaemonService) ListRemoteWorkspaces(targetURL string) ([]embeddeddaemon.WorkspaceInfo, error) {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return client.ListWorkspaces(ctx)
}

// UploadAttachment uploads a local file as an issue attachment using pure Go.
func (d *DaemonService) UploadAttachment(targetURL, filePath, issueID string) (*embeddeddaemon.AttachmentInfo, error) {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return nil, err
	}
	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read attachment file: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return client.UploadIssueAttachment(ctx, fileBytes, filepath.Base(filePath), issueID)
}

// ListProjects returns projects for the authenticated workspace.
func (d *DaemonService) ListProjects(targetURL string) ([]embeddeddaemon.Project, error) {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.ListProjects(ctx)
}

// ListIssues returns issues for the authenticated workspace.
func (d *DaemonService) ListIssues(targetURL string, opts embeddeddaemon.ListIssueOptions) ([]embeddeddaemon.Issue, error) {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.ListIssues(ctx, opts)
}

// GetIssue returns a single issue by ID or identifier.
func (d *DaemonService) GetIssue(targetURL, idOrIdentifier string) (*embeddeddaemon.Issue, error) {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.GetIssue(ctx, idOrIdentifier)
}

// CreateIssue creates a new issue in the workspace using pure Go.
func (d *DaemonService) CreateIssue(targetURL string, req embeddeddaemon.CreateIssueRequest) (*embeddeddaemon.Issue, error) {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return client.CreateIssue(ctx, req)
}

// UpdateIssue updates an issue using pure Go.
func (d *DaemonService) UpdateIssue(targetURL, issueID string, req embeddeddaemon.UpdateIssueRequest) (*embeddeddaemon.Issue, error) {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return client.UpdateIssue(ctx, issueID, req)
}

// ListIssueComments returns comments for an issue.
func (d *DaemonService) ListIssueComments(targetURL, issueID string) ([]embeddeddaemon.IssueComment, error) {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.ListIssueComments(ctx, issueID)
}

// AddIssueComment adds a comment to an issue using pure Go.
func (d *DaemonService) AddIssueComment(targetURL, issueID, content string) (*embeddeddaemon.IssueComment, error) {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return client.AddIssueComment(ctx, issueID, content)
}

// SetIssueStatus changes the status of an issue using pure Go method call.
func (d *DaemonService) SetIssueStatus(targetURL, idOrIdentifier, status string) error {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.SetIssueStatus(ctx, idOrIdentifier, status)
}

// AssignIssue assigns an issue to a user or agent using pure Go method call.
func (d *DaemonService) AssignIssue(targetURL, idOrIdentifier string, assigneeType, assigneeID *string) error {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.AssignIssue(ctx, idOrIdentifier, assigneeType, assigneeID)
}

// DeleteIssue deletes an issue using pure Go method call.
func (d *DaemonService) DeleteIssue(targetURL, idOrIdentifier string) error {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.DeleteIssue(ctx, idOrIdentifier)
}

// ListIssueChildren lists sub-issues of a parent issue.
func (d *DaemonService) ListIssueChildren(targetURL, idOrIdentifier string) ([]embeddeddaemon.Issue, error) {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.ListIssueChildren(ctx, idOrIdentifier)
}

// SearchIssues searches issues by text query.
func (d *DaemonService) SearchIssues(targetURL, query string) ([]embeddeddaemon.Issue, error) {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.SearchIssues(ctx, query)
}

// ArchiveProject archives a project.
func (d *DaemonService) ArchiveProject(targetURL, id string) error {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.ArchiveProject(ctx, id)
}

// RestoreProject restores an archived project.
func (d *DaemonService) RestoreProject(targetURL, id string) error {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.RestoreProject(ctx, id)
}

// DeleteProject deletes a project.
func (d *DaemonService) DeleteProject(targetURL, id string) error {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.DeleteProject(ctx, id)
}

// ListProjectMembers lists members in a project.
func (d *DaemonService) ListProjectMembers(targetURL, id string) ([]embeddeddaemon.ProjectMember, error) {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.ListProjectMembers(ctx, id)
}

// AddProjectMembers adds users to a project.
func (d *DaemonService) AddProjectMembers(targetURL, id string, userIDs []string) error {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.AddProjectMembers(ctx, id, userIDs)
}

// ListAgents lists agents in the workspace.
func (d *DaemonService) ListAgents(targetURL string) ([]embeddeddaemon.WorkspaceAgent, error) {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.ListAgents(ctx)
}

// ListWorkspaceMembers lists all members in the workspace.
func (d *DaemonService) ListWorkspaceMembers(targetURL string) ([]embeddeddaemon.WorkspaceMember, error) {
	client, err := d.GetServerClient(targetURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.ListWorkspaceMembers(ctx)
}
