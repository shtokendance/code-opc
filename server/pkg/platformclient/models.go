package platformclient

import "encoding/json"

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
	Risk        string          `json:"risk,omitempty"`
}

type EnterpriseMcpService struct {
	ServiceID string           `json:"service_id"`
	Tools     []ToolDefinition `json:"tools"`
}

type ListServicesResponse struct {
	Services []EnterpriseMcpService `json:"services"`
}
