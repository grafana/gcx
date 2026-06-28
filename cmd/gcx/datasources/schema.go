package datasources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	dsclient "github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// schemasCmd is the `datasources schemas` noun subgroup.
func schemasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schemas",
		Short: "Inspect datasource plugin schemas",
		Long:  "Inspect the configuration schema of a datasource plugin type, to author manifests correctly.",
	}
	cmd.AddCommand(schemasListCmd())
	cmd.AddCommand(schemasGetCmd())
	return cmd
}

type schemasListOpts struct {
	IO cmdio.Options
}

func (opts *schemasListOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &pluginTypeTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)
}

func (opts *schemasListOpts) Validate() error {
	return opts.IO.Validate()
}

func schemasListCmd() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	opts := &schemasListOpts{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List datasource plugin types installed on the Grafana instance",
		Long: `List the datasource plugin types installed on the Grafana instance.

The TYPE column is the plugin id — pass it to 'schemas get --type', and use it
as spec.type when authoring a datasource manifest.`,
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "medium",
			agent.AnnotationLLMHint:   "-o json",
		},
		Example: `
	# List available datasource plugin types
	gcx datasources schemas list

	# As JSON
	gcx datasources schemas list -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			restCfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			dsClient, err := dsclient.NewClient(restCfg)
			if err != nil {
				return err
			}

			types, err := dsClient.ListPluginTypes(ctx)
			if err != nil {
				return fmt.Errorf("failed to list datasource plugin types: %w", err)
			}

			// Pattern 13: single shape for all formats. The table codec extracts
			// .Types to render rows; JSON/YAML serialize the envelope.
			return opts.IO.Encode(cmd.OutOrStdout(), &pluginTypesResult{Types: types})
		},
	}

	configOpts.BindFlags(cmd.Flags())
	opts.setup(cmd.Flags())
	return cmd
}

// pluginTypesResult is the single shape passed to every codec for `datasources
// schemas list`. JSON/YAML serialize the envelope; the table codec extracts
// .Types to render rows (Pattern 13: format-agnostic data).
type pluginTypesResult struct {
	Types []dsclient.PluginType `json:"types" yaml:"types"`
}

type pluginTypeTableCodec struct{}

func (c *pluginTypeTableCodec) Format() format.Format {
	return "table"
}

func (c *pluginTypeTableCodec) Encode(w io.Writer, data any) error {
	result, ok := data.(*pluginTypesResult)
	if !ok {
		return errors.New("invalid data type for table codec")
	}

	t := style.NewTable("TYPE", "NAME", "CATEGORY")
	for _, pt := range result.Types {
		t.Row(pt.ID, pt.Name, pt.Category)
	}
	return t.Render(w)
}

func (c *pluginTypeTableCodec) Decode(io.Reader, any) error {
	return errors.New("table codec does not support decoding")
}

type schemasGetOpts struct {
	IO   cmdio.Options
	Type string
	Kind string
}

func (opts *schemasGetOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&opts.Type, "type", "t", "", "Datasource plugin id (e.g. grafana-sentry-datasource)")
	flags.StringVar(&opts.Kind, "kind", "config", "Schema kind: config (query is not yet supported)")
	opts.IO.DefaultFormat("yaml")
	opts.IO.BindFlags(flags)
}

func (opts *schemasGetOpts) Validate() error {
	if opts.Type == "" {
		exit := gcxerrors.ExitUsageError
		return gcxerrors.DetailedError{
			Summary: "--type is required",
			Details: "Specify the datasource plugin id whose schema you want (e.g. --type prometheus).",
			Suggestions: []string{
				"Run 'gcx datasources schemas list' to see the installed datasource plugin types.",
			},
			ExitCode: &exit,
		}
	}
	if opts.Kind != "config" {
		exit := gcxerrors.ExitUsageError
		return gcxerrors.DetailedError{
			Summary:  fmt.Sprintf("schema kind %q is not supported", opts.Kind),
			Details:  "Only --kind config is supported. Query-schema introspection is not yet available.",
			ExitCode: &exit,
		}
	}
	return opts.IO.Validate()
}

func schemasGetCmd() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	opts := &schemasGetOpts{}

	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get the configuration schema for a datasource plugin type",
		Long: `Get the configuration schema for a datasource plugin type.

The schema lists the configuration fields available for a plugin, derived from
the server's OpenAPI document. Use it to author create/update manifests.`,
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "medium",
			agent.AnnotationLLMHint:   "--type grafana-sentry-datasource -o json",
		},
		Example: `
	# Show a plugin's configuration schema
	gcx datasources schemas get --type grafana-sentry-datasource

	# As JSON
	gcx datasources schemas get --type grafana-sentry-datasource -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			restCfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			dsClient, err := dsclient.NewClient(restCfg)
			if err != nil {
				return err
			}

			if err := ensureKnownPluginType(ctx, dsClient, opts.Type); err != nil {
				return err
			}

			raw := dsclient.ConfigSchema(opts.Type)
			var schema map[string]any
			if err := json.Unmarshal(raw, &schema); err != nil {
				return fmt.Errorf("failed to decode schema: %w", err)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), schema)
		},
	}

	configOpts.BindFlags(cmd.Flags())
	opts.setup(cmd.Flags())
	return cmd
}

// ensureKnownPluginType fails when pluginType is not an installed datasource
// plugin id, so `schemas get` cannot silently emit a schema for a type that does
// not exist on the instance.
func ensureKnownPluginType(ctx context.Context, c *dsclient.Client, pluginType string) error {
	types, err := c.ListPluginTypes(ctx)
	if err != nil {
		return fmt.Errorf("failed to look up datasource plugin types: %w", err)
	}
	for _, t := range types {
		if strings.EqualFold(t.ID, pluginType) {
			return nil
		}
	}

	exit := gcxerrors.ExitUsageError
	return gcxerrors.DetailedError{
		Summary: fmt.Sprintf("unknown datasource plugin type %q", pluginType),
		Details: "No datasource plugin with this id is installed on the Grafana instance.",
		Suggestions: []string{
			"Run 'gcx datasources schemas list' to see the installed datasource plugin types.",
		},
		ExitCode: &exit,
	}
}
