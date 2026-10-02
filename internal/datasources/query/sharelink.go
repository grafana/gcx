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

// DrilldownLinkOpts controls optional Grafana Drilldown app link output for
// query-like commands (Logs Drilldown, Traces Drilldown, Profiles Drilldown,
// Metrics Drilldown, ...). It mirrors ExploreLinkOpts exactly, as a separate,
// independently-settable pair of flags so a command can offer both an
// Explore link and a Drilldown link at once.
type DrilldownLinkOpts struct {
	ShareLink bool
	Open      bool
	// AppName is the human-readable Drilldown app name (e.g. "Logs Drilldown",
	// "Traces Drilldown") used in flag help and printed/warning messages. Set
	// by Setup.
	AppName string
}

// Setup registers the standard drilldown-link/open-drilldown flags for
// query-like commands. appName is the human-readable Drilldown app name (e.g.
// "Logs Drilldown", "Traces Drilldown") shown in flag help and messages.
func (opts *DrilldownLinkOpts) Setup(flags *pflag.FlagSet, subject, appName string) {
	opts.AppName = appName
	flags.BoolVar(&opts.ShareLink, "drilldown-link", false, "Print the Grafana "+appName+" URL for the "+subject+" to stderr")
	flags.BoolVar(&opts.Open, "open-drilldown", false, "Open the "+subject+" in Grafana "+appName)
}

// Enabled reports whether either share/open behavior was requested.
func (opts *DrilldownLinkOpts) Enabled() bool {
	return opts.ShareLink || opts.Open
}

// DrilldownMessages returns the standard unavailable/open-failed messages for
// a successfully completed command subject. appName is the human-readable
// Drilldown app name (e.g. "Logs Drilldown", "Traces Drilldown").
func DrilldownMessages(subject, appName string) (string, string) {
	return subject + " succeeded, but no " + appName + " URL could be built for this expression; showing the Explore link instead",
		subject + " succeeded, but could not open browser"
}

// handleDrilldownLink prints and/or opens a Grafana Drilldown app URL. Missing
// URLs are warned about but do not fail the command after successful data
// retrieval — this is expected whenever the query expression doesn't
// decompose into the Drilldown app's simple filter model (aggregations,
// parser stages, ...). It has no failure mode of its own (a failed --open
// only warns), so unlike HandleExploreLink it returns nothing.
func handleDrilldownLink(cmd *cobra.Command, opts DrilldownLinkOpts, url string, unavailableMsg, failedOpenMsg string) {
	if !opts.Enabled() {
		return
	}
	if url == "" {
		cmdio.Warning(cmd.ErrOrStderr(), "%s", unavailableMsg)
		return
	}
	if opts.ShareLink {
		cmdio.Info(cmd.ErrOrStderr(), "%s link: %s", opts.AppName, url)
	}
	if opts.Open {
		if err := deeplink.Open(url); err != nil {
			cmdio.Warning(cmd.ErrOrStderr(), "%s: %v", failedOpenMsg, err)
		}
	}
}

// HandleDrilldownLinkWithExploreFallback prints/opens a Grafana Drilldown app
// URL, and — when drilldownURL couldn't be built — makes DrilldownMessages'
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
	handleDrilldownLink(cmd, drilldownOpts, drilldownURL, drilldownUnavailableMsg, drilldownFailedOpenMsg)
	if drilldownURL != "" || !drilldownOpts.Enabled() || exploreEnabled {
		return nil
	}
	fallback := ExploreLinkOpts{ShareLink: drilldownOpts.ShareLink, Open: drilldownOpts.Open}
	return HandleExploreLink(cmd, fallback, exploreURL, exploreUnavailableMsg, exploreFailedOpenMsg)
}
