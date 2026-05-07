package version_test

import (
	"bytes"
	"encoding/json"
	"testing"

	versioncmd "github.com/grafana/gcx/cmd/gcx/version"
	appversion "github.com/grafana/gcx/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionCommand_JSON(t *testing.T) {
	appversion.Set("1.2.3")
	appversion.SetBuildInfo("abc1234", "2026-05-07T12:00:00Z")
	t.Cleanup(func() {
		appversion.Set("")
		appversion.SetBuildInfo("", "")
	})

	cmd := versioncmd.Command()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	var info appversion.InfoData
	require.NoError(t, json.Unmarshal(buf.Bytes(), &info))

	assert.Equal(t, "1.2.3", info.Version)
	assert.Equal(t, "abc1234", info.Commit)
	assert.Equal(t, "2026-05-07T12:00:00Z", info.BuildDate)
	assert.NotEmpty(t, info.Go)
	assert.NotEmpty(t, info.OS)
	assert.NotEmpty(t, info.Arch)
}

func TestVersionCommand_Text(t *testing.T) {
	appversion.Set("2.0.0")
	appversion.SetBuildInfo("def5678", "2026-01-01T00:00:00Z")
	t.Cleanup(func() {
		appversion.Set("")
		appversion.SetBuildInfo("", "")
	})

	cmd := versioncmd.Command()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"-o", "text"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "2.0.0")
	assert.Contains(t, out, "def5678")
	assert.Contains(t, out, "2026-01-01T00:00:00Z")
}

func TestVersionCommand_Defaults(t *testing.T) {
	appversion.Set("")
	appversion.SetBuildInfo("", "")
	t.Cleanup(func() {
		appversion.Set("")
		appversion.SetBuildInfo("", "")
	})

	cmd := versioncmd.Command()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	var info appversion.InfoData
	require.NoError(t, json.Unmarshal(buf.Bytes(), &info))

	assert.Equal(t, "SNAPSHOT", info.Version)
	assert.Equal(t, "unknown", info.Commit)
	assert.Equal(t, "unknown", info.BuildDate)
}
