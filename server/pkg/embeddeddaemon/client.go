package embeddeddaemon

import (
	"context"
	"fmt"
	"os"

	"github.com/multica-ai/multica/server/internal/cli"
)

// UserInfo represents the current authenticated user identity from /api/me.
type UserInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// WorkspaceInfo represents a workspace the user has access to.
type WorkspaceInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Role string `json:"role,omitempty"`
}

// Type aliases from cli
type Project = cli.Project
type Issue = cli.Issue
type IssueComment = cli.IssueComment
type CreateIssueRequest = cli.CreateIssueRequest
type UpdateIssueRequest = cli.UpdateIssueRequest
type ListIssueOptions = cli.ListIssueOptions
type ProjectMember = cli.ProjectMember
type WorkspaceAgent = cli.WorkspaceAgent
type WorkspaceMember = cli.WorkspaceMember

// AttachmentInfo matches the uploaded attachment response metadata.
type AttachmentInfo struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	DownloadURL string `json:"download_url"`
	MarkdownURL string `json:"markdown_url"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	CreatedAt   string `json:"created_at"`
}

// ServerClient provides direct, typed HTTP interactions with the Multica server,
// encapsulating the full capabilities previously held by the CLI's APIClient.
type ServerClient struct {
	inner *cli.APIClient
}

// NewServerClient creates a ServerClient with target base URL, workspace ID, and auth token.
func NewServerClient(baseURL, workspaceID, token string) *ServerClient {
	return &ServerClient{
		inner: cli.NewAPIClient(baseURL, workspaceID, token),
	}
}

// ClientForProfile creates a ServerClient using the persisted configuration for a profile.
func (m *Manager) ClientForProfile(profile string) (*ServerClient, error) {
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return nil, fmt.Errorf("load config for profile %q: %w", profile, err)
	}
	serverURL := cfg.ServerURL
	if serverURL == "" {
		serverURL = os.Getenv("MULTICA_SERVER_URL")
	}
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	return NewServerClient(serverURL, cfg.WorkspaceID, cfg.Token), nil
}

// Whoami validates the auth token and fetches user details from /api/me.
func (c *ServerClient) Whoami(ctx context.Context) (*UserInfo, error) {
	var user UserInfo
	if err := c.inner.GetJSON(ctx, "/api/me", &user); err != nil {
		return nil, fmt.Errorf("check authentication: %w", err)
	}
	return &user, nil
}

// ListWorkspaces retrieves the list of accessible workspaces from /api/workspaces.
func (c *ServerClient) ListWorkspaces(ctx context.Context) ([]WorkspaceInfo, error) {
	var workspaces []WorkspaceInfo
	if err := c.inner.GetJSON(ctx, "/api/workspaces", &workspaces); err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	return workspaces, nil
}

// ListProjects retrieves all projects in the current workspace.
func (c *ServerClient) ListProjects(ctx context.Context) ([]Project, error) {
	return c.inner.ListProjects(ctx)
}

// GetProject retrieves a single project by ID.
func (c *ServerClient) GetProject(ctx context.Context, id string) (*Project, error) {
	return c.inner.GetProject(ctx, id)
}

// ListIssues retrieves issues based on the provided filters.
func (c *ServerClient) ListIssues(ctx context.Context, opts ListIssueOptions) ([]Issue, error) {
	return c.inner.ListIssues(ctx, opts)
}

// GetIssue retrieves a single issue by ID or identifier (e.g. THEB-1).
func (c *ServerClient) GetIssue(ctx context.Context, idOrIdentifier string) (*Issue, error) {
	return c.inner.GetIssue(ctx, idOrIdentifier)
}

// CreateIssue creates a new issue in the workspace.
func (c *ServerClient) CreateIssue(ctx context.Context, req CreateIssueRequest) (*Issue, error) {
	return c.inner.CreateIssue(ctx, req)
}

// UpdateIssue updates an existing issue.
func (c *ServerClient) UpdateIssue(ctx context.Context, id string, req UpdateIssueRequest) (*Issue, error) {
	return c.inner.UpdateIssue(ctx, id, req)
}

// ListIssueComments retrieves all comments for an issue.
func (c *ServerClient) ListIssueComments(ctx context.Context, issueID string) ([]IssueComment, error) {
	return c.inner.ListIssueComments(ctx, issueID)
}

// AddIssueComment posts a new comment to an issue.
func (c *ServerClient) AddIssueComment(ctx context.Context, issueID string, content string) (*IssueComment, error) {
	return c.inner.AddIssueComment(ctx, issueID, content)
}

// SetIssueStatus changes the status of an issue.
func (c *ServerClient) SetIssueStatus(ctx context.Context, idOrIdentifier, status string) error {
	return c.inner.SetIssueStatus(ctx, idOrIdentifier, status)
}

// AssignIssue assigns or unassigns an issue.
func (c *ServerClient) AssignIssue(ctx context.Context, idOrIdentifier string, assigneeType, assigneeID *string) error {
	return c.inner.AssignIssue(ctx, idOrIdentifier, assigneeType, assigneeID)
}

// DeleteIssue deletes an issue.
func (c *ServerClient) DeleteIssue(ctx context.Context, idOrIdentifier string) error {
	return c.inner.DeleteIssue(ctx, idOrIdentifier)
}

// ListIssueChildren lists sub-issues of a parent issue.
func (c *ServerClient) ListIssueChildren(ctx context.Context, idOrIdentifier string) ([]Issue, error) {
	return c.inner.ListIssueChildren(ctx, idOrIdentifier)
}

// SearchIssues searches issues by query string.
func (c *ServerClient) SearchIssues(ctx context.Context, query string) ([]Issue, error) {
	return c.inner.SearchIssues(ctx, query)
}

// UpdateIssueComment edits an existing comment on an issue.
func (c *ServerClient) UpdateIssueComment(ctx context.Context, issueID, commentID, content string) error {
	return c.inner.UpdateIssueComment(ctx, issueID, commentID, content)
}

// DeleteIssueComment deletes a comment on an issue.
func (c *ServerClient) DeleteIssueComment(ctx context.Context, issueID, commentID string) error {
	return c.inner.DeleteIssueComment(ctx, issueID, commentID)
}

// ResolveIssueComment marks a comment thread resolved or unresolved.
func (c *ServerClient) ResolveIssueComment(ctx context.Context, issueID, commentID string, resolve bool) error {
	return c.inner.ResolveIssueComment(ctx, issueID, commentID, resolve)
}

// ArchiveProject archives a project.
func (c *ServerClient) ArchiveProject(ctx context.Context, id string) error {
	return c.inner.ArchiveProject(ctx, id)
}

// RestoreProject restores an archived project.
func (c *ServerClient) RestoreProject(ctx context.Context, id string) error {
	return c.inner.RestoreProject(ctx, id)
}

// DeleteProject deletes a project.
func (c *ServerClient) DeleteProject(ctx context.Context, id string) error {
	return c.inner.DeleteProject(ctx, id)
}

// ListProjectMembers lists members in a project.
func (c *ServerClient) ListProjectMembers(ctx context.Context, id string) ([]ProjectMember, error) {
	return c.inner.ListProjectMembers(ctx, id)
}

// AddProjectMembers adds users to a project.
func (c *ServerClient) AddProjectMembers(ctx context.Context, id string, userIDs []string) error {
	return c.inner.AddProjectMembers(ctx, id, userIDs)
}

// RemoveProjectMembers removes users from a project.
func (c *ServerClient) RemoveProjectMembers(ctx context.Context, id string, userIDs []string) error {
	return c.inner.RemoveProjectMembers(ctx, id, userIDs)
}

// ListAgents lists agents configured in the workspace.
func (c *ServerClient) ListAgents(ctx context.Context) ([]WorkspaceAgent, error) {
	return c.inner.ListAgents(ctx)
}

// ListWorkspaceMembers lists members in the workspace.
func (c *ServerClient) ListWorkspaceMembers(ctx context.Context) ([]WorkspaceMember, error) {
	return c.inner.ListWorkspaceMembers(ctx)
}

// UploadIssueAttachment uploads an attachment to an issue.
func (c *ServerClient) UploadIssueAttachment(ctx context.Context, fileData []byte, filename, issueID string) (*AttachmentInfo, error) {
	resp, err := c.inner.UploadIssueAttachment(ctx, fileData, filename, issueID)
	if err != nil {
		return nil, fmt.Errorf("upload issue attachment: %w", err)
	}
	return &AttachmentInfo{
		ID:          resp.ID,
		URL:         resp.URL,
		DownloadURL: resp.DownloadURL,
		MarkdownURL: resp.MarkdownURL,
		Filename:    resp.Filename,
		ContentType: resp.ContentType,
		SizeBytes:   resp.SizeBytes,
		CreatedAt:   resp.CreatedAt,
	}, nil
}

// UploadChatAttachment uploads an attachment for a chat task.
func (c *ServerClient) UploadChatAttachment(ctx context.Context, fileData []byte, filename, taskID string) (*AttachmentInfo, error) {
	resp, err := c.inner.UploadChatAttachment(ctx, fileData, filename, taskID)
	if err != nil {
		return nil, fmt.Errorf("upload chat attachment: %w", err)
	}
	return &AttachmentInfo{
		ID:          resp.ID,
		URL:         resp.URL,
		DownloadURL: resp.DownloadURL,
		MarkdownURL: resp.MarkdownURL,
		Filename:    resp.Filename,
		ContentType: resp.ContentType,
		SizeBytes:   resp.SizeBytes,
		CreatedAt:   resp.CreatedAt,
	}, nil
}

// DownloadFile retrieves a file directly from the given download URL.
func (c *ServerClient) DownloadFile(ctx context.Context, downloadURL string) ([]byte, error) {
	return c.inner.DownloadFile(ctx, downloadURL)
}

// GetJSON performs an authenticated GET request against the server API.
func (c *ServerClient) GetJSON(ctx context.Context, path string, out any) error {
	return c.inner.GetJSON(ctx, path, out)
}

// PostJSON performs an authenticated POST request against the server API.
func (c *ServerClient) PostJSON(ctx context.Context, path string, body, out any) error {
	return c.inner.PostJSON(ctx, path, body, out)
}

// PatchJSON performs an authenticated PATCH request against the server API.
func (c *ServerClient) PatchJSON(ctx context.Context, path string, body, out any) error {
	return c.inner.PatchJSON(ctx, path, body, out)
}

// DeleteJSON performs an authenticated DELETE request against the server API.
func (c *ServerClient) DeleteJSON(ctx context.Context, path string) error {
	return c.inner.DeleteJSON(ctx, path)
}
