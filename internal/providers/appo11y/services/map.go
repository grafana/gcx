package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"text/tabwriter"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/prometheus/common/model"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

type mapOpts struct {
	IO         cmdio.Options
	Datasource string
	Since      string
	Namespace  string
	Filters    []string
	GroupBy    []string
}

func (o *mapOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &serviceMapTableCodec{})
	o.IO.RegisterCustomCodec("wide", &serviceMapTableCodec{Wide: true})
	o.IO.RegisterCustomCodec("mermaid", &serviceMapMermaidCodec{})
	o.IO.RegisterCustomCodec("dot", &serviceMapDOTCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringVarP(&o.Datasource, "datasource", "d", "", "Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)")
	flags.StringVarP(&o.Namespace, "namespace", "n", "", "Service namespace (only needed when the argument is the bare service name and multiple namespaces are in play)")
	flags.StringVar(&o.Since, "since", defaultRedWindow, "Rate/quantile window applied to service-graph metrics (e.g. 1m, 5m, 1h, 1d) — PromQL duration syntax")
	flags.StringArrayVar(&o.Filters, "filter", nil, "Scope the map to service-graph edges matching a label matcher, e.g. --filter k8s_cluster_name=prod-us (repeatable). Use to break a multi-cluster/multi-region service down one cluster at a time; the label must exist on the service-graph metrics")
	flags.StringSliceVar(&o.GroupBy, "group-by", nil, "Split each edge per distinct value of a label, e.g. --group-by k8s_cluster_name (comma-separated or repeatable). The label must exist on the service-graph metrics — note the Tempo service-graph family often omits cluster labels, in which case no edges match")
}

func (o *mapOpts) Validate(cmd *cobra.Command) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(o.Since) == "" {
		return fail.NewCommandUsageError(cmd, "--since must not be empty", nil)
	}
	if _, err := model.ParseDuration(o.Since); err != nil {
		return fail.NewCommandUsageError(cmd, fmt.Sprintf("--since %q is not a valid PromQL duration", o.Since), err)
	}
	return nil
}

func newMapCommand() *cobra.Command {
	opts := &mapOpts{}
	cmd := &cobra.Command{
		Use:   "map <service> [--namespace ns]",
		Short: "Show a service's neighbourhood: who calls it (callers) and who it calls (callees).",
		Long: `Render the service-graph slice for one service: a callers list (services
calling into this one) and a callees list (services this one calls).

Data comes from Tempo's service-graph metric family
(traces_service_graph_request_*), which is consistent across every
metrics-mode — so there's no --metrics-mode flag here.

The argument is either the bare service name or the canonical
"<namespace>/<name>" form; bare names are auto-resolved against
target_info the same way "gcx appo11y services get" does.

Latency is direction-aware: callers see the server-side p95
(how long this service took to respond), callees see the client-side
p95 (how long this service waited on the peer).

Connection type is empty for HTTP/gRPC peers; "database",
"messaging", or "virtual_node" for typed edges. Virtual-node peers
are uninstrumented callers Tempo synthesises from orphan spans.

Beyond --output table/wide/json/yaml, --output mermaid and
--output dot render the map as a Mermaid or Graphviz graph,
suitable for inlining in markdown / piping to "dot -Tpng".`,
		Example: `
  # Default two-section table
  gcx appo11y services map checkoutservice

  # Mermaid graph — paste into a markdown doc or a PR comment
  gcx appo11y services map checkoutservice -o mermaid

  # Graphviz DOT — pipe to dot for an image
  gcx appo11y services map checkoutservice -o dot | dot -Tpng -o map.png

  # Wide table with p95 / connection-type, last hour
  gcx appo11y services map payments/checkoutservice --since 1h -o wide

  # JSON for scripting
  gcx appo11y services map checkoutservice -o json

  # Break a multi-cluster service's map down to a single cluster
  gcx appo11y services map faro-collector --filter k8s_cluster_name=prod-us-central-0

  # Split each edge per cluster (service-graph metrics must carry the label)
  gcx appo11y services map faro-collector --group-by k8s_cluster_name`,
		Args: cobra.ExactArgs(1),
		RunE: runMap(opts),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   `Service-graph slice for one App Observability service: callers (peers calling into the service) and callees (peers the service calls), with per-edge rate (req/s), error %, and direction-aware p95 latency (server-side for callers, client-side for callees). Connection-type label distinguishes HTTP/gRPC (empty), database, messaging, and virtual_node (uninstrumented upstreams synthesised by Tempo). Output formats: table/wide (default two-section view), json/yaml (structured), mermaid (markdown-renderable graph), dot (Graphviz). Pairs with 'gcx appo11y services get' (single-service RED) and 'gcx appo11y services list-operations' (per-endpoint breakdown). Use --filter <label><op><value> (repeatable) to scope the edges to a subset of series — most usefully a cluster/region label (e.g. --filter k8s_cluster_name=prod-us) to break a multi-cluster service down one cluster at a time. Use --group-by <label> to instead split each edge per distinct value of that label (note: the Tempo service-graph family often omits cluster labels, so this may return no edges — --filter/--group-by on span-metric-backed 'get'/'list-operations' is more reliable for cluster breakdowns). Examples: gcx appo11y services map <name> -o json; gcx appo11y services map <ns>/<name> --since 1h -o mermaid; gcx appo11y services map <name> --filter k8s_cluster_name=<cluster> -o json`,
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runMap(opts *mapOpts) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(cmd); err != nil {
			return err
		}
		namespace, name, err := parseServiceArg(args[0], opts.Namespace)
		if err != nil {
			return fail.NewCommandUsageError(cmd, "", err)
		}
		matchers, err := parseFilters(opts.Filters)
		if err != nil {
			return fail.NewCommandUsageError(cmd, "", err)
		}
		groupBy, err := parseGroupBy(opts.GroupBy)
		if err != nil {
			return fail.NewCommandUsageError(cmd, "", err)
		}

		ctx := cmd.Context()
		var loader providers.ConfigLoader

		cfg, err := loader.LoadGrafanaConfig(ctx)
		if err != nil {
			return err
		}

		var cfgCtx *internalconfig.Context
		if fullCfg, err := loader.LoadFullConfig(ctx); err != nil {
			logging.FromContext(ctx).Warn("could not load config; falling back to auto-discovery", slog.String("error", err.Error()))
		} else {
			cfgCtx = fullCfg.GetCurrentContext()
		}

		datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, &loader, opts.Datasource, cfgCtx, cfg, "prometheus")
		if err != nil {
			return err
		}

		client, err := prometheus.NewClient(cfg)
		if err != nil {
			return fmt.Errorf("failed to create prometheus client: %w", err)
		}

		// Bare-name resolution: same UX as `services get` and `services list-operations`.
		if namespace == "" {
			resolved, err := resolveNamespaceForBareName(ctx, client, datasourceUID, name, matchers)
			if err != nil {
				return err
			}
			namespace = resolved
		}

		result, err := fetchServiceMap(ctx, client, datasourceUID, namespace, name, opts.Since, matchers, groupBy)
		if err != nil {
			return err
		}

		notFound := len(result.Callers) == 0 && len(result.Callees) == 0
		if notFound {
			emitNoDataHint(cmd.ErrOrStderr(), namespace, name)
		}
		if err := opts.IO.Encode(cmd.OutOrStdout(), result); err != nil {
			return err
		}
		if notFound {
			return fmt.Errorf("service %q has no service-graph edges in the requested window", jobLabel(namespace, name))
		}
		return nil
	}
}

// fetchServiceMap runs both direction × {rate, errors, p95} = 6 queries
// in parallel and folds them into a ServiceMap. Each direction's edges
// are independently parsed and merged, then sorted by rate desc.
func fetchServiceMap(ctx context.Context, client *prometheus.Client, datasourceUID, namespace, name, window string, matchers []Matcher, groupBy []string) (*ServiceMap, error) {
	type edgeQuerySet struct {
		rate, errors, p95 *prometheus.QueryResponse
	}
	var callerSet, calleeSet edgeQuerySet

	directions := []struct {
		dir mapDirection
		set *edgeQuerySet
	}{
		{callersDirection, &callerSet},
		{calleesDirection, &calleeSet},
	}

	eg, egCtx := errgroup.WithContext(ctx)
	for _, d := range directions {
		eg.Go(func() error {
			expr, err := buildServiceMapEdgeQuery(serviceGraphRequestTotalMetric, d.dir, namespace, name, window, matchers, groupBy)
			if err != nil {
				return fmt.Errorf("failed to build %s rate query: %w", directionLabel(d.dir), err)
			}
			resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
			if err != nil {
				return fmt.Errorf("%s rate query failed: %w", directionLabel(d.dir), err)
			}
			d.set.rate = resp
			return nil
		})
		eg.Go(func() error {
			expr, err := buildServiceMapEdgeQuery(serviceGraphRequestFailedTotalMetric, d.dir, namespace, name, window, matchers, groupBy)
			if err != nil {
				return fmt.Errorf("failed to build %s error-rate query: %w", directionLabel(d.dir), err)
			}
			resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
			if err != nil {
				return fmt.Errorf("%s error-rate query failed: %w", directionLabel(d.dir), err)
			}
			d.set.errors = resp
			return nil
		})
		eg.Go(func() error {
			expr, err := buildServiceMapLatencyQuery(d.dir, namespace, name, window, 0.95, matchers, groupBy)
			if err != nil {
				return fmt.Errorf("failed to build %s p95 latency query: %w", directionLabel(d.dir), err)
			}
			resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
			if err != nil {
				return fmt.Errorf("%s p95 latency query failed: %w", directionLabel(d.dir), err)
			}
			d.set.p95 = resp
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	parseEdges := func(set edgeQuerySet, dir mapDirection) []Edge {
		peerName, peerNs := dir.peerLabels()
		return mergeEdges(
			extractEdges(set.rate, peerName, peerNs, groupBy),
			extractEdges(set.errors, peerName, peerNs, groupBy),
			extractEdges(set.p95, peerName, peerNs, groupBy),
			groupBy,
		)
	}

	return &ServiceMap{
		Service: Service{Name: name, Namespace: namespace},
		Window:  window,
		GroupBy: groupBy,
		Callers: parseEdges(callerSet, callersDirection),
		Callees: parseEdges(calleeSet, calleesDirection),
	}, nil
}

func directionLabel(d mapDirection) string {
	if d == callersDirection {
		return "callers"
	}
	return "callees"
}

// formatPeer renders a peer for table / graph output. Virtual nodes
// have no real namespace, so collapse "virtual:peer" to just "peer";
// otherwise show "name (namespace)" when namespace is non-empty.
func formatPeer(e Edge) string {
	if e.Peer.Namespace == "" {
		return e.Peer.Name
	}
	return fmt.Sprintf("%s (%s)", e.Peer.Name, e.Peer.Namespace)
}

func orDashConnType(t string) string {
	if t == "" {
		return "-"
	}
	return t
}

// peerDisplayLabel renders a peer for graph output, appending the
// --group-by combination in braces (e.g. "cart (billing) {prod-us}") so
// the same peer in two groups reads as two distinct, labelled nodes.
func peerDisplayLabel(e Edge, groupBy []string) string {
	label := formatPeer(e)
	if len(groupBy) > 0 {
		label += " {" + groupLabelString(e.Labels, groupBy) + "}"
	}
	return label
}

// mermaidPeerID folds the group combination into the Mermaid node ID so
// per-group edges to the same peer don't collapse onto one node.
func mermaidPeerID(e Edge, groupBy []string) string {
	ns := e.Peer.Namespace
	if len(groupBy) > 0 {
		ns += "_" + groupLabelString(e.Labels, groupBy)
	}
	return mermaidNodeID(e.Peer.Name, ns)
}

// serviceMapTableCodec renders a two-section table: callers above,
// callees below, separated by a blank line. Default columns:
// PEER, RATE, ERROR %, P95, TYPE. --output wide is identical for now
// (kept for codec symmetry with other commands).
type serviceMapTableCodec struct {
	Wide bool
}

func (c *serviceMapTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *serviceMapTableCodec) Decode(io.Reader, any) error {
	return errors.New("services map table codec does not support decoding")
}

func (c *serviceMapTableCodec) Encode(w io.Writer, v any) error {
	resp, ok := v.(*ServiceMap)
	if !ok {
		return fmt.Errorf("invalid data type for services map table codec: %T", v)
	}
	if err := c.encodeSection(w, "CALLERS", resp.Callers, resp.GroupBy); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	return c.encodeSection(w, "CALLEES", resp.Callees, resp.GroupBy)
}

func (c *serviceMapTableCodec) encodeSection(w io.Writer, label string, edges []Edge, groupBy []string) error {
	if len(edges) == 0 {
		_, err := fmt.Fprintf(w, "%s: (none)\n", label)
		return err
	}
	// When grouping, a column per group label is inserted after the peer
	// column so each row reads "<peer> <group> ...".
	headers := make([]string, 0, len(groupBy)+5)
	headers = append(headers, label)
	headers = append(headers, upperHeaders(groupBy)...)
	headers = append(headers, "RATE", "ERROR %", "P95", "TYPE")
	t := style.NewTable(headers...)
	for _, e := range edges {
		row := []string{formatPeer(e)}
		for _, l := range groupBy {
			row = append(row, orDash(e.Labels[l]))
		}
		row = append(row,
			formatRateWithUnit(e.RatePerSecond, e.RatePerSecond > 0),
			formatPercentMaybe(e.ErrorPercent, e.RatePerSecond > 0),
			formatDuration(e.P95Seconds, e.HasLatency),
			orDashConnType(e.ConnectionType),
		)
		t.Row(row...)
	}
	return t.Render(w)
}

// mermaidNodeID sanitises a peer identity into a Mermaid-safe node ID.
// Mermaid IDs must be alphanumeric plus underscores; everything else
// is folded into underscores. Namespace is folded in to keep IDs
// unique across same-name peers from different namespaces. The node
// label remains human-readable via the [text] / ((text)) etc. forms
// emitted by mermaidNode.
func mermaidNodeID(name, namespace string) string {
	combined := name
	if namespace != "" {
		combined = namespace + "_" + name
	}
	var b strings.Builder
	for _, r := range combined {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if out == "" {
		return "node"
	}
	return out
}

// mermaidNode renders a node declaration with the right shape per
// connection type:
//
//	default                  → rectangle  ["label"]
//	database peer            → cylinder   [("label")]
//	messaging_system peer    → queue      [/"label"\]
//	virtual_node             → rounded    ("label")
//
// The center node (the queried service) uses a hex-shape {{label}} so
// it stands out from the peers. connection_type label values come from
// Tempo's service-graph processor — see the connType constants below.
func mermaidNode(id, label, connType string, center bool) string {
	escaped := strings.ReplaceAll(label, `"`, `&quot;`)
	if center {
		return fmt.Sprintf("%s{{%q}}", id, escaped)
	}
	switch connType {
	case connTypeDatabase:
		return fmt.Sprintf("%s[(%q)]", id, escaped)
	case connTypeMessagingSystem:
		return fmt.Sprintf("%s[/%q\\]", id, escaped)
	case connTypeVirtualNode:
		return fmt.Sprintf("%s(%q)", id, escaped)
	default:
		return fmt.Sprintf("%s[%q]", id, escaped)
	}
}

// connType* are the literal `connection_type` label values Tempo's
// service-graph processor emits. Empty means HTTP/gRPC (no special
// classification). Centralising the constants keeps the table and
// graph codecs in sync.
const (
	connTypeDatabase        = "database"
	connTypeMessagingSystem = "messaging_system"
	connTypeVirtualNode     = "virtual_node"
)

// serviceMapMermaidCodec emits a Mermaid `graph LR` block — renders
// inline in GitHub, Slack, Claude, and any markdown viewer.
type serviceMapMermaidCodec struct{}

func (c *serviceMapMermaidCodec) Format() format.Format { return "mermaid" }
func (c *serviceMapMermaidCodec) Decode(io.Reader, any) error {
	return errors.New("services map mermaid codec does not support decoding")
}

func (c *serviceMapMermaidCodec) Encode(w io.Writer, v any) error {
	resp, ok := v.(*ServiceMap)
	if !ok {
		return fmt.Errorf("invalid data type for services map mermaid codec: %T", v)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 1, ' ', 0)
	fmt.Fprintln(tw, "graph LR")
	centerID := mermaidNodeID(resp.Service.Name, resp.Service.Namespace)
	centerLabel := resp.Service.Name
	if resp.Service.Namespace != "" {
		centerLabel = resp.Service.Namespace + "/" + resp.Service.Name
	}
	fmt.Fprintf(tw, "  %s\n", mermaidNode(centerID, centerLabel, "", true))

	emitEdges := func(edges []Edge, into bool) {
		for _, e := range edges {
			peerID := mermaidPeerID(e, resp.GroupBy)
			fmt.Fprintf(tw, "  %s\n", mermaidNode(peerID, peerDisplayLabel(e, resp.GroupBy), e.ConnectionType, false))
			label := formatMermaidEdgeLabel(e)
			if into {
				fmt.Fprintf(tw, "  %s -->|%s| %s\n", peerID, label, centerID)
			} else {
				fmt.Fprintf(tw, "  %s -->|%s| %s\n", centerID, label, peerID)
			}
		}
	}
	emitEdges(resp.Callers, true)
	emitEdges(resp.Callees, false)
	return tw.Flush()
}

func formatMermaidEdgeLabel(e Edge) string {
	rate := formatRateWithUnit(e.RatePerSecond, e.RatePerSecond > 0)
	// Mermaid edge labels can't contain pipes or backticks; rate
	// strings don't use either, but keep the helper defensively.
	rate = strings.ReplaceAll(rate, "|", "/")
	return rate
}

// serviceMapDOTCodec emits a Graphviz `digraph` block. Same node-shape
// vocabulary as Mermaid (cylinder/queue/rounded/box). Pipe to
// `dot -Tpng` or `dot -Tsvg` for a rendered image.
type serviceMapDOTCodec struct{}

func (c *serviceMapDOTCodec) Format() format.Format { return "dot" }
func (c *serviceMapDOTCodec) Decode(io.Reader, any) error {
	return errors.New("services map dot codec does not support decoding")
}

func (c *serviceMapDOTCodec) Encode(w io.Writer, v any) error {
	resp, ok := v.(*ServiceMap)
	if !ok {
		return fmt.Errorf("invalid data type for services map dot codec: %T", v)
	}
	fmt.Fprintln(w, "digraph G {")
	fmt.Fprintln(w, "  rankdir=LR;")

	centerLabel := resp.Service.Name
	if resp.Service.Namespace != "" {
		centerLabel = resp.Service.Namespace + "/" + resp.Service.Name
	}
	fmt.Fprintf(w, "  %q [shape=box, style=\"filled,bold\", fillcolor=lightblue];\n", centerLabel)

	emit := func(edges []Edge, into bool) {
		for _, e := range edges {
			peerLabel := peerDisplayLabel(e, resp.GroupBy)
			fmt.Fprintf(w, "  %q [%s];\n", peerLabel, dotPeerAttrs(e.ConnectionType))
			edgeLabel := formatRateWithUnit(e.RatePerSecond, e.RatePerSecond > 0)
			if into {
				fmt.Fprintf(w, "  %q -> %q [label=%q];\n", peerLabel, centerLabel, edgeLabel)
			} else {
				fmt.Fprintf(w, "  %q -> %q [label=%q];\n", centerLabel, peerLabel, edgeLabel)
			}
		}
	}
	emit(resp.Callers, true)
	emit(resp.Callees, false)
	fmt.Fprintln(w, "}")
	return nil
}

func dotPeerAttrs(connType string) string {
	switch connType {
	case connTypeDatabase:
		return `shape=cylinder, style=filled, fillcolor="#fef3c7"`
	case connTypeMessagingSystem:
		return `shape=parallelogram, style=filled, fillcolor="#fde68a"`
	case connTypeVirtualNode:
		return `shape=ellipse, style="filled,dashed", fillcolor="#e5e7eb"`
	default:
		return `shape=box`
	}
}
