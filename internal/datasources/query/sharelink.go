package query

import (
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/deeplink"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ExploreLinkOpts controls optional Grafana Explore link output for query-like commands.
type ExploreLinkOpts struct {
	ShareLink bool
	Open      bool
}

// Setup registers the standard share/open flags for query-like commands.
func (opts *ExploreLinkOpts) Setup(flags *pflag.FlagSet, subject string) {
	flags.BoolVar(&opts.ShareLink, "share-link", false, "Print the Grafana Explore URL for the "+subject+" to stderr")
	flags.BoolVar(&opts.Open, "open", false, "Open the "+subject+" in Grafana Explore")
}

// Enabled reports whether either share/open behavior was requested.
func (opts *ExploreLinkOpts) Enabled() bool {
	return opts.ShareLink || opts.Open
}

// ExploreLink describes optional Grafana Explore link handling after a command succeeds.
type ExploreLink struct {
	URL            string
	UnavailableMsg string
	FailedOpenMsg  string
}

// ExploreMessages returns the standard unavailable/open-failed messages for a
// successfully completed command subject.
func ExploreMessages(subject string) (string, string) {
	return subject + " succeeded, but no Grafana Explore URL could be built",
		subject + " succeeded, but could not open browser"
}

// OrgID returns the Grafana org ID for Explore link generation.
func OrgID(cfgCtx *config.Context) int64 {
	if cfgCtx != nil && cfgCtx.Grafana != nil {
		return cfgCtx.Grafana.OrgID
	}
	return 0
}

// EncodeAndHandleExplore writes command output and then handles the optional
// Explore link side effects.
func EncodeAndHandleExplore(cmd *cobra.Command, encode func() error, opts ExploreLinkOpts, link ExploreLink) error {
	if err := encode(); err != nil {
		return err
	}
	return HandleExploreLink(cmd, opts, link.URL, link.UnavailableMsg, link.FailedOpenMsg)
}

// HandleExploreLink prints and/or opens a Grafana Explore URL.
// Missing URLs are warned about but do not fail the command after successful data retrieval.
func HandleExploreLink(cmd *cobra.Command, opts ExploreLinkOpts, url string, unavailableMsg, failedOpenMsg string) error {
	if !opts.Enabled() {
		return nil
	}
	if url == "" {
		cmdio.Warning(cmd.ErrOrStderr(), "%s", unavailableMsg)
		return nil
	}
	if opts.ShareLink {
		cmdio.Info(cmd.ErrOrStderr(), "Explore link: %s", url)
	}
	if opts.Open {
		if err := deeplink.Open(url); err != nil {
			cmdio.Warning(cmd.ErrOrStderr(), "%s: %v", failedOpenMsg, err)
		}
	}
	return nil
}

// DrilldownLinkOpts controls optional Logs Drilldown link output for
// query-like commands. It mirrors ExploreLinkOpts exactly, as a separate,
// independently-settable pair of flags so a command can offer both an
// Explore link and a Drilldown link at once.
type DrilldownLinkOpts struct {
	ShareLink bool
	Open      bool
}

// Setup registers the standard drilldown-link/open-drilldown flags for
// query-like commands.
func (opts *DrilldownLinkOpts) Setup(flags *pflag.FlagSet, subject string) {
	flags.BoolVar(&opts.ShareLink, "drilldown-link", false, "Print the Grafana Logs Drilldown URL for the "+subject+" to stderr")
	flags.BoolVar(&opts.Open, "open-drilldown", false, "Open the "+subject+" in Grafana Logs Drilldown")
}

// Enabled reports whether either share/open behavior was requested.
func (opts *DrilldownLinkOpts) Enabled() bool {
	return opts.ShareLink || opts.Open
}

// DrilldownMessages returns the standard unavailable/open-failed messages for
// a successfully completed command subject.
func DrilldownMessages(subject string) (string, string) {
	return subject + " succeeded, but no Logs Drilldown URL could be built for this expression; showing the Explore link instead",
		subject + " succeeded, but could not open browser"
}

// HandleDrilldownLink prints and/or opens a Logs Drilldown URL. Missing URLs
// are warned about but do not fail the command after successful data
// retrieval — this is expected whenever the query expression doesn't
// decompose into Drilldown's simple filter model (aggregations, parser
// stages, ...).
func HandleDrilldownLink(cmd *cobra.Command, opts DrilldownLinkOpts, url string, unavailableMsg, failedOpenMsg string) error {
	if !opts.Enabled() {
		return nil
	}
	if url == "" {
		cmdio.Warning(cmd.ErrOrStderr(), "%s", unavailableMsg)
		return nil
	}
	if opts.ShareLink {
		cmdio.Info(cmd.ErrOrStderr(), "Logs Drilldown link: %s", url)
	}
	if opts.Open {
		if err := deeplink.Open(url); err != nil {
			cmdio.Warning(cmd.ErrOrStderr(), "%s: %v", failedOpenMsg, err)
		}
	}
	return nil
}

// HandleDrilldownLinkWithExploreFallback prints/opens a Logs Drilldown URL,
// and — when drilldownURL couldn't be built — makes DrilldownMessages'
// "showing the Explore link instead" promise literal by actually
// printing/opening the Explore URL, using the same share/open intent the
// caller gave to the Drilldown flags. exploreEnabled should be the caller's
// own ExploreLinkOpts.Enabled(): when the Explore link was already
// requested (and thus already shown) via its own flags, the fallback is
// skipped to avoid printing it twice.
func HandleDrilldownLinkWithExploreFallback(
	cmd *cobra.Command,
	drilldownOpts DrilldownLinkOpts, drilldownURL, drilldownUnavailableMsg, drilldownFailedOpenMsg string,
	exploreEnabled bool,
	exploreURL, exploreUnavailableMsg, exploreFailedOpenMsg string,
) error {
	if err := HandleDrilldownLink(cmd, drilldownOpts, drilldownURL, drilldownUnavailableMsg, drilldownFailedOpenMsg); err != nil {
		return err
	}
	if drilldownURL != "" || !drilldownOpts.Enabled() || exploreEnabled {
		return nil
	}
	fallback := ExploreLinkOpts(drilldownOpts)
	return HandleExploreLink(cmd, fallback, exploreURL, exploreUnavailableMsg, exploreFailedOpenMsg)
}
