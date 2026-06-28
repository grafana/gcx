package datasources

import (
	"errors"
	"fmt"
	"io"
	"strconv"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	dsclient "github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type getOpts struct {
	IO cmdio.Options
}

func (opts *getOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("text", &datasourceDetailCodec{})
	opts.IO.DefaultFormat("text")
	opts.IO.BindFlags(flags)
}

func (opts *getOpts) Validate() error {
	return opts.IO.Validate()
}

func getCmd() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	opts := &getOpts{}

	cmd := &cobra.Command{
		Use:   "get UID",
		Short: "Get details of a specific datasource",
		Long: `Get a datasource by its UID.

The default text output shows a human-readable detail view. -o yaml/json emits
an apply-ready manifest that can be edited and re-applied via update -f -.`,
		Args: cobra.ExactArgs(1),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   "<uid> -o yaml",
		},
		Example: `
	# Human-readable detail
	gcx datasources get my-prometheus

	# Apply-ready manifest (round-trips into update -f -)
	gcx datasources get my-prometheus -o yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			uid := args[0]

			restCfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			transport, err := dsclient.NewTransport(restCfg)
			if err != nil {
				return err
			}

			ds, err := transport.GetByUID(ctx, uid)
			if err != nil {
				return fmt.Errorf("failed to get datasource: %w", err)
			}

			// Pattern 13: single shape for all formats. The manifest is the
			// canonical, apply-ready representation; the registered text codec
			// renders the human detail view directly from its fields.
			manifest := dsclient.ManifestFromDatasource(ds)
			manifest.Sanitize()
			return opts.IO.Encode(cmd.OutOrStdout(), manifest)
		},
	}

	configOpts.BindFlags(cmd.Flags())
	opts.setup(cmd.Flags())
	return cmd
}

// datasourceDetailCodec renders a DataSourceManifest as a human-readable table.
// All -o formats share the same manifest input; only the rendering differs.
type datasourceDetailCodec struct{}

func (c *datasourceDetailCodec) Format() format.Format { return "text" }

func (c *datasourceDetailCodec) Encode(w io.Writer, data any) error {
	m, ok := data.(*dsclient.DataSourceManifest)
	if !ok {
		return errors.New("invalid data type for text codec")
	}
	t := style.NewTable("FIELD", "VALUE")
	t.Row("UID", m.Metadata.Name)
	t.Row("Name", m.Spec.Title)
	t.Row("Type", m.Spec.Type)
	t.Row("URL", m.Spec.URL)
	t.Row("Access", m.Spec.Access)
	t.Row("Default", strconv.FormatBool(m.Spec.IsDefault))
	t.Row("ReadOnly", strconv.FormatBool(m.Spec.ReadOnly))
	if m.Spec.Database != "" {
		t.Row("Database", m.Spec.Database)
	}
	t.Row("BasicAuth", strconv.FormatBool(m.Spec.BasicAuth))
	t.Row("WithCredentials", strconv.FormatBool(m.Spec.WithCredentials))
	return t.Render(w)
}

func (c *datasourceDetailCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}
