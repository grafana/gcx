package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// A2AEndpoints holds the A2A API endpoints for a Grafana instance.
type A2AEndpoints struct {
	baseURL string
}

// GetA2AEndpoints returns the A2A API endpoints for the given base URL.
func GetA2AEndpoints(baseURL string) *A2AEndpoints {
	baseURL = strings.TrimSuffix(baseURL, "/")
	return &A2AEndpoints{
		baseURL: baseURL + "/a2a",
	}
}

// AgentEndpoint returns the endpoint for a specific agent.
func (e *A2AEndpoints) AgentEndpoint(agentID string) string {
	return e.baseURL + "/agents/" + agentID
}

// Approval returns the endpoint for submitting an approval response.
func (e *A2AEndpoints) Approval(approvalID string) string {
	return e.baseURL + "/approval/" + approvalID
}

// ChatEndpoints holds the Chat API endpoints for a Grafana instance.
type ChatEndpoints struct {
	baseURL string
}

// GetChatEndpoints returns the Chat API endpoints for the given base URL.
func GetChatEndpoints(baseURL string) *ChatEndpoints {
	baseURL = strings.TrimSuffix(baseURL, "/")
	return &ChatEndpoints{
		baseURL: baseURL,
	}
}

// Chats returns the base endpoint for chats.
func (e *ChatEndpoints) Chats() string {
	return e.baseURL + "/chats"
}

// Chat returns the endpoint for a specific chat.
func (e *ChatEndpoints) Chat(chatID string) string {
	return e.baseURL + "/chats/" + chatID
}

// FetchChat fetches a single chat by ID from the Chat API.
func FetchChat(ctx context.Context, baseURL, token, chatID string) (*Chat, error) {
	endpoints := GetChatEndpoints(baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoints.Chat(chatID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-App-Source", "cli")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("chat not found: %s", chatID)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch chat: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var response struct {
		Data Chat `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response.Data, nil
}

// FetchChatMessages fetches messages for a chat from the REST API.
func FetchChatMessages(ctx context.Context, baseURL, token, chatID string) ([]ChatMessage, error) {
	endpoints := GetChatEndpoints(baseURL)
	url := fmt.Sprintf("%s/%s/all-messages", endpoints.Chats(), chatID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-App-Source", "cli")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch messages: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var response struct {
		Data struct {
			Messages []ChatMessage `json:"messages"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return response.Data.Messages, nil
}

// CreateMessageStreamRequest creates a JSON-RPC request for message/stream.
func CreateMessageStreamRequest(prompt, contextID string) ([]byte, error) {
	params := MessageSendParams{
		Message: A2AMessage{
			Kind:      "message",
			Role:      "user",
			MessageID: uuid.New().String(),
			Parts: []A2APart{
				{
					Kind: "text",
					Text: prompt,
				},
			},
		},
		ContextID: contextID,
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal params: %w", err)
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      uuid.New().String(),
		Method:  "message/stream",
		Params:  paramsJSON,
	}

	return json.Marshal(req)
}

// SubmitApproval sends an approval response to the dedicated approval endpoint.
func SubmitApproval(ctx context.Context, baseURL, token, approvalID, chatID, tenantID, userID string, approved bool) error {
	endpoints := GetA2AEndpoints(baseURL)
	url := endpoints.Approval(approvalID)

	payload := ApprovalResponse{
		ID:       approvalID,
		ChatID:   chatID,
		TenantID: tenantID,
		UserID:   userID,
		Approved: approved,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal approval response: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-App-Source", "cli")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send approval: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to submit approval: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
