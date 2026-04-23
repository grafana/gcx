package infinity

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/infinity"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
)

// QueryCmd returns the `query` subcommand for an Infinity datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	var (
		datasource   string
		queryType    string
		rootSelector string
		method       string
		inline       string
		graphqlQuery string
		headers      []string
	)

	cmd := &cobra.Command{
		Use:   "query [URL]",
		Short: "Fetch data from a URL or inline source via the Infinity datasource",
		Long: `Fetch JSON, CSV, TSV, XML, GraphQL, or HTML data through a Grafana Infinity datasource.

URL is the target endpoint passed as a positional argument.
Use --inline to provide data directly instead of fetching from a URL.
Datasource is resolved from -d flag or datasources.infinity in your context.`,
		Example: `
  # Fetch JSON from a URL
  gcx datasources infinity query https://api.example.com/users --type json

  # Fetch with a JSONPath root selector
  gcx datasources infinity query https://api.example.com/data --type json --root '$.items'

  # Inline JSON data
  gcx datasources infinity query --inline '[{"name":"alice"},{"name":"bob"}]' --type json

  # GraphQL query
  gcx datasources infinity query https://api.example.com/graphql --type graphql --graphql 'query { users { id name } }'

  # CSV with custom headers
  gcx datasources infinity query https://example.com/data.csv --type csv --header 'Authorization=Bearer token'

  # Output as JSON
  gcx datasources infinity query -d UID https://api.example.com/data -o json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
				return err
			}

			var targetURL, source string
			switch {
			case len(args) == 1 && inline != "":
				return errors.New("provide either a URL argument or --inline, not both")
			case len(args) == 1:
				targetURL = args[0]
				source = "url"
			case inline != "":
				source = "inline"
			default:
				return errors.New("URL argument or --inline is required")
			}

			ctx := cmd.Context()

			var cfgCtx *internalconfig.Context
			fullCfg, err := loader.LoadFullConfig(ctx)
			if err != nil {
				logging.FromContext(ctx).Warn("could not load config; falling back to auto-discovery", slog.String("error", err.Error()))
			} else {
				cfgCtx = fullCfg.GetCurrentContext()
			}

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, datasource, cfgCtx, cfg, "infinity")
			if err != nil {
				return err
			}

			dsType, err := dsquery.GetDatasourceType(ctx, cfg, datasourceUID)
			if err != nil {
				return err
			}
			if err := dsquery.ValidateDatasourceType(dsType, "infinity"); err != nil {
				return err
			}

			now := time.Now()
			start, end, _, err := shared.ParseTimes(now)
			if err != nil {
				return err
			}

			client, err := infinity.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			req := infinity.QueryRequest{
				Type:         queryType,
				Source:       source,
				URL:          targetURL,
				Data:         inline,
				RootSelector: rootSelector,
				Method:       method,
				Headers:      ParseHeaders(headers),
				GraphQL:      graphqlQuery,
				Start:        start,
				End:          end,
			}

			resp, err := client.Query(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			return shared.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "large",
		agent.AnnotationLLMHint:   `gcx datasources infinity query -d UID https://api.example.com/data --type json --root '$.items' -o json`,
	}

	dsquery.RegisterCodecs(&shared.IO, false)
	shared.IO.BindFlags(cmd.Flags())
	shared.SetupTimeFlags(cmd.Flags())

	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.infinity is configured)")
	cmd.Flags().StringVar(&queryType, "type", "json", "Data type: json, csv, tsv, xml, graphql, html")
	cmd.Flags().StringVar(&rootSelector, "root", "", "Root selector (JSONPath for JSON, XPath for XML/HTML)")
	cmd.Flags().StringVar(&method, "method", "GET", "HTTP method: GET or POST")
	cmd.Flags().StringVar(&inline, "inline", "", "Inline data string (replaces URL)")
	cmd.Flags().StringVar(&graphqlQuery, "graphql", "", "GraphQL query string")
	cmd.Flags().StringArrayVar(&headers, "header", nil, "Custom header in key=value format (repeatable)")

	return cmd
}

// ParseHeaders parses a slice of "key=value" strings into a map.
// Invalid entries (without '=') are skipped. Returns nil if the input is
// empty or no valid entries are found.
func ParseHeaders(headers []string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	var result map[string]string
	for _, h := range headers {
		parts := strings.SplitN(h, "=", 2)
		if len(parts) == 2 {
			if result == nil {
				result = make(map[string]string, len(headers))
			}
			result[parts[0]] = parts[1]
		}
	}
	return result
}
