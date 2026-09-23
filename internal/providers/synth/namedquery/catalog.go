package namedquery

// Discovery is the third leg of named queries, alongside execution
// (commands.go). It answers "which named queries does this tenant's
// Synthetic Monitoring datasource serve, and with what parameters" -- without
// it, a caller of `query` has to already know a name and its parameters by
// reading synthetic-monitoring-app source it does not have.
//
// Discovery deliberately uses LoadGrafanaConfig, not LoadSMProxyConfig: the
// catalog is an unauthenticated static plugin asset, so fetching it needs no
// SM datasource UID and must not trigger datasource resolution or a config
// write, both of which LoadSMProxyConfig does as a side effect.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/synth/smcfg"
	"github.com/grafana/gcx/internal/query/synth"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// QueryTypeSummary is one row of `queries list`.
type QueryTypeSummary struct {
	Name        string   `json:"name"`
	Required    []string `json:"required"`
	Description string   `json:"description"`
}

// QueryTypeDetail is the full record `queries get` prints: everything
// QueryTypeSummary has, plus the full parameter schema and a ready-to-run
// invocation.
type QueryTypeDetail struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Required    []string        `json:"required"`
	Schema      json.RawMessage `json:"schema"`
	// Example is a ready-to-run `gcx synthetic-monitoring query` invocation
	// with the required parameters as placeholders, so this output can be
	// copied straight into a shell.
	Example string `json:"example"`
}

// QueriesCommands returns the `queries` command group: discovery for the
// named queries `query` executes.
func QueriesCommands(loader smcfg.GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "queries",
		Short: "Discover Synthetic Monitoring named queries.",
	}
	cmd.AddCommand(newQueriesListCommand(loader), newQueriesGetCommand(loader))
	return cmd
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

type queriesListOpts struct {
	IO cmdio.Options
}

func (o *queriesListOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, QueryTypeSummaryTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newQueriesListCommand(loader smcfg.GrafanaConfigLoader) *cobra.Command {
	opts := &queriesListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the named queries the Synthetic Monitoring datasource serves.",
		Long: `List the named queries this tenant's Synthetic Monitoring datasource
publishes. Each entry can be run with 'gcx synthetic-monitoring query NAME'; use
'gcx synthetic-monitoring queries get NAME' for its full parameter schema.

Requires Synthetic Monitoring app v1.62.0 or later -- named-query discovery is
not available on older deployments.`,
		Example: `  gcx synthetic-monitoring queries list`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			result, err := fetchCatalog(cmd.Context(), loader)
			if err != nil {
				return err
			}
			warnOnUnexpectedAPIVersion(cmd, result.APIVersion)

			summaries := make([]QueryTypeSummary, 0, len(result.QueryTypes))
			for _, qt := range result.QueryTypes {
				summaries = append(summaries, QueryTypeSummary{
					Name:        qt.Name,
					Required:    requiredOrEmpty(qt.Required),
					Description: qt.Description,
				})
			}

			return opts.IO.Encode(cmd.OutOrStdout(), summaries)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// QueryTypeSummaryTable declares the `queries list` table. REQUIRED never
// shows the full schema -- a datasource plugin's catalog can be tens of KB,
// and dumping every entry's parameter constraints on every discovery call is
// a token cost an agent should not pay just to see what exists.
func QueryTypeSummaryTable() cmdio.Table[QueryTypeSummary] {
	return cmdio.Table[QueryTypeSummary]{
		Columns: []cmdio.Column[QueryTypeSummary]{
			{Header: "NAME", Content: func(q QueryTypeSummary) string { return q.Name }},
			{Header: "REQUIRED", Content: func(q QueryTypeSummary) string { return strings.Join(q.Required, ", ") }},
			{Header: "DESCRIPTION", Content: func(q QueryTypeSummary) string { return q.Description }},
		},
	}
}

// ---------------------------------------------------------------------------
// get
// ---------------------------------------------------------------------------

type queriesGetOpts struct {
	IO cmdio.Options
}

func (o *queriesGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &queryTypeDetailCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newQueriesGetCommand(loader smcfg.GrafanaConfigLoader) *cobra.Command {
	opts := &queriesGetOpts{}
	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Show a named query's parameter schema.",
		Long: `Show one named query's description and full parameter schema, plus a
ready-to-run 'gcx synthetic-monitoring query' invocation.

Requires Synthetic Monitoring app v1.62.0 or later.`,
		Example: `  gcx synthetic-monitoring queries get checks_uptime`,
		Args:    cobra.ExactArgs(1),

		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			result, err := fetchCatalog(cmd.Context(), loader)
			if err != nil {
				return err
			}
			warnOnUnexpectedAPIVersion(cmd, result.APIVersion)

			name := args[0]
			for _, qt := range result.QueryTypes {
				if qt.Name != name {
					continue
				}

				return opts.IO.Encode(cmd.OutOrStdout(), QueryTypeDetail{
					Name:        qt.Name,
					Description: qt.Description,
					Required:    requiredOrEmpty(qt.Required),
					Schema:      qt.Schema,
					Example:     exampleInvocation(qt),
				})
			}

			return fmt.Errorf(
				"named query %q not found; run 'gcx synthetic-monitoring queries list' to see available queries", name,
			)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// requiredOrEmpty normalizes a nil Required (a query with no required
// parameters) to an empty slice, so JSON output shows "[]" rather than
// "null" -- both mean the same thing to a caller, but "null" reads as a
// missing field rather than an empty one.
func requiredOrEmpty(required []string) []string {
	if required == nil {
		return []string{}
	}
	return required
}

// exampleInvocation builds a ready-to-run `query` command using the entry's
// required parameters as placeholders, single-quoted so `<name>` isn't parsed
// as shell redirection, in the same order as Required (and the table's
// REQUIRED column) rather than alphabetical.
func exampleInvocation(qt synth.QueryType) string {
	var b strings.Builder
	b.WriteString("gcx synthetic-monitoring query ")
	b.WriteString(qt.Name)
	for _, name := range qt.Required {
		fmt.Fprintf(&b, " -p '%s=<%s>'", name, name)
	}

	return b.String()
}

// queryTypeDetailCodec renders a QueryTypeDetail as readable text: name,
// description, required parameters, the full JSON Schema, and the
// ready-to-run invocation.
type queryTypeDetailCodec struct{}

func (c *queryTypeDetailCodec) Format() format.Format { return "table" }

func (c *queryTypeDetailCodec) Encode(w io.Writer, v any) error {
	detail, ok := v.(QueryTypeDetail)
	if !ok {
		return fmt.Errorf("expected QueryTypeDetail, got %T", v)
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "NAME\t%s\n", detail.Name)
	fmt.Fprintf(tw, "DESCRIPTION\t%s\n", detail.Description)
	fmt.Fprintf(tw, "REQUIRED\t%s\n", strings.Join(detail.Required, ", "))
	if err := tw.Flush(); err != nil {
		return err
	}

	fmt.Fprintln(w, "\nPARAMETERS:")
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, detail.Schema, "", "  "); err != nil {
		fmt.Fprintln(w, string(detail.Schema))
	} else {
		fmt.Fprintln(w, pretty.String())
	}

	fmt.Fprintln(w, "RUN:")
	fmt.Fprintln(w, "  "+detail.Example)

	return nil
}

func (c *queryTypeDetailCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("decoding is not supported")
}

// ---------------------------------------------------------------------------
// shared
// ---------------------------------------------------------------------------

// fetchCatalog resolves the caller's Grafana REST config and fetches the
// named-query catalog.
func fetchCatalog(ctx context.Context, loader smcfg.GrafanaConfigLoader) (*synth.CatalogResult, error) {
	restCfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, err
	}

	client, err := synth.NewCatalogClient(restCfg)
	if err != nil {
		return nil, err
	}

	return client.Catalog(ctx)
}

// warnOnUnexpectedAPIVersion writes a one-line stderr warning when the served
// catalog uses an apiVersion this package was not built against. The file is
// additive, so this is advisory, not fatal -- Catalog already parsed it.
func warnOnUnexpectedAPIVersion(cmd *cobra.Command, apiVersion string) {
	if apiVersion == "" || apiVersion == synth.ExpectedQueryTypesAPIVersion {
		return
	}

	cmdio.Warning(cmd.ErrOrStderr(),
		"named-query catalog uses apiVersion %q, expected %q; some fields may not be understood",
		apiVersion, synth.ExpectedQueryTypesAPIVersion)
}
