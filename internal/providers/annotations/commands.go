package annotations

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const defaultLookback = 24 * time.Hour

// ---- list ----

type listOpts struct {
	IO       cmdio.Options
	Lookback time.Duration
	From     int64
	To       int64
	Tags     []string
	Limit    int
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ListTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.DurationVar(&o.Lookback, "lookback", defaultLookback, "Lookback duration (e.g. 24h, 48h, 7d); ignored if --from/--to are set")
	flags.Int64Var(&o.From, "from", 0, "Start time in epoch milliseconds")
	flags.Int64Var(&o.To, "to", 0, "End time in epoch milliseconds")
	flags.StringSliceVar(&o.Tags, "tags", nil, "Filter by tags (comma-separated or repeated)")
	flags.IntVar(&o.Limit, "limit", 100, "Maximum results to return (0 = unlimited)")
}

// resolveRange returns the from/to window based on the user's flags.
// --from/--to override --lookback; when neither is set, --lookback applies
// ending at now.
func (o *listOpts) resolveRange(cmd *cobra.Command) (int64, int64, error) {
	flags := cmd.Flags()
	fromChanged := flags.Changed("from")
	toChanged := flags.Changed("to")

	if flags.Changed("lookback") && (fromChanged || toChanged) {
		return 0, 0, errors.New("--lookback cannot be used together with --from or --to")
	}

	if fromChanged || toChanged {
		return o.From, o.To, nil
	}

	now := time.Now()
	return now.Add(-o.Lookback).UnixMilli(), now.UnixMilli(), nil
}

func newListCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List annotations (last 24h by default).",
		Long: `List annotations from Grafana.

By default, returns only annotations from the last 24 hours. Use --lookback
to widen the window, or --from/--to for an explicit time range (epoch ms).`,
		Example: "  gcx annotations list\n" +
			"  gcx annotations list --lookback 168h\n" +
			"  gcx annotations list --tags deploy,prod\n" +
			"  gcx annotations list --limit 20",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			from, to, err := opts.resolveRange(cmd)
			if err != nil {
				return err
			}

			ctx := cmd.Context()

			base := ListOptions{
				From:  from,
				To:    to,
				Tags:  opts.Tags,
				Limit: opts.Limit,
			}

			crud, _, err := NewTypedCRUD(ctx, loader, base)
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
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return s[:maxLen]
	}
	return s[:maxLen-1] + "…"
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

func newGetCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:     "get ID",
		Short:   "Get an annotation by ID.",
		Example: "  gcx annotations get 1",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			// Validate the ID up front so users get a clear error before we
			// open a client connection.
			if err := validateID(args[0]); err != nil {
				return err
			}

			ctx := cmd.Context()

			crud, _, err := NewTypedCRUD(ctx, loader, ListOptions{})
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
	File string
}

func newCreateCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:     "create",
		Short:   "Create an annotation from a JSON file.",
		Example: "  gcx annotations create -f annotation.json\n  cat annotation.json | gcx annotations create -f -",
		RunE: func(cmd *cobra.Command, _ []string) error {
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

			crud, restCfg, err := NewTypedCRUD(ctx, loader, ListOptions{})
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

			cmdio.Success(cmd.OutOrStdout(), "Annotation created id=%d", created.Spec.ID)
			return nil
		},
	}
	cmd.Flags().StringVarP(&opts.File, "file", "f", "", "JSON file containing the annotation (use - for stdin)")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

// ---- update ----

type updateOpts struct {
	File string
}

func newUpdateCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:     "update ID",
		Short:   "Update an annotation from a JSON file (PATCH).",
		Example: "  gcx annotations update 1 -f patch.json",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateID(args[0]); err != nil {
				return err
			}
			if opts.File == "" {
				return errors.New("--file is required")
			}

			a, err := readAnnotationFromFile(cmd.InOrStdin(), opts.File)
			if err != nil {
				return err
			}

			ctx := cmd.Context()

			crud, restCfg, err := NewTypedCRUD(ctx, loader, ListOptions{})
			if err != nil {
				return err
			}

			typedObj := &adapter.TypedObject[Annotation]{Spec: *a}
			typedObj.SetName(args[0])
			typedObj.SetNamespace(restCfg.Namespace)

			if _, err := crud.Update(ctx, args[0], typedObj); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Annotation updated id=%s", args[0])
			return nil
		},
	}
	cmd.Flags().StringVarP(&opts.File, "file", "f", "", "JSON file containing the patch (use - for stdin)")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

// ---- delete ----

func newDeleteCommand(loader RESTConfigLoader) *cobra.Command {
	return &cobra.Command{
		Use:     "delete ID",
		Short:   "Delete an annotation by ID.",
		Example: "  gcx annotations delete 1",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateID(args[0]); err != nil {
				return err
			}

			ctx := cmd.Context()

			crud, _, err := NewTypedCRUD(ctx, loader, ListOptions{})
			if err != nil {
				return err
			}

			if err := crud.Delete(ctx, args[0]); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Annotation deleted id=%s", args[0])
			return nil
		},
	}
}

// ---- helpers ----

func validateID(s string) error {
	if _, err := strconv.ParseInt(s, 10, 64); err != nil {
		return fmt.Errorf("invalid annotation ID %q: %w", s, err)
	}
	return nil
}

// readAnnotationFromFile reads the given path (or stdin for "-") and parses it
// as a JSON Annotation spec.
func readAnnotationFromFile(stdin io.Reader, path string) (*Annotation, error) {
	data, err := readFileOrStdin(stdin, path)
	if err != nil {
		return nil, err
	}
	var a Annotation
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("failed to parse annotation: %w", err)
	}
	return &a, nil
}

// readFileOrStdin reads the given path, or stdin when path is "-".
func readFileOrStdin(stdin io.Reader, path string) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to read stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %q: %w", path, err)
	}
	return data, nil
}
