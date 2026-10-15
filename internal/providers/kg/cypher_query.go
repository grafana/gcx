package kg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/shared"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newGraphCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Query Knowledge Graph projections and paths.",
	}
	cmd.AddCommand(newGraphQueryCommand(loader))
	return cmd
}

func newGraphQueryCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &graphQueryOpts{}
	cmd := &cobra.Command{
		Use:   "query <cypher-query>",
		Short: "Run a read-only Cypher query and return its columns and rows.",
		Long: `Run a read-only Cypher query against the Knowledge Graph using /v1/query/cypher.

Run 'gcx kg meta schema' to discover entity types, properties, and relationships.
The time window defaults to the last hour. Use --since or both --from and --to.
Every matched node and relationship must overlap the requested window; this is
not a historical snapshot and does not require an entire path to coexist.

The response contains columns, rows, and stats (rowCount, columnCount, elapsedMs).
Scalars, nulls, lists, maps, nodes, relationships, and paths retain their JSON
shape. Table output uses the projected columns; nested values render as JSON.
Node/relationship refs are local to one response, not durable graph identities.
Use --json columns,rows,stats to select envelope fields or --jq '.rows' for rows.

A literal LIMIT is required in the query. Results describe that bounded query,
not a complete graph inventory; there is no lastPage indicator or automatic paging.
Use literal SKIP/LIMIT in Cypher to select a result window, with ORDER BY for
predictable ordering. The legacy entities/edges envelope, --page, and
--insights-only remain available via the deprecated 'gcx kg entities query'.
Use 'gcx kg entities list' with --insight for entity health evidence.
Caller parameters are not supported. Use literals and escape strings as Cypher literals.
Variable-length paths require a finite upper bound within the server's hop limit.
The server validates its language allowlist and execution budgets; refusals include
stable CYPHER_* error codes. There is no fallback to the legacy search endpoint.`,
		Example: `  gcx kg graph query "MATCH (s:Service) RETURN s.name AS name LIMIT 10"
  gcx kg graph query "MATCH (s:Service) RETURN count(s) AS services LIMIT 1" --since 1h
  gcx kg graph query "MATCH p = (s:Service {name: 'checkout'})-[:CALLS*1..3]->(d) RETURN p, length(p) AS hops LIMIT 5" --since 24h`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.query = args[0]
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
			resp, err := client.CypherQuery(cmd.Context(), opts.request)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type graphQueryOpts struct {
	IO              cmdio.Options
	query           string
	from, to, since string
	flags           *pflag.FlagSet
	request         CypherQueryRequest
}

func (o *graphQueryOpts) setup(flags *pflag.FlagSet) {
	o.flags = flags
	flags.StringVar(&o.from, "from", "", "Start time (RFC3339, Unix seconds, or relative like 'now-1h'); requires --to")
	flags.StringVar(&o.to, "to", "", "End time (RFC3339, Unix seconds, or relative like 'now'); requires --from")
	flags.StringVar(&o.since, "since", "", "Positive duration ending now (e.g. 1h, 30m, 7d); defaults to 1h; mutually exclusive with --from/--to")
	o.IO.RegisterCustomCodec("table", &CypherRowsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func (o *graphQueryOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(o.query) == "" {
		return errors.New("cypher query must not be blank; for example: RETURN 1 AS value LIMIT 1")
	}
	for _, name := range []string{"from", "to", "since"} {
		if o.flags.Changed(name) && strings.TrimSpace(o.flags.Lookup(name).Value.String()) == "" {
			return fmt.Errorf("--%s must not be empty; use --since 1h or both --from and --to", name)
		}
	}
	if o.since != "" && (o.from != "" || o.to != "") {
		return errors.New("--since is mutually exclusive with --from/--to")
	}
	now := time.Now()
	start, end := now.Add(-time.Hour), now
	if o.from != "" || o.to != "" {
		if o.from == "" || o.to == "" {
			return errors.New("--from and --to must be supplied together")
		}
		var err error
		start, err = shared.ParseTime(o.from, now)
		if err != nil {
			return fmt.Errorf("invalid --from: %w", err)
		}
		end, err = shared.ParseTime(o.to, now)
		if err != nil {
			return fmt.Errorf("invalid --to: %w", err)
		}
	}
	if o.since != "" {
		duration, err := shared.ParseDuration(o.since)
		if err != nil {
			return fmt.Errorf("invalid --since: %w", err)
		}
		if duration <= 0 {
			return errors.New("--since must be positive; for example --since 1h")
		}
		start = now.Add(-duration)
	}
	if start.UnixMilli() <= 0 || end.UnixMilli() <= 0 || start.UnixMilli() >= end.UnixMilli() {
		return errors.New("time window must have positive epoch milliseconds and start before end")
	}
	o.request = CypherQueryRequest{Query: o.query, Start: start.UnixMilli(), End: end.UnixMilli()}
	return nil
}

// CypherRowsTableCodec renders projected columns without collapsing graph values or rows.
type CypherRowsTableCodec struct{}

func (c *CypherRowsTableCodec) Format() format.Format { return "table" }

func (c *CypherRowsTableCodec) Encode(w io.Writer, v any) error {
	resp, ok := v.(*CypherQueryResponse)
	if !ok || resp == nil {
		return errors.New("invalid data type for table codec: expected *CypherQueryResponse")
	}
	table := style.NewTable(resp.Columns...).MultilineCells(true)
	for _, row := range resp.Rows {
		values := make([]string, len(row))
		for i, cell := range row {
			var compact bytes.Buffer
			if err := json.Compact(&compact, cell); err != nil {
				return fmt.Errorf("invalid cypher cell: %w", err)
			}
			values[i] = compact.String()
			// Strings are readable as table cells; other values keep their JSON spelling.
			if len(cell) > 0 && compact.Bytes()[0] == '"' {
				var value string
				if err := json.Unmarshal(cell, &value); err != nil {
					return err
				}
				values[i] = strings.ReplaceAll(strings.ReplaceAll(value, "\t", `\t`), "\r", `\r`)
			}
		}
		table.Row(values...)
	}
	return table.Render(w)
}

func (c *CypherRowsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
