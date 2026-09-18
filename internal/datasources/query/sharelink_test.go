package query_test

import (
	"bytes"
	"testing"

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
			dsquery.DrilldownLinkOpts{ShareLink: true}, "https://example.grafana.net/a/grafana-lokiexplore-app/explore/app/foo/logs", "unavailable", "failed",
			false, "https://example.grafana.net/explore?x=1", "explore unavailable", "explore failed",
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
			false, "https://example.grafana.net/explore?x=1", "explore unavailable", "explore failed",
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
			true, "https://example.grafana.net/explore?x=1", "explore unavailable", "explore failed",
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
			false, "https://example.grafana.net/explore?x=1", "explore unavailable", "explore failed",
		)
		require.NoError(t, err)
		assert.Empty(t, stderr.String())
	})
}
