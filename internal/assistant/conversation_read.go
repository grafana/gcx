package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

type assistantAPIError struct {
	operation string
	status    int
	message   string
}

func (e *assistantAPIError) Error() string {
	if e.message != "" {
		return fmt.Sprintf("Assistant %s request failed with HTTP %d: %s", e.operation, e.status, e.message)
	}
	return fmt.Sprintf("Assistant %s request failed with HTTP %d", e.operation, e.status)
}

func (e *assistantAPIError) HTTPStatusCode() int    { return e.status }
func (e *assistantAPIError) APIServiceName() string { return "Assistant" }
func (e *assistantAPIError) APIUserMessage() string { return e.message }

type conversationMetadata struct {
	Chat

	IsPublic bool            `json:"isPublic,omitempty"`
	Messages json.RawMessage `json:"messages"`
}

type conversationMetadataEnvelope struct {
	Data json.RawMessage `json:"data"`
}

type messageCollectionEnvelope struct {
	Data json.RawMessage `json:"data"`
}

type messageCollection struct {
	Messages json.RawMessage `json:"messages"`
}

type uiMessageCollection struct {
	Thread   string          `json:"thread"`
	Messages json.RawMessage `json:"messages"`
}

type uiMessage struct {
	ID       string          `json:"id"`
	Role     string          `json:"role"`
	Created  string          `json:"created"`
	Parts    json.RawMessage `json:"parts"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// GetConversation resolves metadata and reads the engine-specific transcript.
func (c *Client) GetConversation(ctx context.Context, ref ConversationReference) (*ConversationTranscript, error) {
	if err := validateConversationID(ref.ID); err != nil {
		return nil, err
	}

	shared := ref.Shared
	metadata, err := c.readConversationMetadata(ctx, ref.ID, shared)
	if err != nil && !shared && isAssistantStatus(err, http.StatusNotFound) {
		shared = true
		metadata, err = c.readConversationMetadata(ctx, ref.ID, true)
		if err != nil && isAssistantStatus(err, http.StatusNotFound) {
			return nil, &assistantAPIError{
				operation: "conversation metadata",
				status:    http.StatusNotFound,
				message:   "conversation not found or inaccessible",
			}
		}
	}
	if err != nil {
		return nil, err
	}

	metadata.Shared = shared || metadata.IsPublic
	transcript := &ConversationTranscript{Chat: metadata.Chat, Messages: []ChatMessage{}}
	switch metadata.Engine {
	case "", "legacy":
		if shared {
			messages, err := decodeLegacyMessages(metadata.Messages)
			if err != nil {
				return nil, fmt.Errorf("decode shared conversation messages: %w", err)
			}
			transcript.Messages = messages
			return transcript, nil
		}
		messages, err := c.readLegacyConversationMessages(ctx, ref.ID)
		if err != nil {
			return nil, err
		}
		transcript.Messages = messages
		return transcript, nil
	case "aisdk":
		messages, err := c.readUIConversationMessages(ctx, ref.ID, shared)
		if err != nil {
			return nil, err
		}
		transcript.Messages = messages
		transcript.Scope = "main"
		return transcript, nil
	default:
		return nil, fmt.Errorf("unsupported conversation engine %q", metadata.Engine)
	}
}

func (c *Client) readConversationMetadata(ctx context.Context, id string, shared bool) (*conversationMetadata, error) {
	endpoint := c.transcriptEndpoint(id, shared, "")
	var envelope conversationMetadataEnvelope
	if err := c.doTranscriptRequest(ctx, "conversation metadata", endpoint, &envelope); err != nil {
		return nil, err
	}
	if len(envelope.Data) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Data), []byte("null")) {
		return nil, errors.New("assistant conversation metadata response is missing conversation data")
	}
	var metadata conversationMetadata
	if err := json.Unmarshal(envelope.Data, &metadata); err != nil {
		return nil, fmt.Errorf("decode Assistant conversation metadata: %w", err)
	}
	if metadata.ID == "" {
		return nil, errors.New("assistant conversation metadata response is missing conversation ID")
	}
	return &metadata, nil
}

func (c *Client) readLegacyConversationMessages(ctx context.Context, id string) ([]ChatMessage, error) {
	endpoint := c.transcriptEndpoint(id, false, "/all-messages")
	var envelope messageCollectionEnvelope
	if err := c.doTranscriptRequest(ctx, "conversation messages", endpoint, &envelope); err != nil {
		return nil, err
	}
	return decodeMessageEnvelope(envelope.Data, "Assistant conversation messages")
}

func (c *Client) readUIConversationMessages(ctx context.Context, id string, shared bool) ([]ChatMessage, error) {
	endpoint := c.transcriptEndpoint(id, shared, "/ui-messages") + "?thread=main"
	var envelope messageCollectionEnvelope
	if err := c.doTranscriptRequest(ctx, "conversation messages", endpoint, &envelope); err != nil {
		return nil, err
	}
	if len(envelope.Data) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Data), []byte("null")) {
		return nil, errors.New("assistant UI messages response is missing data")
	}
	var collection uiMessageCollection
	if err := json.Unmarshal(envelope.Data, &collection); err != nil {
		return nil, fmt.Errorf("decode Assistant UI messages response: %w", err)
	}
	if collection.Thread != "main" {
		return nil, fmt.Errorf("unexpected UI message thread %q; expected main", collection.Thread)
	}
	if len(collection.Messages) == 0 || bytes.Equal(bytes.TrimSpace(collection.Messages), []byte("null")) {
		return nil, errors.New("assistant UI messages response is missing messages collection")
	}
	var wireMessages []uiMessage
	if err := json.Unmarshal(collection.Messages, &wireMessages); err != nil {
		return nil, fmt.Errorf("decode Assistant UI messages: %w", err)
	}
	messages := make([]ChatMessage, 0, len(wireMessages))
	for _, wire := range wireMessages {
		message, err := normalizeUIMessage(wire)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func decodeMessageEnvelope(data json.RawMessage, label string) ([]ChatMessage, error) {
	rawMessages, err := decodeRawMessageCollection(data, label)
	if err != nil {
		return nil, err
	}
	var messages []ChatMessage
	if err := json.Unmarshal(rawMessages, &messages); err != nil {
		return nil, fmt.Errorf("decode %s: %w", label, err)
	}
	if messages == nil {
		messages = []ChatMessage{}
	}
	return messages, nil
}

func decodeLegacyMessages(raw json.RawMessage) ([]ChatMessage, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, errors.New("missing messages collection")
	}
	var messages []ChatMessage
	if err := json.Unmarshal(raw, &messages); err != nil {
		return nil, fmt.Errorf("decode messages collection: %w", err)
	}
	if messages == nil {
		messages = []ChatMessage{}
	}
	return messages, nil
}

func decodeRawMessageCollection(data json.RawMessage, label string) (json.RawMessage, error) {
	if len(data) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, fmt.Errorf("%s response is missing data", label)
	}
	var collection messageCollection
	if err := json.Unmarshal(data, &collection); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", label, err)
	}
	if len(collection.Messages) == 0 || bytes.Equal(bytes.TrimSpace(collection.Messages), []byte("null")) {
		return nil, fmt.Errorf("%s response is missing messages collection", label)
	}
	return collection.Messages, nil
}

func normalizeUIMessage(wire uiMessage) (ChatMessage, error) {
	if wire.ID == "" || wire.Role == "" {
		return ChatMessage{}, errors.New("assistant UI message is missing required id or role")
	}
	if len(wire.Parts) == 0 || bytes.Equal(bytes.TrimSpace(wire.Parts), []byte("null")) {
		return ChatMessage{}, fmt.Errorf("assistant UI message %q is missing parts collection", wire.ID)
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(wire.Parts, &parts); err != nil {
		return ChatMessage{}, fmt.Errorf("decode Assistant UI message %q parts: %w", wire.ID, err)
	}
	content := ContentJSON{}
	for _, raw := range parts {
		var part struct {
			Type string          `json:"type"`
			Text json.RawMessage `json:"text"`
		}
		if err := json.Unmarshal(raw, &part); err != nil || part.Type == "" {
			return ChatMessage{}, fmt.Errorf("invalid part in Assistant UI message %q", wire.ID)
		}
		if part.Type != "text" {
			continue
		}
		var text *string
		if len(part.Text) == 0 || json.Unmarshal(part.Text, &text) != nil || text == nil {
			return ChatMessage{}, fmt.Errorf("invalid text part in Assistant UI message %q", wire.ID)
		}
		content = append(content, ContentBlock{Type: "text", Text: *text})
	}
	return ChatMessage{
		ID:        wire.ID,
		Role:      wire.Role,
		CreatedAt: wire.Created,
		Content:   content,
		Parts:     append(json.RawMessage(nil), wire.Parts...),
		Hidden:    uiMessageHidden(wire.Metadata),
	}, nil
}

func uiMessageHidden(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var metadata struct {
		Hidden bool `json:"hidden"`
	}
	return json.Unmarshal(raw, &metadata) == nil && metadata.Hidden
}

func (c *Client) doTranscriptRequest(ctx context.Context, operation, endpoint string, target any) error {
	token, err := c.freshToken(ctx)
	if err != nil {
		return fmt.Errorf("refresh authentication token: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create Assistant %s request: %w", operation, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-App-Source", "cli")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send Assistant %s request: %w", operation, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return &assistantAPIError{
			operation: operation,
			status:    resp.StatusCode,
			message:   transcriptErrorMessage(resp),
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode Assistant %s response: %w", operation, err)
	}
	return nil
}

func (c *Client) transcriptEndpoint(id string, shared bool, suffix string) string {
	kind := "chats"
	if shared {
		kind = "shared"
	}
	return strings.TrimSuffix(c.baseURL, "/") + "/" + kind + "/" + url.PathEscape(id) + suffix
}

func isAssistantStatus(err error, status int) bool {
	var statusErr interface{ HTTPStatusCode() int }
	return errors.As(err, &statusErr) && statusErr.HTTPStatusCode() == status
}

// transcriptErrorMessage retains bounded server diagnostics without dumping response payloads.
func transcriptErrorMessage(resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return ""
	}
	var fields map[string]json.RawMessage
	message := ""
	if json.Unmarshal(body, &fields) == nil {
		for _, key := range []string{"message", "error"} {
			if json.Unmarshal(fields[key], &message) == nil && strings.TrimSpace(message) != "" {
				break
			}
			message = ""
		}
	} else if mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err == nil && mediaType == "text/plain" {
		plain := strings.TrimSpace(string(body))
		if strings.HasPrefix(plain, "{") || strings.HasPrefix(plain, "[") || strings.HasPrefix(plain, "<") {
			return ""
		}
		message = plain
	}
	message = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, message)
	return strings.Join(strings.Fields(message), " ")
}
