package pyroscope

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type profileDiffOpts struct {
	IO          cmdio.Options
	Datasource  string
	ProfileType string
	BeforeFrom  string
	BeforeTo    string
	AfterFrom   string
	AfterTo     string
	MaxNodes    int64
}

func (opts *profileDiffOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.pyroscope is configured)")
	flags.StringVar(&opts.ProfileType, "profile-type", "", "Profile type ID shared by both sides (required; see 'gcx profiles list-profile-types')")
	flags.StringVar(&opts.BeforeFrom, "before-from", "", "Before window start (required; RFC3339, Unix seconds, or relative like 'now-1h')")
	flags.StringVar(&opts.BeforeTo, "before-to", "", "Before window end (required; RFC3339, Unix seconds, or relative like 'now-30m')")
	flags.StringVar(&opts.AfterFrom, "after-from", "", "After window start (required; RFC3339, Unix seconds, or relative like 'now-30m')")
	flags.StringVar(&opts.AfterTo, "after-to", "", "After window end (required; RFC3339, Unix seconds, or 'now')")
	flags.Int64Var(&opts.MaxNodes, "max-nodes", defaultMaxNodes, "Backend graph node budget shared by both sides (positive); pruned costs are disclosed in omitted and complete")
}

func (opts *profileDiffOpts) Validate(flags *pflag.FlagSet) error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(opts.ProfileType) == "" {
		return errors.New("--profile-type is required; use 'gcx profiles list-profile-types' to discover IDs")
	}
	if flags.Changed("datasource") && strings.TrimSpace(opts.Datasource) == "" {
		return errors.New("--datasource must not be empty when explicitly provided")
	}
	if opts.MaxNodes <= 0 {
		return errors.New("--max-nodes must be greater than 0")
	}
	return nil
}

func (opts *profileDiffOpts) request(args []string, now time.Time) (pyroscope.DiffRequest, error) {
	req := pyroscope.DiffRequest{ProfileType: opts.ProfileType, MaxNodes: opts.MaxNodes}
	for i, side := range []struct {
		name     string
		from, to string
		target   *pyroscope.ProfileSelection
	}{
		{"before", opts.BeforeFrom, opts.BeforeTo, &req.Before},
		{"after", opts.AfterFrom, opts.AfterTo, &req.After},
	} {
		if strings.TrimSpace(args[i]) == "" {
			return req, fmt.Errorf("%s selector must not be empty; use '{}' to explicitly select all profiles", side.name)
		}
		side.target.Selector = args[i]
		for _, bound := range []struct {
			name, value string
			target      *time.Time
		}{
			{"from", side.from, &side.target.From},
			{"to", side.to, &side.target.To},
		} {
			flag := "--" + side.name + "-" + bound.name
			if strings.TrimSpace(bound.value) == "" {
				return req, fmt.Errorf("%s is required; specify RFC3339, Unix seconds, or relative time", flag)
			}
			parsed, err := dsquery.ParseTime(bound.value, now)
			if err != nil || parsed.IsZero() || parsed.UnixMilli() <= 0 {
				return req, fmt.Errorf("invalid %s %q: expected a positive RFC3339, Unix seconds, or relative time", flag, bound.value)
			}
			// Drilldown's render-diff client uses unix()*1000. Resolve the RPC
			// to whole seconds too, so a link never executes a different range.
			*bound.target = time.Unix(parsed.Unix(), 0).UTC()
		}
		if !side.target.From.Before(side.target.To) {
			return req, fmt.Errorf("--%s-from must be earlier than --%s-to", side.name, side.name)
		}
	}
	return req, nil
}

// DiffCmd compares two selectors using the backend's aligned diff graph, rather
// than subtracting two independently pruned top-function lists.
func DiffCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &profileDiffOpts{}
	share := &dsquery.ExploreLinkOpts{}
	drilldown := &dsquery.DrilldownLinkOpts{}
	cmd := &cobra.Command{
		Use:   "diff BEFORE AFTER",
		Short: "Compare profile symbol costs across two selectors and time windows",
		Long: `Compare two label selectors over explicit before/after windows.

Both sides use the same datasource and profile type. Results include symbol
costs, raw after-minus-before deltas, and profile/comparison links.`,
		Example: `
  # Compare the same selector across adjacent windows, preserving all returned symbols
  gcx profiles diff '{service_name="frontend"}' '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds \
    --before-from now-1h --before-to now-30m --after-from now-30m --after-to now -o json`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd.Flags()); err != nil {
				return gcxerrors.NewCommandUsageError(cmd, "", err)
			}
			req, err := opts.request(args, time.Now())
			if err != nil {
				return gcxerrors.NewCommandUsageError(cmd, "", err)
			}
			ctx := cmd.Context()
			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}
			uid, dsType, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "pyroscope")
			if err != nil {
				return err
			}
			client, err := pyroscope.NewClient(cfg)
			if err != nil {
				return err
			}
			resp, err := client.Diff(ctx, uid, req)
			if err != nil {
				return fmt.Errorf("profile diff failed: %w", err)
			}
			result, err := pyroscope.BuildDiffResult(resp, req)
			if err != nil {
				return err
			}
			orgID := dsquery.OrgID(cfgCtx)
			result.Links = profileDiffLinks(cfg.GrafanaURL, uid, dsType, orgID, req)
			if result.Links["comparison"] == "" {
				result.Warnings = append(result.Warnings, "Profiles Drilldown cannot represent both selectors; links.explore preserves them in separate panes")
			}
			unavailable, failedOpen := dsquery.ExploreMessages("profile diff")
			if err := dsquery.EncodeAndHandleExplore(cmd, func() error { return opts.IO.Encode(cmd.OutOrStdout(), result) }, *share,
				dsquery.ExploreLink{URL: result.Links["explore"], UnavailableMsg: unavailable, FailedOpenMsg: failedOpen}); err != nil {
				return err
			}
			for _, warning := range result.Warnings {
				cmdio.Warning(cmd.ErrOrStderr(), "%s", warning)
			}
			drilldownUnavailable, drilldownFailedOpen := dsquery.DrilldownMessages("profile diff", "Profiles Drilldown")
			return dsquery.HandleDrilldownLinkWithExploreFallback(cmd, *drilldown, result.Links["comparison"], drilldownUnavailable, drilldownFailedOpen,
				*share, result.Links["explore"], unavailable, failedOpen)
		},
	}
	opts.setup(cmd.Flags())
	share.Setup(cmd.Flags(), "profile comparison (two Explore panes)")
	drilldown.Setup(cmd.Flags(), "profile comparison", "Profiles Drilldown")
	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "large",
		agent.AnnotationLLMHint:   `gcx profiles diff '{service_name="frontend"}' '{service_name="frontend"}' --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --before-from now-1h --before-to now-30m --after-from now-30m --after-to now -o agents`,
	}
	return cmd
}
