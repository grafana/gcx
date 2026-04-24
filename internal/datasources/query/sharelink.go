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
func (opts ExploreLinkOpts) Enabled() bool {
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
