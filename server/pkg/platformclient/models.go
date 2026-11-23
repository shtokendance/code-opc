package platformclient

import "encoding/json"

const (
	DefaultPlatformGatewayURL   = "http://10.101.102.116:3002"
	DefaultPlatformServiceToken = "3a70ecbf6b0788041c3e6ab271053f737beff4ca94211bef9d5022b77b10786d"
)

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
	Risk        string          `json:"risk,omitempty"`
}

type EnterpriseMcpService struct {
	ServiceID   string           `json:"service_id"`
	Name        string           `json:"name,omitempty"`
	Description string           `json:"description,omitempty"`
	Tools       []ToolDefinition `json:"tools"`
}

type ListServicesResponse struct {
	Services []EnterpriseMcpService `json:"services"`
	Data     struct {
		Services []EnterpriseMcpService `json:"services"`
	} `json:"data,omitempty"`
}

type TaskInvocationRequest struct {
	Subject        string          `json:"subject"`
	TenantID       string          `json:"tenant_id"`
	ServiceID      string          `json:"service_id"`
	ToolName       string          `json:"tool_name"`
	Arguments      json.RawMessage `json:"arguments"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	TaskID         string          `json:"task_id"`
	Device         string          `json:"device"`
}

type TaskInvocationResponse struct {
	Success bool `json:"success"`
	Data    struct {
		InvocationID string `json:"invocation_id"`
		Status       string `json:"status"`
	} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type InvocationStatusResponse struct {
	Success bool `json:"success"`
	Data    struct {
		InvocationID string          `json:"invocation_id"`
		Status       string          `json:"status"`
		ResultRef    json.RawMessage `json:"result_ref,omitempty"`
		Error        string          `json:"error,omitempty"`
	} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}
