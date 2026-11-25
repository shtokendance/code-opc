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

// ResourceSkillItem mirrors platform-opc's normalized skill_catalog resource item.
type ResourceSkillItem struct {
	ResourceKind      string `json:"resource_kind"`
	TenantID          string `json:"tenant_id"`
	TenantName        string `json:"tenant_name"`
	SourceTenantID    string `json:"source_tenant_id,omitempty"`
	SourceTenantName  string `json:"source_tenant_name,omitempty"`
	Permission        string `json:"permission,omitempty"`
	CatalogID         string `json:"catalog_id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	Version           string `json:"version,omitempty"`
	BundleSHA256      string `json:"bundle_sha256,omitempty"`
	Availability      string `json:"availability"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
	DetailURL         string `json:"detail_url,omitempty"`
}

type ResourceTenantFailure struct {
	TenantID string `json:"tenant_id"`
	Code     string `json:"code"`
}

type ResourceSkillListResponse struct {
	Items    []ResourceSkillItem     `json:"items"`
	Total    int                     `json:"total"`
	Offset   int                     `json:"offset"`
	Limit    int                     `json:"limit"`
	Complete bool                    `json:"complete"`
	Failures []ResourceTenantFailure `json:"failures"`
}

// ResourceKnowledgeBaseItem mirrors platform-opc's normalized knowledge_base resource item.
type ResourceKnowledgeBaseItem struct {
	ResourceKind      string `json:"resource_kind"`
	TenantID          string `json:"tenant_id"`
	TenantName        string `json:"tenant_name"`
	SourceTenantID    string `json:"source_tenant_id,omitempty"`
	SourceTenantName  string `json:"source_tenant_name,omitempty"`
	Permission        string `json:"permission,omitempty"`
	KnowledgeBaseID   string `json:"knowledge_base_id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	Availability      string `json:"availability"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
	DetailURL         string `json:"detail_url,omitempty"`
}

type ResourceKnowledgeBaseListResponse struct {
	Items    []ResourceKnowledgeBaseItem `json:"items"`
	Total    int                         `json:"total"`
	Offset   int                         `json:"offset"`
	Limit    int                         `json:"limit"`
	Complete bool                        `json:"complete"`
	Failures []ResourceTenantFailure     `json:"failures"`
}
