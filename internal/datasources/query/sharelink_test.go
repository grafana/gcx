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
