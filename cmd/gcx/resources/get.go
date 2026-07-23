package resources

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/deeplink"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/terminal"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/cli-runtime/pkg/printers"
)

// printFieldDiscoveryResults writes the sorted field paths from a sample
// resource to out, one per line, for --json ? discovery.
func printFieldDiscoveryResults(out io.Writer, obj map[string]any) {
	for _, field := range cmdio.DiscoverFields(obj) {
		fmt.Fprintln(out, field)
	}
}

// schemaToFieldPaths converts an OpenAPI spec schema to field paths compatible
// with the --json ? output format (top-level keys + spec.* sub-fields).
func schemaToFieldPaths(specSchema map[string]any) []string {
	// Always include the standard K8s envelope fields plus the deep link URL.
	paths := []string{"apiVersion", "kind", "metadata", "spec", "status", "url"}

	if props, ok := specSchema["properties"].(map[string]any); ok {
		for key := range props {
			paths = append(paths, "spec."+key)
		}
	}

	sort.Strings(paths)
	return paths
}

// discoverFieldsViaOpenAPI resolves field paths for a resource type using the
// OpenAPI v3 schema endpoint. Returns an error if the schema is unavailable
// (e.g. provider-backed resources), in which case the caller should fall back
// to sample-fetch introspection.
func discoverFieldsViaOpenAPI(ctx context.Context, cfg config.NamespacedRESTConfig, args []string) ([]string, error) {
	sels, err := resources.ParseSelectors(args)
	if err != nil {
		return nil, err
	}

	reg, err := discovery.NewDefaultRegistry(ctx, cfg)
	if err != nil {
		return nil, err
	}

	filters, err := reg.MakeFilters(discovery.MakeFiltersOptions{
		Selectors:            sels,
		PreferredVersionOnly: true,
	})
	if err != nil {
		return nil, err
	}

	if len(filters) == 0 {
		return nil, errors.New("no matching resource types")
	}

	// Use the first filter's descriptor.
	desc := filters[0].Descriptor
	descs := resources.Descriptors{desc}

	fetcher, err := discovery.NewSchemaFetcher(&cfg.Config)
	if err != nil {
		return nil, err
	}

	schemas, err := fetcher.FetchSpecSchemas(ctx, descs)
	if err != nil {
		return nil, err
	}

	key := desc.GroupVersion.Group + "/" + desc.GroupVersion.Version + "/" + desc.Kind
	specSchema, ok := schemas[key]
	if !ok {
		return nil, fmt.Errorf("no OpenAPI schema for %s", key)
	}

	return schemaToFieldPaths(specSchema), nil
}

// defaultListLimit is the default number of items returned per resource type.
// Use --limit=0 to fetch all items.
const defaultListLimit = 50

type getOpts struct {
	IO      cmdio.Options
	OnError OnErrorMode
	Limit   int64
	Open    bool
}

func (opts *getOpts) setup(flags *pflag.FlagSet) {
	// Setup some additional formatting options
	bindOnErrorFlag(flags, &opts.OnError)
	opts.IO.RegisterCustomCodec("text", &tableCodec{wide: false})
	opts.IO.RegisterCustomCodec("wide", &tableCodec{wide: true})
	opts.IO.DefaultFormat("text")

	flags.Int64Var(&opts.Limit, "limit", defaultListLimit, "Maximum number of items to fetch per resource type (0 for all)")

	// Bind all the flags
	opts.IO.BindFlags(flags)
	flags.BoolVar(&opts.Open, "open", false, "Open the resource in the default browser")
}

func (opts *getOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}

	if opts.Limit < 0 {
		return errors.New("--limit must be a non-negative integer")
	}

	return opts.OnError.Validate()
}

func getCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &getOpts{}

	cmd := &cobra.Command{
		Use:   "get [RESOURCE_SELECTOR]...",
		Args:  cobra.ArbitraryArgs,
		Short: "Get resources from Grafana",
		Long:  "Get resources from Grafana using a specific format. See examples below for more details.",
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "large",
			agent.AnnotationLLMHint:   "dashboards/my-uid -o json",
		},
		Example: `
	# Everything:

	gcx resources get dashboards/foo

	# All instances for a given kind(s):

	gcx resources get dashboards
	gcx resources get dashboards folders

	# Single resource kind, one or more resource instances:

	gcx resources get dashboards/foo
	gcx resources get dashboards/foo,bar

	# Single resource kind, long kind format:

	gcx resources get dashboard.dashboards/foo
	gcx resources get dashboard.dashboards/foo,bar

	# Single resource kind, long kind format with version:

	gcx resources get dashboards.v1alpha1.dashboard.grafana.app/foo
	gcx resources get dashboards.v1alpha1.dashboard.grafana.app/foo,bar

	# Multiple resource kinds, one or more resource instances:

	gcx resources get dashboards/foo folders/qux
	gcx resources get dashboards/foo,bar folders/qux,quux

	# Multiple resource kinds, long kind format:

	gcx resources get dashboard.dashboards/foo folder.folders/qux
	gcx resources get dashboard.dashboards/foo,bar folder.folders/qux,quux

	# Multiple resource kinds, long kind format with version:

	gcx resources get dashboards.v1alpha1.dashboard.grafana.app/foo folders.v1alpha1.folder.grafana.app/qux

	# Provider-backed resource types (SLO, Synthetic Monitoring, Alerting):

	gcx resources get slo
	gcx resources get slo/my-slo-uuid
	gcx resources get checks
	gcx resources get rules

	# Discover available JSON fields for a resource type:

	gcx resources get dashboards --json list

	# Select specific fields (no external parsing needed):

	gcx resources get dashboards --json metadata.name,spec.title`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			// FR-007: --json ? requires a resource selector to know which resource type to introspect.
			if opts.IO.JSONDiscovery && len(args) == 0 {
				return errors.New("--json field discovery requires a resource selector argument (e.g. gcx resources get dashboards --json list)")
			}

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			// --json ? discovery: try OpenAPI schema first (no instances needed),
			// fall back to fetching a sample resource if OpenAPI is unavailable.
			if opts.IO.JSONDiscovery {
				fields, schemaErr := discoverFieldsViaOpenAPI(ctx, cfg, args)
				if schemaErr == nil {
					for _, f := range fields {
						fmt.Fprintln(cmd.OutOrStdout(), f)
					}
					return nil
				}
				// Fall through to sample-fetch approach.
			}

			fetchReq := FetchRequest{
				Config:      cfg,
				StopOnError: opts.OnError.StopOnError(),
				Limit:       opts.Limit,
			}
			// --json ? only needs one resource for field introspection; avoid
			// a full list operation to satisfy NC-005.
			if opts.IO.JSONDiscovery {
				fetchReq.Limit = 1
			}
			res, err := FetchResources(ctx, fetchReq, args)
			if err != nil {
				return err
			}

			output := res.Resources.ToUnstructuredList()
			resources.SortUnstructured(output.Items)

			// Inject deep link URLs into each resource.
			deeplink.InjectURLs(output.Items, cfg.GrafanaURL)

			// --json ? discovery fallback: print fields from a fetched sample.
			if opts.IO.JSONDiscovery {
				if len(output.Items) == 0 {
					return errors.New("no resources found for field discovery: provide a selector that matches at least one resource")
				}
				printFieldDiscoveryResults(cmd.OutOrStdout(), output.Items[0].Object)
				return nil
			}

			// --open: open the resource in the default browser.
			if opts.Open {
				if !res.IsSingleTarget || len(output.Items) != 1 {
					return errors.New("--open requires exactly one resource (e.g. gcx resources get dashboards/my-uid --open)")
				}
				url, _ := output.Items[0].Object["url"].(string)
				if url == "" {
					return fmt.Errorf("no deep link URL available for %s/%s", output.Items[0].GetKind(), output.Items[0].GetName())
				}
				cmdio.Info(cmd.ErrOrStderr(), "Opening %s", url)
				return deeplink.Open(url)
			}

			return writeGetOutput(cmd.OutOrStdout(), cmd.ErrOrStderr(), opts, res, output)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

// writeGetOutput renders the fetched resources to stdout in the resolved
// output mode (--json field selection, single object, list envelope, or
// table) and surfaces the truncation hint on stderr. Split from RunE so the
// output path is testable without a live server.
func writeGetOutput(stdout, stderr io.Writer, opts *getOpts, res *FetchResponse, output unstructured.UnstructuredList) error {
	// --json field1,field2: use FieldSelectCodec for output. The truncation
	// hint must fire on this path too — field-selected output is truncated by
	// the same per-resource-type limit as every other mode — but, as on the
	// path below, only after a successful encode.
	if len(opts.IO.JSONFields) > 0 {
		err := writeFieldSelect(stdout, stderr, opts, res, output)
		// The truncation hint fires whenever the document was written
		// successfully — including the partial-failure case, which returns
		// an EmittedError precisely because the document is complete.
		var emitted *gcxerrors.EmittedError
		if err == nil || errors.As(err, &emitted) {
			emitGetTruncationHint(stderr, opts, res)
		}
		return err
	}

	// Agent-mode partial failure on a JSON-family format: fuse items and
	// error into ONE gcx.partial_result document, exactly like the
	// field-select path — the failure must be readable from stdout alone
	// (Constitution: an agent never needs both streams). Non-JSON formats
	// (explicit -o yaml/text/wide) keep their shape and surface the
	// failure via the typed stderr diagnostic + exit 4 below. An active
	// --jq is treated the same way: the fused envelope would silently drop
	// the user's transformation and change the document shape between
	// success and partial-failure runs, so jq output keeps its shape and
	// the failure travels via stderr + exit 4.
	hasPartialFailure := opts.OnError.FailOnErrors() && res.PullSummary.FailedCount() > 0
	if hasPartialFailure && agent.IsAgentMode() && !opts.IO.JQActive() &&
		(opts.IO.OutputFormat == "agents" || opts.IO.OutputFormat == "json") {
		itemMaps := make([]map[string]any, len(output.Items))
		for i, item := range output.Items {
			itemMaps[i] = item.Object
		}
		errSummary := fmt.Sprintf("%d resource(s) failed to get", res.PullSummary.FailedCount())
		detErr := gcxerrors.DetailedError{Summary: errSummary}
		if err := detErr.WriteJSONWithItems(stdout, gcxerrors.ExitPartialFailure, itemMaps); err != nil {
			return err
		}
		emitGetTruncationHint(stderr, opts, res)
		return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, errors.New(errSummary))
	}

	var encodeErr error
	if opts.IO.OutputFormat != "text" && opts.IO.OutputFormat != "wide" {
		// Avoid printing a list of results if a single resource is being pulled,
		// and we are not using the table output format.
		if res.IsSingleTarget && len(output.Items) == 1 {
			encodeErr = opts.IO.Encode(stdout, output.Items[0].Object)
		} else {
			// For JSON / YAML output we don't want to have "object" keys in the output,
			// so use the custom printItems type instead.
			formatted := printItems{
				Items: make([]map[string]any, len(output.Items)),
			}
			for i, item := range output.Items {
				formatted.Items[i] = item.Object
			}
			encodeErr = opts.IO.Encode(stdout, formatted)
		}
	} else {
		encodeErr = opts.IO.Encode(stdout, output)
	}

	if encodeErr != nil {
		return encodeErr
	}

	emitGetTruncationHint(stderr, opts, res)

	if opts.OnError.FailOnErrors() && res.PullSummary.FailedCount() > 0 {
		return partialGetFailure(stderr, res)
	}

	return nil
}

// partialGetFailure reports a partial fetch after the result document was
// already written: a typed stderr diagnostic (JSONL in agent mode, prose on
// a TTY) plus an EmittedError so the process exits ExitPartialFailure
// without a second stdout document. Previously this path returned a bare
// error — exit 1 instead of the taxonomy's 4, and a duplicate error JSON
// appended to stdout in agent mode.
func partialGetFailure(stderr io.Writer, res *FetchResponse) error {
	summary := fmt.Sprintf("%d resource(s) failed to get", res.PullSummary.FailedCount())
	cmdio.EmitWarn(stderr, summary)
	return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, errors.New(summary))
}

// emitGetTruncationHint surfaces the per-resource-type truncation hint on
// stderr. K8s per-resource-type paging: the limit applies to each resource
// type independently, so this is not a list_meta envelope case — a typed
// hint keeps it agent-legible (JSONL {"class":"hint"} in agent mode,
// "hint: ..." on a TTY).
func emitGetTruncationHint(stderr io.Writer, opts *getOpts, res *FetchResponse) {
	if res == nil || res.PullSummary == nil || !res.PullSummary.IsTruncated() {
		return
	}
	cmdio.EmitHint(stderr,
		fmt.Sprintf("showing first %d items per resource type; use --limit=0 to fetch all", opts.Limit),
		"")
}

// writeFieldSelect handles --json field1,field2 output for the get command.
// It uses FieldSelectCodec to emit only selected fields, and emits a combined
// {"items": [...], "error": {...}} envelope (FR-012) on partial failure in agent mode.
func writeFieldSelect(out, stderr io.Writer, opts *getOpts, res *FetchResponse, output unstructured.UnstructuredList) error {
	codec := cmdio.NewFieldSelectCodec(opts.IO.JSONFields)
	hasPartialFailure := opts.OnError.FailOnErrors() && res.PullSummary.FailedCount() > 0

	// FR-012: when agent mode is active and there are partial failures,
	// write a single combined {"items": [...], "error": {...}} envelope.
	// The envelope is the complete result document, so the process exit
	// code (ExitPartialFailure) travels via EmittedError — previously this
	// path returned nil and the process exited 0 despite the embedded
	// exitCode 4.
	if hasPartialFailure && agent.IsAgentMode() {
		itemMaps := make([]map[string]any, len(output.Items))
		for i, item := range output.Items {
			itemMaps[i] = cmdio.ExtractFields(item.Object, codec.Fields())
		}
		errSummary := fmt.Sprintf("%d resource(s) failed to get", res.PullSummary.FailedCount())
		detErr := gcxerrors.DetailedError{Summary: errSummary}
		if err := detErr.WriteJSONWithItems(out, gcxerrors.ExitPartialFailure, itemMaps); err != nil {
			return err
		}
		return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, errors.New(errSummary))
	}

	var encodeErr error
	if res.IsSingleTarget && len(output.Items) == 1 {
		encodeErr = codec.Encode(out, output.Items[0])
	} else {
		encodeErr = codec.Encode(out, output)
	}
	if encodeErr != nil {
		return encodeErr
	}
	if hasPartialFailure {
		return partialGetFailure(stderr, res)
	}
	return nil
}

// hack: unstructured objects are serialized with a top-level "object" key,
// which we don't want, so instead we have a different type for JSON / YAML outputs.
type printItems struct {
	Items []map[string]any `json:"items" yaml:"items"`
}

type tableCodec struct {
	wide bool
}

func (c *tableCodec) Format() format.Format {
	if c.wide {
		return "wide"
	}

	return "text"
}

func (c *tableCodec) Encode(output io.Writer, input any) error {
	//nolint:forcetypeassert
	items := input.(unstructured.UnstructuredList)

	// TODO: support per-kind column definitions.
	//
	// Read more about type & format here:
	// https://github.com/OAI/OpenAPI-Specification/blob/main/versions/2.0.md#data-types
	//
	// Priority is 0-based (from most important to least important)
	// and controls whether columns are omitted in (wide: false) tables.
	table := &metav1.Table{
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "KIND", Type: "string", Priority: 0, Description: "The kind of the resource."},
			{Name: "GROUP", Type: "string", Priority: 0, Description: "The API group name."},
			{Name: "VERSION", Type: "string", Priority: 1, Description: "The API version."},
			{Name: "NAME", Type: "string", Format: "name", Priority: 0, Description: "The name of the resource."},
			{Name: "AGE", Type: "string", Format: "date-time", Priority: 1, Description: "The age of the resource."},
			{Name: "URL", Type: "string", Priority: 1, Description: "The deep link URL for the resource."},
		},
	}

	noTruncate := terminal.NoTruncate()
	for _, r := range items.Items {
		gvk := r.GroupVersionKind()
		age := duration.HumanDuration(time.Since(r.GetCreationTimestamp().Time))
		url, _ := r.Object["url"].(string)

		table.Rows = append(table.Rows, metav1.TableRow{
			Cells: []any{
				sanitizeCell(r.GetKind(), noTruncate),
				sanitizeCell(gvk.Group, noTruncate),
				sanitizeCell(gvk.Version, noTruncate),
				sanitizeCell(r.GetName(), noTruncate),
				sanitizeCell(age, noTruncate),
				sanitizeCell(url, noTruncate),
			},
			Object: runtime.RawExtension{Object: &r},
		})
	}

	printer := printers.NewTablePrinter(printers.PrintOptions{
		Wide: c.wide,
		// TODO: sorting doesn't actually do anything,
		// though it is supported in the options.
		// SortBy:     "name",
	})

	return printer.PrintObj(table, output)
}

func (c *tableCodec) Decode(io.Reader, any) error {
	return errors.New("table codec does not support decoding")
}

// sanitizeCell returns the cell value unchanged normally. When noTruncate is
// true, newlines, carriage returns, and form feeds are replaced with a space so
// the k8s table printer does not truncate multi-line values with "...".
func sanitizeCell(v string, noTruncate bool) string {
	if !noTruncate {
		return v
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\f' {
			return ' '
		}
		return r
	}, v)
}
