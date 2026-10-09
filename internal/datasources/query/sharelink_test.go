package query_test

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrgID(t *testing.T) {
	assert.Zero(t, dsquery.OrgID(nil))
	assert.Zero(t, dsquery.OrgID(&config.Context{}))
	assert.Equal(t, int64(42), dsquery.OrgID(&config.Context{Grafana: &config.GrafanaConfig{OrgID: 42}}))
}

func TestExploreMessages(t *testing.T) {
	unavailable, failedOpen := dsquery.ExploreMessages("query")
	assert.Equal(t, "query succeeded, but no Grafana Explore URL could be built", unavailable)
	assert.Equal(t, "query succeeded, but could not open browser", failedOpen)
}

func TestEncodeAndHandleExplore(t *testing.T) {
	t.Run("encodes output then prints share link", func(t *testing.T) {
		cmd := &cobra.Command{Use: "test"}
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)

		called := false
		err := dsquery.EncodeAndHandleExplore(cmd, func() error {
			called = true
			_, writeErr := stdout.WriteString("ok\n")
			return writeErr
		}, dsquery.ExploreLinkOpts{ShareLink: true}, dsquery.ExploreLink{
			URL:            "https://example.grafana.net/explore?x=1",
			UnavailableMsg: "unavailable",
			FailedOpenMsg:  "failed",
		})
		require.NoError(t, err)
		assert.True(t, called)
		assert.Equal(t, "ok\n", stdout.String())
		assert.Contains(t, stderr.String(), "Explore link: https://example.grafana.net/explore?x=1")
	})

	t.Run("warns when no explore url is available", func(t *testing.T) {
		cmd := &cobra.Command{Use: "test"}
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)

		err := dsquery.EncodeAndHandleExplore(cmd, func() error { return nil }, dsquery.ExploreLinkOpts{ShareLink: true}, dsquery.ExploreLink{
			UnavailableMsg: "no url",
			FailedOpenMsg:  "failed",
		})
		require.NoError(t, err)
		assert.Contains(t, stderr.String(), "no url")
	})
}

func TestHandleDrilldownLinkWithExploreFallback(t *testing.T) {
	t.Run("prints drilldown link when available, no fallback needed", func(t *testing.T) {
		cmd := &cobra.Command{Use: "test"}
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)

		err := dsquery.HandleDrilldownLinkWithExploreFallback(cmd,
			dsquery.DrilldownLinkOpts{ShareLink: true, AppName: "Logs Drilldown"}, "https://example.grafana.net/a/grafana-lokiexplore-app/explore/app/foo/logs", "unavailable", "failed",
			dsquery.ExploreLinkOpts{}, "https://example.grafana.net/explore?x=1", "explore unavailable", "explore failed",
		)
		require.NoError(t, err)
		assert.Contains(t, stderr.String(), "Logs Drilldown link: https://example.grafana.net/a/grafana-lokiexplore-app/explore/app/foo/logs")
		assert.NotContains(t, stderr.String(), "Explore link:")
	})

	t.Run("falls back to actually printing the explore link when drilldown link is unavailable", func(t *testing.T) {
		cmd := &cobra.Command{Use: "test"}
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)

		err := dsquery.HandleDrilldownLinkWithExploreFallback(cmd,
			dsquery.DrilldownLinkOpts{ShareLink: true}, "", "no drilldown url", "failed",
			dsquery.ExploreLinkOpts{}, "https://example.grafana.net/explore?x=1", "explore unavailable", "explore failed",
		)
		require.NoError(t, err)
		assert.Contains(t, stderr.String(), "no drilldown url")
		assert.Contains(t, stderr.String(), "Explore link: https://example.grafana.net/explore?x=1")
	})

	t.Run("does not duplicate the explore link when the caller already requested it", func(t *testing.T) {
		cmd := &cobra.Command{Use: "test"}
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)

		err := dsquery.HandleDrilldownLinkWithExploreFallback(cmd,
			dsquery.DrilldownLinkOpts{ShareLink: true}, "", "no drilldown url", "failed",
			dsquery.ExploreLinkOpts{ShareLink: true}, "https://example.grafana.net/explore?x=1", "explore unavailable", "explore failed",
		)
		require.NoError(t, err)
		assert.Contains(t, stderr.String(), "no drilldown url")
		assert.NotContains(t, stderr.String(), "Explore link:")
	})

	t.Run("no-op when drilldown link was not requested at all", func(t *testing.T) {
		cmd := &cobra.Command{Use: "test"}
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)

		err := dsquery.HandleDrilldownLinkWithExploreFallback(cmd,
			dsquery.DrilldownLinkOpts{}, "", "no drilldown url", "failed",
			dsquery.ExploreLinkOpts{}, "https://example.grafana.net/explore?x=1", "explore unavailable", "explore failed",
		)
		require.NoError(t, err)
		assert.Empty(t, stderr.String())
	})
}

// withAgentModeCapturingBrowserHints turns on agent mode so deeplink.Open never
// launches a browser, and returns a func yielding what Open wrote to os.Stderr.
func withAgentModeCapturingBrowserHints(t *testing.T) func() string {
	t.Helper()

	prev := agent.IsAgentMode()
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(prev) })

	r, w, err := os.Pipe()
	require.NoError(t, err)
	origStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	return func() string {
		os.Stderr = origStderr
		require.NoError(t, w.Close())
		out, err := io.ReadAll(r)
		require.NoError(t, err)
		return string(out)
	}
}

func TestHandleDrilldownLinkWithExploreFallback_MixedFlagsFulfillBothRequestedActions(t *testing.T) {
	const exploreURL = "https://example.grafana.net/explore?x=1"

	t.Run("--open with --drilldown-link opens Explore and prints the fallback link", func(t *testing.T) {
		browserHints := withAgentModeCapturingBrowserHints(t)
		cmd := &cobra.Command{Use: "test"}
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)

		// Mirrors the caller: the Explore flags are handled first.
		explore := dsquery.ExploreLinkOpts{Open: true}
		require.NoError(t, dsquery.HandleExploreLink(cmd, explore, exploreURL, "unavailable", "failed"))
		require.NoError(t, dsquery.HandleDrilldownLinkWithExploreFallback(cmd,
			dsquery.DrilldownLinkOpts{ShareLink: true}, "", "no drilldown url", "failed",
			explore, exploreURL, "unavailable", "failed",
		))

		assert.Contains(t, stderr.String(), "Explore link: "+exploreURL, "the requested fallback print must happen")
		assert.Equal(t, 1, strings.Count(browserHints(), exploreURL), "Explore must be opened exactly once")
	})

	t.Run("--share-link with --open-drilldown prints once and still opens the fallback", func(t *testing.T) {
		browserHints := withAgentModeCapturingBrowserHints(t)
		cmd := &cobra.Command{Use: "test"}
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)

		explore := dsquery.ExploreLinkOpts{ShareLink: true}
		require.NoError(t, dsquery.HandleExploreLink(cmd, explore, exploreURL, "unavailable", "failed"))
		require.NoError(t, dsquery.HandleDrilldownLinkWithExploreFallback(cmd,
			dsquery.DrilldownLinkOpts{Open: true}, "", "no drilldown url", "failed",
			explore, exploreURL, "unavailable", "failed",
		))

		assert.Equal(t, 1, bytes.Count(stderr.Bytes(), []byte("Explore link: ")), "must not print twice")
		assert.Contains(t, browserHints(), exploreURL, "the requested fallback open must happen")
	})
}
