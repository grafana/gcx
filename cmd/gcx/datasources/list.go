package datasources

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	dsclient "github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type listOpts struct {
	IO    cmdio.Options
	Type  string
	Name  string
	Limit int
}

func (opts *listOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &datasourceTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Type, "type", "t", "", "Filter by datasource type (e.g., prometheus, loki)")
	flags.StringVar(&opts.Name, "name", "", "Filter by datasource name (case-insensitive substring match)")
	// Cheaply complete source (no server-side limit exists), so the default
	// is the full set; a default cap would only hide data.
	opts.IO.BindListLimit(flags, &opts.Limit, "datasources", 0)
}

func (opts *listOpts) Validate() error {
	return opts.IO.Validate()
}

func listCmd() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	opts := &listOpts{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all datasources",
		Long:  "List all datasources configured in Grafana. Filter by type and/or name (case-insensitive substring match).",
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "medium",
			agent.AnnotationLLMHint:   "--type prometheus --name prod -o json",
		},
		Example: `
	# List all datasources
	gcx datasources list

	# List only Prometheus datasources
	gcx datasources list --type prometheus

	# Filter by name substring (matches "prometheus-prod-eu", "loki-prod-us", ...)
	gcx datasources list --name prod

	# Combine type and name filters
	gcx datasources list --type prometheus --name prod

	# Output as JSON
	gcx datasources list -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			restCfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			dsClient, err := dsclient.NewTransport(restCfg)
			if err != nil {
				return err
			}

			datasources, err := dsClient.List(ctx)
			if err != nil {
				return fmt.Errorf("failed to list datasources: %w", err)
			}

			// Name is a case-insensitive substring match, composable (AND) with
			// the type filter. Grafana's /api/datasources API has no server-side
			// name filter, so both are applied client-side before the limit trim.
			name := strings.ToLower(opts.Name)
			infos := make([]*datasourceInfo, 0, len(datasources))
			for _, ds := range datasources {
				if opts.Type != "" && !strings.EqualFold(ds.Type, opts.Type) {
					continue
				}
				if name != "" && !strings.Contains(strings.ToLower(ds.Name), name) {
					continue
				}
				infos = append(infos, &datasourceInfo{
					UID:      ds.UID,
					Name:     ds.Name,
					Type:     ds.Type,
					URL:      ds.URL,
					Access:   ds.Access,
					Default:  ds.IsDefault,
					ReadOnly: ds.ReadOnly,
				})
			}

			// The full set is already in hand (the /api/datasources transport
			// has no server-side limit), so the limit is purely a display trim
			// and the observed total is exact. Truncation is machine-legible
			// (list_meta in the envelope) and human-legible (stderr hint).
			infos, meta := cmdio.TruncateCompleteList(infos, opts.Limit)
			meta = cmdio.AttachListMeta(meta, os.Args)

			// Pattern 13: single shape for all formats. The table codec extracts
			// .Datasources to render rows; JSON/YAML serialize the envelope.
			if err := opts.IO.Encode(cmd.OutOrStdout(), &datasourceListResult{Datasources: infos, ListMeta: meta}); err != nil {
				return err
			}
			cmdio.EmitListTruncationHint(cmd.ErrOrStderr(), meta)
			return nil
		},
	}

	configOpts.BindFlags(cmd.Flags())
	opts.setup(cmd.Flags())
	return cmd
}

type datasourceInfo struct {
	UID      string `json:"uid" yaml:"uid"`
	Name     string `json:"name" yaml:"name"`
	Type     string `json:"type" yaml:"type"`
	URL      string `json:"url" yaml:"url"`
	Access   string `json:"access" yaml:"access"`
	Default  bool   `json:"default" yaml:"default"`
	ReadOnly bool   `json:"readOnly" yaml:"readOnly"`
}

// datasourceListResult is the single shape passed to every codec for
// `datasources list`. JSON/YAML serialize the envelope; the table codec
// extracts .Datasources to render rows (Pattern 13: format-agnostic data).
type datasourceListResult struct {
	Datasources []*datasourceInfo `json:"datasources" yaml:"datasources"`
	// ListMeta is attached only when the output is a truncated page, so
	// agents cannot mistake a page for the complete set. Reserved key —
	// see docs/design/output.md § List Truncation Contract.
	ListMeta *cmdio.ListMeta `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
}

type datasourceTableCodec struct{}

func (c *datasourceTableCodec) Format() format.Format {
	return "table"
}

func (c *datasourceTableCodec) Encode(w io.Writer, data any) error {
	result, ok := data.(*datasourceListResult)
	if !ok {
		return errors.New("invalid data type for table codec")
	}

	// we haven't added ACCESS here, because it doesn't provide much value (its nearly always "proxy")
	t := style.NewTable("UID", "NAME", "TYPE", "URL", "DEFAULT")
	for _, ds := range result.Datasources {
		defaultStr := ""
		if ds.Default {
			defaultStr = "*"
		}
		t.Row(ds.UID, ds.Name, ds.Type, ds.URL, defaultStr)
	}

	return t.Render(w)
}

func (c *datasourceTableCodec) Decode(io.Reader, any) error {
	return errors.New("table codec does not support decoding")
}
