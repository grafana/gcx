package integrations

import (
	"encoding/json"
	"time"
)

// Integration represents a Grafana Assistant integration (e.g. an MCP server).
type Integration struct {
	ID                   string          `json:"id"`
	Name                 string          `json:"name"`
	Type                 string          `json:"type"`
	Scope                string          `json:"scope"`
	Enabled              *bool           `json:"enabled"`
	Description          string          `json:"description,omitempty"`
	Applications         []string        `json:"applications,omitempty"`
	Configuration        json.RawMessage `json:"configuration,omitempty"`
	CustomHeaders        []Header        `json:"custom_headers,omitempty"`
	AuthenticationFailed *bool           `json:"authenticationFailed,omitempty"`
	DynamicClientID      string          `json:"dynamic_client_id,omitempty"`
	UserID               string          `json:"userId,omitempty"`
	CreatedAt            time.Time       `json:"created"`
	ModifiedAt           time.Time       `json:"modified"`
	CreatedBy            string          `json:"createdBy"`
	UpdatedBy            string          `json:"updatedBy"`
}

// Header is a custom HTTP header attached to an integration.
type Header struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Modified bool   `json:"modified,omitempty"`
}

// Pagination holds paging metadata from list responses.
type Pagination struct {
	Total  int64 `json:"total"`
	Limit  int64 `json:"limit"`
	Offset int64 `json:"offset"`
}

// CreateRequest is the body for POST /integrations.
type CreateRequest struct {
	Name          string          `json:"name"`
	Type          string          `json:"type"`
	Scope         string          `json:"scope"`
	Enabled       *bool           `json:"enabled"`
	Applications  []string        `json:"applications"`
	Description   string          `json:"description,omitempty"`
	Configuration json.RawMessage `json:"configuration,omitempty"`
	CustomHeaders []Header        `json:"custom_headers,omitempty"`
}

// UpdateRequest is the body for PUT /integrations/{id}.
type UpdateRequest struct {
	Scope         string          `json:"scope"`
	Name          string          `json:"name,omitempty"`
	Description   *string         `json:"description,omitempty"`
	Enabled       *bool           `json:"enabled,omitempty"`
	Applications  []string        `json:"applications,omitempty"`
	Configuration json.RawMessage `json:"configuration,omitempty"`
	CustomHeaders []Header        `json:"custom_headers,omitempty"`
}

// ValidationResult is from GET /integrations/{id}/validate.
type ValidationResult struct {
	Status  string    `json:"status"`
	Message string    `json:"message,omitempty"`
	Error   string    `json:"error,omitempty"`
	Tools   []MCPTool `json:"tools,omitempty"`
}

// MCPTool describes an MCP tool discovered during validation.
type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Annotations *ToolAnnotation `json:"annotations,omitempty"`
}

// ToolAnnotation holds MCP tool annotation hints.
type ToolAnnotation struct {
	ReadOnlyHint    bool   `json:"readOnlyHint,omitempty"`
	DestructiveHint bool   `json:"destructiveHint,omitempty"`
	IdempotentHint  bool   `json:"idempotentHint,omitempty"`
	OpenWorldHint   bool   `json:"openWorldHint,omitempty"`
	Title           string `json:"title,omitempty"`
}
