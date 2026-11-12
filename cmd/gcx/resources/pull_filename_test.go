package resources //nolint:testpackage // Exercise pull command wiring using the shared discovery/API fixture.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestPull_FilenameBy(t *testing.T) {
	for _, tt := range []struct {
		name       string
		filenameBy string
		format     string
		want       string
		agentMode  bool
	}{
		{name: "default", format: "json", want: "read-only.json"},
		{name: "explicit name", filenameBy: "name", format: "json", want: "read-only.json"},
		{name: "title json", filenameBy: "title", format: "json", want: "sales-overview.json"},
		{name: "title yaml agent", filenameBy: "title", format: "yaml", want: "sales-overview.yaml", agentMode: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newReadOnlyCommandFixture(t, "TemplateGroup")
			require.NoError(t, unstructured.SetNestedField(f.readOnly.Object, "Sales / Overview", "spec", "title"))
			if tt.agentMode {
				t.Setenv("GCX_AGENT_MODE", "true")
			}
			agent.ResetForTesting()
			destination := filepath.Join(t.TempDir(), "resources")
			cmd := pullCmd(f.config)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			args := []string{f.plural, "--path", destination, "-o", tt.format}
			if tt.filenameBy != "" {
				args = append(args, "--filename-by", tt.filenameBy)
			}
			cmd.SetArgs(args)
			require.NoError(t, cmd.ExecuteContext(t.Context()))
			entries, err := os.ReadDir(filepath.Join(destination, f.groupDir))
			require.NoError(t, err)
			require.Len(t, entries, 2)
			require.FileExists(t, filepath.Join(destination, f.groupDir, tt.want))
			require.FileExists(t, filepath.Join(destination, f.groupDir, "writable."+tt.format))
			require.EqualValues(t, 1, f.reads.Load())
			require.Zero(t, f.writes.Load())
		})
	}
}

func TestPull_FilenameByRejectsInvalidBeforeIO(t *testing.T) {
	for _, value := range []string{"foo", "", " ", "TITLE"} {
		t.Run(value, func(t *testing.T) {
			cmd := pullCmd(&cmdconfig.Options{ConfigFile: filepath.Join(t.TempDir(), "missing-config.yaml")})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"--filename-by=" + value})
			err := cmd.ExecuteContext(t.Context())
			var usageErr *fail.UsageError
			require.ErrorAs(t, err, &usageErr)
			require.EqualError(t, err, fmt.Sprintf("invalid --filename-by %q: must be 'name' or 'title'", value))
			require.Equal(t, gcxerrors.ExitUsageError, *fail.ErrorToDetailedError(err).ExitCode)
		})
	}
}

func TestPull_FilenameByExistingFiles(t *testing.T) {
	for _, tt := range []struct {
		name     string
		mode     string
		oldPath  string
		oldName  string
		wantPath string
		warn     bool
		preserve bool
	}{
		{name: "title collision", mode: "title", oldPath: "sales-overview.json", oldName: "other", wantPath: "sales-overview-read-only.json", preserve: true},
		{name: "title rename", mode: "title", oldPath: "old-title.json", oldName: "read-only", wantPath: "sales-overview.json", warn: true, preserve: true},
		{name: "switch to title", mode: "title", oldPath: "read-only.json", oldName: "read-only", wantPath: "sales-overview.json", warn: true, preserve: true},
		{name: "name mode unchanged", mode: "name", oldPath: "read-only.json", oldName: "other", wantPath: "read-only.json"},
	} {
		for _, agentMode := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/agent=%t", tt.name, agentMode), func(t *testing.T) {
				f := newReadOnlyCommandFixture(t, "TemplateGroup")
				t.Setenv("GCX_AGENT_MODE", strconv.FormatBool(agentMode))
				agent.ResetForTesting()
				require.NoError(t, unstructured.SetNestedField(f.readOnly.Object, "Sales Overview", "spec", "title"))
				old := f.readOnly.DeepCopy()
				old.SetName(tt.oldName)
				destination := t.TempDir()
				dir := filepath.Join(destination, f.groupDir)
				require.NoError(t, os.MkdirAll(dir, 0o755))
				original, err := json.Marshal(old)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(dir, tt.oldPath), original, 0o600))
				var stdout, stderr bytes.Buffer
				cmd := pullCmd(f.config)
				cmd.SetOut(&stdout)
				cmd.SetErr(&stderr)
				cmd.SetArgs([]string{f.plural, "--path", destination, "--filename-by", tt.mode})
				require.NoError(t, cmd.ExecuteContext(t.Context()))
				require.FileExists(t, filepath.Join(dir, tt.wantPath))
				current, err := os.ReadFile(filepath.Join(dir, tt.oldPath))
				require.NoError(t, err)
				if tt.preserve {
					require.Equal(t, original, current)
				} else {
					var refreshed unstructured.Unstructured
					require.NoError(t, json.Unmarshal(current, &refreshed))
					require.Equal(t, "read-only", refreshed.GetName())
				}
				require.NotContains(t, stdout.String(), "stale file")
				if tt.warn {
					require.Contains(t, stderr.String(), "stale file")
					require.Contains(t, stderr.String(), tt.oldPath)
					require.Contains(t, stderr.String(), tt.wantPath)
					if agentMode {
						require.Contains(t, stderr.String(), `"class":"warning"`)
					} else {
						require.Contains(t, stderr.String(), "warn:")
					}
				} else {
					require.Empty(t, stderr.String())
				}
			})
		}
	}
}
