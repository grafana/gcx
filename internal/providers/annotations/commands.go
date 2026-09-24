package annotations

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/shared"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// clientFromLoader loads the REST config and builds an annotations Client. Used
// by commands (tags, mass-delete) that operate outside the TypedCRUD adapter.
func clientFromLoader(ctx context.Context, loader RESTConfigLoader) (*Client, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load REST config for annotations: %w", err)
	}
	return NewClient(cfg)
}

// ---- list ----

type listOpts struct {
	IO    cmdio.Options
	Since string
	From  string
	To    string
	Tags  []string
	Limit int
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ListTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Since, "since", "24h", "Time window (e.g. 24h or 7d); cannot be used with --from/--to")
	flags.StringVar(&o.From, "from", "", "Start time (RFC3339, Unix seconds, or now-1h); empty is invalid")
	flags.StringVar(&o.To, "to", "", "End time (RFC3339, Unix seconds, or now); empty is invalid")
	flags.StringSliceVar(&o.Tags, "tags", nil, "Filter by tags (comma-separated or repeated)")
	flags.IntVar(&o.Limit, "limit", 100, "Maximum results to return (0 = server default of 100)")
}

// resolveRange resolves the time flags before any request.
func (o *listOpts) resolveRange(cmd *cobra.Command) (int64, int64, error) {
	flags := cmd.Flags()
	fromChanged, toChanged := flags.Changed("from"), flags.Changed("to")
	if flags.Changed("since") && (fromChanged || toChanged) {
		return 0, 0, errors.New("--since cannot be used together with --from or --to")
	}
	now := time.Now()
	if fromChanged || toChanged {
		var from int64
		to := now.UnixMilli()
		for _, bound := range []struct {
			name, value string
			changed     bool
			target      *int64
		}{{"from", o.From, fromChanged, &from}, {"to", o.To, toChanged, &to}} {
			if !bound.changed {
				continue
			}
			if strings.TrimSpace(bound.value) == "" {
				return 0, 0, fmt.Errorf("--%s cannot be empty; use RFC3339, Unix seconds, or now-1h", bound.name)
			}
			t, err := shared.ParseTime(bound.value, now)
			if err != nil {
				return 0, 0, fmt.Errorf("invalid --%s %q: %w", bound.name, bound.value, err)
			}
			*bound.target = t.UnixMilli()
		}
		return from, to, nil
	}
	duration, err := shared.ParseDuration(o.Since)
	if err != nil || duration <= 0 {
		return 0, 0, fmt.Errorf("invalid --since %q: use a positive duration, for example --since 24h", o.Since)
	}
	return now.Add(-duration).UnixMilli(), now.UnixMilli(), nil
}

func (o *listOpts) Validate() error { return o.IO.Validate() }

func newListCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List annotations (last 24h by default).",
		Long: `List annotations from Grafana.

By default, returns only annotations from the last 24 hours. Use --since
to widen the window, or --from/--to for an explicit time range (RFC3339, Unix seconds, or relative time).`,
		Example: "  gcx annotations list\n" +
			"  gcx annotations list --since 168h\n" +
			"  gcx annotations list --tags deploy,prod\n" +
			"  gcx annotations list --limit 20",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			from, to, err := opts.resolveRange(cmd)
			if err != nil {
				return err
			}
			if opts.Limit < 0 || from < 0 || to < 0 || (to > 0 && from > to) {
				return errors.New("invalid annotation time range or limit")
			}

			ctx := cmd.Context()

			base := ListOptions{
				From:  from,
				To:    to,
				Tags:  opts.Tags,
				Limit: opts.Limit,
			}

			crud, _, err := newCommandCRUD(ctx, loader, base)
			if err != nil {
				return err
			}

			// The per-call limit in TypedCRUD.List is a client-side cap on
			// TypedObject results. The server-side limit is already carried by
			// base.Limit, so we pass 0 here to avoid double-truncation.
			typedObjs, err := crud.List(ctx, 0)
			if err != nil {
				return err
			}
			effectiveLimit := opts.Limit
			if effectiveLimit == 0 {
				effectiveLimit = 100
			}
			if len(typedObjs) >= effectiveLimit {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: results may be incomplete at limit %d; increase --limit or narrow the time range\n", effectiveLimit)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), typedObjs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ListTableCodec renders annotations as a tabular table.
// It accepts []adapter.TypedObject[Annotation] and extracts .Spec for each row.
type ListTableCodec struct{}

// Format returns the output format name.
func (c *ListTableCodec) Format() format.Format { return "table" }

// Encode writes annotations to the writer as a table.
func (c *ListTableCodec) Encode(w io.Writer, v any) error {
	typedObjs, ok := v.([]adapter.TypedObject[Annotation])
	if !ok {
		return errors.New("invalid data type for table codec: expected []TypedObject[Annotation]")
	}

	t := style.NewTable("ID", "TIME", "DASHBOARD", "TAGS", "TEXT")
	for _, obj := range typedObjs {
		a := obj.Spec
		ts := ""
		if a.Time > 0 {
			ts = time.Unix(a.Time/1000, 0).UTC().Format(time.RFC3339)
		}
		t.Row(
			strconv.FormatInt(a.ID, 10),
			ts,
			a.DashboardUID,
			strings.Join(a.Tags, ","),
			truncate(a.Text, 60),
		)
	}
	return t.Render(w)
}

// Decode is not supported for the table format.
func (c *ListTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

func truncate(s string, maxLen int) string {
	if len([]rune(s)) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return string([]rune(s)[:maxLen])
	}
	return string([]rune(s)[:maxLen-1]) + "…"
}

// ---- get ----

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ListTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func (o *getOpts) Validate() error { return o.IO.Validate() }

func newGetCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:     "get ID",
		Short:   "Get an annotation by ID.",
		Example: "  gcx annotations get 1",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			// Validate the ID up front so users get a clear error before we
			// open a client connection.
			if err := validateID(args[0]); err != nil {
				return err
			}

			ctx := cmd.Context()

			crud, _, err := newCommandCRUD(ctx, loader, ListOptions{})
			if err != nil {
				return err
			}

			typedObj, err := crud.Get(ctx, args[0])
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), []adapter.TypedObject[Annotation]{*typedObj})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---- create ----

type createOpts struct {
	IO   cmdio.Options
	File string
}

func (o *createOpts) Validate() error { return o.IO.Validate() }

func (o *createOpts) setup(flags *pflag.FlagSet) {
	setupMutationOutput(&o.IO, flags, func(m cmdio.SingleMutation) string { return "Annotation created id=" + m.Target.ID })
	flags.StringVarP(&o.File, "file", "f", "", "JSON file containing the annotation (use - for stdin)")
}

func newCreateCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:     "create",
		Short:   "Create an annotation from a JSON file.",
		Example: "  gcx annotations create -f annotation.json\n  cat annotation.json | gcx annotations create -f -",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if opts.File == "" {
				return errors.New("--file is required")
			}

			a, err := readAnnotationFromFile(cmd.InOrStdin(), opts.File)
			if err != nil {
				return err
			}
			if a.Text == "" {
				return errors.New("annotation text is required")
			}

			ctx := cmd.Context()

			crud, restCfg, err := newCommandCRUD(ctx, loader, ListOptions{})
			if err != nil {
				return err
			}

			typedObj := &adapter.TypedObject[Annotation]{Spec: *a}
			typedObj.SetName(a.GetResourceName())
			typedObj.SetNamespace(restCfg.Namespace)

			created, err := crud.Create(ctx, typedObj)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), cmdio.NewSingleMutation("created", cmdio.MutationTarget{Kind: "annotation", ID: strconv.FormatInt(created.Spec.ID, 10)}))
		},
	}
	opts.setup(cmd.Flags())
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

// ---- update ----

type updateOpts struct {
	IO   cmdio.Options
	File string
}

func (o *updateOpts) Validate() error { return o.IO.Validate() }

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	setupMutationOutput(&o.IO, flags, func(m cmdio.SingleMutation) string { return "Annotation updated id=" + m.Target.ID })
	flags.StringVarP(&o.File, "file", "f", "", "JSON file containing the patch (use - for stdin)")
}

func newUpdateCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:     "update ID",
		Short:   "Update an annotation from a JSON file (PATCH).",
		Example: "  gcx annotations update 1 -f patch.json",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := validateID(args[0]); err != nil {
				return err
			}
			if opts.File == "" {
				return errors.New("--file is required")
			}

			var patch map[string]any
			if err := providers.ReadFileOrStdin(opts.File, cmd.InOrStdin(), &patch); err != nil {
				return err
			}
			if len(patch) == 0 {
				return errors.New("annotation patch must be a non-empty JSON object")
			}
			delete(patch, "id")
			if len(patch) == 0 {
				return errors.New("annotation patch must contain a field other than id")
			}
			ctx := cmd.Context()
			client, err := clientFromLoader(ctx, loader)
			if err != nil {
				return err
			}
			id, _ := strconv.ParseInt(args[0], 10, 64)
			if err := client.Update(ctx, id, patch); err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), cmdio.NewSingleMutation("updated", cmdio.MutationTarget{Kind: "annotation", ID: args[0]}))
		},
	}
	opts.setup(cmd.Flags())
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

// ---- delete ----

type deleteOpts struct {
	IO    cmdio.Options
	Force bool
}

func (o *deleteOpts) Validate() error { return o.IO.Validate() }
func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	setupMutationOutput(&o.IO, flags, func(m cmdio.SingleMutation) string { return "Annotation deleted id=" + m.Target.ID })
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
}

func newDeleteCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:     "delete ID",
		Short:   "Delete an annotation by ID.",
		Example: "  gcx annotations delete 1",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := validateID(args[0]); err != nil {
				return err
			}
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force, fmt.Sprintf("Delete annotation %s?", args[0]))
			if err != nil {
				return err
			}
			if !proceed {
				code := gcxerrors.ExitCancelled
				return gcxerrors.DetailedError{Summary: "delete cancelled", ExitCode: &code}
			}

			ctx := cmd.Context()

			crud, _, err := newCommandCRUD(ctx, loader, ListOptions{})
			if err != nil {
				return err
			}

			if err := crud.Delete(ctx, args[0]); err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), cmdio.NewSingleMutation("deleted", cmdio.MutationTarget{Kind: "annotation", ID: args[0]}))
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---- tags ----

type tagsOpts struct {
	IO    cmdio.Options
	Limit int
}

func (o *tagsOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TagsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.Limit, "limit", 100, "Maximum tags to return")
}

func (o *tagsOpts) Validate() error { return o.IO.Validate() }

func newTagsCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &tagsOpts{}
	cmd := &cobra.Command{
		Use:     "list-tags",
		Short:   "List annotation tags with usage counts.",
		Example: "  gcx annotations list-tags",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if opts.Limit <= 0 {
				return errors.New("--limit must be positive")
			}

			ctx := cmd.Context()
			client, err := clientFromLoader(ctx, loader)
			if err != nil {
				return err
			}

			tags, err := client.Tags(ctx, opts.Limit)
			if err != nil {
				return err
			}
			if len(tags) >= opts.Limit {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: tags may be incomplete at limit %d; increase --limit\n", opts.Limit)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), tags)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// TagsTableCodec renders annotation tags as a table.
type TagsTableCodec struct{}

// Format returns the output format name.
func (c *TagsTableCodec) Format() format.Format { return "table" }

// Encode writes annotation tags to the writer as a table.
func (c *TagsTableCodec) Encode(w io.Writer, v any) error {
	tags, ok := v.([]AnnotationTag)
	if !ok {
		return errors.New("invalid data type for table codec: expected []AnnotationTag")
	}
	t := style.NewTable("TAG", "COUNT")
	for _, tag := range tags {
		t.Row(tag.Tag, strconv.FormatInt(tag.Count, 10))
	}
	return t.Render(w)
}

// Decode is not supported for the table format.
func (c *TagsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---- mass-delete ----

type massDeleteOpts struct {
	IO           cmdio.Options
	DashboardUID string
	DashboardID  int64
	PanelID      int64
	Force        bool
}

func (o *massDeleteOpts) Validate() error { return o.IO.Validate() }

func (o *massDeleteOpts) setup(flags *pflag.FlagSet) {
	setupMutationOutput(&o.IO, flags, func(cmdio.SingleMutation) string { return "Annotations deleted" })
	flags.StringVar(&o.DashboardUID, "dashboard-uid", "", "Dashboard UID (with --panel-id)")
	flags.Int64Var(&o.DashboardID, "dashboard-id", 0, "Dashboard numeric ID (with --panel-id)")
	flags.Int64Var(&o.PanelID, "panel-id", 0, "Panel ID (with a dashboard selector)")
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
}

func newMassDeleteCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &massDeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete-many",
		Short: "Delete annotations for a dashboard panel.",
		Long: `Delete multiple annotations in a single request.

Provide one dashboard selector (--dashboard-uid or --dashboard-id)
together with --panel-id.`,
		Example: "  gcx annotations delete-many --dashboard-uid abcdef123 --panel-id 3",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			opts.DashboardUID = strings.TrimSpace(opts.DashboardUID)
			req := MassDeleteRequest{
				DashboardUID: opts.DashboardUID,
				DashboardID:  opts.DashboardID,
				PanelID:      opts.PanelID,
			}

			hasDashboard := strings.TrimSpace(opts.DashboardUID) != "" || opts.DashboardID > 0
			switch {
			case hasDashboard && opts.PanelID > 0 && opts.DashboardID >= 0 && (opts.DashboardUID == "" || opts.DashboardID == 0):
				// delete all annotations for a dashboard panel
			default:
				return errors.New("provide one dashboard selector (--dashboard-uid/--dashboard-id) and a positive --panel-id")
			}
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force, "Delete selected annotations?")
			if err != nil {
				return err
			}
			if !proceed {
				code := gcxerrors.ExitCancelled
				return gcxerrors.DetailedError{Summary: "delete cancelled", ExitCode: &code}
			}

			ctx := cmd.Context()
			client, err := clientFromLoader(ctx, loader)
			if err != nil {
				return err
			}

			if err := client.MassDelete(ctx, req); err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), cmdio.NewSingleMutation("deleted", cmdio.MutationTarget{Kind: "annotations", Name: "selection"}))
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---- helpers ----

func validateID(s string) error {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid annotation ID %q: %w", s, err)
	}
	if id <= 0 {
		return fmt.Errorf("invalid annotation ID %q: must be positive", s)
	}
	return nil
}

// readAnnotationFromFile reads the given path (or stdin for "-") and parses it
// as a JSON Annotation spec.
func readAnnotationFromFile(stdin io.Reader, path string) (*Annotation, error) {
	var a Annotation
	if err := providers.ReadFileOrStdin(path, stdin, &a); err != nil {
		return nil, fmt.Errorf("failed to parse annotation: %w", err)
	}
	return &a, nil
}
