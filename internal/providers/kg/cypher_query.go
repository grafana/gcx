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
The response has columns, rows and stats (rowCount, columnCount, elapsedMs);
values keep their JSON shape, and node/relationship refs are local to one
response. Use --json columns,rows,stats or --jq '.rows' to select fields.

Time window. Every query runs over a window: --since (ending now) or both
--from and --to, the last hour by default. A node or relationship matches if
it existed at any point in the window; this is checked at every MATCH and
before aggregation. It is not a snapshot: elements of one path need not
coexist, entities can expire and reappear, and no match now does not mean it
never existed. Use the same --from/--to for kg meta schema, kg meta scopes,
kg entities, metrics and logs. --since is relative to now; use --from/--to for
a past incident. --from must be before --to; for an instant, query a short
window around it (e.g. one minute either side), not a wide window that also
covers other times.

LIMIT. Every query needs a literal LIMIT (CYPHER_LIMIT_REQUIRED). It is capped
per tenant, 300 by default; a larger LIMIT is rejected (CYPHER_LIMIT_TOO_LARGE)
and the error names the cap. There is no paging indicator: if rowCount equals
LIMIT the result may be truncated, so don't report it as complete or as a
total. Page with ORDER BY and SKIP/LIMIT.

Syntax. Inline values as Cypher literals; '$name' parameters are not
supported (CYPHER_PARAMETERS_NOT_SUPPORTED). Only a fixed subset of Cypher is
accepted, and anything else is refused (CYPHER_UNSUPPORTED_CONSTRUCT) with a
message naming the supported form. Common refusals: disconnected or multiple
MATCH patterns, more than one OPTIONAL MATCH, UNION, id()/elementId(),
shortestPath(), EXISTS{}/COUNT{} subqueries and unbounded variable-length paths.

Scope. Match an entity on name, type (node label, e.g. :Service) and scope
(env, site, namespace) together. Scope is stack-specific: relabel rules decide
which label feeds each dimension (often cluster becomes env), and not every
type carries all three. Discover values with 'gcx kg meta scopes', over the same
window and once per window; never guess them. A dimension that scopes lists only
as "none" is unset: leave it out rather than filling it with another
dimension's value. Scope values are plain node properties, e.g.
'MATCH (s:Service {name: "checkout", env: "prod"}) RETURN s LIMIT 1'.

Schema. Read it once per window, before writing queries:
'gcx kg meta schema --from <start> --to <end> -o text' prints each entity type
with its properties, then every 'A --TYPE--> B' relationship ('gcx kg meta all'
with the same flags also prints scopes, metrics, logs, traces and profiles).
Use only the labels, properties, relationship types and directions it lists.
A property that is not listed returns null.

Relationships. Match both directions when looking for what depends on an
entity. One query can do it with the undirected form, returning both ends to
keep the direction:
'MATCH (a)-[r:TYPE]-(b) RETURN startNode(r).name, endNode(r).name LIMIT 50'.

A property, scope value, label, relationship type or direction that does not
exist matches nothing: the result is empty, not an error.

Insights. This command never returns insights (Saturation, Anomaly, Amend,
Failure, Error), and none should be inferred from its rows. For current health,
use 'gcx kg entities list --type <type> --property name=<name>' (add
--insight any to keep only entities with an active insight); use
'gcx kg entities inspect' for the full timeline. Call both with the same window
and all three of --env, --site and --namespace (leave out a null one), so
return each node's env, site and namespace in this query.`,
		Example: `  # Read a few schema-listed properties of one Volume, matched on name and scope, instead of returning the whole node (scope values from 'gcx kg meta scopes')
  gcx kg graph query "MATCH (v:Volume {name: '<name>', env: '<env>', namespace: '<namespace>'}) RETURN v.storageclass AS storageclass, v.capacity_bytes AS capacity_bytes LIMIT 5" --from <start> --to <end>

  # Count one env's Nodes per zone and instance type (Nodes have no namespace); while rowCount equals LIMIT, fetch the next page with SKIP 50
  gcx kg graph query "MATCH (n:Node {env: '<env>'}) RETURN n.zone AS zone, n.instance_type AS instance_type, count(n) AS nodes ORDER BY zone, instance_type SKIP 0 LIMIT 50" --from <start> --to <end>

  # Match one schema relationship type undirected around an env's Nodes and keep each edge's direction with startNode(r)/endNode(r)
  gcx kg graph query "MATCH (n:Node {env: '<env>'})-[r:HOSTS]-(x) RETURN DISTINCT labels(startNode(r))[0] AS from_type, labels(endNode(r))[0] AS to_type ORDER BY from_type, to_type LIMIT 50" --from <start> --to <end>

  # Trace the hosting paths from an env's cluster down to one ServiceInstance, up to 4 HOSTS hops, returning hop names rather than whole paths
  gcx kg graph query "MATCH p = (c:KubeCluster {env: '<env>'})-[:HOSTS*1..4]->(i:ServiceInstance {name: '<name>', env: '<env>', namespace: '<namespace>'}) RETURN length(p) AS hops, nodes(p)[1].name AS hop1, nodes(p)[2].name AS hop2, nodes(p)[3].name AS hop3, nodes(p)[4].name AS hop4 ORDER BY hops LIMIT 50" --from <start> --to <end>`,
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
