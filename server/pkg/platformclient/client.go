package platformclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL      string
	serviceToken string
	httpClient   *http.Client
}

func NewClient(baseURL, serviceToken string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	return &Client{
		baseURL:      strings.TrimRight(baseURL, "/"),
		serviceToken: strings.TrimSpace(serviceToken),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) BaseURL() string {
	return c.baseURL
}

func (c *Client) ServiceToken() string {
	return c.serviceToken
}

func (c *Client) ListServices(ctx context.Context, tenant string, includeSchema bool) ([]EnterpriseMcpService, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("platform client base URL is empty")
	}
	endpoint := fmt.Sprintf("%s/integration/v1/mcp/services", c.baseURL)
	q := url.Values{}
	if tenant != "" {
		q.Set("tenant", tenant)
	}
	if includeSchema {
		q.Set("include_schema", "true")
	}
	fullURL := endpoint
	if qStr := q.Encode(); qStr != "" {
		fullURL += "?" + qStr
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create list services request: %w", err)
	}
	if c.serviceToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.serviceToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute list services: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("list services failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var res ListServicesResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("decode list services response: %w", err)
	}
	if len(res.Services) == 0 && len(res.Data.Services) > 0 {
		return res.Data.Services, nil
	}
	return res.Services, nil
}

func (c *Client) InvokeTask(ctx context.Context, req TaskInvocationRequest) (*TaskInvocationResponse, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("platform client base URL is empty")
	}
	endpoint := fmt.Sprintf("%s/integration/v1/mcp/invoke-task", c.baseURL)
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal invoke task request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create invoke task request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.serviceToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.serviceToken)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute invoke task: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read invoke task response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusCreated {
		var errResp TaskInvocationResponse
		if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil && errResp.Error != nil {
			return &errResp, fmt.Errorf("invoke task failed with status %d: [%s] %s", resp.StatusCode, errResp.Error.Code, errResp.Error.Message)
		}
		return nil, fmt.Errorf("invoke task failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var res TaskInvocationResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("decode invoke task response: %w", err)
	}
	return &res, nil
}

func (c *Client) GetInvocation(ctx context.Context, invocationID, subject, tenantID, taskID, device string) (*InvocationStatusResponse, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("platform client base URL is empty")
	}
	endpoint := fmt.Sprintf("%s/integration/v1/mcp/invocations/%s", c.baseURL, url.PathEscape(invocationID))
	q := url.Values{}
	if subject != "" {
		q.Set("subject", subject)
	}
	if tenantID != "" {
		q.Set("tenant", tenantID)
	}
	if taskID != "" {
		q.Set("task_id", taskID)
	}
	if device != "" {
		q.Set("device", device)
	}
	fullURL := endpoint
	if qStr := q.Encode(); qStr != "" {
		fullURL += "?" + qStr
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create get invocation request: %w", err)
	}
	if c.serviceToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.serviceToken)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute get invocation: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read get invocation response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp InvocationStatusResponse
		if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil && errResp.Error != nil {
			return &errResp, fmt.Errorf("get invocation failed with status %d: [%s] %s", resp.StatusCode, errResp.Error.Code, errResp.Error.Message)
		}
		return nil, fmt.Errorf("get invocation failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var res InvocationStatusResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("decode get invocation response: %w", err)
	}
	return &res, nil
}

// APIError represents an HTTP error returned by platform-opc.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code != "" && e.Message != "" {
		return fmt.Sprintf("platform api error %d [%s]: %s", e.Status, e.Code, e.Message)
	}
	if e.Message != "" {
		return fmt.Sprintf("platform api error %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("platform api error %d", e.Status)
}

func parseAPIError(status int, body []byte) error {
	var envelope struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Error   *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		if envelope.Error != nil && (envelope.Error.Code != "" || envelope.Error.Message != "") {
			return &APIError{Status: status, Code: envelope.Error.Code, Message: envelope.Error.Message}
		}
		if envelope.Code != "" || envelope.Message != "" {
			return &APIError{Status: status, Code: envelope.Code, Message: envelope.Message}
		}
	}
	return &APIError{Status: status, Message: strings.TrimSpace(string(body))}
}

// ListResourceSkills queries /integration/v1/resources/skills with the user's bearer token
// to fetch all accessible skill catalogs across entitled spaces.
func (c *Client) ListResourceSkills(ctx context.Context, userToken, tenantID, q string, offset, limit int) (*ResourceSkillListResponse, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("platform client base URL is empty")
	}
	userToken = strings.TrimSpace(userToken)
	if userToken == "" {
		return nil, fmt.Errorf("user token is required")
	}

	endpoint := fmt.Sprintf("%s/integration/v1/resources/skills", c.baseURL)
	params := url.Values{}
	if tenantID != "" {
		params.Set("tenant_id", tenantID)
	}
	if q != "" {
		params.Set("q", q)
	}
	if offset > 0 {
		params.Set("offset", strconv.Itoa(offset))
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	} else {
		params.Set("limit", "100")
	}
	fullURL := endpoint + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create list resource skills request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+userToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute list resource skills: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read list resource skills response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseAPIError(resp.StatusCode, body)
	}

	var res ResourceSkillListResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("decode list resource skills response: %w", err)
	}
	if res.Items == nil {
		res.Items = []ResourceSkillItem{}
	}
	if res.Failures == nil {
		res.Failures = []ResourceTenantFailure{}
	}
	return &res, nil
}

// DownloadResourceSkillArchive downloads a skill catalog zip archive from platform-opc
// with the user's bearer token and SHA-256 verification.
func (c *Client) DownloadResourceSkillArchive(ctx context.Context, userToken, catalogID, tenantID, expectedSHA256 string, maxBytes int64) ([]byte, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("platform client base URL is empty")
	}
	userToken = strings.TrimSpace(userToken)
	if userToken == "" {
		return nil, fmt.Errorf("user token is required")
	}
	if maxBytes <= 0 {
		maxBytes = 16 << 20
	}

	endpoint := fmt.Sprintf("%s/integration/v1/resources/skills/%s/archive", c.baseURL, url.PathEscape(catalogID))
	params := url.Values{}
	params.Set("tenant_id", tenantID)
	params.Set("expected_sha256", expectedSHA256)
	fullURL := endpoint + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create download skill archive request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+userToken)

	dlClient := &http.Client{Timeout: 120 * time.Second}
	if c.httpClient != nil && c.httpClient.Transport != nil {
		dlClient.Transport = c.httpClient.Transport
	}
	resp, err := dlClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute download skill archive: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, parseAPIError(resp.StatusCode, errBody)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read skill archive body: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("skill archive exceeds max size of %d bytes", maxBytes)
	}
	return data, nil
}

// ListKnowledgeBases queries /integration/v1/resources/knowledge-bases with the user's bearer token.
func (c *Client) ListKnowledgeBases(ctx context.Context, userToken, tenantID, q string, offset, limit int) (*ResourceKnowledgeBaseListResponse, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("platform client base URL is empty")
	}
	userToken = strings.TrimSpace(userToken)
	if userToken == "" {
		return nil, fmt.Errorf("user token is required")
	}

	endpoint := fmt.Sprintf("%s/integration/v1/resources/knowledge-bases", c.baseURL)
	params := url.Values{}
	if tenantID != "" {
		params.Set("tenant_id", tenantID)
	}
	if q != "" {
		params.Set("q", q)
	}
	if offset > 0 {
		params.Set("offset", strconv.Itoa(offset))
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	} else {
		params.Set("limit", "100")
	}
	fullURL := endpoint + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create list knowledge bases request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+userToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute list knowledge bases: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read list knowledge bases response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseAPIError(resp.StatusCode, body)
	}

	var res ResourceKnowledgeBaseListResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("decode list knowledge bases response: %w", err)
	}
	if res.Items == nil {
		res.Items = []ResourceKnowledgeBaseItem{}
	}
	if res.Failures == nil {
		res.Failures = []ResourceTenantFailure{}
	}
	return &res, nil
}

// SearchTaskKnowledge queries /integration/v1/knowledge/search-task using the
// service token and the task's user subject.
func (c *Client) SearchTaskKnowledge(ctx context.Context, req TaskKnowledgeSearchRequest) (*TaskKnowledgeSearchResponse, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("platform client base URL is empty")
	}
	endpoint := fmt.Sprintf("%s/integration/v1/knowledge/search-task", c.baseURL)
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal task knowledge search request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create task knowledge search request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.serviceToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.serviceToken)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute task knowledge search: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read task knowledge search response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseAPIError(resp.StatusCode, body)
	}

	var res TaskKnowledgeSearchResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("decode task knowledge search response: %w", err)
	}
	if res.Results == nil {
		res.Results = []KnowledgeSearchHit{}
	}
	return &res, nil
}
