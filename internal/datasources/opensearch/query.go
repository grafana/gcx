package opensearch

import (
	"fmt"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/opensearch"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	defaultLimit = 100
	maxLimit     = 1000

	// modeDocuments and modeLogs are the two --mode values query accepts.
	// documents is the raw_data document search; logs is the same search,
	// returned newest-first, with plugin-internal fields (_source, sort,
	// highlight) omitted. They share the Lucene expression, time controls,
	// and result shape, so they are one command with a mode rather than two
	// competing leaves.
	modeDocuments = "documents"
	modeLogs      = "logs"
)

type queryOpts struct {
	dsquery.SharedOpts

	Datasource string
	Mode       string
	Limit      int
}

func (opts *queryOpts) setup(flags *pflag.FlagSet) {
	opts.Setup(flags, false)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.opensearch is configured)")
	flags.StringVar(&opts.Mode, "mode", modeDocuments, fmt.Sprintf("Search mode: %q (raw documents) or %q (newest-first, plugin-internal fields omitted)", modeDocuments, modeLogs))
	flags.IntVar(&opts.Limit, "limit", defaultLimit, fmt.Sprintf("Max documents to return (%d-%d)", 1, maxLimit))
}

func (opts *queryOpts) Validate() error {
	if err := opts.SharedOpts.Validate(); err != nil {
		return err
	}
	switch opts.Mode {
	case modeDocuments, modeLogs:
	default:
		return fmt.Errorf("--mode must be %q or %q, got %q", modeDocuments, modeLogs, opts.Mode)
	}
	if opts.Limit < 1 || opts.Limit > maxLimit {
		return fmt.Errorf("--limit must be between 1 and %d, got %d", maxLimit, opts.Limit)
	}
	return nil
}

// QueryCmd returns the `query` subcommand for an OpenSearch datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &queryOpts{}
	share := &dsquery.ExploreLinkOpts{}

	cmd := &cobra.Command{
		Use:   "query [EXPR]",
		Short: "Search documents in an OpenSearch datasource",
		Long: `Search documents in an OpenSearch datasource with a Lucene query.

EXPR is a Lucene query string (e.g. 'app:frontend AND level:error'); omit it to
match all documents in the time range. The index pattern comes from the
datasource configuration.

--mode documents (default) returns raw source documents. --mode logs returns
the same documents newest-first with plugin-internal fields (_source, sort,
highlight, _type) omitted, matching how Grafana Explore's Logs view reads them.

Datasource is resolved from -d flag or datasources.opensearch in your context.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.`,
		Example: `
  # Match all documents in the last hour
  gcx datasources opensearch query --since 1h

  # Lucene query with explicit datasource
  gcx datasources opensearch query -d UID 'app:frontend AND level:error' --since 1h

  # Newest-first logs, plugin-internal fields omitted
  gcx datasources opensearch query -d UID 'level:error' --mode logs --since 6h --limit 50

  # Output as JSON, limit results
  gcx datasources opensearch query -d UID 'datacenter:us-east' --limit 20 -o json

  # Print a Grafana Explore share link for the executed query
  gcx datasources opensearch query 'level:error' --since 1h --share-link

  # Continue the same search in Grafana Explore
  gcx datasources opensearch query 'level:error' --since 1h --open`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			return runQuery(cmd, args, loader, opts, *share)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "large",
		agent.AnnotationLLMHint:   `gcx datasources opensearch query -d UID 'app:frontend AND level:error' --since 1h --limit 20`,
	}

	opts.setup(cmd.Flags())
	share.Setup(cmd.Flags(), "executed query")

	return cmd
}

// runQuery executes the document search for the resolved mode and handles
// output/Explore linking. logs mode differs only in which client call runs,
// which Explore query model it builds, and the Explore-unavailable wording.
func runQuery(cmd *cobra.Command, args []string, loader *providers.ConfigLoader, opts *queryOpts, share dsquery.ExploreLinkOpts) error {
	resolved, err := prepareQuery(cmd, args, loader, &opts.SharedOpts, opts.Datasource)
	if err != nil {
		return err
	}

	// req carries the user-facing limit — used for the Explore link, so the
	// URL never leaks the sentinel below. sentinelReq is what actually goes
	// on the wire: size+1, so a full page back means more documents matched.
	req := opensearch.SearchRequest{
		Query:  resolved.Expr,
		Size:   opts.Limit,
		Start:  resolved.Start,
		End:    resolved.End,
		StepMs: resolved.StepMs,
	}
	sentinelReq := req
	sentinelReq.Size = opts.Limit + 1

	search := resolved.Client.Search
	explore := QueryExploreURL
	exploreSubject := "query"
	if opts.Mode == modeLogs {
		search = resolved.Client.Logs
		explore = LogsExploreURL
		exploreSubject = "logs query"
	}

	resp, err := search(cmd.Context(), resolved.DatasourceUID, sentinelReq)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}
	if opensearch.TruncateRows(resp, opts.Limit) {
		cmdio.Warning(cmd.ErrOrStderr(), "showing the first %d rows; more rows match — raise --limit (max %d) to see more", opts.Limit, maxLimit)
	}

	exploreURL := explore(resolved.Cfg.GrafanaURL, resolved.ExploreBase(&opts.SharedOpts), req)
	unavailableMsg, failedOpenMsg := dsquery.ExploreMessages(exploreSubject)

	return dsquery.EncodeAndHandleExplore(cmd, func() error {
		return opts.IO.Encode(cmd.OutOrStdout(), resp)
	}, share, dsquery.ExploreLink{
		URL:            exploreURL,
		UnavailableMsg: unavailableMsg,
		FailedOpenMsg:  failedOpenMsg,
	})
}
