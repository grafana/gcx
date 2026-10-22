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
The response has columns, rows and stats (rowCount, columnCount, elapsedMs).
Each row is an array aligned with columns, values keep their JSON shape, and
node/relationship refs are local to one response.

Time window. Every query runs over a window: --since (ending now) or both
--from and --to, the last hour by default. A node or relationship matches if
it existed at any point in the window; this is checked at every MATCH and
before aggregation. A result is not a snapshot: elements of one path need not
coexist, entities can expire and reappear, and no match now does not mean it
never existed. --from must be before --to, so an instant is a short window
around it. The schema, scope values, entities, metrics and logs also depend on
the window, and their commands take the same --from/--to.

LIMIT. Every query needs a literal LIMIT (CYPHER_LIMIT_REQUIRED). It is capped
per tenant, 300 by default; a larger LIMIT is rejected (CYPHER_LIMIT_TOO_LARGE)
and the error names the cap. rowCount counts returned rows, not matches, and
there is no total or next-page marker: a rowCount equal to LIMIT can mean more
rows exist. ORDER BY with SKIP and LIMIT pages through a result.

Syntax. Values are inline Cypher literals; '$name' parameters are not
supported (CYPHER_PARAMETERS_NOT_SUPPORTED). Only a fixed subset of Cypher is
accepted, and anything else is refused (CYPHER_UNSUPPORTED_CONSTRUCT) with a
message naming the supported form. Common refusals: disconnected or multiple
MATCH patterns, more than one OPTIONAL MATCH, UNION, id()/elementId(),
shortestPath(), EXISTS{}/COUNT{} subqueries, list comprehensions, reduce() and
unbounded variable-length paths.

Scope. Match an entity on name, type (node label, e.g. :Service) and scope
(env, site, namespace) together. Scope is stack-specific: relabel rules decide
which label feeds each dimension (often cluster becomes env), and not every
type carries all three. Discover values with 'gcx kg meta scopes', over the same
window and once per window; never guess them. A dimension that scopes lists only
as "none" is unset: leave it out rather than filling it with another
dimension's value. Scope values are plain node properties, e.g.
'MATCH (s:Service {name: "<name>", env: "<env>"}) RETURN s.name AS name, s.namespace AS namespace LIMIT 5'.

Schema. Read it once per window, before writing queries:
'gcx kg meta schema --from <start> --to <end> -o text' prints each entity type
with its properties, then every 'A --TYPE--> B' relationship ('gcx kg meta all'
with the same flags also prints scopes, metrics, logs, traces and profiles).
Use only the labels, properties, relationship types and directions it lists.
Types, properties and relationships differ between stacks and windows. A
relationship is stored in the direction the schema lists; the undirected form
'(a)-[r:TYPE]-(b)' matches it from either end, and startNode(r)/endNode(r)
return its stored ends.

Queries are not checked against the schema. A label, property, relationship
type, direction or scope value that does not exist matches nothing, and a
property a node lacks, or a list index past the end such as nodes(p)[4] on a
shorter path, returns null. Neither is an error.

Insights. The graph holds entities, their properties and relationships, not
health: no row carries insights (Saturation, Anomaly, Amend, Failure, Error).
An entity's active insights come from 'gcx kg entities list --type <type>
--property name=<name>' ('--insight any' keeps only entities that have one),
and its insight timeline from 'gcx kg entities inspect'. Both take the window
and --env, --site and --namespace, which match the node properties of the same
names. With no scope flag, inspect first looks the entity up by type and name
and uses the single match it gets, which can be a same-named entity in another
scope.`,
		Example: `  # Project two properties of one Volume, matched on name and scope
  gcx kg graph query "MATCH (v:Volume {name: '<name>', env: '<env>', namespace: '<namespace>'}) RETURN v.storageclass AS storageclass, v.capacity_bytes AS capacity_bytes LIMIT 5" --from <start> --to <end>

  # Count an env's Nodes per zone and instance type, 50 groups per page (the next page is SKIP 50)
  gcx kg graph query "MATCH (n:Node {env: '<env>'}) RETURN n.zone AS zone, n.instance_type AS instance_type, count(n) AS nodes ORDER BY zone, instance_type SKIP 0 LIMIT 50" --from <start> --to <end>

  # List the entity types at each end of the HOSTS edges that touch an env's Nodes, in stored direction
  gcx kg graph query "MATCH (n:Node {env: '<env>'})-[r:HOSTS]-(x) RETURN DISTINCT labels(startNode(r))[0] AS from_type, labels(endNode(r))[0] AS to_type ORDER BY from_type, to_type LIMIT 50" --from <start> --to <end>

  # Name the hops of HOSTS paths up to 4 long from an env's cluster to one ServiceInstance; hops past a path's length are null
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
