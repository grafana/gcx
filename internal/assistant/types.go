// Package assistant provides a Go client for interacting with Grafana Assistant via the A2A protocol.
package assistant

import (
	"encoding/json"
	"strings"
)

// ============================================================================
// JSON-RPC 2.0 Types (A2A Protocol)
// ============================================================================

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

// ============================================================================
// A2A Message Types
// ============================================================================

// A2AMessage represents a message in A2A format.
type A2AMessage struct {
	Kind      string         `json:"kind"`
	Role      string         `json:"role"`
	Parts     []A2APart      `json:"parts"`
	MessageID string         `json:"messageId"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// A2APart represents a content part in a message or artifact.
type A2APart struct {
	Kind     string          `json:"kind"`
	Text     string          `json:"text,omitempty"`
	File     *A2AFileContent `json:"file,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// A2AFileContent represents file content in a part.
type A2AFileContent struct {
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Bytes    string `json:"bytes,omitempty"`
	URI      string `json:"uri,omitempty"`
}

// MessageSendParams represents parameters for the message/send or message/stream method.
type MessageSendParams struct {
	Message   A2AMessage `json:"message"`
	ContextID string     `json:"contextId,omitempty"`
}

// ============================================================================
// A2A Event Types (SSE Streaming)
// ============================================================================

// A2AStatusUpdate represents a status-update event from SSE.
type A2AStatusUpdate struct {
	Kind      string    `json:"kind"`
	TaskID    string    `json:"taskId"`
	ContextID string    `json:"contextId"`
	Status    A2AStatus `json:"status"`
	Final     bool      `json:"final,omitempty"`
}

// A2AStatus represents task status.
type A2AStatus struct {
	State   string      `json:"state"`
	Message *A2AMessage `json:"message,omitempty"`
}

// A2AArtifactUpdate represents an artifact-update event from SSE.
type A2AArtifactUpdate struct {
	Kind      string      `json:"kind"`
	TaskID    string      `json:"taskId"`
	ContextID string      `json:"contextId"`
	Artifact  A2AArtifact `json:"artifact"`
	Append    bool        `json:"append,omitempty"`
	LastChunk bool        `json:"lastChunk,omitempty"`
}

// A2AArtifact represents an artifact in the A2A protocol.
type A2AArtifact struct {
	Kind        string          `json:"kind"`
	ArtifactID  string          `json:"artifactId"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Parts       []A2APart       `json:"parts"`
	Index       int             `json:"index,omitempty"`
	LastChunk   bool            `json:"lastChunk,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}

// A2ATask represents a task returned from the A2A API.
type A2ATask struct {
	Kind      string           `json:"kind"`
	ID        string           `json:"id"`
	ContextID string           `json:"contextId"`
	Status    A2AStatus        `json:"status"`
	Artifacts []A2AArtifact    `json:"artifacts,omitempty"`
	History   []A2AMessage     `json:"history,omitempty"`
	Metadata  *A2ATaskMetadata `json:"metadata,omitempty"`
}

// A2ATaskMetadata contains metadata for a task, including error information.
type A2ATaskMetadata struct {
	Error string `json:"error,omitempty"`
}

// ============================================================================
// Chat API Types (REST API)
// ============================================================================

// ChatMessage represents a message from the Chat REST API.
type ChatMessage struct {
	ID        string      `json:"id"`
	Role      string      `json:"role"`
	Content   ContentJSON `json:"content"`
	CreatedAt string      `json:"created"`
	Type      string      `json:"type,omitempty"`
	Hidden    bool        `json:"hidden,omitempty"`
}

// ContentJSON is an array of content blocks from the API.
type ContentJSON []ContentBlock

// ContentBlock represents a single content block in a message.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ExtractText extracts all text content from a ChatMessage.
// It strips out <context>...</context> tags that are injected by the system.
func (m *ChatMessage) ExtractText() string {
	var result string
	for _, block := range m.Content {
		if block.Type == "text" && block.Text != "" {
			if result != "" {
				result += "\n"
			}
			result += block.Text
		}
	}
	return stripContextTags(result)
}

// stripContextTags removes <context>...</context> tags from text.
func stripContextTags(text string) string {
	for {
		start := strings.Index(text, "<context>")
		if start == -1 {
			break
		}
		end := strings.Index(text, "</context>")
		if end == -1 {
			break
		}
		text = strings.TrimSpace(text[:start] + text[end+len("</context>"):])
	}
	return text
}

// ============================================================================
// Client Types
// ============================================================================

// StreamResult represents the result of a streaming chat.
type StreamResult struct {
	TaskID            string
	ContextID         string
	Completed         bool
	TimedOut          bool
	Failed            bool
	Canceled          bool
	ErrorMessage      string
	Response          string
	ErrorEventEmitted bool
}

// TokenRefresher is called before each API request to ensure the token is fresh.
type TokenRefresher func() (string, error)

// ClientOptions represents options for creating a Client.
type ClientOptions struct {
	GrafanaURL     string
	Token          string
	APIEndpoint    string
	TokenRefresher TokenRefresher
}

// StreamOptions represents options for streaming.
type StreamOptions struct {
	Timeout   int
	ContextID string
	OnEvent   func(StreamEvent)
}

// StreamEvent represents a single event emitted during streaming.
type StreamEvent struct {
	Type      string `json:"type"`
	TaskID    string `json:"taskId,omitempty"`
	ContextID string `json:"contextId,omitempty"`
	State     string `json:"state,omitempty"`
	Final     bool   `json:"final,omitempty"`
	ToolName  string `json:"toolName,omitempty"`
	Text      string `json:"text,omitempty"`
	Error     string `json:"error,omitempty"`
	Timeout   int    `json:"timeout,omitempty"`
}

// Logger interface for events.
type Logger interface {
	Info(message string)
	Debug(message string)
	Warning(message string)
}

// NopLogger is a logger that does nothing.
type NopLogger struct{}

func (NopLogger) Info(string)    {}
func (NopLogger) Debug(string)   {}
func (NopLogger) Warning(string) {}

// ============================================================================
// Chat API Types
// ============================================================================

// Chat represents a chat conversation.
type Chat struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category,omitempty"`
	Source   string `json:"source"`
}

// ============================================================================
// Approval Types
// ============================================================================

// ApprovalRequest represents an approval request from the backend.
type ApprovalRequest struct {
	ID          string          `json:"id"`
	ChatID      string          `json:"chatId"`
	TenantID    string          `json:"tenantId"`
	UserID      string          `json:"userId"`
	ToolName    string          `json:"toolName"`
	ToolInput   json.RawMessage `json:"toolInput,omitempty"`
	Description string          `json:"description,omitempty"`
}

// ApprovalResponse represents the user's response to an approval request.
type ApprovalResponse struct {
	ID       string `json:"id"`
	ChatID   string `json:"chatId"`
	TenantID string `json:"tenantId"`
	UserID   string `json:"userId"`
	Approved bool   `json:"approved"`
}

// ============================================================================
// Helper Functions
// ============================================================================

// ExtractTextFromParts extracts all text content from A2A parts.
func ExtractTextFromParts(parts []A2APart) string {
	var result string
	for _, part := range parts {
		if part.Kind == "text" && part.Text != "" {
			if result != "" {
				result += "\n"
			}
			result += part.Text
		}
	}
	return result
}
