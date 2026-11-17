package investigations_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/assistant/investigations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Synthetic aisdk fixtures. They follow the ui-messages wire shape (part
// types, tool part fields, data-error payload) but carry made-up content.

const chatPathPrefix = "/api/plugins/grafana-assistant-app/resources/api/v1/chats/"

// chatServer answers chat metadata with engine and serves body on the
// transcript endpoint that engine expects. Any other request fails the test.
func chatServer(t *testing.T, engine, body string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, chatPathPrefix)
		switch {
		case path == "chat-1":
			writeJSON(w, map[string]any{"data": map[string]any{"id": "chat-1", "engine": engine}})
		case engine == "aisdk" && path == "chat-1/ui-messages" && r.URL.Query().Get("thread") == "main":
			_, _ = w.Write([]byte(body))
		case engine != "aisdk" && path == "chat-1/all-messages":
			_, _ = w.Write([]byte(body))
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			w.WriteHeader(http.StatusBadRequest)
		}
	})
}

func uiBody(messages ...string) string {
	return `{"data":{"thread":"main","messages":[` + strings.Join(messages, ",") + `]}}`
}

func uiMessage(id, role, parts string) string {
	return fmt.Sprintf(`{"id":%q,"role":%q,"created":"2026-01-01T00:00:00Z","parts":%s}`, id, role, parts)
}

func TestGetChatThread_RoutesByEngine(t *testing.T) {
	legacy := `{"data":{"messages":[{"id":"m1","role":"assistant","content":[{"type":"text","text":"legacy"}]}]}}`
	aisdk := uiBody(uiMessage("m1", "assistant", `[{"type":"text","text":"aisdk"}]`))

	tests := []struct {
		name     string
		engine   string
		body     string
		wantText string
		wantErr  string
	}{
		{name: "missing engine reads legacy", engine: "", body: legacy, wantText: "legacy"},
		{name: "legacy", engine: "legacy", body: legacy, wantText: "legacy"},
		{name: "aisdk", engine: "aisdk", body: aisdk, wantText: "aisdk"},
		{name: "unknown engine", engine: "future", wantErr: `engine "future"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := newTestClient(t, chatServer(t, tc.engine, tc.body))
			messages, err := client.GetChatThread(t.Context(), "chat-1")
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, messages, 1)
			assert.Equal(t, tc.wantText, messages[0].Content[0].Text)
		})
	}
}

func TestGetChatThread_AISDKConversion(t *testing.T) {
	tests := []struct {
		name  string
		parts string
		meta  string
		want  string // JSON of the resulting thread messages
	}{
		{
			name:  "text only",
			parts: `[{"type":"step-start"},{"type":"text","text":"hello"}]`,
			want:  `[{"id":"m1","role":"assistant","type":"message","created":"2026-01-01T00:00:00Z","content":[{"type":"text","text":"hello"}]}]`,
		},
		{
			name: "finished tool call splits like legacy",
			parts: `[{"type":"text","text":"checking"},` +
				`{"type":"tool-run_query","toolName":"run_query","toolCallId":"c1","state":"output-available","input":{"q":"up"},"output":{"content":[{"type":"text","text":"1"}],"isError":false,"durationMs":12,"modelOutput":"ignored"}},` +
				`{"type":"text","text":"done"}]`,
			want: `[
				{"id":"m1","role":"assistant","type":"message","created":"2026-01-01T00:00:00Z","content":[{"type":"text","text":"checking"},{"type":"tool_use","toolId":"c1","toolName":"run_query","toolInput":{"q":"up"}}]},
				{"id":"m1:1","role":"user","type":"message","created":"2026-01-01T00:00:00Z","content":[{"type":"tool_result","toolUseId":"c1","toolName":"run_query","toolResult":[{"type":"text","text":"1"}],"durationMs":12}]},
				{"id":"m1:2","role":"assistant","type":"message","created":"2026-01-01T00:00:00Z","content":[{"type":"text","text":"done"}]}]`,
		},
		{
			name:  "running tool call has no result",
			parts: `[{"type":"tool-run_query","toolCallId":"c1","state":"input-available","input":{"q":"up"}}]`,
			want:  `[{"id":"m1","role":"assistant","type":"message","created":"2026-01-01T00:00:00Z","content":[{"type":"tool_use","toolId":"c1","toolName":"run_query","toolInput":{"q":"up"}}]}]`,
		},
		{
			name:  "failed tool call with only errorText",
			parts: `[{"type":"tool-run_query","toolName":"run_query","toolCallId":"c1","state":"output-error","input":{},"output":null,"errorText":"timed out"}]`,
			want: `[
				{"id":"m1","role":"assistant","type":"message","created":"2026-01-01T00:00:00Z","content":[{"type":"tool_use","toolId":"c1","toolName":"run_query","toolInput":{}}]},
				{"id":"m1:1","role":"user","type":"message","created":"2026-01-01T00:00:00Z","content":[{"type":"tool_result","toolUseId":"c1","toolName":"run_query","toolResult":[{"type":"text","text":"timed out"}],"isError":true}]}]`,
		},
		{
			name:  "tool error flagged in output, null duration",
			parts: `[{"type":"tool-run_query","toolName":"run_query","toolCallId":"c1","state":"output-available","input":{},"output":{"content":[{"type":"text","text":"bad query"}],"isError":true,"durationMs":null}}]`,
			want: `[
				{"id":"m1","role":"assistant","type":"message","created":"2026-01-01T00:00:00Z","content":[{"type":"tool_use","toolId":"c1","toolName":"run_query","toolInput":{}}]},
				{"id":"m1:1","role":"user","type":"message","created":"2026-01-01T00:00:00Z","content":[{"type":"tool_result","toolUseId":"c1","toolName":"run_query","toolResult":[{"type":"text","text":"bad query"}],"isError":true}]}]`,
		},
		{
			name:  "dynamic (MCP) tool call maps like a typed one",
			parts: `[{"type":"dynamic-tool","toolName":"mcp_search","toolCallId":"c1","state":"output-available","input":{"q":"x"},"output":{"content":[{"type":"text","text":"hit"}]}}]`,
			want: `[
				{"id":"m1","role":"assistant","type":"message","created":"2026-01-01T00:00:00Z","content":[{"type":"tool_use","toolId":"c1","toolName":"mcp_search","toolInput":{"q":"x"}}]},
				{"id":"m1:1","role":"user","type":"message","created":"2026-01-01T00:00:00Z","content":[{"type":"tool_result","toolUseId":"c1","toolName":"mcp_search","toolResult":[{"type":"text","text":"hit"}]}]}]`,
		},
		{
			name:  "denied tool call is an error result",
			parts: `[{"type":"tool-screen","toolName":"screen","toolCallId":"c1","state":"output-denied","input":{},"output":{"content":[]}}]`,
			want: `[
				{"id":"m1","role":"assistant","type":"message","created":"2026-01-01T00:00:00Z","content":[{"type":"tool_use","toolId":"c1","toolName":"screen","toolInput":{}}]},
				{"id":"m1:1","role":"user","type":"message","created":"2026-01-01T00:00:00Z","content":[{"type":"tool_result","toolUseId":"c1","toolName":"screen","toolResult":[],"isError":true}]}]`,
		},
		{
			name:  "output without content is kept whole",
			parts: `[{"type":"tool-launch","toolCallId":"c1","state":"output-available","input":{},"output":{"extension":{"id":"x1"}}}]`,
			want: `[
				{"id":"m1","role":"assistant","type":"message","created":"2026-01-01T00:00:00Z","content":[{"type":"tool_use","toolId":"c1","toolName":"launch","toolInput":{}}]},
				{"id":"m1:1","role":"user","type":"message","created":"2026-01-01T00:00:00Z","content":[{"type":"tool_result","toolUseId":"c1","toolName":"launch","toolResult":{"extension":{"id":"x1"}}}]}]`,
		},
		{
			name:  "reasoning becomes a hidden thinking message",
			parts: `[{"type":"reasoning","text":"consider metrics","state":"done"},{"type":"text","text":"answer"}]`,
			want: `[
				{"id":"m1","role":"assistant","type":"message","hidden":true,"created":"2026-01-01T00:00:00Z","content":[{"type":"thinking","thinking":"consider metrics"}]},
				{"id":"m1:1","role":"assistant","type":"message","created":"2026-01-01T00:00:00Z","content":[{"type":"text","text":"answer"}]}]`,
		},
		{
			name:  "data-error becomes an error block",
			parts: `[{"type":"data-error","id":"e1","data":{"kind":"system","text":"model unavailable"}}]`,
			want:  `[{"id":"m1","role":"assistant","type":"message","created":"2026-01-01T00:00:00Z","content":[{"type":"error","text":"model unavailable"}]}]`,
		},
		{
			name:  "unknown part is passed through raw",
			parts: `[{"type":"data-plan","id":"p1","data":{"steps":2}}]`,
			want:  `[{"id":"m1","role":"assistant","type":"message","created":"2026-01-01T00:00:00Z","content":[{"type":"data-plan","raw":{"type":"data-plan","id":"p1","data":{"steps":2}}}]}]`,
		},
		{
			name:  "hidden message hides every piece",
			parts: `[{"type":"text","text":"a"},{"type":"tool-t","toolCallId":"c1","state":"output-available","input":{},"output":{"content":[]}}]`,
			meta:  `{"hidden":true}`,
			want: `[
				{"id":"m1","role":"assistant","type":"message","hidden":true,"created":"2026-01-01T00:00:00Z","content":[{"type":"text","text":"a"},{"type":"tool_use","toolId":"c1","toolName":"t","toolInput":{}}]},
				{"id":"m1:1","role":"user","type":"message","hidden":true,"created":"2026-01-01T00:00:00Z","content":[{"type":"tool_result","toolUseId":"c1","toolName":"t","toolResult":[]}]}]`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := uiMessage("m1", "assistant", tc.parts)
			if tc.meta != "" {
				msg = strings.TrimSuffix(msg, "}") + `,"metadata":` + tc.meta + `}`
			}
			client := newTestClient(t, chatServer(t, "aisdk", uiBody(msg)))
			messages, err := client.GetChatThread(t.Context(), "chat-1")
			require.NoError(t, err)
			got, err := json.Marshal(messages)
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(got))
		})
	}
}

func TestGetChatThread_AISDKInvalidResponse(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "wrong thread", body: `{"data":{"thread":"side","messages":[]}}`},
		{name: "missing messages", body: `{"data":{"thread":"main"}}`},
		{name: "part without type", body: uiBody(uiMessage("m1", "assistant", `[{"text":"x"}]`))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := newTestClient(t, chatServer(t, "aisdk", tc.body))
			_, err := client.GetChatThread(t.Context(), "chat-1")
			require.Error(t, err)
		})
	}
}

func TestAISDKThreadViews(t *testing.T) {
	body := uiBody(
		uiMessage("u1", "user", `[{"type":"text","text":"why slow?"}]`),
		uiMessage("a1", "assistant", `[`+
			`{"type":"reasoning","text":"private plan"},`+
			`{"type":"text","text":"Checking latency."},`+
			`{"type":"tool-run_query","toolName":"run_query","toolCallId":"c1","state":"output-available","input":{"q":"p99"},"output":{"content":[{"type":"text","text":"420ms"}],"isError":false,"durationMs":7}},`+
			`{"type":"data-plan","data":{}},`+
			`{"type":"text","text":"p99 is high."},`+
			`{"type":"data-error","data":{"kind":"system","text":"turn\naborted"}}]`),
	)
	client := newTestClient(t, chatServer(t, "aisdk", body))
	messages, err := client.GetChatThread(t.Context(), "chat-1")
	require.NoError(t, err)

	t.Run("narrative", func(t *testing.T) {
		assert.Equal(t, "Checking latency.\n\np99 is high.\n\n[turn failed: turn aborted]", investigations.Narrative(messages))
	})

	t.Run("tool calls", func(t *testing.T) {
		calls := investigations.ExtractToolCalls(messages)
		require.Len(t, calls, 1)
		assert.Equal(t, "run_query", calls[0].Name)
		assert.Equal(t, json.Number("7"), calls[0].DurationMs)
		assert.JSONEq(t, `[{"type":"text","text":"420ms"}]`, string(calls[0].Result))
	})

	t.Run("text output skips raw parts", func(t *testing.T) {
		var out bytes.Buffer
		require.NoError(t, (&investigations.ChatThreadTextCodec{}).Encode(&out, messages))
		assert.Contains(t, out.String(), "← tool_result ✓ run_query durationMs=7 result=420ms")
		assert.Contains(t, out.String(), "! error turn aborted")
		assert.NotContains(t, out.String(), "data-plan")
	})
}

// TestListMessages_ReasoningHiddenOnlyOnAISDK checks that aisdk reasoning
// needs --include-hidden while legacy thinking blocks still show by default.
func TestListMessages_ReasoningHiddenOnlyOnAISDK(t *testing.T) {
	t.Setenv("GCX_ASSISTANT_API_VERSION", "v2")
	t.Setenv("GCX_AGENT_MODE", "false")

	legacy := `{"data":{"messages":[{"id":"m1","role":"assistant","content":[{"type":"thinking","thinking":"legacy thought"},{"type":"text","text":"reply"}]}]}}`
	aisdk := uiBody(uiMessage("m1", "assistant", `[{"type":"reasoning","text":"aisdk thought"},{"type":"text","text":"reply"}]`))

	tests := []struct {
		name          string
		engine        string
		body          string
		includeHidden bool
		wantThought   bool
		thought       string
	}{
		{name: "legacy default", engine: "legacy", body: legacy, wantThought: true, thought: "legacy thought"},
		{name: "aisdk default", engine: "aisdk", body: aisdk, wantThought: false, thought: "aisdk thought"},
		{name: "aisdk include hidden", engine: "aisdk", body: aisdk, includeHidden: true, wantThought: true, thought: "aisdk thought"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chats := chatServer(t, tc.engine, tc.body)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, v2InvestigationsPath+"/") {
					writeJSON(w, map[string]any{"data": map[string]any{"investigationId": "inv-1", "chatId": "chat-1"}})
					return
				}
				if !strings.HasPrefix(r.URL.Path, chatPathPrefix) {
					w.WriteHeader(http.StatusNotFound) // startup probes such as /bootdata
					return
				}
				chats.ServeHTTP(w, r)
			})
			args := []string{"list-messages", "inv-1"}
			if tc.includeHidden {
				args = append(args, "--include-hidden")
			}
			cmd := investigations.Commands(newGetLoader(t, handler))
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(args)
			require.NoError(t, cmd.Execute())
			assert.Contains(t, out.String(), "reply")
			assert.Equal(t, tc.wantThought, strings.Contains(out.String(), tc.thought), out.String())
		})
	}
}
