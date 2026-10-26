package resources

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/search"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/util/validation"
)

const experimentalSearchPreamble = "This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions."

type searchConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

type searchOpts struct {
	IO        cmdio.Options
	Limit     int
	Resources []string
	Folders   []string
}

func (o *searchOpts) setup(flags *pflag.FlagSet) {
	table := searchTable()
	o.IO.RegisterCustomCodec("table", table.Codec("table"))
	o.IO.RegisterCustomCodec("wide", table.Codec("wide"))
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.Limit, "limit", 50, "Maximum number of top matches (1-200)")
	flags.StringArrayVar(&o.Resources, "resource", nil, "Search a resource.group type (repeatable; overrides server defaults)")
	flags.StringArrayVar(&o.Folders, "folder", nil, "Filter by exact folder UID (repeatable; empty or general selects root)")
}

func (o *searchOpts) Validate(cmd *cobra.Command, args []string) (search.Query, error) {
	bad := func(message string) (search.Query, error) {
		return search.Query{}, gcxerrors.NewCommandUsageError(cmd, message, nil)
	}
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return bad("provide exactly one nonblank search query")
	}
	if len([]byte(args[0])) > 1000 {
		return bad("search query must be at most 1000 UTF-8 bytes")
	}
	if !utf8.ValidString(args[0]) {
		return bad("search query must be valid UTF-8")
	}
	if o.Limit == 0 {
		return bad("--limit must be 1-200; search has no all-results mode")
	}
	if o.Limit < 0 || o.Limit > 200 {
		return bad("--limit must be between 1 and 200")
	}
	if err := o.IO.Validate(); err != nil {
		return search.Query{}, gcxerrors.NewCommandUsageError(cmd, err.Error(), err)
	}
	query := search.Query{Query: args[0], Limit: o.Limit}
	seen := map[string]bool{}
	for _, value := range o.Resources {
		parts := strings.SplitN(value, ".", 2)
		if len(parts) != 2 || len(validation.IsDNS1035Label(parts[0])) != 0 || len(validation.IsDNS1123Subdomain(parts[1])) != 0 {
			return bad(fmt.Sprintf("invalid --resource %q: expected resource.group (for example dashboards.dashboard.grafana.app)", value))
		}
		if seen[value] {
			return bad(fmt.Sprintf("duplicate --resource %q", value))
		}
		seen[value] = true
		query.Resources = append(query.Resources, search.Resource{Resource: parts[0], Group: parts[1]})
	}
	if len(o.Folders) > 1000 {
		return bad("--folder accepts at most 1000 values")
	}
	if len(o.Folders) > 0 {
		values := make([]string, 0, len(o.Folders))
		for _, folder := range o.Folders {
			if strings.Contains(folder, "*") {
				return bad("--folder does not accept wildcards")
			}
			if folder == "general" {
				folder = ""
			}
			values = append(values, folder)
		}
		query.Filters = []search.Filter{{Field: "folder", Values: values}}
	}
	return query, nil
}

func searchCmd(loader searchConfigLoader) *cobra.Command {
	opts := &searchOpts{}
	cmd := &cobra.Command{
		Use:     "search QUERY",
		Short:   "[experimental] Search Grafana resources by relevance",
		Long:    experimentalSearchPreamble + "\n\nFind bounded top matches across configured resource types through Grafana global hybrid search. Results are not an exhaustive inventory. Use --resource to override server defaults or supply types when none are configured; --folder accepts exact UIDs.",
		Example: "  gcx resources search 'kubernetes memory problems'\n  gcx resources search memory --resource dashboards.dashboard.grafana.app --folder team-platform --limit 10\n  gcx resources search memory -o wide",
		Args:    cobra.ArbitraryArgs,
		Annotations: map[string]string{
			agent.AnnotationStability: agent.StabilityExperimental,
			agent.AnnotationTokenCost: "large",
			agent.AnnotationLLMHint:   "Use --resource and --folder filters, a smaller --limit, and --json field selection to reduce large search results.",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			query, err := opts.Validate(cmd, args)
			if err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := search.NewClient(cfg)
			if err != nil {
				return err
			}
			result, err := client.Search(cmd.Context(), query)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
