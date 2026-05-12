package investigations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
)

const chatAllMessagesFmt = "/chats/%s/all-messages"

// ChatThreadMessage is one message in a Lodestone chat thread. The thread
// interleaves user prompts, assistant prose, tool invocations, and tool
// results — all the substantive content lives here, not in the legacy
// timeline/todos/report endpoints (which return empty stubs for Lodestone).
type ChatThreadMessage struct {
	ID        string             `json:"id"`
	Role      string             `json:"role"`
	Type      string             `json:"type,omitempty"`
	Hidden    bool               `json:"hidden,omitempty"`
	CreatedAt string             `json:"created,omitempty"`
	Content   []ChatContentBlock `json:"content"`
}

// ChatContentBlock is one entry in a message's content array. Tool calls and
// tool results are first-class — they're how Lodestone exposes what the
// agent did (search_skills match, prometheus_query, etc.).
type ChatContentBlock struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	ID         string          `json:"id,omitempty"`
	Name       string          `json:"name,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	ToolUseID  string          `json:"tool_use_id,omitempty"`
	ToolResult json.RawMessage `json:"content,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
	DurationMs int64           `json:"durationMs,omitempty"`
}

// GetChatThread fetches the full message list for a Lodestone chat. Messages
// are returned in server order (typically chronological).
func (c *Client) GetChatThread(ctx context.Context, chatID string) ([]ChatThreadMessage, error) {
	path := fmt.Sprintf(chatAllMessagesFmt, url.PathEscape(chatID))
	resp, err := c.base.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("get chat thread: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}
	var envelope struct {
		Data struct {
			Messages []ChatThreadMessage `json:"messages"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode chat thread: %w", err)
	}
	if envelope.Data.Messages == nil {
		return []ChatThreadMessage{}, nil
	}
	return envelope.Data.Messages, nil
}

// Narrative returns the assistant-authored prose from a chat thread,
// stripped of <context>...</context> tags. This is the text a human would
// see in the workspace, without tool plumbing.
func Narrative(messages []ChatThreadMessage) string {
	var sb strings.Builder
	for _, m := range messages {
		if m.Role != "assistant" || m.Hidden {
			continue
		}
		for _, b := range m.Content {
			if b.Type != "text" || b.Text == "" {
				continue
			}
			text := stripContextTags(b.Text)
			if text == "" {
				continue
			}
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(text)
		}
	}
	return sb.String()
}

// ToolCall summarises a single tool_use block paired with its matching
// tool_result (matched by ToolUseID). Used by the `tools` subcommand.
type ToolCall struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Input      json.RawMessage `json:"input,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	IsError    bool            `json:"isError,omitempty"`
	DurationMs int64           `json:"durationMs,omitempty"`
}

// ExtractToolCalls walks the chat thread and pairs every tool_use with its
// tool_result. Tool uses with no matching result (e.g. still running) are
// returned with an empty Result.
func ExtractToolCalls(messages []ChatThreadMessage) []ToolCall {
	results := map[string]ChatContentBlock{}
	for _, m := range messages {
		for _, b := range m.Content {
			if b.Type == "tool_result" && b.ToolUseID != "" {
				results[b.ToolUseID] = b
			}
		}
	}

	var calls []ToolCall
	for _, m := range messages {
		for _, b := range m.Content {
			if b.Type != "tool_use" {
				continue
			}
			call := ToolCall{
				ID:    b.ID,
				Name:  b.Name,
				Input: b.Input,
			}
			if r, ok := results[b.ID]; ok {
				call.Result = r.ToolResult
				call.IsError = r.IsError
				call.DurationMs = r.DurationMs
			}
			calls = append(calls, call)
		}
	}
	return calls
}

// SkillMatch is one chunk returned by a search_skills tool call. The
// upstream payload nests results inside the tool_result content blocks; this
// type flattens them for terminal-friendly display.
type SkillMatch struct {
	ToolUseID string  `json:"toolUseId"`
	Query     string  `json:"query,omitempty"`
	SkillID   string  `json:"skillId,omitempty"`
	SkillName string  `json:"skillName,omitempty"`
	Score     float64 `json:"score,omitempty"`
	Chunk     string  `json:"chunk,omitempty"`
	Source    string  `json:"source,omitempty"`
}

// ExtractSkillMatches pulls structured skill-search results out of every
// search_skills tool call in the thread. The payload shape varies across
// stacks, so this is best-effort: unknown shapes are skipped rather than
// erroring (use `-o json` on the `tools` command for the raw payload).
func ExtractSkillMatches(messages []ChatThreadMessage) []SkillMatch {
	type searchInput struct {
		Query string `json:"query"`
	}
	queries := map[string]string{}
	for _, m := range messages {
		for _, b := range m.Content {
			if b.Type != "tool_use" || b.Name != "search_skills" || len(b.Input) == 0 {
				continue
			}
			var in searchInput
			_ = json.Unmarshal(b.Input, &in)
			queries[b.ID] = in.Query
		}
	}

	var matches []SkillMatch
	for _, m := range messages {
		for _, b := range m.Content {
			if b.Type != "tool_result" || b.ToolUseID == "" || len(b.ToolResult) == 0 {
				continue
			}
			if _, ok := queries[b.ToolUseID]; !ok {
				continue
			}
			matches = append(matches, parseSkillResult(b.ToolUseID, queries[b.ToolUseID], b.ToolResult)...)
		}
	}
	return matches
}

// parseSkillResult handles the two payload shapes observed: (1) a JSON
// object/array directly on tool_result.content; (2) an Anthropic-style
// array of {type:"text", text:"<json-string>"} blocks where the JSON is
// embedded as a string. Both are tolerated.
func parseSkillResult(toolUseID, query string, raw json.RawMessage) []SkillMatch {
	type rawChunk struct {
		ID    string  `json:"id,omitempty"`
		Name  string  `json:"name,omitempty"`
		Title string  `json:"title,omitempty"`
		Score float64 `json:"score,omitempty"`
		Text  string  `json:"text,omitempty"`
		Chunk string  `json:"chunk,omitempty"`
		Body  string  `json:"body,omitempty"`
		Path  string  `json:"path,omitempty"`
	}
	type rawResult struct {
		Results []rawChunk `json:"results,omitempty"`
		Skills  []rawChunk `json:"skills,omitempty"`
		Matches []rawChunk `json:"matches,omitempty"`
	}

	tryDecode := func(b []byte) []rawChunk {
		var r rawResult
		if err := json.Unmarshal(b, &r); err == nil {
			switch {
			case len(r.Results) > 0:
				return r.Results
			case len(r.Skills) > 0:
				return r.Skills
			case len(r.Matches) > 0:
				return r.Matches
			}
		}
		var arr []rawChunk
		if err := json.Unmarshal(b, &arr); err == nil {
			return arr
		}
		return nil
	}

	// Anthropic-style nested text blocks come as [{"type":"text","text":"<json>"}].
	// Check for that envelope first — a naive []rawChunk decode would otherwise
	// silently consume it and yield bogus entries with text=<json>.
	var textBlocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	var chunks []rawChunk
	if err := json.Unmarshal(raw, &textBlocks); err == nil && len(textBlocks) > 0 && textBlocks[0].Type == "text" {
		for _, tb := range textBlocks {
			if tb.Type != "text" || tb.Text == "" {
				continue
			}
			if c := tryDecode([]byte(tb.Text)); len(c) > 0 {
				chunks = append(chunks, c...)
			}
		}
	}
	if len(chunks) == 0 {
		chunks = tryDecode(raw)
	}

	out := make([]SkillMatch, 0, len(chunks))
	for _, c := range chunks {
		name := c.Name
		if name == "" {
			name = c.Title
		}
		body := c.Chunk
		if body == "" {
			body = c.Text
		}
		if body == "" {
			body = c.Body
		}
		out = append(out, SkillMatch{
			ToolUseID: toolUseID,
			Query:     query,
			SkillID:   c.ID,
			SkillName: name,
			Score:     c.Score,
			Chunk:     body,
			Source:    c.Path,
		})
	}
	return out
}

// stripContextTags removes <context>...</context> blocks injected by the
// server. Duplicates the helper in internal/assistant/types.go to keep this
// package free of cross-package dependencies on the legacy assistant types.
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
