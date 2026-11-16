package dashboards_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/dashboards"
	"github.com/spf13/cobra"
)

var errServerUnreachable = errors.New("server unreachable")

// unreachableLoader stands in for a Grafana that cannot be reached and counts
// how often the command tries.
type unreachableLoader struct{ calls int }

func (l *unreachableLoader) LoadGrafanaConfig(context.Context) (config.NamespacedRESTConfig, error) {
	l.calls++
	return config.NamespacedRESTConfig{}, errServerUnreachable
}

// TestMutation_ReadsManifestBeforeContactingGrafana pins the order: an input
// error is reported without loading the Grafana config, and a valid manifest
// still reaches the loader.
func TestMutation_ReadsManifestBeforeContactingGrafana(t *testing.T) {
	dir := t.TempDir()
	export := filepath.Join(dir, "export.json")
	if err := os.WriteFile(export, []byte(`{"uid":"d","title":"D","panels":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifest, []byte(`{"apiVersion":"dashboard.grafana.app/v1beta1","kind":"Dashboard","metadata":{"name":"d"},"spec":{"title":"D"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	commands := []struct {
		name string
		new  func(dashboards.GrafanaConfigLoader) *cobra.Command
		args []string
	}{
		{name: "create", new: dashboards.NewTestCreateCommandWithLoader},
		{name: "update", new: dashboards.NewTestUpdateCommandWithLoader, args: []string{"d"}},
	}

	for _, c := range commands {
		t.Run(c.name+" with a UI export", func(t *testing.T) {
			loader := &unreachableLoader{}
			err := runMutation(c.new(loader), append(c.args, "-f", export))

			if _, ok := errors.AsType[*gcxerrors.UsageError](err); !ok {
				t.Fatalf("Execute() error = %v (%T), want *gcxerrors.UsageError", err, err)
			}
			if loader.calls != 0 {
				t.Errorf("LoadGrafanaConfig calls = %d, want 0", loader.calls)
			}
		})

		t.Run(c.name+" with a valid manifest", func(t *testing.T) {
			loader := &unreachableLoader{}
			err := runMutation(c.new(loader), append(c.args, "-f", manifest))

			if !errors.Is(err, errServerUnreachable) {
				t.Fatalf("Execute() error = %v, want the loader error", err)
			}
			if loader.calls != 1 {
				t.Errorf("LoadGrafanaConfig calls = %d, want 1", loader.calls)
			}
		})
	}
}

func runMutation(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)
	return cmd.Execute()
}
