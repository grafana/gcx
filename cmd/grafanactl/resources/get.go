package resources

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	cmdconfig "github.com/grafana/grafanactl/cmd/grafanactl/config"
	"github.com/grafana/grafanactl/cmd/grafanactl/fail"
	cmdio "github.com/grafana/grafanactl/cmd/grafanactl/io"
	"github.com/grafana/grafanactl/internal/agent"
	"github.com/grafana/grafanactl/internal/format"
	"github.com/grafana/grafanactl/internal/resources"
	"github.com/grafana/grafanactl/internal/terminal"
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

type getOpts struct {
	IO      cmdio.Options
	OnError OnErrorMode
}

func (opts *getOpts) setup(flags *pflag.FlagSet) {
	// Setup some additional formatting options
	bindOnErrorFlag(flags, &opts.OnError)
	opts.IO.RegisterCustomCodec("text", &tableCodec{wide: false})
	opts.IO.RegisterCustomCodec("wide", &tableCodec{wide: true})
	opts.IO.DefaultFormat("text")

	// Bind all the flags
	opts.IO.BindFlags(flags)
}

func (opts *getOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
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
		Example: `
	# Everything:

	grafanactl resources get dashboards/foo

	# All instances for a given kind(s):

	grafanactl resources get dashboards
	grafanactl resources get dashboards folders

	# Single resource kind, one or more resource instances:

	grafanactl resources get dashboards/foo
	grafanactl resources get dashboards/foo,bar

	# Single resource kind, long kind format:

	grafanactl resources get dashboard.dashboards/foo
	grafanactl resources get dashboard.dashboards/foo,bar

	# Single resource kind, long kind format with version:

	grafanactl resources get dashboards.v1alpha1.dashboard.grafana.app/foo
	grafanactl resources get dashboards.v1alpha1.dashboard.grafana.app/foo,bar

	# Multiple resource kinds, one or more resource instances:

	grafanactl resources get dashboards/foo folders/qux
	grafanactl resources get dashboards/foo,bar folders/qux,quux

	# Multiple resource kinds, long kind format:

	grafanactl resources get dashboard.dashboards/foo folder.folders/qux
	grafanactl resources get dashboard.dashboards/foo,bar folder.folders/qux,quux

	# Multiple resource kinds, long kind format with version:

	grafanactl resources get dashboards.v1alpha1.dashboard.grafana.app/foo folders.v1alpha1.folder.grafana.app/qux`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			// FR-007: --json ? requires a resource selector to know which resource type to introspect.
			if opts.IO.JSONDiscovery && len(args) == 0 {
				return errors.New("--json ? requires a resource selector argument (e.g. grafanactl resources get dashboards --json ?)")
			}

			cfg, err := configOpts.LoadRESTConfig(ctx)
			if err != nil {
				return err
			}

			fetchReq := FetchRequest{
				Config:      cfg,
				StopOnError: opts.OnError.StopOnError(),
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

			// --json ? discovery: print available fields from the first fetched
			// resource and exit without producing normal output.
			if opts.IO.JSONDiscovery {
				if len(output.Items) == 0 {
					return errors.New("no resources found for field discovery: provide a selector that matches at least one resource")
				}
				printFieldDiscoveryResults(cmd.OutOrStdout(), output.Items[0].Object)
				return nil
			}

			// --json field1,field2: use FieldSelectCodec for output.
			if len(opts.IO.JSONFields) > 0 {
				return writeFieldSelect(cmd.OutOrStdout(), opts, res, output)
			}

			var encodeErr error
			if opts.IO.OutputFormat != "text" && opts.IO.OutputFormat != "wide" {
				// Avoid printing a list of results if a single resource is being pulled,
				// and we are not using the table output format.
				if res.IsSingleTarget && len(output.Items) == 1 {
					encodeErr = opts.IO.Encode(cmd.OutOrStdout(), output.Items[0].Object)
				} else {
					// For JSON / YAML output we don't want to have "object" keys in the output,
					// so use the custom printItems type instead.
					formatted := printItems{
						Items: make([]map[string]any, len(output.Items)),
					}
					for i, item := range output.Items {
						formatted.Items[i] = item.Object
					}
					encodeErr = opts.IO.Encode(cmd.OutOrStdout(), formatted)
				}
			} else {
				encodeErr = opts.IO.Encode(cmd.OutOrStdout(), output)
			}

			if encodeErr != nil {
				return encodeErr
			}

			if opts.OnError.FailOnErrors() && res.PullSummary.FailedCount() > 0 {
				return fmt.Errorf("%d resource(s) failed to get", res.PullSummary.FailedCount())
			}

			return nil
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

// writeFieldSelect handles --json field1,field2 output for the get command.
// It uses FieldSelectCodec to emit only selected fields, and emits a combined
// {"items": [...], "error": {...}} envelope (FR-012) on partial failure in agent mode.
func writeFieldSelect(out io.Writer, opts *getOpts, res *FetchResponse, output unstructured.UnstructuredList) error {
	codec := cmdio.NewFieldSelectCodec(opts.IO.JSONFields)
	hasPartialFailure := opts.OnError.FailOnErrors() && res.PullSummary.FailedCount() > 0

	// FR-012: when agent mode is active and there are partial failures,
	// write a single combined {"items": [...], "error": {...}} envelope.
	if hasPartialFailure && agent.IsAgentMode() {
		itemMaps := make([]map[string]any, len(output.Items))
		for i, item := range output.Items {
			itemMaps[i] = cmdio.ExtractFields(item.Object, codec.Fields())
		}
		errSummary := fmt.Sprintf("%d resource(s) failed to get", res.PullSummary.FailedCount())
		detErr := fail.DetailedError{Summary: errSummary}
		return detErr.WriteJSONWithItems(out, fail.ExitPartialFailure, itemMaps)
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
		return fmt.Errorf("%d resource(s) failed to get", res.PullSummary.FailedCount())
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
			{
				Name:        "KIND",
				Type:        "string",
				Priority:    0,
				Description: "The kind of the resource.",
			},
			{
				Name:        "NAME",
				Type:        "string",
				Format:      "name",
				Priority:    0,
				Description: "The name of the resource.",
			},
			{
				Name:        "NAMESPACE",
				Priority:    0,
				Description: "The namespace of the resource.",
			},
			{
				Name:        "AGE",
				Type:        "string",
				Format:      "date-time",
				Priority:    1,
				Description: "The age of the resource.",
			},
		},
	}

	if c.wide {
		table.ColumnDefinitions = append(table.ColumnDefinitions, metav1.TableColumnDefinition{
			Name:     "GROUPVERSION",
			Type:     "string",
			Priority: 0,
		})
	}

	noTruncate := terminal.NoTruncate()
	for _, r := range items.Items {
		age := duration.HumanDuration(time.Since(r.GetCreationTimestamp().Time))
		var row metav1.TableRow
		if c.wide {
			row = metav1.TableRow{
				Cells: []any{
					sanitizeCell(r.GetKind(), noTruncate),
					sanitizeCell(r.GetName(), noTruncate),
					sanitizeCell(r.GetNamespace(), noTruncate),
					sanitizeCell(age, noTruncate),
					sanitizeCell(r.GroupVersionKind().GroupVersion().String(), noTruncate),
				},
				Object: runtime.RawExtension{
					Object: &r,
				},
			}
		} else {
			row = metav1.TableRow{
				Cells: []any{
					sanitizeCell(r.GetKind(), noTruncate),
					sanitizeCell(r.GetName(), noTruncate),
					sanitizeCell(r.GetNamespace(), noTruncate),
					sanitizeCell(age, noTruncate),
				},
				Object: runtime.RawExtension{
					Object: &r,
				},
			}
		}

		table.Rows = append(table.Rows, row)
	}

	printer := printers.NewTablePrinter(printers.PrintOptions{
		Wide:       c.wide,
		ShowLabels: c.wide,
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
