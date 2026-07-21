package conversations

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	base, err := aio11yhttp.NewClientFromCommand(cmd, loader)
	if err != nil {
		return nil, err
	}
	return NewClient(base), nil
}

// Commands returns the conversations command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "conversations",
		Short: "Query Agent Observability conversations.",
	}

	cmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newSearchCommand(loader),
		newListAnnotationsCommand(loader),
		newAnnotateCommand(loader),
	)
	return cmd
}

// --- list ---

type listOpts struct {
	IO    cmdio.Options
	Limit int
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TableCodec{})
	o.IO.RegisterCustomCodec("wide", &TableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.Limit, "limit", 50, "Maximum number of conversations to return (0 for no limit)")
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List conversations.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			convs, err := client.List(cmd.Context(), opts.Limit)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), convs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- get ---

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <conversation-id>",
		Short: "Get a single conversation with all generations.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			detail, err := client.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), detail)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- search ---

type searchOpts struct {
	IO       cmdio.Options
	Filters  string
	From     string
	To       string
	PageSize int
}

func (o *searchOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &SearchTableCodec{})
	o.IO.RegisterCustomCodec("wide", &SearchTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Filters, "filters", "", "Filter expression for conversation search")
	flags.StringVar(&o.From, "from", "", "Start of time range (RFC3339, e.g. 2026-01-01T00:00:00Z)")
	flags.StringVar(&o.To, "to", "", "End of time range (RFC3339, e.g. 2026-12-31T23:59:59Z)")
	flags.IntVar(&o.PageSize, "page-size", 50, "Number of results per page")
}

func newSearchCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &searchOpts{}
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search conversations with filters.",
		Long: `Search conversations using filter expressions and time ranges.

Defaults to the last 24 hours. Use --from and --to for custom ranges (both required).

Filter syntax: key operator "value" (multiple filters separated by spaces).

Filter keys (trace): model, provider, agent, agent.version, status,
  error.type, error.category, duration, tool.name, operation, namespace, cluster, service
Filter keys (metadata): generation_count, eval.passed, eval.evaluator_id, eval.score_key, eval.score
Operators: =, !=, >, <, >=, <=, =~ (regex)

Returns a single page of results (controlled by --page-size). A warning is
shown when more results are available.`,
		Example: `  gcx agento11y conversations search --filters 'agent = "claude-code"'
  gcx agento11y conversations search --filters 'agent = "claude-code" model = "claude-opus-4-6"'
  gcx agento11y conversations search --filters 'status = "error"' --from 2026-04-01T00:00:00Z --to 2026-04-02T00:00:00Z`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			tr, err := parseTimeRange(opts.From, opts.To)
			if err != nil {
				return err
			}

			req := SearchRequest{
				Filters:   opts.Filters,
				PageSize:  opts.PageSize,
				TimeRange: tr,
			}

			resp, err := client.Search(cmd.Context(), req)
			if err != nil {
				return err
			}
			if err := opts.IO.Encode(cmd.OutOrStdout(), resp.Conversations); err != nil {
				return err
			}
			if resp.HasMore {
				cmdio.Warning(cmd.ErrOrStderr(), "Results truncated. %d shown, more available. Use --page-size to adjust.", len(resp.Conversations))
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- list table codec (Conversation — upstream fields) ---

type TableCodec struct {
	Wide bool
}

func (c *TableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *TableCodec) Encode(w io.Writer, v any) error {
	convs, ok := v.([]Conversation)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Conversation")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "TITLE", "GENERATIONS", "CREATED", "LAST ACTIVITY")
	} else {
		t = style.NewTable("ID", "TITLE", "GENERATIONS", "LAST ACTIVITY")
	}

	for _, conv := range convs {
		title := aio11yhttp.Truncate(conv.Title, 40)
		lastActivity := aio11yhttp.FormatTime(conv.LastGenerationAt)

		if c.Wide {
			created := aio11yhttp.FormatTime(conv.CreatedAt)
			t.Row(conv.ID, title, strconv.Itoa(conv.GenerationCount), created, lastActivity)
		} else {
			t.Row(conv.ID, title, strconv.Itoa(conv.GenerationCount), lastActivity)
		}
	}
	return t.Render(w)
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- search table codec (SearchResult — plugin fields) ---

type SearchTableCodec struct {
	Wide bool
}

func (c *SearchTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *SearchTableCodec) Encode(w io.Writer, v any) error {
	results, ok := v.([]SearchResult)
	if !ok {
		return errors.New("invalid data type for table codec: expected []SearchResult")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "TITLE", "GENERATIONS", "MODELS", "AGENTS", "ERRORS", "LAST ACTIVITY")
	} else {
		t = style.NewTable("ID", "TITLE", "GENERATIONS", "MODELS", "LAST ACTIVITY")
	}

	for _, r := range results {
		title := aio11yhttp.Truncate(r.ConversationTitle, 40)
		models := strings.Join(r.Models, ", ")
		if models == "" {
			models = "-"
		}
		lastActivity := aio11yhttp.FormatTime(r.LastGenerationAt)

		if c.Wide {
			agents := strings.Join(r.Agents, ", ")
			if agents == "" {
				agents = "-"
			}
			errCount := "-"
			if r.ErrorCount > 0 {
				errCount = strconv.Itoa(r.ErrorCount)
			}
			t.Row(r.ConversationID, title, strconv.Itoa(r.GenerationCount), models, agents, errCount, lastActivity)
		} else {
			t.Row(r.ConversationID, title, strconv.Itoa(r.GenerationCount), models, lastActivity)
		}
	}
	return t.Render(w)
}

func (c *SearchTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- annotations ---

type annotationsListOpts struct {
	IO     cmdio.Options
	Limit  int
	Cursor string
}

func (o *annotationsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &AnnotationsTableCodec{})
	o.IO.RegisterCustomCodec("wide", &AnnotationsTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.Limit, "limit", 50, "Number of annotations to request")
	flags.StringVar(&o.Cursor, "cursor", "", "Pagination cursor from a previous response")
}

func (o *annotationsListOpts) Validate() error {
	if o.Limit <= 0 {
		return errors.New("--limit must be greater than 0")
	}
	return o.IO.Validate()
}

func newListAnnotationsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &annotationsListOpts{}
	cmd := &cobra.Command{
		Use:   "list-annotations <conversation-id>",
		Short: "List annotations for a conversation.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			resp, err := client.ListAnnotations(cmd.Context(), args[0], opts.Limit, opts.Cursor)
			if err != nil {
				return err
			}
			if opts.IO.OutputFormat == "table" || opts.IO.OutputFormat == "wide" {
				return opts.IO.Encode(cmd.OutOrStdout(), resp.Items)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type annotationsAddOpts struct {
	IO             cmdio.Options
	AnnotationID   string
	AnnotationType string
	Body           string
	GenerationID   string
	Tags           []string
	MetadataJSON   string
}

func (o *annotationsAddOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.AnnotationID, "annotation-id", "", "Annotation ID; generated when omitted")
	flags.StringVar(&o.AnnotationType, "type", "NOTE", "Annotation type: NOTE, LABEL, TRIAGE_STATUS, ROOT_CAUSE, or FOLLOW_UP")
	flags.StringVar(&o.Body, "body", "", "Annotation body")
	flags.StringVar(&o.GenerationID, "generation-id", "", "Generation ID to attach the annotation to")
	flags.StringArrayVar(&o.Tags, "tag", nil, "Tag in key=value form (repeatable)")
	flags.StringVar(&o.MetadataJSON, "metadata-json", "", "Metadata object as JSON")
}

func (o *annotationsAddOpts) Validate() error {
	if strings.TrimSpace(o.Body) == "" {
		return errors.New("--body is required")
	}
	switch strings.TrimSpace(o.AnnotationType) {
	case "NOTE", "LABEL", "TRIAGE_STATUS", "ROOT_CAUSE", "FOLLOW_UP":
	default:
		return errors.New("--type must be one of NOTE, LABEL, TRIAGE_STATUS, ROOT_CAUSE, or FOLLOW_UP")
	}
	return o.IO.Validate()
}

func newAnnotateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &annotationsAddOpts{}
	cmd := &cobra.Command{
		Use:   "annotate <conversation-id>",
		Short: "Annotate a conversation.",
		Example: `  gcx agento11y conversations annotate conv-123 --body "Needs review"
  gcx agento11y conversations annotate conv-123 --type TRIAGE_STATUS --body "Escalated" --tag status=needs_review`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			tags, err := parseAnnotationTags(opts.Tags)
			if err != nil {
				return err
			}
			metadata, err := parseAnnotationMetadata(opts.MetadataJSON)
			if err != nil {
				return err
			}
			annotationID := strings.TrimSpace(opts.AnnotationID)
			if annotationID == "" {
				annotationID = "ann-" + uuid.NewString()
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			resp, err := client.CreateAnnotation(cmd.Context(), args[0], CreateAnnotationRequest{
				AnnotationID:   annotationID,
				AnnotationType: strings.TrimSpace(opts.AnnotationType),
				Body:           strings.TrimSpace(opts.Body),
				Tags:           tags,
				Metadata:       metadata,
				GenerationID:   strings.TrimSpace(opts.GenerationID),
			})
			if err != nil {
				return err
			}
			cmdio.Success(cmd.ErrOrStderr(), "Added annotation %s", resp.Annotation.AnnotationID)
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func parseAnnotationTags(raw []string) (map[string]string, error) {
	tags := make(map[string]string, len(raw))
	if len(raw) == 0 {
		return tags, nil
	}
	for _, t := range raw {
		k, v, ok := strings.Cut(t, "=")
		if !ok {
			return nil, fmt.Errorf("invalid --tag %q: expected key=value", t)
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" {
			return nil, fmt.Errorf("invalid --tag %q: empty key", t)
		}
		tags[k] = v
	}
	return tags, nil
}

func parseAnnotationMetadata(raw string) (map[string]any, error) {
	metadata := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return metadata, nil
	}
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return nil, fmt.Errorf("invalid --metadata-json: %w", err)
	}
	if metadata == nil {
		return nil, errors.New("--metadata-json must be a JSON object")
	}
	return metadata, nil
}

// AnnotationsTableCodec renders conversation annotations.
type AnnotationsTableCodec struct {
	Wide bool
}

func (c *AnnotationsTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *AnnotationsTableCodec) Encode(w io.Writer, v any) error {
	annotations, ok := v.([]ConversationAnnotation)
	if !ok {
		return errors.New("invalid data type for table codec: expected []ConversationAnnotation")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "TYPE", "BODY", "TAGS", "OPERATOR", "GENERATION", "CREATED")
	} else {
		t = style.NewTable("ID", "TYPE", "BODY", "OPERATOR", "CREATED")
	}

	for _, annotation := range annotations {
		body := aio11yhttp.Truncate(annotation.Body, 56)
		operator := firstNonEmpty(annotation.OperatorName, annotation.OperatorLogin, annotation.OperatorID, "-")
		created := aio11yhttp.FormatTime(annotation.CreatedAt)
		if c.Wide {
			t.Row(annotation.AnnotationID, annotation.AnnotationType, body, formatTags(annotation.Tags), operator, firstNonEmpty(annotation.GenerationID, "-"), created)
		} else {
			t.Row(annotation.AnnotationID, annotation.AnnotationType, body, operator, created)
		}
	}
	return t.Render(w)
}

func (c *AnnotationsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

func formatTags(tags map[string]string) string {
	if len(tags) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(tags))
	for k, v := range tags {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func parseTimeRange(from, to string) (*SearchTimeRange, error) {
	if from == "" && to == "" {
		// Default to last 24 hours — Agent Observability requires both bounds.
		now := time.Now().UTC()
		return &SearchTimeRange{From: now.Add(-24 * time.Hour), To: now}, nil
	}
	if from == "" || to == "" {
		return nil, errors.New("both --from and --to are required (Agent Observability requires a complete time range)")
	}
	fromT, err := time.Parse(time.RFC3339, from)
	if err != nil {
		return nil, fmt.Errorf("invalid --from value: %w", err)
	}
	toT, err := time.Parse(time.RFC3339, to)
	if err != nil {
		return nil, fmt.Errorf("invalid --to value: %w", err)
	}
	if !fromT.Before(toT) {
		return nil, errors.New("--from must be before --to")
	}
	return &SearchTimeRange{From: fromT, To: toT}, nil
}
