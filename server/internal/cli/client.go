package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ClientVersion is the CLI version sent on every request as X-Client-Version.
// Set by the multica binary at init() so the package doesn't depend on the
// concrete cmd package. Defaults to "dev" when running unset (e.g. tests).
var ClientVersion = "dev"

// ClientPlatform identifies this client to the server. Override for tests
// or alternative entry points; defaults to "cli".
var ClientPlatform = "cli"

// ClientOS is the normalized operating system string sent as X-Client-OS.
// Computed once from runtime.GOOS so the server doesn't need to reverse-map
// Go's os names ("darwin"/"windows"/"linux") into the protocol vocabulary.
var ClientOS = normalizeGOOS(runtime.GOOS)

func normalizeGOOS(goos string) string {
	switch goos {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	case "linux":
		return "linux"
	default:
		return goos
	}
}

// APIClient is a REST client for the Multica server API.
// Used by ctrl subcommands (agent, runtime, status, etc.). Requests
// automatically include auth and execution context headers when configured.
type APIClient struct {
	BaseURL     string
	WorkspaceID string
	Token       string
	// AgentID / TaskID travel as X-Agent-ID / X-Task-ID execution context.
	// They are NOT what makes the server treat a request as the agent's: the
	// server strips both headers on entry and re-stamps them from the bound
	// mat_ task token, so attribution follows the token, not these fields
	// (MUL-3428). The CLI only sets them inside a daemon-managed task, where
	// MULTICA_TOKEN is that mat_ token.
	AgentID    string
	TaskID     string
	HTTPClient *http.Client

	// Identity overrides. Empty values fall back to the package-level
	// ClientPlatform / ClientVersion / ClientOS.
	Platform string
	Version  string
	OS       string
}

// TaskTokenPrefix marks a task-scoped agent token, as minted by
// internal/auth.NewAgentTaskToken. A 401 on one is not the "your login
// expired" that a 401 on a member credential is: the token belongs to a single
// run and there is no sign-in that brings it back. Why it was rejected is not
// something the CLI can see — a terminal task is the usual cause, but a
// malformed token, one sent to the wrong server, and one dropped by an
// unrelated cleanup all look identical from here.
const TaskTokenPrefix = "mat_"

type HTTPError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
	// TaskScoped records that the failing request actually carried a
	// task-scoped `mat_` token. It changes nothing about the request; it only
	// lets FormatError tell a 401 worth signing in again for from one where
	// the right move is to stop (GH #7522).
	TaskScoped bool
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s %s returned %d: %s", e.Method, e.Path, e.StatusCode, strings.TrimSpace(e.Body))
}

// maxErrorBodyBytes caps how much of an error response is kept. It must hold
// a whole structured error: a workflow's 409 status mapping plan lists one row
// per status in use (up to 50) and runs past 4 KiB, and a truncated body no
// longer parses as JSON. Only --debug prints the raw body.
const maxErrorBodyBytes = 64 << 10

// newHTTPError builds a *HTTPError from an error response (status >= 400),
// reading a capped slice of the body. Every Multica API helper funnels its
// >= 400 responses through this so the top-level FormatError / ExitCodeFor can
// classify the failure via errors.As(err, **HTTPError) regardless of which
// HTTP verb the command used.
func newHTTPError(method, path string, resp *http.Response) *HTTPError {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	return &HTTPError{
		Method:     method,
		Path:       path,
		StatusCode: resp.StatusCode,
		Body:       strings.TrimSpace(string(data)),
		TaskScoped: RequestUsedTaskToken(resp),
	}
}

// RequestUsedTaskToken reports whether the request that produced resp actually
// sent a task token.
//
// Reading the client's own Token field would be wrong twice over. DownloadFile
// deliberately sends no Authorization header for an absolute signed URL, so a
// 401 from object storage would be reported as a rejected task token purely
// because the client happened to hold a `mat_` token. And Go strips Authorization
// across a cross-host redirect, so the request that was sent is not always the
// one the caller built. resp.Request is the request that actually went out,
// after redirects, which is the only thing this claim can honestly rest on.
func RequestUsedTaskToken(resp *http.Response) bool {
	if resp == nil || resp.Request == nil {
		return false
	}
	return strings.HasPrefix(resp.Request.Header.Get("Authorization"), "Bearer "+TaskTokenPrefix)
}

// defaultHTTPTimeout is the per-request timeout for the CLI's HTTP client.
// It can be overridden with the MULTICA_HTTP_TIMEOUT environment variable
// (see httpTimeout). 30s is chosen over the historical 15s because complex
// networks (notably in mainland China) routinely need more than 15s to
// complete the TLS handshake plus request round-trip, which surfaced as an
// opaque "context deadline exceeded" to users.
const defaultHTTPTimeout = 30 * time.Second

// httpTimeout returns the HTTP client timeout, honoring MULTICA_HTTP_TIMEOUT.
// The value may be a Go duration string ("45s", "2m") or a plain integer
// number of seconds ("45"). Invalid or non-positive values fall back to the
// default.
func httpTimeout() time.Duration {
	v := strings.TrimSpace(os.Getenv("MULTICA_HTTP_TIMEOUT"))
	if v == "" {
		return defaultHTTPTimeout
	}
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return defaultHTTPTimeout
}

// apiContextGrace is added on top of the HTTP transport timeout when deriving
// a command-level context deadline, so the transport timeout (which produces a
// clean, classifiable "request timed out" error) is the one that fires rather
// than the outer context being canceled first.
const apiContextGrace = 5 * time.Second

// APITimeout returns the deadline budget for a single CLI API command. It is
// always at least the configured HTTP transport timeout (see httpTimeout,
// which honors MULTICA_HTTP_TIMEOUT) plus a small grace margin, so a
// command-level context never truncates an in-flight request below the timeout
// the user configured. This is the fix for command contexts that previously
// hardcoded a 15s deadline shorter than the 30s/env transport timeout.
func APITimeout() time.Duration {
	return AtLeastAPITimeout(0)
}

// AtLeastAPITimeout returns max(min, APITimeout()). Use it for commands that
// need a larger floor than usual (for example file uploads, which historically
// used a 60s budget).
func AtLeastAPITimeout(min time.Duration) time.Duration {
	budget := httpTimeout() + apiContextGrace
	if min > budget {
		return min
	}
	return budget
}

// APIContext derives a command-scoped context whose deadline is APITimeout().
// The returned cancel func must be called (typically via defer) to release
// resources. Commands should use this instead of context.WithTimeout with a
// hardcoded duration so the deadline always respects MULTICA_HTTP_TIMEOUT.
func APIContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, APITimeout())
}

// NewAPIClient creates a new API client for ctrl commands.
func NewAPIClient(baseURL, workspaceID, token string) *APIClient {
	return &APIClient{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		WorkspaceID: workspaceID,
		Token:       token,
		HTTPClient:  &http.Client{Timeout: httpTimeout()},
	}
}

// clientCapabilities is the X-Client-Capabilities value every CLI request
// advertises. It is a protocol detail, not a user-visible flag: the CLI always
// knows how to handle these response shapes, so there is nothing for a caller
// to opt into.
//
// stable_attachment_urls asks bulk responses to return the stable
// /api/attachments/{id}/download path instead of a ~800-char CloudFront
// signature that is re-minted on every request (MUL-5372 / GitHub #5999). The
// CLI never hands an attachment URL to a native loader — `multica attachment
// download <id>` fetches a fresh signature from the single-attachment endpoint,
// which keeps signing regardless of this capability — so the signature in list
// payloads was pure cost: raw bytes, a per-attachment RSA sign, and bytes that
// differ on every read and therefore defeat agent prompt caching.
const clientCapabilities = "stable_attachment_urls"

func (c *APIClient) setHeaders(req *http.Request) {
	req.Header.Set("X-Client-Capabilities", clientCapabilities)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.WorkspaceID != "" {
		req.Header.Set("X-Workspace-ID", c.WorkspaceID)
	}
	if c.AgentID != "" {
		req.Header.Set("X-Agent-ID", c.AgentID)
	}
	if c.TaskID != "" {
		req.Header.Set("X-Task-ID", c.TaskID)
	}

	platform := c.Platform
	if platform == "" {
		platform = ClientPlatform
	}
	if platform != "" {
		req.Header.Set("X-Client-Platform", platform)
	}
	version := c.Version
	if version == "" {
		version = ClientVersion
	}
	if version != "" {
		req.Header.Set("X-Client-Version", version)
	}
	osName := c.OS
	if osName == "" {
		osName = ClientOS
	}
	if osName != "" {
		req.Header.Set("X-Client-OS", osName)
	}
}

// GetJSON performs a GET request and decodes the JSON response.
//
// On an HTTP error response (status >= 400) the returned error is a
// *HTTPError so callers can use errors.As to inspect the status code
// (for example to recognize a 404 from a server that does not expose a
// given endpoint and degrade gracefully). The error string format
// ("GET <path> returned <code>: <body>") is preserved by HTTPError.Error().
func (c *APIClient) GetJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	err = wrapTransport(req, err)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return newHTTPError(http.MethodGet, path, resp)
	}
	if out == nil {
		return nil
	}
	return wrapBodyRead(req, json.NewDecoder(resp.Body).Decode(out))
}

// GetJSONWithHeaders performs a GET request, decodes the JSON response, and
// returns the response headers. Useful when callers need header values like
// X-Total-Count for pagination.
func (c *APIClient) GetJSONWithHeaders(ctx context.Context, path string, out any) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	err = wrapTransport(req, err)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, newHTTPError(http.MethodGet, path, resp)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.Header, wrapBodyRead(req, err)
		}
	}
	return resp.Header, nil
}

// DeleteJSON performs a DELETE request.
func (c *APIClient) DeleteJSON(ctx context.Context, path string) error {
	return c.DeleteJSONResponse(ctx, path, nil)
}

// DeleteJSONResponse performs a DELETE request and optionally decodes the JSON response.
func (c *APIClient) DeleteJSONResponse(ctx context.Context, path string, out any) error {
	return c.deleteJSON(ctx, path, out, nil)
}

// DeleteJSONWithConflict lets callers consume a structured 409 in full before
// deciding whether a follow-up request is safe. Other HTTP errors use the
// usual bounded diagnostic body.
func (c *APIClient) DeleteJSONWithConflict(ctx context.Context, path string, onConflict func(io.Reader) error) error {
	return c.deleteJSON(ctx, path, nil, onConflict)
}

func (c *APIClient) deleteJSON(ctx context.Context, path string, out any, onConflict func(io.Reader) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	err = wrapTransport(req, err)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict && onConflict != nil {
		return onConflict(resp.Body)
	}
	if resp.StatusCode >= 400 {
		return newHTTPError(http.MethodDelete, path, resp)
	}
	if out != nil {
		return wrapBodyRead(req, json.NewDecoder(resp.Body).Decode(out))
	}
	return nil
}

// DeleteJSONWithBody performs a DELETE request with a JSON body.
func (c *APIClient) DeleteJSONWithBody(ctx context.Context, path string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	err = wrapTransport(req, err)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return newHTTPError(http.MethodDelete, path, resp)
	}
	return nil
}

// PostJSON performs a POST request with a JSON body.
func (c *APIClient) PostJSON(ctx context.Context, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	err = wrapTransport(req, err)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return newHTTPError(http.MethodPost, path, resp)
	}
	if out == nil {
		return nil
	}
	return wrapBodyRead(req, json.NewDecoder(resp.Body).Decode(out))
}

// PutJSON performs a PUT request with a JSON body.
func (c *APIClient) PutJSON(ctx context.Context, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	err = wrapTransport(req, err)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return newHTTPError(http.MethodPut, path, resp)
	}
	if out == nil {
		return nil
	}
	return wrapBodyRead(req, json.NewDecoder(resp.Body).Decode(out))
}

// PatchJSON performs a PATCH request with a JSON body.
func (c *APIClient) PatchJSON(ctx context.Context, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	err = wrapTransport(req, err)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return newHTTPError(http.MethodPatch, path, resp)
	}
	if out == nil {
		return nil
	}
	return wrapBodyRead(req, json.NewDecoder(resp.Body).Decode(out))
}

// AttachmentResponse mirrors the server's upload-file response.
type AttachmentResponse struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	DownloadURL string `json:"download_url"`
	// MarkdownURL is the durable, persistable URL to embed in markdown bodies
	// (chat replies, comments). Unlike DownloadURL it never carries a TTL.
	MarkdownURL string `json:"markdown_url"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	CreatedAt   string `json:"created_at"`
}

// UploadFile uploads a file via multipart form to /api/upload-file.
// It returns the attachment ID from the server response.
func (c *APIClient) UploadFile(ctx context.Context, fileData []byte, filename string, issueID string) (string, error) {
	att, err := c.UploadIssueAttachment(ctx, fileData, filename, issueID)
	if err != nil {
		return "", err
	}
	return att.ID, nil
}

// UploadIssueAttachment uploads a file via multipart form to /api/upload-file
// and returns the full AttachmentResponse. An empty issueID leaves the row
// unbound, which is what `issue create` needs: the file is uploaded before the
// issue exists, its `markdown_url` goes into the description, and the create
// call binds it via `attachment_ids`.
func (c *APIClient) UploadIssueAttachment(ctx context.Context, fileData []byte, filename string, issueID string) (AttachmentResponse, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return AttachmentResponse{}, fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(fileData); err != nil {
		return AttachmentResponse{}, fmt.Errorf("write file data: %w", err)
	}

	if issueID != "" {
		if err := writer.WriteField("issue_id", issueID); err != nil {
			return AttachmentResponse{}, fmt.Errorf("write issue_id field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return AttachmentResponse{}, fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/upload-file", &body)
	if err != nil {
		return AttachmentResponse{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	c.setHeaders(req)

	// Honor a longer context deadline for large files: callers widen the
	// context for uploads, which the default client timeout would shadow.
	// Same shape as UploadChatAttachment / UploadFileWithURL.
	httpClient := c.HTTPClient
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > httpClient.Timeout {
			clientCopy := *httpClient
			clientCopy.Timeout = remaining
			httpClient = &clientCopy
		}
	}

	resp, err := httpClient.Do(req)
	err = wrapTransport(req, err)
	if err != nil {
		return AttachmentResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return AttachmentResponse{}, newHTTPError(http.MethodPost, "/api/upload-file", resp)
	}

	var result AttachmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return AttachmentResponse{}, fmt.Errorf("decode upload response: %w", wrapBodyRead(req, err))
	}
	if result.ID == "" {
		return AttachmentResponse{}, fmt.Errorf("upload response missing attachment id")
	}
	return result, nil
}

// UploadChatAttachment uploads a file via multipart form to /api/upload-file
// tagged with a chat task (task_id). The server binds the row to the assistant
// reply that task produces on completion. Returns the full AttachmentResponse
// (id + markdown_url) so the agent can embed the image inline in its reply.
func (c *APIClient) UploadChatAttachment(ctx context.Context, fileData []byte, filename, taskID string) (AttachmentResponse, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return AttachmentResponse{}, fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(fileData); err != nil {
		return AttachmentResponse{}, fmt.Errorf("write file data: %w", err)
	}
	if taskID != "" {
		if err := writer.WriteField("task_id", taskID); err != nil {
			return AttachmentResponse{}, fmt.Errorf("write task_id field: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return AttachmentResponse{}, fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/upload-file", &body)
	if err != nil {
		return AttachmentResponse{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	c.setHeaders(req)

	// Honor a longer context deadline for large images, same as UploadFileWithURL.
	httpClient := c.HTTPClient
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > httpClient.Timeout {
			clientCopy := *httpClient
			clientCopy.Timeout = remaining
			httpClient = &clientCopy
		}
	}

	resp, err := httpClient.Do(req)
	err = wrapTransport(req, err)
	if err != nil {
		return AttachmentResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return AttachmentResponse{}, newHTTPError(http.MethodPost, "/api/upload-file", resp)
	}

	var result AttachmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return AttachmentResponse{}, fmt.Errorf("decode upload response: %w", wrapBodyRead(req, err))
	}
	if result.ID == "" {
		return AttachmentResponse{}, fmt.Errorf("upload response missing attachment id")
	}
	return result, nil
}

// UploadFileWithURL uploads a file via multipart form to /api/upload-file
// without associating it with an issue or comment. It decodes the full
// AttachmentResponse and returns the attachment ID and URL.
func (c *APIClient) UploadFileWithURL(ctx context.Context, fileData []byte, filename string) (string, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return "", "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(fileData); err != nil {
		return "", "", fmt.Errorf("write file data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", "", fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/upload-file", &body)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	c.setHeaders(req)

	// Use a client that respects the context deadline for slow uploads
	// (e.g. avatar uploads with 5MB files). The default HTTP client timeout
	// shadows any longer context deadline.
	httpClient := c.HTTPClient
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > httpClient.Timeout {
			clientCopy := *httpClient
			clientCopy.Timeout = remaining
			httpClient = &clientCopy
		}
	}

	resp, err := httpClient.Do(req)
	err = wrapTransport(req, err)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", "", newHTTPError(http.MethodPost, "/api/upload-file", resp)
	}

	var result AttachmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("decode upload response: %w", wrapBodyRead(req, err))
	}
	if result.URL == "" {
		return "", "", fmt.Errorf("upload response missing attachment url")
	}
	// Allow empty ID: the server returns id="" in the fallback path where
	// S3 upload succeeded but the attachment DB record failed. The file
	// is still usable via its URL.
	return result.ID, result.URL, nil
}

// ImportSkillFile imports a skill from a local archive (.skill / .zip) by
// POSTing it as multipart/form-data to /api/skills/import, alongside the
// on_conflict strategy. The structured import result is decoded into out.
func (c *APIClient) ImportSkillFile(ctx context.Context, fileData []byte, filename, onConflict string, out any) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(fileData); err != nil {
		return fmt.Errorf("write file data: %w", err)
	}
	if onConflict != "" {
		if err := writer.WriteField("on_conflict", onConflict); err != nil {
			return fmt.Errorf("write on_conflict field: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/skills/import", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	c.setHeaders(req)

	// Respect a longer context deadline for slow uploads, mirroring
	// UploadFileWithURL: the default client timeout would otherwise shadow it.
	httpClient := c.HTTPClient
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > httpClient.Timeout {
			clientCopy := *httpClient
			clientCopy.Timeout = remaining
			httpClient = &clientCopy
		}
	}

	resp, err := httpClient.Do(req)
	err = wrapTransport(req, err)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return newHTTPError(http.MethodPost, "/api/skills/import", resp)
	}
	if out == nil {
		return nil
	}
	return wrapBodyRead(req, json.NewDecoder(resp.Body).Decode(out))
}

// UploadPrivatePlugin installs workspace-private Plugin archive bytes. The
// filename is transport metadata only; the Server derives source identity from
// validated manifest content and digests.
func (c *APIClient) UploadPrivatePlugin(ctx context.Context, path string, archive []byte, filename string, out any) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("artifact", filepath.Base(filename))
	if err != nil {
		return fmt.Errorf("create Plugin artifact form file: %w", err)
	}
	if _, err := part.Write(archive); err != nil {
		return fmt.Errorf("write Plugin artifact: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close Plugin artifact upload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	c.setHeaders(req)
	resp, err := c.HTTPClient.Do(req)
	err = wrapTransport(req, err)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return newHTTPError(http.MethodPost, path, resp)
	}
	if out == nil {
		return nil
	}
	return wrapBodyRead(req, json.NewDecoder(resp.Body).Decode(out))
}

// DownloadFile downloads a file from the given URL and returns the response body.
// This is used for downloading attachments via their signed download_url.
// Downloads are limited to 100 MB to match the upload size limit.
//
// The URL may be absolute (a signed CloudFront/S3 URL) or relative
// (a server-relative path like "/api/attachments/{id}/download" or
// "/uploads/...") depending on how the
// server is configured. Relative URLs are resolved against the client's
// BaseURL and sent with the standard auth headers; absolute URLs are
// used as-is so that their query-string signatures are not disturbed.
func (c *APIClient) DownloadFile(ctx context.Context, downloadURL string) ([]byte, error) {
	isRelative := !strings.HasPrefix(downloadURL, "http://") && !strings.HasPrefix(downloadURL, "https://")
	if isRelative {
		if c.BaseURL == "" {
			return nil, fmt.Errorf("download URL %q is relative but client has no BaseURL", downloadURL)
		}
		downloadURL = c.BaseURL + downloadURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	if isRelative {
		c.setHeaders(req)
	}

	resp, err := c.HTTPClient.Do(req)
	err = wrapTransport(req, err)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, newHTTPError(http.MethodGet, downloadURL, resp)
	}

	const maxDownloadSize = 100 << 20 // 100 MB
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadSize))
	return data, wrapBodyRead(req, err)
}

// Project represents a workspace project.
type Project struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Status      string  `json:"status"`
	Priority    string  `json:"priority"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	IssueCount  int64   `json:"issue_count"`
	DoneCount   int64   `json:"done_count"`
}

// Issue represents a task or problem tracked in Multica.
type Issue struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspace_id"`
	Number       int32   `json:"number"`
	Identifier   string  `json:"identifier"`
	Title        string  `json:"title"`
	Description  *string `json:"description"`
	Status       string  `json:"status"`
	StatusName   string  `json:"status_name"`
	Priority     string  `json:"priority"`
	AssigneeType *string `json:"assignee_type"`
	AssigneeID   *string `json:"assignee_id"`
	CreatorType  string  `json:"creator_type"`
	CreatorID    string  `json:"creator_id"`
	ProjectID    *string              `json:"project_id"`
	Attachments  []AttachmentResponse `json:"attachments,omitempty"`
	CreatedAt    string               `json:"created_at"`
	UpdatedAt    string               `json:"updated_at"`
}

// IssueComment represents a discussion comment on an issue.
type IssueComment struct {
	ID          string               `json:"id"`
	IssueID     string               `json:"issue_id"`
	ParentID    *string              `json:"parent_id,omitempty"`
	Content     string               `json:"content"`
	AuthorType  string               `json:"author_type"`
	AuthorID    string               `json:"author_id"`
	Attachments []AttachmentResponse `json:"attachments,omitempty"`
	CreatedAt   string               `json:"created_at"`
	UpdatedAt   string               `json:"updated_at"`
}

// CreateIssueRequest defines the payload for creating a new issue.
type CreateIssueRequest struct {
	Title         string   `json:"title"`
	Description   *string  `json:"description,omitempty"`
	Status        string   `json:"status,omitempty"`
	Priority      string   `json:"priority,omitempty"`
	AssigneeType  *string  `json:"assignee_type,omitempty"`
	AssigneeID    *string  `json:"assignee_id,omitempty"`
	ParentIssueID *string  `json:"parent_issue_id,omitempty"`
	ProjectID     *string  `json:"project_id,omitempty"`
	DueDate       *string  `json:"due_date,omitempty"`
	AttachmentIDs []string `json:"attachment_ids,omitempty"`
	LabelIDs      []string `json:"label_ids,omitempty"`
	OriginType    *string  `json:"origin_type,omitempty"`
	OriginID      *string  `json:"origin_id,omitempty"`
}

// UpdateIssueRequest defines the payload for updating an existing issue.
type UpdateIssueRequest struct {
	Title        *string `json:"title,omitempty"`
	Description  *string `json:"description,omitempty"`
	Status       *string `json:"status,omitempty"`
	Priority     *string `json:"priority,omitempty"`
	AssigneeType *string `json:"assignee_type,omitempty"`
	AssigneeID   *string `json:"assignee_id,omitempty"`
	ProjectID    *string `json:"project_id,omitempty"`
	DueDate      *string `json:"due_date,omitempty"`
}

// ListIssueOptions defines query filters for listing issues.
type ListIssueOptions struct {
	ProjectID string
	Status    string
	Limit     int
}

// ListProjects retrieves all projects in the current workspace.
func (c *APIClient) ListProjects(ctx context.Context) ([]Project, error) {
	var projects []Project
	if err := c.GetJSON(ctx, "/api/projects", &projects); err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return projects, nil
}

// GetProject retrieves a single project by ID.
func (c *APIClient) GetProject(ctx context.Context, id string) (*Project, error) {
	var project Project
	if err := c.GetJSON(ctx, "/api/projects/"+url.PathEscape(id), &project); err != nil {
		return nil, fmt.Errorf("get project %s: %w", id, err)
	}
	return &project, nil
}

// ListIssues retrieves issues based on the provided filters.
func (c *APIClient) ListIssues(ctx context.Context, opts ListIssueOptions) ([]Issue, error) {
	v := url.Values{}
	if opts.ProjectID != "" {
		v.Set("project_id", opts.ProjectID)
	}
	if opts.Status != "" {
		v.Set("status", opts.Status)
	}
	if opts.Limit > 0 {
		v.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}

	path := "/api/issues"
	if len(v) > 0 {
		path += "?" + v.Encode()
	}

	type listIssueResp struct {
		Issues []Issue `json:"issues"`
	}
	var resp listIssueResp
	if err := c.GetJSON(ctx, path, &resp); err != nil {
		var sliceResp []Issue
		if errSlice := c.GetJSON(ctx, path, &sliceResp); errSlice == nil {
			return sliceResp, nil
		}
		return nil, fmt.Errorf("list issues: %w", err)
	}
	return resp.Issues, nil
}

// GetIssue retrieves a single issue by ID or identifier (e.g. THEB-1).
func (c *APIClient) GetIssue(ctx context.Context, idOrIdentifier string) (*Issue, error) {
	var issue Issue
	if err := c.GetJSON(ctx, "/api/issues/"+url.PathEscape(idOrIdentifier), &issue); err != nil {
		return nil, fmt.Errorf("get issue %s: %w", idOrIdentifier, err)
	}
	return &issue, nil
}

// CreateIssue creates a new issue in the workspace.
func (c *APIClient) CreateIssue(ctx context.Context, req CreateIssueRequest) (*Issue, error) {
	if req.Status == "" {
		req.Status = "todo"
	}
	var created Issue
	if err := c.PostJSON(ctx, "/api/issues", req, &created); err != nil {
		return nil, fmt.Errorf("create issue: %w", err)
	}
	return &created, nil
}

// UpdateIssue updates an existing issue.
func (c *APIClient) UpdateIssue(ctx context.Context, id string, req UpdateIssueRequest) (*Issue, error) {
	var updated Issue
	if err := c.PatchJSON(ctx, "/api/issues/"+url.PathEscape(id), req, &updated); err != nil {
		return nil, fmt.Errorf("update issue %s: %w", id, err)
	}
	return &updated, nil
}

// ListIssueComments retrieves all comments for an issue.
func (c *APIClient) ListIssueComments(ctx context.Context, issueID string) ([]IssueComment, error) {
	var comments []IssueComment
	if err := c.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/comments", &comments); err != nil {
		return nil, fmt.Errorf("list issue comments: %w", err)
	}
	return comments, nil
}

// AddIssueComment posts a new comment to an issue.
func (c *APIClient) AddIssueComment(ctx context.Context, issueID string, content string) (*IssueComment, error) {
	body := map[string]string{
		"content": content,
	}
	var comment IssueComment
	if err := c.PostJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/comments", body, &comment); err != nil {
		return nil, fmt.Errorf("add issue comment: %w", err)
	}
	return &comment, nil
}

// SetIssueStatus changes the status of an issue (equivalent to `multica issue status`).
func (c *APIClient) SetIssueStatus(ctx context.Context, idOrIdentifier, status string) error {
	body := map[string]any{"status": status}
	var result map[string]any
	if err := c.PutJSON(ctx, "/api/issues/"+url.PathEscape(idOrIdentifier), body, &result); err != nil {
		return fmt.Errorf("set issue status: %w", err)
	}
	return nil
}

// AssignIssue assigns or unassigns an issue (equivalent to `multica issue assign`).
func (c *APIClient) AssignIssue(ctx context.Context, idOrIdentifier string, assigneeType, assigneeID *string) error {
	body := map[string]any{
		"assignee_type": assigneeType,
		"assignee_id":   assigneeID,
	}
	var result map[string]any
	if err := c.PutJSON(ctx, "/api/issues/"+url.PathEscape(idOrIdentifier), body, &result); err != nil {
		return fmt.Errorf("assign issue: %w", err)
	}
	return nil
}

// DeleteIssue deletes an issue (equivalent to `multica issue delete`).
func (c *APIClient) DeleteIssue(ctx context.Context, idOrIdentifier string) error {
	if err := c.DeleteJSON(ctx, "/api/issues/"+url.PathEscape(idOrIdentifier)); err != nil {
		return fmt.Errorf("delete issue: %w", err)
	}
	return nil
}

// ListIssueChildren lists sub-issues of a parent issue.
func (c *APIClient) ListIssueChildren(ctx context.Context, idOrIdentifier string) ([]Issue, error) {
	var resp struct {
		Issues []Issue `json:"issues"`
	}
	if err := c.GetJSON(ctx, "/api/issues/"+url.PathEscape(idOrIdentifier)+"/children", &resp); err != nil {
		return nil, fmt.Errorf("list issue children: %w", err)
	}
	return resp.Issues, nil
}

// SearchIssues searches issues by query string (equivalent to `multica issue search`).
func (c *APIClient) SearchIssues(ctx context.Context, query string) ([]Issue, error) {
	v := url.Values{}
	v.Set("q", query)
	var resp struct {
		Issues []Issue `json:"issues"`
	}
	if err := c.GetJSON(ctx, "/api/issues/search?"+v.Encode(), &resp); err != nil {
		return nil, fmt.Errorf("search issues: %w", err)
	}
	return resp.Issues, nil
}

// UpdateIssueComment edits an existing comment on an issue.
func (c *APIClient) UpdateIssueComment(ctx context.Context, issueID, commentID, content string) error {
	body := map[string]string{"content": content}
	return c.PatchJSON(ctx, fmt.Sprintf("/api/issues/%s/comments/%s", url.PathEscape(issueID), url.PathEscape(commentID)), body, nil)
}

// DeleteIssueComment deletes a comment on an issue.
func (c *APIClient) DeleteIssueComment(ctx context.Context, issueID, commentID string) error {
	return c.DeleteJSON(ctx, fmt.Sprintf("/api/issues/%s/comments/%s", url.PathEscape(issueID), url.PathEscape(commentID)))
}

// ResolveIssueComment marks a comment thread resolved or unresolved.
func (c *APIClient) ResolveIssueComment(ctx context.Context, issueID, commentID string, resolve bool) error {
	action := "resolve"
	if !resolve {
		action = "unresolve"
	}
	return c.PostJSON(ctx, fmt.Sprintf("/api/issues/%s/comments/%s/%s", url.PathEscape(issueID), url.PathEscape(commentID), action), nil, nil)
}

// ArchiveProject archives a project (equivalent to `multica project archive`).
func (c *APIClient) ArchiveProject(ctx context.Context, id string) error {
	var result map[string]any
	return c.PostJSON(ctx, "/api/projects/"+url.PathEscape(id)+"/archive", map[string]any{}, &result)
}

// RestoreProject restores an archived project (equivalent to `multica project restore`).
func (c *APIClient) RestoreProject(ctx context.Context, id string) error {
	var result map[string]any
	return c.PostJSON(ctx, "/api/projects/"+url.PathEscape(id)+"/restore", map[string]any{}, &result)
}

// DeleteProject deletes a project (equivalent to `multica project delete`).
func (c *APIClient) DeleteProject(ctx context.Context, id string) error {
	return c.DeleteJSON(ctx, "/api/projects/"+url.PathEscape(id))
}

// ProjectMember represents a member assigned to a project.
type ProjectMember struct {
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
	Name      string `json:"name,omitempty"`
	Email     string `json:"email,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// ListProjectMembers lists members in a project.
func (c *APIClient) ListProjectMembers(ctx context.Context, id string) ([]ProjectMember, error) {
	var members []ProjectMember
	if err := c.GetJSON(ctx, "/api/projects/"+url.PathEscape(id)+"/members", &members); err != nil {
		return nil, fmt.Errorf("list project members: %w", err)
	}
	return members, nil
}

// AddProjectMembers adds users to a project.
func (c *APIClient) AddProjectMembers(ctx context.Context, id string, userIDs []string) error {
	body := map[string]any{"user_ids": userIDs}
	return c.PostJSON(ctx, "/api/projects/"+url.PathEscape(id)+"/members", body, nil)
}

// RemoveProjectMembers removes users from a project.
func (c *APIClient) RemoveProjectMembers(ctx context.Context, id string, userIDs []string) error {
	for _, uid := range userIDs {
		if err := c.DeleteJSON(ctx, fmt.Sprintf("/api/projects/%s/members/%s", url.PathEscape(id), url.PathEscape(uid))); err != nil {
			return err
		}
	}
	return nil
}

// WorkspaceAgent represents an AI agent in the workspace.
type WorkspaceAgent struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Instructions string   `json:"instructions,omitempty"`
	Model        string   `json:"model,omitempty"`
	SystemKey    string   `json:"system_key,omitempty"`
	Skills       []string `json:"skills,omitempty"`
}

// ListAgents lists agents configured in the current workspace (equivalent to `multica agent list`).
func (c *APIClient) ListAgents(ctx context.Context) ([]WorkspaceAgent, error) {
	var agents []WorkspaceAgent
	if err := c.GetJSON(ctx, "/api/agents", &agents); err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	return agents, nil
}

// WorkspaceMember represents a human user or bot member in a workspace.
type WorkspaceMember struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// ListWorkspaceMembers lists members in the current workspace (equivalent to `multica workspace member list`).
func (c *APIClient) ListWorkspaceMembers(ctx context.Context) ([]WorkspaceMember, error) {
	var members []WorkspaceMember
	if err := c.GetJSON(ctx, "/api/workspaces/members", &members); err != nil {
		return nil, fmt.Errorf("list workspace members: %w", err)
	}
	return members, nil
}
