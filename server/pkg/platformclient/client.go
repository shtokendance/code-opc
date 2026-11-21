package platformclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
