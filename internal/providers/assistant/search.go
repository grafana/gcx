package assistant

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/assistant"
	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/shared"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type searchOpts struct {
	IO          cmdio.Options
	query       string
	collections []string
	from        string
	to          string
	startTime   *time.Time
	endTime     *time.Time
	timeout     int
}

func (o *searchOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &searchCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.StringSliceVar(&o.collections, "collections", nil, "Required comma-separated collections (case-insensitive): "+searchCollectionNames()+"; searches the union server-side")
	flags.StringVar(&o.from, "from", "", "Include investigations and incidents created at or after this time (RFC3339, Unix timestamp, or relative like 'now-30d'); when both time flags are omitted, the backend searches the last three calendar months")
	flags.StringVar(&o.to, "to", "", "Include investigations and incidents created before this time (exclusive; RFC3339, Unix timestamp, or relative like 'now'); supplying either time flag removes the backend's default lower bound")
	flags.IntVar(&o.timeout, "timeout", 60, "Maximum time in seconds for configuration resolution and search")
}

func (o *searchOpts) Validate(cmd *cobra.Command, now time.Time) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	o.query = strings.TrimSpace(o.query)
	if o.query == "" {
		return errors.New("query must not be empty; example: gcx assistant search 'checkout latency' --collections dashboards")
	}
	if err := validateTimeoutSeconds(o.timeout); err != nil {
		return err
	}
	if len(o.collections) == 0 {
		return errors.New("--collections is required; choose from " + searchCollectionNames())
	}
	canonical := make([]string, 0, len(o.collections))
	seen := make(map[string]bool)
	for _, raw := range o.collections {
		name := ""
		for _, allowed := range searchCollections() {
			if strings.EqualFold(strings.TrimSpace(raw), allowed) {
				name = allowed
				break
			}
		}
		if name == "" {
			return fmt.Errorf("invalid collection %q: choose from %s; example: --collections dashboards,investigations", raw, searchCollectionNames())
		}
		if !seen[name] {
			canonical = append(canonical, name)
			seen[name] = true
		}
	}
	o.collections = canonical
	o.startTime = nil
	o.endTime = nil
	if cmd.Flags().Changed("from") && strings.TrimSpace(o.from) == "" {
		return errors.New("--from must not be empty; example: --from now-30d")
	}
	if cmd.Flags().Changed("to") && strings.TrimSpace(o.to) == "" {
		return errors.New("--to must not be empty; example: --to now")
	}
	if strings.TrimSpace(o.from) != "" {
		parsed, err := shared.ParseTime(o.from, now)
		if err != nil {
			return fmt.Errorf("invalid --from %q: expected RFC3339, a Unix timestamp, or a relative time such as now-30d; example: --from now-30d: %w", o.from, err)
		}
		parsed = parsed.UTC()
		o.startTime = &parsed
	}
	if strings.TrimSpace(o.to) != "" {
		parsed, err := shared.ParseTime(o.to, now)
		if err != nil {
			return fmt.Errorf("invalid --to %q: expected RFC3339, a Unix timestamp, or a relative time such as now; example: --to now: %w", o.to, err)
		}
		parsed = parsed.UTC()
		o.endTime = &parsed
	}
	if o.startTime != nil && o.endTime != nil && !o.startTime.Before(*o.endTime) {
		return fmt.Errorf("invalid history range: --from %q must be before --to %q; example: --from now-30d --to now", o.from, o.to)
	}
	return nil
}

func searchCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &searchOpts{}
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search historical and indexed Grafana evidence.",
		Long: `Search infrastructure memories, dashboards (including panel queries), past
investigations, mostly resolved incidents, and alert-rule definitions.
Choose the smallest set of collections that answers your question.

For investigations and incidents, omitting --from and --to searches the last
three calendar months. Supplying either flag replaces that default; the omitted
side is unbounded. These flags do not affect other collections.

Search runs on the server in retrieval-only mode: no generative query rewriting,
relevance filtering, or enrichment agent. Results are ranked and capped by the
backend, not exhaustive; total counts returned hits, not all matching resources.
Collection errors and disabled collections are preserved in the output. A partial
failure exits 4; failure of every searched collection exits 1.

For current telemetry use metrics/logs/traces query; for current alert state or
active incidents use alert and IRM commands. Use assistant prompt for reasoning.
The OAuth Grafana proxy currently requires grafana-api:write for this POST
endpoint, even though retrieval is read-only.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return err
			}
			opts.query = args[0]
			return opts.Validate(cmd, time.Now().UTC())
		},
		Example: `  gcx assistant search "checkout latency" --collections dashboards
  gcx assistant search "checkout timeouts" --collections investigations,incidents --from now-1y
  gcx assistant search "checkout dependencies" --collections infrastructure -o json`,
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "large",
			agent.AnnotationLLMHint:   "Search indexed/historical evidence, not live telemetry. Require --collections with the smallest covering set (infrastructure,dashboards,investigations,incidents,alertRules). Investigations/incidents default to the last three calendar months; use --from/--to for another window. Use specific service names or error strings. Results are ranked and source-capped, not exhaustive; inspect collection errors and suppressedCollections. Use assistant prompt for reasoning.",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(opts.timeout)*time.Second)
			defer cancel()
			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := assistanthttp.NewClient(cfg)
			if err != nil {
				return err
			}
			result, err := assistant.Search(ctx, client, opts.query, opts.collections, opts.startTime, opts.endTime)
			if err != nil {
				return err
			}
			failed, searched := reconcileSearchCollections(result, opts.collections)
			if err := opts.IO.Encode(cmd.OutOrStdout(), result); err != nil {
				return err
			}
			if failed > 0 {
				code := gcxerrors.ExitPartialFailure
				if failed == searched {
					code = gcxerrors.ExitGeneralError
				}
				return gcxerrors.NewEmittedError(code, fmt.Errorf("assistant search failed for %d of %d collections", failed, searched))
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type searchCodec struct{}

func (*searchCodec) Format() format.Format { return "text" }
func (*searchCodec) Encode(dst io.Writer, value any) error {
	result, ok := value.(*assistant.SearchResult)
	if !ok {
		return fmt.Errorf("expected *assistant.SearchResult, got %T", value)
	}
	var b strings.Builder
	b.WriteString("Ranked search results (not exhaustive; counts are returned hits).\n")
	for _, collection := range result.Collections {
		fmt.Fprintf(&b, "\n%s (%d returned)\n", assistant.EscapeTerminalText(collection.Collection), len(collection.Results))
		if collection.Error != "" {
			fmt.Fprintf(&b, "  Search failed: %s\n", assistant.EscapeTerminalText(collection.Error))
		} else if len(collection.Results) == 0 {
			b.WriteString("  No matching results.\n")
		}
		for _, hit := range collection.Results {
			fmt.Fprintf(&b, "  %s [%s]\n", assistant.EscapeTerminalText(hit.Title), assistant.EscapeTerminalText(hit.SourceID))
			if hit.SourceURL != "" {
				fmt.Fprintf(&b, "    %s\n", assistant.EscapeTerminalText(hit.SourceURL))
			}
			if hit.Summary != "" {
				fmt.Fprintf(&b, "    %s\n", assistant.EscapeTerminalText(hit.Summary))
			}
		}
	}
	if len(result.SuppressedCollections) > 0 {
		suppressed := make([]string, len(result.SuppressedCollections))
		for i, collection := range result.SuppressedCollections {
			suppressed[i] = assistant.EscapeTerminalText(collection)
		}
		fmt.Fprintf(&b, "\nNot searched (disabled by workspace policy): %s\n", strings.Join(suppressed, ", "))
	}
	_, err := io.WriteString(dst, b.String())
	return err
}
func (*searchCodec) Decode(io.Reader, any) error {
	return errors.New("decode not supported for text format")
}

func searchCollectionNames() string {
	return strings.Join(searchCollections(), ", ")
}

func searchCollections() []string {
	return []string{"infrastructure", "dashboards", "investigations", "incidents", "alertRules"}
}

func reconcileSearchCollections(result *assistant.SearchResult, requested []string) (int, int) {
	failed := 0
	searched := 0
	errorsByCollection := make(map[string]string, len(result.Collections))
	for _, collection := range result.Collections {
		errorsByCollection[collection.Collection] = collection.Error
	}
	suppressed := make(map[string]bool, len(result.SuppressedCollections))
	for _, collection := range result.SuppressedCollections {
		suppressed[collection] = true
	}
	for _, collection := range requested {
		if suppressed[collection] {
			continue
		}
		searched++
		message, returned := errorsByCollection[collection]
		if returned {
			if message != "" {
				failed++
			}
			continue
		}
		failed++
		result.Collections = append(result.Collections, assistant.SearchCollection{
			Collection: collection,
			Results:    []assistant.SearchHit{},
			Error:      "backend omitted the requested collection",
		})
	}
	return failed, searched
}
