package datasources_test

// The unsupported-kind message names every kind the generic command routes,
// derived from the routing tables rather than hand-maintained (#1137). It is
// the only user-visible text this refactor changes, so it is pinned exactly,
// in both output modes.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/datasources"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const wantUnsupportedMessage = `datasource type "tempo" is not supported ` +
	`(supported: clickhouse, cloudwatch, influxdb, loki, postgres, prometheus, pyroscope)`

// runGenericSilenced mirrors the production root (cmd/gcx/root): usage and
// errors are reported by the caller, never printed into the output streams. The
// characterization suite deliberately uses a bare root instead, so this does
// not reuse its helper.
func runGenericSilenced(t *testing.T, f *fakeGrafana, args ...string) (string, error) {
	t.Helper()

	srv := f.start()
	configFile := newConfigFileForServer(t, srv.URL)

	root := helperRoot(datasources.QueryCmd())
	root.SilenceUsage = true
	root.SilenceErrors = true

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(append(args, "--config", configFile))

	return stdout.String(), root.Execute()
}

func TestGenericQueryUnsupportedKindMessage_HumanMode(t *testing.T) {
	f := &fakeGrafana{t: t, dsType: "tempo"}

	stdout, err := runGenericSilenced(t, f, "query", "uid", "whatever")
	require.Error(t, err)
	assert.Equal(t, wantUnsupportedMessage, err.Error())
	assert.Empty(t, stdout, "a failed query writes no partial document to stdout")
}

// Agent mode must not reword or truncate the message, and must not leave a
// half-written document behind. The in-band JSON error envelope is assembled by
// the production root reporter, which this package-level root does not mount;
// what is pinned here is the text that reporter receives.
func TestGenericQueryUnsupportedKindMessage_AgentMode(t *testing.T) {
	testutils.SetAgentMode(t, true)

	f := &fakeGrafana{t: t, dsType: "tempo"}

	stdout, err := runGenericSilenced(t, f, "query", "uid", "whatever")
	require.Error(t, err)
	assert.Equal(t, wantUnsupportedMessage, err.Error())
	assert.Empty(t, strings.TrimSpace(stdout))
}
