package kg

import (
	"errors"
	"io"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type schemaListOpts struct {
	IO         cmdio.Options
	expand     bool
	latestOnly bool
}

func (o *schemaListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &schemaTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.BoolVar(&o.expand, "expand", false, "Include complete schema bundles; use JSON or YAML to see the definitions")
	flags.BoolVar(&o.latestOnly, "latest-only", true, "Return only the highest-priority version per domain; set false to include all installed versions")
}

func (o *schemaListOpts) Validate() error {
	return o.IO.Validate()
}

func newSchemasCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schemas",
		Short: "Discover installed Knowledge Graph schemas.",
	}
	opts := &schemaListOpts{}
	list := &cobra.Command{
		Use:   "list",
		Short: "[experimental] List effective declared Knowledge Graph schemas.",
		Long: `This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

List installed schema domains and versions for the configured stack namespace.
Use this command to discover declared schemas, even when their entity types
do not appear in the graph. Use 'gcx kg meta schema' for observed graph metadata.

By default, the server returns only the highest-priority version per domain
and omits bundle definitions. Version priority follows Kubernetes ordering:
stable versions outrank beta versions, which outrank alpha versions.

Use --expand with JSON or YAML output to include imports, entity types,
relationship types, and relationship type bindings. The table shows domain
metadata only. Use --latest-only=false to include all installed versions.

The schema discovery endpoint requires the Knowledge Graph write API to be
enabled on the stack. This command only reads schemas; it does not modify them.`,
		Example: `  gcx kg schemas list
  gcx kg schemas list --expand -o json
  gcx kg schemas list --expand --latest-only=false -o yaml`,
		Args:        cobra.NoArgs,
		Annotations: map[string]string{agent.AnnotationStability: agent.StabilityExperimental},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			result, err := client.ListSchemas(cmd.Context(), opts.expand, opts.latestOnly)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(list.Flags())
	cmd.AddCommand(list)
	return cmd
}

type schemaTableCodec struct{}

func (*schemaTableCodec) Format() format.Format { return "table" }

func (*schemaTableCodec) Encode(w io.Writer, v any) error {
	result, ok := v.(*SchemaList)
	if !ok {
		return errors.New("invalid data type for table codec: expected *SchemaList")
	}
	domainValue := func(schema map[string]any, field string) string {
		domain, _ := schema["domain"].(map[string]any)
		value, _ := domain[field].(string)
		return value
	}
	table := cmdio.Table[map[string]any]{Columns: []cmdio.Column[map[string]any]{
		{Header: "DOMAIN", Content: func(schema map[string]any) string { return domainValue(schema, "name") }},
		{Header: "VERSION", Content: func(schema map[string]any) string { return domainValue(schema, "version") }},
		{Header: "DISPLAY NAME", Content: func(schema map[string]any) string { return domainValue(schema, "displayName") }},
	}}
	return table.Codec("table").Encode(w, result.Schemas)
}

func (*schemaTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}
