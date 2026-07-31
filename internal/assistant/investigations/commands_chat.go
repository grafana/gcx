package investigations

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// --- list-messages ---

type chatOpts struct {
	IO     cmdio.Options
	Role   string
	Hidden bool
}

func (o *chatOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ChatThreadTextCodec{})
	o.IO.RegisterCustomCodec("wide", &ChatThreadTextCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Role, "role", "", "Filter messages by role (user|assistant|tool)")
	flags.BoolVar(&o.Hidden, "include-hidden", false, "Include hidden system messages")
}

func newChatCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &chatOpts{}
	cmd := &cobra.Command{
		Use:   "list-messages <id>",
		Short: "List the chat thread messages for a v2 investigation.",
		Long: "List the chat thread that backs a v2 investigation: " +
			"assistant prose, tool calls (search_skills, prometheus_query_handler, " +
			"loki_query_handler_investigator, tempo_query_handler, ...), and tool " +
			"results. This is the substantive view of what the agent did; " +
			"use `get-narrative` for the prose-only rendering.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := requireV2(cmd, loader)
			if err != nil {
				return err
			}
			chatID, err := resolveChatID(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			messages, err := client.GetChatThread(cmd.Context(), chatID)
			if err != nil {
				return err
			}
			messages = filterMessages(messages, opts.Role, opts.Hidden)
			return opts.IO.Encode(cmd.OutOrStdout(), messages)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func filterMessages(messages []ChatThreadMessage, role string, includeHidden bool) []ChatThreadMessage {
	if role == "" && includeHidden {
		return messages
	}
	out := make([]ChatThreadMessage, 0, len(messages))
	for _, m := range messages {
		if !includeHidden && m.Hidden {
			continue
		}
		if role != "" && !strings.EqualFold(m.Role, role) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// --- narrative ---

type narrativeOpts struct{ IO cmdio.Options }

func (o *narrativeOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &NarrativeCodec{})
	o.IO.RegisterCustomCodec("wide", &NarrativeCodec{})
	// No "agents" override: like every sibling investigations command, agent
	// mode falls through to the standard agents codec, so the default agent
	// output is exactly one JSON value (the narrative as a JSON string).
	// Humans keep the raw markdown via the table/wide codecs.
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newNarrativeCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &narrativeOpts{}
	cmd := &cobra.Command{
		Use:   "get-narrative <id>",
		Short: "Show the assistant-authored prose for a v2 investigation.",
		Long:  "Show just the assistant-authored prose from a v2 investigation's chat thread — the text a human would read in the workspace, with tool plumbing stripped.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := requireV2(cmd, loader)
			if err != nil {
				return err
			}
			chatID, err := resolveChatID(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			messages, err := client.GetChatThread(cmd.Context(), chatID)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), Narrative(messages))
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- list-tool-calls ---

type toolsOpts struct {
	IO   cmdio.Options
	Name string
}

func (o *toolsOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ToolsTableCodec{})
	o.IO.RegisterCustomCodec("wide", &ToolsTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Name, "name", "", "Filter to tool calls with this name (e.g. search_skills, prometheus_query_handler)")
}

func newToolsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &toolsOpts{}
	cmd := &cobra.Command{
		Use:   "list-tool-calls <id>",
		Short: "List tool calls made during a v2 investigation.",
		Long:  "List every tool call the agent made during a v2 investigation, paired with its result. Use --name to filter (e.g. search_skills, prometheus_query_handler).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := requireV2(cmd, loader)
			if err != nil {
				return err
			}
			chatID, err := resolveChatID(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			messages, err := client.GetChatThread(cmd.Context(), chatID)
			if err != nil {
				return err
			}
			calls := ExtractToolCalls(messages)
			if opts.Name != "" {
				filtered := calls[:0]
				for _, c := range calls {
					if c.Name == opts.Name {
						filtered = append(filtered, c)
					}
				}
				calls = filtered
			}
			return opts.IO.Encode(cmd.OutOrStdout(), calls)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- codecs ---

// ChatThreadTextCodec renders the chat thread as a human-readable stream.
type ChatThreadTextCodec struct{ Wide bool }

func (c *ChatThreadTextCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *ChatThreadTextCodec) Encode(w io.Writer, v any) error {
	messages, ok := v.([]ChatThreadMessage)
	if !ok {
		return errors.New("invalid data type for chat codec: expected []ChatThreadMessage")
	}
	for i, m := range messages {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "[%s]", m.Role)
		if m.Type != "" {
			fmt.Fprintf(w, " type=%s", m.Type)
		}
		if c.Wide && m.ID != "" {
			fmt.Fprintf(w, " id=%s", m.ID)
		}
		if c.Wide && m.CreatedAt != "" {
			fmt.Fprintf(w, " created=%s", m.CreatedAt)
		}
		fmt.Fprintln(w)
		for _, b := range m.Content {
			renderChatBlock(w, b, c.Wide)
		}
	}
	return nil
}

func (c *ChatThreadTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}

func renderChatBlock(w io.Writer, b ChatContentBlock, wide bool) {
	switch b.Type {
	case "text":
		text := stripContextTags(b.Text)
		if text == "" {
			return
		}
		fmt.Fprintln(w, text)
	case "thinking":
		if b.Thinking == "" {
			return
		}
		fmt.Fprintf(w, "  ~ %s\n", b.Thinking)
	case "tool_use":
		fmt.Fprintf(w, "  → tool_use %s", b.ToolName)
		if wide && b.ToolID != "" {
			fmt.Fprintf(w, " id=%s", b.ToolID)
		}
		if len(b.ToolInput) > 0 {
			fmt.Fprintf(w, " input=%s", compactJSON(b.ToolInput, wide))
		}
		fmt.Fprintln(w)
	case "tool_result":
		marker := "✓"
		if b.IsError {
			marker = "✗"
		}
		fmt.Fprintf(w, "  ← tool_result %s", marker)
		if b.ToolName != "" {
			fmt.Fprintf(w, " %s", b.ToolName)
		}
		if wide && b.ToolUseID != "" {
			fmt.Fprintf(w, " for=%s", b.ToolUseID)
		}
		if b.DurationMs > 0 {
			fmt.Fprintf(w, " durationMs=%d", b.DurationMs)
		}
		text := joinToolResultText(b.ToolResult)
		if text != "" {
			limit := 120
			if wide {
				limit = 320
			}
			text = strings.ReplaceAll(text, "\n", " ")
			if len(text) > limit {
				text = text[:limit-3] + "..."
			}
			fmt.Fprintf(w, " result=%s", text)
		}
		fmt.Fprintln(w)
	case "artifact":
		fmt.Fprintf(w, "  ◆ artifact %s", b.ArtifactType)
		if ids := panelIDs(b.Panel); len(ids) > 0 {
			fmt.Fprintf(w, " panels=%s", strings.Join(ids, ","))
		}
		fmt.Fprintln(w)
	default:
		fmt.Fprintf(w, "  · %s\n", b.Type)
	}
}

// joinToolResultText concatenates all text parts of a tool_result with
// blank-line separators. Non-text parts are ignored.
func joinToolResultText(parts []ToolResultPart) string {
	var sb strings.Builder
	for _, p := range parts {
		if p.Type != "text" || p.Text == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(p.Text)
	}
	return sb.String()
}

// panelIDs extracts the panelId values from an artifact's panel payload.
// The shape is [{"panelId":"p5",...},{"panelId":"p7",...}]; missing or
// malformed payloads return nil.
func panelIDs(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var panels []struct {
		PanelID string `json:"panelId"`
	}
	if err := json.Unmarshal(raw, &panels); err != nil {
		return nil
	}
	out := make([]string, 0, len(panels))
	for _, p := range panels {
		if p.PanelID != "" {
			out = append(out, p.PanelID)
		}
	}
	return out
}

func compactJSON(raw json.RawMessage, wide bool) string {
	limit := 120
	if wide {
		limit = 320
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	s := string(b)
	if len(s) > limit {
		return s[:limit-3] + "..."
	}
	return s
}

// NarrativeCodec renders the narrative string raw, with a trailing newline.
// JSON/YAML/agents codecs handle the string natively (as a JSON/YAML string
// value); this codec is registered under "table"/"wide" for terminal use so
// humans read the markdown directly.
type NarrativeCodec struct{}

func (c NarrativeCodec) Format() format.Format {
	return "table"
}

func (NarrativeCodec) Encode(w io.Writer, v any) error {
	s, ok := v.(string)
	if !ok {
		return errors.New("invalid data type for narrative codec: expected string")
	}
	if s == "" {
		return nil
	}
	if _, err := io.WriteString(w, s); err != nil {
		return err
	}
	if !strings.HasSuffix(s, "\n") {
		_, err := io.WriteString(w, "\n")
		return err
	}
	return nil
}

func (NarrativeCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("narrative format does not support decoding")
}

// ToolsTableCodec renders []ToolCall as a table.
type ToolsTableCodec struct{ Wide bool }

func (c *ToolsTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *ToolsTableCodec) Encode(w io.Writer, v any) error {
	calls, ok := v.([]ToolCall)
	if !ok {
		return errors.New("invalid data type for table codec: expected []ToolCall")
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "NAME\tSTATUS\tDURATION_MS\tID\tINPUT")
	} else {
		fmt.Fprintln(tw, "NAME\tSTATUS\tDURATION_MS\tINPUT")
	}
	for _, call := range calls {
		status := "ok"
		switch {
		case call.IsError:
			status = "error"
		case len(call.Result) == 0:
			status = "pending"
		}
		input := "-"
		if len(call.Input) > 0 {
			input = compactJSON(call.Input, c.Wide)
		}
		dur := "-"
		if call.DurationMs > 0 {
			dur = strconv.FormatInt(call.DurationMs, 10)
		}
		if c.Wide {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", call.Name, status, dur, call.ID, input)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", call.Name, status, dur, input)
		}
	}
	return tw.Flush()
}

func (c *ToolsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
