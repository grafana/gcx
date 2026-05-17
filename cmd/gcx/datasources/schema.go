package datasources

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/schemads"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// sqlSchemaCmd returns the `sql schema` subcommand for inspecting the
// schemads schema a datasource plugin advertises (tables, columns, hints,
// capabilities). Useful for picking table/column names to use from
// `gcx datasources sql query` and for comparing the abstraction's view of
// a datasource against its native commands.
func sqlSchemaCmd() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	opts := &schemaOpts{}

	cmd := &cobra.Command{
		Use:   "schema DATASOURCE_UID",
		Short: "Show the abstraction-schema a datasource exposes (tables, columns, hints, capabilities)",
		Long: `Show the schema a datasource plugin exposes via the abstractionSchema
protocol. The default view lists table names plus datasource-level
capabilities. Use --table to drill into a single table's columns,
hints, and parameters.

Only datasources whose plugin implements the abstractionSchema endpoints
respond; others return 404 or 501.`,
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "medium",
			agent.AnnotationLLMHint:   "gcx datasources sql schema UID --match up -o json",
		},
		Example: `
  # List tables (with --match to filter when there are many)
  gcx datasources sql schema bfh6nkyxwj7cwf --match up

  # Drill into a single table's columns and hints
  gcx datasources sql schema bfh6nkyxwj7cwf --table up

  # Full schema as JSON
  gcx datasources sql schema bfh6nkyxwj7cwf -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := schemads.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}
			schema, err := client.FullSchema(ctx, args[0])
			if err != nil {
				return fmt.Errorf("failed to fetch schema: %w", err)
			}

			// When the user drills into a single table, resolve it against
			// the schema first so a typo errors out explicitly rather than
			// silently rendering an empty result. After resolving, fetch
			// dynamic columns + metadata — fullSchema only returns static
			// columns (e.g. for Prometheus, just timestamp/value), and the
			// columns endpoint also carries any table-level metadata the
			// producer populates lazily (e.g. Prom HELP/TYPE).
			if opts.Table != "" {
				resolved, ok := findTable(schema, opts.Table)
				if !ok {
					return fmt.Errorf("table %q not found in datasource %q", opts.Table, args[0])
				}
				opts.Table = resolved
				cr, err := client.Columns(ctx, args[0], []string{resolved}, nil)
				if err != nil {
					return fmt.Errorf("failed to fetch columns for %q: %w", resolved, err)
				}
				if dyn, ok := cr.Columns[resolved]; ok {
					mergeDynamicColumns(schema, resolved, dyn)
				}
				if md, ok := cr.TableMetadata[resolved]; ok && !md.IsZero() {
					mergeTableMetadata(schema, resolved, md)
				}
			}

			view := buildSchemaView(schema, opts)
			return opts.IO.Encode(cmd.OutOrStdout(), view)
		},
	}

	configOpts.BindFlags(cmd.Flags())
	opts.setup(cmd.Flags())
	return cmd
}

// findTable does a case-insensitive lookup for the requested table name in
// the schema. Returns the canonical (case-correct) name and true on hit,
// "" and false on miss.
func findTable(s *schemads.Schema, name string) (string, bool) {
	for i := range s.Tables {
		if strings.EqualFold(s.Tables[i].Name, name) {
			return s.Tables[i].Name, true
		}
	}
	return "", false
}

// mergeDynamicColumns replaces the columns of the named table in s with the
// dynamic columns returned by the columns endpoint. The match is
// case-insensitive to align with --table resolution. No-op when the table is
// not in the schema.
func mergeDynamicColumns(s *schemads.Schema, name string, cols []schemads.Column) {
	for i := range s.Tables {
		if strings.EqualFold(s.Tables[i].Name, name) {
			s.Tables[i].Columns = cols
			return
		}
	}
}

// mergeTableMetadata sets the table-level metadata for the named table. No-op
// when the table is not in the schema.
func mergeTableMetadata(s *schemads.Schema, name string, md schemads.Metadata) {
	for i := range s.Tables {
		if strings.EqualFold(s.Tables[i].Name, name) {
			s.Tables[i].Metadata = md
			return
		}
	}
}

type schemaOpts struct {
	IO    cmdio.Options
	Table string
	Match string
	Limit int
}

func (opts *schemaOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &schemaTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)
	flags.StringVar(&opts.Table, "table", "", "Drill into a specific table (show columns, hints, parameters)")
	flags.StringVar(&opts.Match, "match", "", "Case-insensitive substring filter applied to table names")
	flags.IntVar(&opts.Limit, "limit", 200, "Max number of tables to display in the list view (0 = unlimited)")
}

func (opts *schemaOpts) Validate() error {
	return opts.IO.Validate()
}

// schemaView is the codec-agnostic shape rendered by `gcx datasources sql schema`.
// In list mode (no --table) the Tables/TablesShown fields are populated; in
// single-table mode only Table is. TablesShown is a pointer so it round-trips
// as absent in JSON when not applicable, rather than misleadingly reporting 0.
type schemaView struct {
	Capabilities *schemads.DatasourceCapabilities `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	Functions    []string                         `json:"functions,omitempty" yaml:"functions,omitempty"`
	TablesTotal  int                              `json:"tablesTotal" yaml:"tablesTotal"`
	TablesShown  *int                             `json:"tablesShown,omitempty" yaml:"tablesShown,omitempty"`
	Tables       []string                         `json:"tables,omitempty" yaml:"tables,omitempty"`
	Table        *schemads.Table                  `json:"table,omitempty" yaml:"table,omitempty"`
}

func buildSchemaView(s *schemads.Schema, opts *schemaOpts) *schemaView {
	out := &schemaView{
		Capabilities: s.Capabilities,
		Functions:    s.Functions,
		TablesTotal:  len(s.Tables),
	}

	// Single-table mode: caller has already resolved the name (findTable
	// returned ok), so this lookup is guaranteed to hit.
	if opts.Table != "" {
		for i := range s.Tables {
			if strings.EqualFold(s.Tables[i].Name, opts.Table) {
				t := s.Tables[i]
				out.Table = &t
				return out
			}
		}
		return out
	}

	names := make([]string, 0, len(s.Tables))
	for _, t := range s.Tables {
		if opts.Match != "" && !strings.Contains(strings.ToLower(t.Name), strings.ToLower(opts.Match)) {
			continue
		}
		names = append(names, t.Name)
	}
	sort.Strings(names)
	if opts.Limit > 0 && len(names) > opts.Limit {
		names = names[:opts.Limit]
	}
	out.Tables = names
	shown := len(names)
	out.TablesShown = &shown
	return out
}

type schemaTableCodec struct{}

func (c *schemaTableCodec) Format() format.Format { return "table" }

func (c *schemaTableCodec) Encode(w io.Writer, data any) error {
	v, ok := data.(*schemaView)
	if !ok {
		return errors.New("invalid data type for table codec")
	}

	if v.Capabilities != nil {
		if len(v.Capabilities.AggregateFunctions) > 0 {
			fmt.Fprintln(w, "Capabilities:")
			fmt.Fprintf(w, "  aggregateFunctions: %s\n", strings.Join(v.Capabilities.AggregateFunctions, ", "))
		}
		if v.Capabilities.OrderBy {
			fmt.Fprintln(w, "  orderBy: yes")
		}
		if v.Capabilities.Limit {
			fmt.Fprintln(w, "  limit: yes")
		}
		fmt.Fprintln(w)
	}
	if len(v.Functions) > 0 {
		fmt.Fprintf(w, "Functions: %s\n\n", strings.Join(v.Functions, ", "))
	}

	if v.Table != nil {
		return renderSingleTable(w, v.Table)
	}

	shown := 0
	if v.TablesShown != nil {
		shown = *v.TablesShown
	}
	if shown == 0 {
		fmt.Fprintln(w, "(no matching tables)")
		return nil
	}

	if v.TablesTotal != shown {
		fmt.Fprintf(w, "Tables (%d shown of %d total):\n", shown, v.TablesTotal)
	} else {
		fmt.Fprintf(w, "Tables (%d):\n", v.TablesTotal)
	}
	t := style.NewTable("NAME")
	for _, n := range v.Tables {
		t.Row(n)
	}
	return t.Render(w)
}

func (c *schemaTableCodec) Decode(io.Reader, any) error {
	return errors.New("table codec does not support decoding")
}

func sortedKeys(m map[string]any) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func renderSingleTable(w io.Writer, t *schemads.Table) error {
	fmt.Fprintf(w, "Table: %s\n", t.Name)
	if !t.Metadata.IsZero() {
		if t.Metadata.Description != "" {
			fmt.Fprintf(w, "Description: %s\n", t.Metadata.Description)
		}
		if t.Metadata.Unit != "" {
			fmt.Fprintf(w, "Unit: %s\n", t.Metadata.Unit)
		}
		for _, k := range sortedKeys(t.Metadata.Custom) {
			fmt.Fprintf(w, "%s: %v\n", k, t.Metadata.Custom[k])
		}
	}
	fmt.Fprintln(w)

	if len(t.Columns) > 0 {
		fmt.Fprintln(w, "Columns:")
		ct := style.NewTable("NAME", "TYPE", "OPERATORS", "DESCRIPTION")
		for _, c := range t.Columns {
			ops := make([]string, len(c.Operators))
			for i, o := range c.Operators {
				ops[i] = string(o)
			}
			desc := c.Metadata.Description
			if desc == "" {
				desc = c.Description
			}
			ct.Row(c.Name, c.Type, strings.Join(ops, " "), desc)
		}
		if err := ct.Render(w); err != nil {
			return err
		}
		fmt.Fprintln(w)
	}

	if len(t.TableHints) > 0 {
		fmt.Fprintln(w, "Hints:")
		ht := style.NewTable("NAME", "VALUE", "DESCRIPTION")
		for _, h := range t.TableHints {
			val := "no"
			if h.HasValue {
				val = "yes"
			}
			ht.Row(h.Name, val, h.Description)
		}
		if err := ht.Render(w); err != nil {
			return err
		}
		fmt.Fprintln(w)
	}

	if len(t.TableParameters) > 0 {
		fmt.Fprintln(w, "Parameters:")
		pt := style.NewTable("NAME", "REQUIRED", "ROOT", "DEPENDS ON")
		for _, p := range t.TableParameters {
			req := "no"
			if p.Required {
				req = "yes"
			}
			root := "no"
			if p.Root {
				root = "yes"
			}
			pt.Row(p.Name, req, root, strings.Join(p.DependsOn, ", "))
		}
		if err := pt.Render(w); err != nil {
			return err
		}
	}

	return nil
}
