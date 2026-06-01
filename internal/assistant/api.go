package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
func FetchChat(ctx context.Context, baseURL, token, chatID string, httpClient *http.Client) (*Chat, error) {
	endpoints := GetChatEndpoints(baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoints.Chat(chatID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-App-Source", "cli")

	resp, err := httpClient.Do(req)
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

// ListChatsOptions controls filtering and pagination for FetchChats.
type ListChatsOptions struct {
	// Source filters chats by origin. Supports a single value ("assistant"),
	// comma-separated values ("assistant,cli"), or "all" for every source.
	// When empty the backend defaults to "assistant".
	//
	// Filtering happens server-side, so --limit/--offset are honest for the
	// default and explicit comma-separated sources. For "all", ephemeral
	// inline-assistant chats are stripped client-side after the server
	// applies limit/offset, so a page may return fewer rows than --limit;
	// pagination is best-effort for that case (see FetchChats).
	Source string
	// Limit caps the number of chats returned. Zero uses the backend default.
	Limit int
	// Offset skips the first N chats for pagination.
	Offset int
	// IncludeArchived also returns archived chats.
	IncludeArchived bool
	// ArchivedOnly returns only archived chats (overrides IncludeArchived).
	ArchivedOnly bool
}

// FetchChats lists the caller's chats from the REST API.
func FetchChats(ctx context.Context, baseURL, token string, opts ListChatsOptions, httpClient *http.Client) ([]Chat, error) {
	endpoints := GetChatEndpoints(baseURL)

	query := url.Values{}
	if opts.Source != "" {
		query.Set("source", opts.Source)
	}
	if opts.Limit > 0 {
		query.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Offset > 0 {
		query.Set("offset", strconv.Itoa(opts.Offset))
	}
	if opts.IncludeArchived {
		query.Set("includeArchived", "true")
	}
	if opts.ArchivedOnly {
		query.Set("archivedOnly", "true")
	}

	endpoint := endpoints.Chats()
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-App-Source", "cli")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list chats: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var response struct {
		Data struct {
			Items []Chat `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Inline-assistant chats are ephemeral inline-generation artifacts, not
	// conversations worth listing or continuing, so drop them regardless of
	// the requested source filter.
	//
	// For the default and explicit comma-separated sources, inline-assistant
	// is never requested, so this loop is purely a safety net and pagination
	// stays honest. For Source == "all" the backend applies limit/offset
	// before we strip inline chats here, so a page can return fewer rows than
	// --limit even when more non-inline chats exist at later offsets. We
	// accept that best-effort behavior for "all" (a power-user/debug path)
	// rather than couple gcx to a backend excludeSource param or hard-code a
	// non-inline source allowlist that would silently hide future sources.
	chats := make([]Chat, 0, len(response.Data.Items))
	for _, chat := range response.Data.Items {
		if chat.Source == inlineAssistantSource {
			continue
		}
		chats = append(chats, chat)
	}

	return chats, nil
}

// inlineAssistantSource is the chat source used by the inline (in-editor)
// assistant for short, single-shot generations.
const inlineAssistantSource = "inline-assistant"

// DefaultConversationSources is the source allowlist 'gcx assistant
// conversation list' requests by default, covering the conversation kinds a
// user typically wants to resume. Filtering happens server-side, keeping
// --limit/--offset honest. Ephemeral inline-assistant chats are excluded.
const DefaultConversationSources = "assistant,slack,cli"

// FetchChatMessages fetches messages for a chat from the REST API.
func FetchChatMessages(ctx context.Context, baseURL, token, chatID string, httpClient *http.Client) ([]ChatMessage, error) {
	endpoints := GetChatEndpoints(baseURL)
	url := fmt.Sprintf("%s/%s/all-messages", endpoints.Chats(), chatID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-App-Source", "cli")

	resp, err := httpClient.Do(req)
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
			MessageID: newUUID(),
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
		ID:      newUUID(),
		Method:  "message/stream",
		Params:  paramsJSON,
	}

	return json.Marshal(req)
}

// SubmitApproval sends an approval response to the dedicated approval endpoint.
func SubmitApproval(ctx context.Context, baseURL, token, approvalID, chatID, tenantID, userID string, approved bool, httpClient *http.Client) error {
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

	resp, err := httpClient.Do(req)
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
