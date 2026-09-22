package assistant_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/assistant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientGetConversationRoutesByReferenceAndEngine(t *testing.T) {
	const (
		legacyMetadata = `{"data":{"id":"chat-1","name":"Legacy","engine":"legacy","source":"assistant"}}`
		absentEngine   = `{"data":{"id":"chat-1","name":"Legacy","source":"assistant"}}`
		aiMetadata     = `{"data":{"id":"chat-1","name":"AI SDK","engine":"aisdk","source":"assistant"}}`
		sharedAI       = `{"data":{"id":"chat-1","name":"AI SDK","engine":"aisdk","source":"assistant","isPublic":true}}`
		legacyMessages = `{"data":{"messages":[{"id":"m1","role":"user","created":"2026-01-01T00:00:00Z","content":[{"type":"text","text":"legacy"}]}]}}`
		uiMessages     = `{"data":{"thread":"main","messages":[{"id":"m1","role":"assistant","created":"2026-01-01T00:00:00Z","parts":[{"type":"text","text":"first"},{"type":"tool-example","toolCallId":"call-1"},{"type":"file","mediaType":"text/plain","url":"https://example.invalid/synthetic.txt"},{"type":"future-part","data":{"value":1}},{"type":"text","text":"second"}]},{"id":"m2","role":"user","created":"2026-01-01T00:01:00Z","parts":[{"type":"text","text":"third"}]},{"id":"m3","role":"assistant","created":"2026-01-01T00:02:00Z","metadata":{"hidden":true},"parts":[{"type":"text","text":"hidden"}]}],"fastMode":false}}`
	)

	tests := []struct {
		name         string
		ref          assistant.ConversationReference
		responses    map[string]testHTTPResponse
		wantRequests []string
		wantEngine   string
		wantShared   bool
		wantScope    string
		wantText     string
		wantParts    string
	}{
		{
			name: "ordinary legacy",
			ref:  assistant.ConversationReference{ID: "chat-1"},
			responses: map[string]testHTTPResponse{
				"/chats/chat-1":              {body: legacyMetadata},
				"/chats/chat-1/all-messages": {body: legacyMessages},
			},
			wantRequests: []string{"/chats/chat-1", "/chats/chat-1/all-messages"},
			wantEngine:   "legacy",
			wantText:     "legacy",
		},
		{
			name: "absent engine remains legacy compatible",
			ref:  assistant.ConversationReference{ID: "chat-1"},
			responses: map[string]testHTTPResponse{
				"/chats/chat-1":              {body: absentEngine},
				"/chats/chat-1/all-messages": {body: legacyMessages},
			},
			wantRequests: []string{"/chats/chat-1", "/chats/chat-1/all-messages"},
			wantText:     "legacy",
		},
		{
			name: "ordinary AI SDK",
			ref:  assistant.ConversationReference{ID: "chat-1"},
			responses: map[string]testHTTPResponse{
				"/chats/chat-1":                         {body: aiMetadata},
				"/chats/chat-1/ui-messages?thread=main": {body: uiMessages},
			},
			wantRequests: []string{"/chats/chat-1", "/chats/chat-1/ui-messages?thread=main"},
			wantEngine:   "aisdk",
			wantScope:    "main",
			wantText:     "first\nsecond",
			wantParts:    `[{"type":"text","text":"first"},{"type":"tool-example","toolCallId":"call-1"},{"type":"file","mediaType":"text/plain","url":"https://example.invalid/synthetic.txt"},{"type":"future-part","data":{"value":1}},{"type":"text","text":"second"}]`,
		},
		{
			name: "explicit shared AI SDK",
			ref:  assistant.ConversationReference{ID: "chat-1", Shared: true},
			responses: map[string]testHTTPResponse{
				"/shared/chat-1":                         {body: sharedAI},
				"/shared/chat-1/ui-messages?thread=main": {body: uiMessages},
			},
			wantRequests: []string{"/shared/chat-1", "/shared/chat-1/ui-messages?thread=main"},
			wantEngine:   "aisdk",
			wantShared:   true,
			wantScope:    "main",
			wantText:     "first\nsecond",
			wantParts:    `[{"type":"text","text":"first"},{"type":"tool-example","toolCallId":"call-1"},{"type":"file","mediaType":"text/plain","url":"https://example.invalid/synthetic.txt"},{"type":"future-part","data":{"value":1}},{"type":"text","text":"second"}]`,
		},
		{
			name: "bare ID falls back to shared metadata on 404",
			ref:  assistant.ConversationReference{ID: "chat-1"},
			responses: map[string]testHTTPResponse{
				"/chats/chat-1":                          {status: http.StatusNotFound},
				"/shared/chat-1":                         {body: sharedAI},
				"/shared/chat-1/ui-messages?thread=main": {body: uiMessages},
			},
			wantRequests: []string{"/chats/chat-1", "/shared/chat-1", "/shared/chat-1/ui-messages?thread=main"},
			wantEngine:   "aisdk",
			wantShared:   true,
			wantScope:    "main",
			wantText:     "first\nsecond",
			wantParts:    `[{"type":"text","text":"first"},{"type":"tool-example","toolCallId":"call-1"},{"type":"file","mediaType":"text/plain","url":"https://example.invalid/synthetic.txt"},{"type":"future-part","data":{"value":1}},{"type":"text","text":"second"}]`,
		},
		{
			name: "ordinary metadata takes precedence for a public chat",
			ref:  assistant.ConversationReference{ID: "chat-1"},
			responses: map[string]testHTTPResponse{
				"/chats/chat-1":                         {body: sharedAI},
				"/chats/chat-1/ui-messages?thread=main": {body: uiMessages},
			},
			wantRequests: []string{"/chats/chat-1", "/chats/chat-1/ui-messages?thread=main"},
			wantEngine:   "aisdk",
			wantShared:   true,
			wantScope:    "main",
			wantText:     "first\nsecond",
			wantParts:    `[{"type":"text","text":"first"},{"type":"tool-example","toolCallId":"call-1"},{"type":"file","mediaType":"text/plain","url":"https://example.invalid/synthetic.txt"},{"type":"future-part","data":{"value":1}},{"type":"text","text":"second"}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, requests := newConversationTestClient(t, tt.responses, nil)
			got, err := client.GetConversation(context.Background(), tt.ref)
			require.NoError(t, err)
			assert.Equal(t, tt.wantRequests, *requests)
			assert.Equal(t, tt.wantEngine, got.Chat.Engine)
			assert.Equal(t, tt.wantShared, got.Chat.Shared)
			assert.Equal(t, tt.wantScope, got.Scope)
			if tt.wantParts != "" {
				require.Len(t, got.Messages, 3)
				assert.Equal(t, []string{"m1", "m2", "m3"}, []string{got.Messages[0].ID, got.Messages[1].ID, got.Messages[2].ID})
				assert.Equal(t, []string{"2026-01-01T00:00:00Z", "2026-01-01T00:01:00Z", "2026-01-01T00:02:00Z"}, []string{got.Messages[0].CreatedAt, got.Messages[1].CreatedAt, got.Messages[2].CreatedAt})
				assert.Equal(t, tt.wantText, got.Messages[0].ExtractText())
				assert.Equal(t, "third", got.Messages[1].ExtractText())
				assert.True(t, got.Messages[2].Hidden)
				visible := got.VisibleMessages()
				require.Len(t, visible, 2)
				assert.Equal(t, []string{"m1", "m2"}, []string{visible[0].ID, visible[1].ID})
				assert.JSONEq(t, tt.wantParts, string(got.Messages[0].Parts))
				assert.JSONEq(t, `[{"type":"text","text":"third"}]`, string(got.Messages[1].Parts))
				assert.JSONEq(t, `[{"type":"text","text":"hidden"}]`, string(got.Messages[2].Parts))
				return
			}
			require.Len(t, got.Messages, 1)
			assert.Equal(t, tt.wantText, got.Messages[0].ExtractText())
		})
	}
}

func TestClientGetConversationReadsEmbeddedLegacySharedMessages(t *testing.T) {
	responses := map[string]testHTTPResponse{
		"/shared/shared-1": {body: `{"data":{"id":"shared-1","name":"Shared legacy","engine":"legacy","source":"assistant","messages":[{"id":"m1","role":"user","created":"2026-01-01T00:00:00Z","content":[{"type":"text","text":"snapshot"}]}]}}`},
	}
	client, requests := newConversationTestClient(t, responses, nil)

	got, err := client.GetConversation(context.Background(), assistant.ConversationReference{ID: "shared-1", Shared: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"/shared/shared-1"}, *requests)
	assert.Empty(t, got.Scope)
	assert.True(t, got.Chat.Shared)
	require.Len(t, got.Messages, 1)
	assert.Equal(t, "snapshot", got.Messages[0].ExtractText())
}

func TestClientGetConversationMetadataFailuresDoNotProbe(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client, requests := newConversationTestClient(t, map[string]testHTTPResponse{
				"/chats/chat-1": {status: status, body: "sensitive transcript body"},
			}, nil)

			_, err := client.GetConversation(context.Background(), assistant.ConversationReference{ID: "chat-1"})
			require.Error(t, err)
			assert.Equal(t, []string{"/chats/chat-1"}, *requests)
			assert.NotContains(t, err.Error(), "sensitive transcript body")
			var statusErr interface {
				HTTPStatusCode() int
				APIServiceName() string
				APIUserMessage() string
			}
			require.ErrorAs(t, err, &statusErr)
			assert.Equal(t, status, statusErr.HTTPStatusCode())
			assert.Equal(t, "Assistant", statusErr.APIServiceName())
		})
	}
}

func TestClientGetConversationBothMetadataRoutesNotFound(t *testing.T) {
	client, requests := newConversationTestClient(t, map[string]testHTTPResponse{
		"/chats/missing":  {status: http.StatusNotFound},
		"/shared/missing": {status: http.StatusNotFound},
	}, nil)

	_, err := client.GetConversation(context.Background(), assistant.ConversationReference{ID: "missing"})
	require.ErrorContains(t, err, "conversation not found or inaccessible")
	assert.Equal(t, []string{"/chats/missing", "/shared/missing"}, *requests)
}

func TestClientGetConversationMessageFailureDoesNotFallback(t *testing.T) {
	client, requests := newConversationTestClient(t, map[string]testHTTPResponse{
		"/chats/chat-1":                         {body: `{"data":{"id":"chat-1","engine":"aisdk"}}`},
		"/chats/chat-1/ui-messages?thread=main": {status: http.StatusNotFound},
	}, nil)

	_, err := client.GetConversation(context.Background(), assistant.ConversationReference{ID: "chat-1"})
	require.Error(t, err)
	assert.Equal(t, []string{"/chats/chat-1", "/chats/chat-1/ui-messages?thread=main"}, *requests)
}

func TestClientGetConversationRejectsUnknownEngine(t *testing.T) {
	client, requests := newConversationTestClient(t, map[string]testHTTPResponse{
		"/chats/chat-1": {body: `{"data":{"id":"chat-1","engine":"future"}}`},
	}, nil)

	_, err := client.GetConversation(context.Background(), assistant.ConversationReference{ID: "chat-1"})
	require.ErrorContains(t, err, `unsupported conversation engine "future"`)
	assert.Equal(t, []string{"/chats/chat-1"}, *requests)
}

func TestClientGetConversationValidatesResponseStructure(t *testing.T) {
	tests := []struct {
		name        string
		responses   map[string]testHTTPResponse
		wantErr     string
		wantContent assistant.ContentJSON
	}{
		{
			name: "metadata missing data",
			responses: map[string]testHTTPResponse{
				"/chats/chat-1": {body: `{}`},
			},
			wantErr: "missing conversation data",
		},
		{
			name: "metadata missing ID",
			responses: map[string]testHTTPResponse{
				"/chats/chat-1": {body: `{"data":{"engine":"legacy"}}`},
			},
			wantErr: "missing conversation ID",
		},
		{
			name: "UI messages missing collection",
			responses: map[string]testHTTPResponse{
				"/chats/chat-1":                         {body: `{"data":{"id":"chat-1","engine":"aisdk"}}`},
				"/chats/chat-1/ui-messages?thread=main": {body: `{"data":{"thread":"main","fastMode":false}}`},
			},
			wantErr: "missing messages collection",
		},
		{
			name: "UI messages wrong thread",
			responses: map[string]testHTTPResponse{
				"/chats/chat-1":                         {body: `{"data":{"id":"chat-1","engine":"aisdk"}}`},
				"/chats/chat-1/ui-messages?thread=main": {body: `{"data":{"thread":"worker","messages":[],"fastMode":false}}`},
			},
			wantErr: "unexpected UI message thread",
		},
		{
			name: "text part missing string text",
			responses: map[string]testHTTPResponse{
				"/chats/chat-1":                         {body: `{"data":{"id":"chat-1","engine":"aisdk"}}`},
				"/chats/chat-1/ui-messages?thread=main": {body: `{"data":{"thread":"main","messages":[{"id":"m1","role":"assistant","created":"2026-01-01T00:00:00Z","parts":[{"type":"text","text":3}]}],"fastMode":false}}`},
			},
			wantErr: "invalid text part",
		},
		{
			name: "text part null",
			responses: map[string]testHTTPResponse{
				"/chats/chat-1":                         {body: `{"data":{"id":"chat-1","engine":"aisdk"}}`},
				"/chats/chat-1/ui-messages?thread=main": {body: `{"data":{"thread":"main","messages":[{"id":"m1","role":"assistant","created":"2026-01-01T00:00:00Z","parts":[{"type":"text","text":null}]}],"fastMode":false}}`},
			},
			wantErr: "invalid text part",
		},
		{
			name: "empty text part remains valid",
			responses: map[string]testHTTPResponse{
				"/chats/chat-1":                         {body: `{"data":{"id":"chat-1","engine":"aisdk"}}`},
				"/chats/chat-1/ui-messages?thread=main": {body: `{"data":{"thread":"main","messages":[{"id":"m1","role":"assistant","created":"2026-01-01T00:00:00Z","parts":[{"type":"text","text":""}]}],"fastMode":false}}`},
			},
			wantContent: assistant.ContentJSON{{Type: "text", Text: ""}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := newConversationTestClient(t, tt.responses, nil)
			got, err := client.GetConversation(context.Background(), assistant.ConversationReference{ID: "chat-1"})
			if tt.wantContent != nil {
				require.NoError(t, err)
				require.Len(t, got.Messages, 1)
				assert.Equal(t, tt.wantContent, got.Messages[0].Content)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestClientGetConversationPreservesValidEmptyMessages(t *testing.T) {
	client, _ := newConversationTestClient(t, map[string]testHTTPResponse{
		"/chats/chat-1":                         {body: `{"data":{"id":"chat-1","engine":"aisdk"}}`},
		"/chats/chat-1/ui-messages?thread=main": {body: `{"data":{"thread":"main","messages":[],"fastMode":false}}`},
	}, nil)

	got, err := client.GetConversation(context.Background(), assistant.ConversationReference{ID: "chat-1"})
	require.NoError(t, err)
	assert.NotNil(t, got.Messages)
	assert.Empty(t, got.Messages)
}

func TestClientGetConversationRefreshesTokenForEveryRequest(t *testing.T) {
	refreshes := 0
	client, _ := newConversationTestClient(t, map[string]testHTTPResponse{
		"/chats/chat-1":              {body: `{"data":{"id":"chat-1","engine":"legacy"}}`},
		"/chats/chat-1/all-messages": {body: `{"data":{"messages":[]}}`},
	}, func(context.Context) (string, error) {
		refreshes++
		return fmt.Sprintf("fresh-%d", refreshes), nil
	})

	_, err := client.GetConversation(context.Background(), assistant.ConversationReference{ID: "chat-1"})
	require.NoError(t, err)
	assert.Equal(t, 2, refreshes)
}

func TestClientGetConversationDirectCLIRoutes(t *testing.T) {
	tests := []struct {
		name         string
		ref          assistant.ConversationReference
		responses    map[string]testHTTPResponse
		wantRequests []string
		wantErr      string
	}{
		{
			name: "ordinary AI SDK is supported",
			ref:  assistant.ConversationReference{ID: "chat-1"},
			responses: map[string]testHTTPResponse{
				"/api/cli/v1/chats/chat-1":                         {body: `{"data":{"id":"chat-1","engine":"aisdk"}}`},
				"/api/cli/v1/chats/chat-1/ui-messages?thread=main": {body: `{"data":{"thread":"main","messages":[],"fastMode":false}}`},
			},
			wantRequests: []string{"/api/cli/v1/chats/chat-1", "/api/cli/v1/chats/chat-1/ui-messages?thread=main"},
		},
		{
			name: "shared allowlist denial is preserved",
			ref:  assistant.ConversationReference{ID: "shared-1", Shared: true},
			responses: map[string]testHTTPResponse{
				"/api/cli/v1/shared/shared-1": {status: http.StatusForbidden, body: "Access denied: path not allowed for CLI tokens"},
			},
			wantRequests: []string{"/api/cli/v1/shared/shared-1"},
			wantErr:      "HTTP 403",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				requests = append(requests, r.URL.RequestURI())
				response, ok := tt.responses[r.URL.RequestURI()]
				if !ok {
					http.NotFound(w, r)
					return
				}
				status := response.status
				if status == 0 {
					status = http.StatusOK
				}
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, response.body)
			}))
			t.Cleanup(server.Close)
			client := assistant.New(assistant.ClientOptions{GrafanaURL: server.URL, APIEndpoint: server.URL, Token: "test-token", HTTPClient: server.Client()})

			_, err := client.GetConversation(context.Background(), tt.ref)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				assert.Contains(t, err.Error(), "path not allowed")
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantRequests, requests)
		})
	}
}

type testHTTPResponse struct {
	status int
	body   string
}

func newConversationTestClient(t *testing.T, responses map[string]testHTTPResponse, refresher assistant.TokenRefresher) (*assistant.Client, *[]string) {
	t.Helper()
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		key := r.URL.RequestURI()
		prefix := "/api/plugins/grafana-assistant-app/resources/api/v1"
		requests = append(requests, key[len(prefix):])
		response, ok := responses[key[len(prefix):]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		status := response.status
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, response.body)
	}))
	t.Cleanup(server.Close)
	return assistant.New(assistant.ClientOptions{
		GrafanaURL:     server.URL,
		Token:          "test-token",
		TokenRefresher: refresher,
		HTTPClient:     server.Client(),
	}), &requests
}

func TestClientGetConversationErrorDiagnostics(t *testing.T) {
	tests := []struct{ name, contentType, body, want string }{
		{"JSON message", "application/json", `{"message":"Access denied: path not allowed for CLI tokens","data":"private"}`, "Access denied: path not allowed for CLI tokens"},
		{"JSON error", "application/json", `{"error":"missing scope"}`, "missing scope"},
		{"JSON without content type", "", `{"error":"missing scope"}`, "missing scope"},
		{"JSON message precedence", "application/json", `{"message":"first","error":"second"}`, "first"},
		{"JSON nonstring message", "application/json", `{"message":{"private":"value"},"error":"missing scope"}`, "missing scope"},
		{"JSON unrelated fields", "application/json", `{"data":{"text":"private"}}`, ""},
		{"JSON malformed", "application/json", `{"message":"private"`, ""},
		{"HTML", "text/html", "<html>private</html>", ""},
		{"plain text", "text/plain; charset=utf-8", "Access denied: path not allowed for CLI tokens", "Access denied: path not allowed for CLI tokens"},
		{"plain text controls", "text/plain", "denied\nnext\r\t\x00\x1b", "denied next"},
		{"JSON controls", "application/json", `{"error":"denied\nnext\u0000\u001b"}`, "denied next"},
		{"empty", "application/json", "", ""},
		{"HTML marked plain", "text/plain", "  <html>private</html>", ""},
		{"truncated JSON marked plain", "text/plain", `{"message":"` + strings.Repeat("x", 5000) + `"}`, ""},
		{"malformed JSON array marked plain", "text/plain", ` ["private"`, ""},
		{"JSON array marked plain", "text/plain", `["private"]`, ""},
		{"oversized JSON", "application/json", `{"message":"` + strings.Repeat("x", 5000) + `"}`, ""},
		{"bounded plain text", "text/plain", strings.Repeat("x", 4096) + "private tail", strings.Repeat("x", 4096)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header()["Content-Type"] = []string{tt.contentType}
				w.WriteHeader(http.StatusForbidden)
				_, _ = fmt.Fprint(w, tt.body)
			}))
			t.Cleanup(server.Close)
			client := assistant.New(assistant.ClientOptions{GrafanaURL: server.URL, Token: "test-token", HTTPClient: server.Client()})
			_, err := client.GetConversation(context.Background(), assistant.ConversationReference{ID: "chat-1"})
			var apiErr interface {
				HTTPStatusCode() int
				APIUserMessage() string
			}
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusForbidden, apiErr.HTTPStatusCode())
			assert.Equal(t, tt.want, apiErr.APIUserMessage())
			assert.Equal(t, 1, strings.Count(err.Error(), "HTTP 403"))
		})
	}
}
