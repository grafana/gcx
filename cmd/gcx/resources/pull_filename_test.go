package resources //nolint:testpackage // Exercise pull command wiring using the shared discovery/API fixture.

import (
	"io"
	"os"
	"path/filepath"
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
			require.Contains(t, err.Error(), "--filename-by")
			require.Contains(t, err.Error(), "'name' or 'title'")
			require.Equal(t, gcxerrors.ExitUsageError, *fail.ErrorToDetailedError(err).ExitCode)
		})
	}
}
