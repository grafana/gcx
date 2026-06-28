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

			// Human detail view.
			if opts.IO.OutputFormat == "text" {
				return opts.IO.Encode(cmd.OutOrStdout(), detailFromDatasource(ds))
			}

			// Machine formats emit the apply-ready manifest.
			manifest := dsclient.ManifestFromDatasource(ds)
			manifest.Sanitize()
			return opts.IO.Encode(cmd.OutOrStdout(), manifest)
		},
	}

	configOpts.BindFlags(cmd.Flags())
	opts.setup(cmd.Flags())
	return cmd
}

type datasourceDetail struct {
	UID       string `json:"uid" yaml:"uid"`
	Name      string `json:"name" yaml:"name"`
	Type      string `json:"type" yaml:"type"`
	URL       string `json:"url" yaml:"url"`
	Access    string `json:"access" yaml:"access"`
	Default   bool   `json:"default" yaml:"default"`
	ReadOnly  bool   `json:"readOnly" yaml:"readOnly"`
	Database  string `json:"database,omitempty" yaml:"database,omitempty"`
	BasicAuth bool   `json:"basicAuth" yaml:"basicAuth"`
	WithCreds bool   `json:"withCredentials" yaml:"withCredentials"`
	JSONData  any    `json:"jsonData,omitempty" yaml:"jsonData,omitempty"`
}

func detailFromDatasource(ds *dsclient.Datasource) *datasourceDetail {
	return &datasourceDetail{
		UID:       ds.UID,
		Name:      ds.Name,
		Type:      ds.Type,
		URL:       ds.URL,
		Access:    ds.Access,
		Default:   ds.IsDefault,
		ReadOnly:  ds.ReadOnly,
		Database:  ds.Database,
		BasicAuth: ds.BasicAuth,
		WithCreds: ds.WithCredentials,
		JSONData:  ds.JSONData,
	}
}

// datasourceDetailCodec renders a datasourceDetail as a human-readable table.
type datasourceDetailCodec struct{}

func (c *datasourceDetailCodec) Format() format.Format { return "text" }

func (c *datasourceDetailCodec) Encode(w io.Writer, data any) error {
	d, ok := data.(*datasourceDetail)
	if !ok {
		return errors.New("invalid data type for text codec")
	}
	t := style.NewTable("FIELD", "VALUE")
	t.Row("UID", d.UID)
	t.Row("Name", d.Name)
	t.Row("Type", d.Type)
	t.Row("URL", d.URL)
	t.Row("Access", d.Access)
	t.Row("Default", strconv.FormatBool(d.Default))
	t.Row("ReadOnly", strconv.FormatBool(d.ReadOnly))
	if d.Database != "" {
		t.Row("Database", d.Database)
	}
	t.Row("BasicAuth", strconv.FormatBool(d.BasicAuth))
	t.Row("WithCredentials", strconv.FormatBool(d.WithCreds))
	return t.Render(w)
}

func (c *datasourceDetailCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}
