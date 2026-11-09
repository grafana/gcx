package loki //nolint:testpackage // white-box: exercises the unexported validateDroppedLogs

import (
	"testing"
	"time"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDroppedLogsCommand(t *testing.T) {
	start := time.Date(2026, 1, 2, 15, 0, 0, 0, time.FixedZone("plus2", 2*60*60))
	end := start.Add(15*time.Minute + 500*time.Millisecond)
	window := []string{"--from", "2026-01-02T13:00:00Z", "--to", "2026-01-02T13:15:00.5Z"}

	tests := []struct {
		name  string
		argv  []string
		limit int
		want  []string
	}{
		{
			name:  "relative window and link flags are replaced",
			argv:  []string{"gcx", "--context", "dev", "logs", "query", "-d", "uid", `{job="app"}`, "--since", "1h", "--limit", "5", "--share-link", "--open-drilldown", "-o", "json"},
			limit: 5,
			want:  append([]string{"gcx", "--context", "dev", "logs", "query", "-d", "uid", `{job="app"}`, "--limit", "5", "-o", "json"}, append(window, "--dropped-logs")...),
		},
		{
			name:  "absolute window and step are replaced",
			argv:  []string{"gcx", "datasources", "loki", "query", `{job="app"}`, "--from=now-1h", "--to", "now", "--step", "1m"},
			limit: 50,
			want:  append([]string{"gcx", "datasources", "loki", "query", `{job="app"}`}, append(window, "--dropped-logs")...),
		},
		{
			name:  "limit above the time machine cap is clamped",
			argv:  []string{"gcx", "logs", "query", `{job="app"}`, "--limit=20000"},
			limit: 20000,
			want:  append(append([]string{"gcx", "logs", "query", `{job="app"}`}, window...), "--limit", "10000", "--dropped-logs"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, cmdio.ShellCommand(tt.want), DroppedLogsCommand(tt.argv, start, end, tt.limit))
		})
	}

	assert.Empty(t, DroppedLogsCommand(nil, start, end, 5))
}

func TestGenericDroppedLogsCommand(t *testing.T) {
	start := time.Date(2026, 1, 2, 13, 0, 0, 0, time.UTC)
	end := start.Add(time.Minute)
	window := []string{"--from", "2026-01-02T13:00:00Z", "--to", "2026-01-02T13:01:00Z"}

	tests := []struct {
		name                            string
		binary, configFile, contextName string
		limit                           int
		limitSet                        bool
		want                            []string
	}{
		{
			name:   "defaults",
			binary: "gcx",
			limit:  50,
			want:   append(append([]string{"gcx", "logs", "query", "-d", "uid", `{job="app"}`}, window...), "--dropped-logs"),
		},
		{
			name:        "config, context and clamped limit",
			binary:      "/usr/local/bin/gcx",
			configFile:  "/tmp/gcx.yaml",
			contextName: "dev",
			limit:       50000,
			limitSet:    true,
			want: append(append([]string{"/usr/local/bin/gcx", "--config", "/tmp/gcx.yaml", "--context", "dev", "logs", "query", "-d", "uid", `{job="app"}`}, window...),
				"--limit", "10000", "--dropped-logs"),
		},
		{
			name:     "empty binary falls back to gcx",
			limit:    5,
			limitSet: true,
			want:     append(append([]string{"gcx", "logs", "query", "-d", "uid", `{job="app"}`}, window...), "--limit", "5", "--dropped-logs"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenericDroppedLogsCommand(tt.binary, tt.configFile, tt.contextName, "uid", `{job="app"}`, start, end, tt.limit, tt.limitSet)
			assert.Equal(t, cmdio.ShellCommand(tt.want), got)
		})
	}
}

func TestValidateDroppedLogs(t *testing.T) {
	tests := []struct {
		name      string
		step      string
		limit     int
		share     dsquery.ExploreLinkOpts
		drilldown dsquery.DrilldownLinkOpts
		wantErr   string
	}{
		{name: "plain", limit: 50},
		{name: "limit at cap", limit: 10000},
		{name: "step", step: "1m", limit: 50, wantErr: "--step"},
		{name: "share link", limit: 50, share: dsquery.ExploreLinkOpts{ShareLink: true}, wantErr: "--share-link"},
		{name: "open", limit: 50, share: dsquery.ExploreLinkOpts{Open: true}, wantErr: "--open"},
		{name: "drilldown link", limit: 50, drilldown: dsquery.DrilldownLinkOpts{ShareLink: true}, wantErr: "--drilldown-link"},
		{name: "open drilldown", limit: 50, drilldown: dsquery.DrilldownLinkOpts{Open: true}, wantErr: "--open-drilldown"},
		{name: "limit over cap", limit: 10001, wantErr: "at most 10000 lines"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDroppedLogs(tt.step, tt.limit, tt.share, tt.drilldown)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
