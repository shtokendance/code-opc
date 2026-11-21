package platformclient

import (
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
	fullURL := endpoint + "?" + q.Encode()

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
		return nil, fmt.Errorf("list services failed with status %d: %s", resp.StatusCode, string(body))
	}

	var res ListServicesResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("decode list services response: %w", err)
	}
	return res.Services, nil
}
