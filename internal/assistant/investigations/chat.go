package investigations

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
)

const chatAllMessagesFmt = "/chats/%s/all-messages"

// ChatThreadMessage is one message in a Lodestone chat thread. The thread
// interleaves user prompts, assistant prose, tool invocations, tool results,
// and panel artifacts — all the substantive content lives here, not in the
// legacy timeline/todos/report endpoints (which return empty stubs for
// Lodestone).
type ChatThreadMessage struct {
	ID        string             `json:"id"`
	Role      string             `json:"role"`
	Type      string             `json:"type,omitempty"`
	Hidden    bool               `json:"hidden,omitempty"`
	CreatedAt string             `json:"created,omitempty"`
	Audience  string             `json:"audience,omitempty"`
	Content   []ChatContentBlock `json:"content"`
}

// ChatContentBlock is one entry in a message's content array. Field tags
// follow the actual Grafana Assistant plugin shape (toolName/toolInput/
// toolUseId/toolResult — camelCase), not the Anthropic content-block
// convention. Tool calls and tool results are first-class because they're
// how Lodestone exposes what the agent did.
type ChatContentBlock struct {
	Type         string           `json:"type"`
	Text         string           `json:"text,omitempty"`
	Thinking     string           `json:"thinking,omitempty"`
	ToolID       string           `json:"toolId,omitempty"`
	ToolName     string           `json:"toolName,omitempty"`
	ToolInput    json.RawMessage  `json:"toolInput,omitempty"`
	ToolUseID    string           `json:"toolUseId,omitempty"`
	ToolResult   []ToolResultPart `json:"toolResult,omitempty"`
	IsError      bool             `json:"isError,omitempty"`
	DurationMs   int64            `json:"durationMs,omitempty"`
	ArtifactType string           `json:"artifactType,omitempty"`
	Panel        json.RawMessage  `json:"panel,omitempty"`
}

// ToolResultPart is one entry in the tool_result envelope. The server may
// emit multiple parts (e.g. one text block per query in a multi-query call).
type ToolResultPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// GetChatThread fetches the full message list for a Lodestone chat. Messages
// are returned in server order (chronological by `sequence`).
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

// Narrative returns the assistant-authored prose from a chat thread —
// text-type content blocks only, with <context>...</context> tags stripped.
// Thinking blocks and tool plumbing are excluded; this is the workspace
// reader's view.
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

// ToolCall is a single tool_use block paired with its matching tool_result.
// Used by the `tools` subcommand. Result keeps the original part list so
// `-o json` is lossless; the table codec joins parts for display.
type ToolCall struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Input      json.RawMessage  `json:"input,omitempty"`
	Result     []ToolResultPart `json:"result,omitempty"`
	IsError    bool             `json:"isError,omitempty"`
	DurationMs int64            `json:"durationMs,omitempty"`
}

// ExtractToolCalls walks the chat thread and pairs every tool_use with its
// tool_result (via toolUseId == toolId). Tool uses with no matching result
// (e.g. still running) come back with an empty Result.
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
				ID:    b.ToolID,
				Name:  b.ToolName,
				Input: b.ToolInput,
			}
			if r, ok := results[b.ToolID]; ok {
				call.Result = r.ToolResult
				call.IsError = r.IsError
				call.DurationMs = r.DurationMs
			}
			calls = append(calls, call)
		}
	}
	return calls
}

// SkillMatch is one chunk returned by a search_skills tool call.
type SkillMatch struct {
	ToolUseID string `json:"toolUseId"`
	Query     string `json:"query,omitempty"`
	SkillID   string `json:"skillId,omitempty"`
	Title     string `json:"title,omitempty"`
	Chunk     string `json:"chunk,omitempty"`
}

// ExtractSkillMatches pulls structured skill-search results out of every
// search_skills tool call in the thread. The server emits
// toolResult[*].text as a loose XML envelope:
//
//	<results count="N">
//	  <chunk skill_id="..." offset="N" length="N" title="...">body</chunk>
//	  ...
//	</results>
//
// We parse the chunk tags with a regex (the format has HTML entities and
// embedded newlines that make encoding/xml awkward).
func ExtractSkillMatches(messages []ChatThreadMessage) []SkillMatch {
	queries := map[string]string{}
	for _, m := range messages {
		for _, b := range m.Content {
			if b.Type != "tool_use" || b.ToolName != "search_skills" || len(b.ToolInput) == 0 {
				continue
			}
			queries[b.ToolID] = parseSearchSkillsQueries(b.ToolInput)
		}
	}

	var matches []SkillMatch
	for _, m := range messages {
		for _, b := range m.Content {
			if b.Type != "tool_result" || b.ToolUseID == "" || len(b.ToolResult) == 0 {
				continue
			}
			query, ok := queries[b.ToolUseID]
			if !ok {
				continue
			}
			for _, part := range b.ToolResult {
				if part.Type != "text" || part.Text == "" {
					continue
				}
				matches = append(matches, parseSkillResultXML(b.ToolUseID, query, part.Text)...)
			}
		}
	}
	return matches
}

// parseSearchSkillsQueries reads `{"queries":[{"keywords":"...","query":"..."}]}`
// and returns a "; "-joined list of the .query strings.
func parseSearchSkillsQueries(input json.RawMessage) string {
	var in struct {
		Queries []struct {
			Keywords string `json:"keywords"`
			Query    string `json:"query"`
		} `json:"queries"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return ""
	}
	parts := make([]string, 0, len(in.Queries))
	for _, q := range in.Queries {
		switch {
		case q.Query != "":
			parts = append(parts, q.Query)
		case q.Keywords != "":
			parts = append(parts, q.Keywords)
		}
	}
	return strings.Join(parts, "; ")
}

// chunkPattern matches one `<chunk skill_id="..." title="...">body</chunk>`
// entry. The `(?s)` flag makes `.` span newlines, which the bodies contain.
var chunkPattern = regexp.MustCompile(`(?s)<chunk\s+([^>]*)>(.*?)</chunk>`)

// attrPattern extracts key="value" pairs from the chunk attributes. Values
// don't contain quotes in the observed payload, so a simple regex is enough.
var attrPattern = regexp.MustCompile(`(\w+)="([^"]*)"`)

func parseSkillResultXML(toolUseID, query, raw string) []SkillMatch {
	found := chunkPattern.FindAllStringSubmatch(raw, -1)
	matches := make([]SkillMatch, 0, len(found))
	for _, m := range found {
		attrs := map[string]string{}
		for _, a := range attrPattern.FindAllStringSubmatch(m[1], -1) {
			attrs[a[1]] = html.UnescapeString(a[2])
		}
		matches = append(matches, SkillMatch{
			ToolUseID: toolUseID,
			Query:     query,
			SkillID:   attrs["skill_id"],
			Title:     attrs["title"],
			Chunk:     html.UnescapeString(strings.TrimSpace(m[2])),
		})
	}
	return matches
}

// stripContextTags removes <context>...</context> blocks injected by the
// server. Duplicates the helper in internal/assistant/types.go (unexported
// there) to keep this package free of cross-package dependencies.
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
