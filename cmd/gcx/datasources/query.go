package datasources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/query/clickhouse"
	"github.com/grafana/gcx/internal/query/dataframe"
	"github.com/grafana/gcx/internal/query/grafanaquery"
	"github.com/grafana/gcx/internal/query/influxdb"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/spf13/cobra"
)

// QueryCmd returns the auto-detecting query command for the datasources group.
func QueryCmd() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	shared := &dsquery.SharedOpts{}
	var profileType string
	var maxNodes int64
	var limit int
	var rawQuery string

	cmd := &cobra.Command{
		Use:   "query DATASOURCE_UID [EXPR]",
		Short: "Execute a query against any datasource (auto-detects type)",
		Long: `Execute a query against any datasource, automatically detecting the datasource type.

DATASOURCE_UID is always required (no default resolution for generic).
EXPR is the query expression appropriate for the datasource type.

The datasource type is detected via the Grafana API and the appropriate query
client is used automatically. This is the escape hatch for datasource types
that do not have a dedicated subcommand.

Use --query to provide a raw query JSON object for any datasource type,
bypassing type-specific logic. The object is merged into the Grafana datasource
query envelope — datasource UID, type, refId, and time range are injected
automatically. Use @file to read from a file or @- for stdin.`,
		Example: `
  # Auto-detect and query any supported datasource
  gcx datasources query ds-001 'up{job="grafana"}' --from now-1h --to now

  # Loki via auto-detect (with limit)
  gcx datasources query loki-001 '{job="varlogs"}' --from now-1h --to now --limit 200

  # Pyroscope via auto-detect
  gcx datasources query pyro-001 '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --from now-1h --to now

  # Raw query JSON (plugin-agnostic) — Infinity example
  gcx datasources query infinity-01 --query '{
    "type": "json", "source": "url",
    "url": "https://api.example.com/data",
    "format": "table", "root_selector": "$.items"
  }' --since 30m

  # Raw query from file
  gcx datasources query ds-001 --query @query.json --since 1h

  # Raw query from stdin
  cat query.json | gcx datasources query ds-001 --query @- --since 1h`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
				return err
			}

			queryFlagSet := cmd.Flags().Changed("query")

			// Default empty --query to "{}".
			if queryFlagSet && rawQuery == "" {
				rawQuery = "{}"
			}

			// --query is mutually exclusive with EXPR and --expr.
			if queryFlagSet {
				if len(args) > 1 {
					return errors.New("--query is mutually exclusive with a positional EXPR argument")
				}
				if shared.Expr != "" {
					return errors.New("--query is mutually exclusive with --expr")
				}
			}

			// Reject "both positional and --expr" before any HTTP call.
			if !queryFlagSet && len(args) > 1 && shared.Expr != "" {
				return errors.New("provide the expression as a positional argument or via --expr, not both")
			}

			ctx := cmd.Context()
			datasourceUID := args[0]

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			// Raw query path: skip type-specific dispatch.
			if queryFlagSet {
				return executeRawQuery(ctx, cmd, cfg, datasourceUID, rawQuery, shared)
			}

			return executeTypedQuery(ctx, cmd, cfg, datasourceUID, args, shared, profileType, maxNodes, limit)
		},
	}

	configOpts.BindFlags(cmd.Flags())
	shared.Setup(cmd.Flags(), true)
	cmd.Flags().StringVar(&profileType, "profile-type", "", "Profile type ID for pyroscope queries (e.g., 'process_cpu:cpu:nanoseconds:cpu:nanoseconds')")
	cmd.Flags().Int64Var(&maxNodes, "max-nodes", 1024, "Maximum nodes in flame graph (pyroscope only)")
	cmd.Flags().IntVar(&limit, "limit", dsquery.DefaultLokiLimit, "Maximum number of log lines to return for loki queries (0 means no limit)")
	cmd.Flags().StringVar(&rawQuery, "query", "", "Raw query JSON object (plugin-agnostic); use @file for file, @- for stdin")

	return cmd
}

// executeTypedQuery handles auto-detect dispatch for known datasource types.
func executeTypedQuery(
	ctx context.Context,
	cmd *cobra.Command,
	cfg config.NamespacedRESTConfig,
	datasourceUID string,
	args []string,
	shared *dsquery.SharedOpts,
	profileType string,
	maxNodes int64,
	limit int,
) error {
	rawType, err := dsquery.GetDatasourceType(ctx, cfg, datasourceUID)
	if err != nil {
		return err
	}
	dsType := dsquery.NormalizeKind(rawType)

	// Short-circuit unsupported types before requiring an expression, so the
	// user sees a helpful hint instead of "expression is required".
	switch dsType {
	case "prometheus", "loki", "pyroscope", "influxdb", "clickhouse":
		// supported — fall through to expression resolution
	case "cloudwatch":
		return errors.New("CloudWatch queries are structured (namespace, metric, dimensions, region, statistic, period); " +
			"the generic `gcx datasources query <uid> <expr>` form can't carry them — " +
			"use `gcx datasources cloudwatch query --namespace ... --metric ... --region ...` instead")
	default:
		return fmt.Errorf("datasource type %q is not supported by auto-detect (supported: prometheus, loki, pyroscope, influxdb, clickhouse); "+
			"use --query to provide a raw query JSON for this datasource type", dsType)
	}

	expr, err := shared.ResolveExpr(args, 1)
	if err != nil {
		return err
	}

	now := time.Now()
	start, end, step, err := shared.ParseTimes(now)
	if err != nil {
		return err
	}

	switch dsType {
	case "prometheus":
		client, err := prometheus.NewClient(cfg)
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}

		req := prometheus.QueryRequest{
			Query: expr,
			Start: start,
			End:   end,
			Step:  step,
		}

		resp, err := client.Query(ctx, datasourceUID, req)
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}

		return shared.IO.Encode(cmd.OutOrStdout(), resp)

	case "loki":
		client, err := loki.NewClient(cfg)
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}

		req := loki.QueryRequest{
			Query: expr,
			Start: start,
			End:   end,
			Step:  step,
			Limit: limit,
		}

		resp, err := client.Query(ctx, datasourceUID, req)
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}

		return shared.IO.Encode(cmd.OutOrStdout(), resp)

	case "pyroscope":
		if profileType == "" {
			return errors.New("--profile-type is required for pyroscope queries")
		}

		client, err := pyroscope.NewClient(cfg)
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}

		req := pyroscope.QueryRequest{
			LabelSelector: expr,
			ProfileTypeID: profileType,
			Start:         start,
			End:           end,
			MaxNodes:      maxNodes,
		}

		resp, err := client.Query(ctx, datasourceUID, req)
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}

		return shared.IO.Encode(cmd.OutOrStdout(), resp)

	case "influxdb":
		influxClient, err := influxdb.NewClient(cfg)
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}

		modeStr, err := dsquery.GetInfluxDBMode(ctx, cfg, datasourceUID)
		if err != nil {
			return fmt.Errorf("failed to detect influxdb mode: %w", err)
		}

		req := influxdb.QueryRequest{
			Query: expr,
			Start: start,
			End:   end,
			Step:  step,
			Mode:  influxdb.Mode(modeStr),
		}

		resp, err := influxClient.Query(ctx, datasourceUID, req)
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}

		return shared.IO.Encode(cmd.OutOrStdout(), resp)

	case "clickhouse":
		client, err := clickhouse.NewClient(cfg)
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}

		req := clickhouse.QueryRequest{
			RawSQL: clickhouse.EnforceLimit(expr, 100, 1000),
			Start:  start,
			End:    end,
		}
		if step > 0 {
			req.IntervalMs = step.Milliseconds()
		}

		resp, err := client.Query(ctx, datasourceUID, req)
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}

		return shared.IO.Encode(cmd.OutOrStdout(), resp)

	default:
		// Unreachable: unsupported types are rejected before expression resolution.
		return fmt.Errorf("unsupported datasource type %q; use --query", dsType)
	}
}

// executeRawQuery executes a plugin-agnostic raw query. The user-provided JSON
// is merged into the Grafana datasource query envelope with the datasource UID,
// plugin type, refId, and time range injected automatically.
func executeRawQuery(
	ctx context.Context,
	cmd *cobra.Command,
	cfg config.NamespacedRESTConfig,
	datasourceUID string,
	rawQueryInput string,
	shared *dsquery.SharedOpts,
) error {
	queryJSON, err := resolveQueryJSON(cmd, rawQueryInput)
	if err != nil {
		return err
	}

	var queryObj map[string]any
	if err := json.Unmarshal([]byte(queryJSON), &queryObj); err != nil {
		return fmt.Errorf("invalid --query JSON: %w", err)
	}

	// Look up the datasource type so the query envelope is well-formed.
	rawType, err := dsquery.GetDatasourceType(ctx, cfg, datasourceUID)
	if err != nil {
		return err
	}

	// Build the full query object: inject refId and datasource, then merge
	// user-provided fields (user fields override defaults like refId).
	fullQuery := map[string]any{
		"refId": "A",
		"datasource": map[string]any{
			"type": rawType,
			"uid":  datasourceUID,
		},
	}
	maps.Copy(fullQuery, queryObj)

	// Resolve time range.
	now := time.Now()
	start, end, _, err := shared.ParseTimes(now)
	if err != nil {
		return err
	}

	var from, to string
	if !start.IsZero() && !end.IsZero() {
		from = strconv.FormatInt(start.UnixMilli(), 10)
		to = strconv.FormatInt(end.UnixMilli(), 10)
	} else {
		from = "now-1h"
		to = "now"
	}

	bodyMap := map[string]any{
		"queries": []any{fullQuery},
		"from":    from,
		"to":      to,
	}

	body, err := json.Marshal(bodyMap)
	if err != nil {
		return fmt.Errorf("failed to marshal query request: %w", err)
	}

	client, err := grafanaquery.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create query client: %w", err)
	}

	respBody, err := client.Execute(ctx, body, rawType, "query")
	if err != nil {
		return err
	}

	var grafanaResp dataframe.Response
	if err := json.Unmarshal(respBody, &grafanaResp); err != nil {
		return fmt.Errorf("failed to parse query response: %w", err)
	}

	if result, ok := grafanaResp.Results["A"]; ok && result.Error != "" {
		status := result.Status
		if status == 0 {
			status = http.StatusBadRequest
		}
		return queryerror.New(rawType, "query", status, result.Error, result.ErrorSource)
	}

	return shared.IO.Encode(cmd.OutOrStdout(), dataframe.ConvertResponse(&grafanaResp))
}

// resolveQueryJSON reads the raw query JSON from inline text, a file (@path),
// or stdin (@-). It follows the same convention as `gcx api -d`.
func resolveQueryJSON(cmd *cobra.Command, input string) (string, error) {
	if input == "" {
		return "", errors.New("--query value is empty")
	}
	if input == "@-" {
		b, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", fmt.Errorf("failed to read query from stdin: %w", err)
		}
		return string(b), nil
	}
	if strings.HasPrefix(input, "@") {
		b, err := os.ReadFile(input[1:])
		if err != nil {
			return "", fmt.Errorf("failed to read query file: %w", err)
		}
		return string(b), nil
	}
	return input, nil
}
