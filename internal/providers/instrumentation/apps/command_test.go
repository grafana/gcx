package apps_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/providers/instrumentation/apps"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Command structure
// ---------------------------------------------------------------------------

func TestCommands_Structure(t *testing.T) {
	loader := new(providers.ConfigLoader)
	cmd := apps.Commands(loader)

	assert.Equal(t, "apps", cmd.Use)
	assert.Contains(t, cmd.Aliases, "app")

	subCmds := make(map[string]bool)
	for _, c := range cmd.Commands() {
		subCmds[c.Name()] = true
	}

	for _, name := range []string{"list", "get", "create", "update", "delete"} {
		assert.True(t, subCmds[name], "missing subcommand %q", name)
	}
}

func TestCommands_ListHasClusterFlag(t *testing.T) {
	loader := new(providers.ConfigLoader)
	cmd := apps.Commands(loader)

	for _, c := range cmd.Commands() {
		if c.Name() == "list" {
			f := c.Flags().Lookup("cluster")
			require.NotNil(t, f, "list command must have --cluster flag")
			assert.Equal(t, "cluster", f.Name)
			return
		}
	}
	t.Fatal("list subcommand not found")
}

func TestCommands_ListHasLimitFlag(t *testing.T) {
	loader := new(providers.ConfigLoader)
	cmd := apps.Commands(loader)

	for _, c := range cmd.Commands() {
		if c.Name() == "list" {
			f := c.Flags().Lookup("limit")
			require.NotNil(t, f, "list command must have --limit flag")
			return
		}
	}
	t.Fatal("list subcommand not found")
}

// ---------------------------------------------------------------------------
// AC1: create with mismatched metadata.name returns validation error
// ---------------------------------------------------------------------------

func TestCreate_MetadataNameMismatch(t *testing.T) {
	tests := []struct {
		name            string
		manifest        string
		wantErrContains []string
	}{
		{
			name: "metadata.name mismatch",
			manifest: `apiVersion: instrumentation.grafana.app/v1alpha1
kind: App
metadata:
  name: wrong-name
spec:
  cluster: prod-east
  namespace: payments
`,
			wantErrContains: []string{
				"metadata.name",
				"spec.cluster",
				"spec.namespace",
			},
		},
		{
			name: "absent metadata.name is valid (fails later at network)",
			manifest: `apiVersion: instrumentation.grafana.app/v1alpha1
kind: App
metadata: {}
spec:
  cluster: prod-east
  namespace: payments
`,
			// Absent name passes validation — error will be from config loader
			wantErrContains: nil,
		},
		{
			name: "correct metadata.name is valid (fails later at network)",
			manifest: `apiVersion: instrumentation.grafana.app/v1alpha1
kind: App
metadata:
  name: prod-east-payments
spec:
  cluster: prod-east
  namespace: payments
`,
			// Correct name passes validation — error will be from config loader
			wantErrContains: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "app.yaml")
			require.NoError(t, os.WriteFile(file, []byte(tt.manifest), 0o600))

			loader := new(providers.ConfigLoader)
			cmd := apps.Commands(loader)

			var errBuf bytes.Buffer
			cmd.SetErr(&errBuf)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetContext(context.Background())

			cmd.SetArgs([]string{"create", "-f", file})
			err := cmd.Execute()

			if len(tt.wantErrContains) == 0 {
				// Absent or correct name: validation passes, subsequent error is from missing config
				// We just verify the error is NOT about metadata.name identity mismatch.
				if err != nil {
					assert.NotContains(t, err.Error(), "identity mismatch",
						"validation should pass for absent/correct metadata.name")
				}
				return
			}

			require.Error(t, err, "expected an error for mismatched metadata.name")
			for _, want := range tt.wantErrContains {
				assert.Contains(t, err.Error(), want,
					"error message should name %q", want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TableCodec
// ---------------------------------------------------------------------------

func toTypedObjs(appList []instrumentation.App) []adapter.TypedObject[instrumentation.App] {
	objs := make([]adapter.TypedObject[instrumentation.App], len(appList))
	for i, a := range appList {
		objs[i] = adapter.TypedObject[instrumentation.App]{Spec: a}
	}
	return objs
}

func TestTableCodec_Encode(t *testing.T) {
	tests := []struct {
		name     string
		wide     bool
		appList  []instrumentation.App
		wantCols []string
		wantRows []string
	}{
		{
			name: "standard columns",
			wide: false,
			appList: []instrumentation.App{
				{Cluster: "prod-east", Namespace: "payments", Tracing: true, Logging: false, Profiling: true},
			},
			wantCols: []string{"CLUSTER", "NAMESPACE", "TRACING", "LOGGING", "PROFILING"},
			wantRows: []string{"prod-east", "payments", "true", "false", "true"},
		},
		{
			name: "wide columns",
			wide: true,
			appList: []instrumentation.App{
				{Cluster: "prod-east", Namespace: "payments", Selection: "all", Tracing: true},
			},
			wantCols: []string{"CLUSTER", "NAMESPACE", "SELECTION", "TRACING", "LOGGING", "PROCESS METRICS", "EXTENDED METRICS", "PROFILING"},
			wantRows: []string{"prod-east", "payments", "all"},
		},
		{
			name:     "empty list shows header only",
			wide:     false,
			appList:  []instrumentation.App{},
			wantCols: []string{"CLUSTER", "NAMESPACE"},
		},
		{
			name: "empty selection shows dash",
			wide: true,
			appList: []instrumentation.App{
				{Cluster: "c", Namespace: "ns"},
			},
			wantRows: []string{"-"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec := &apps.TableCodec{Wide: tt.wide}
			var buf bytes.Buffer

			err := codec.Encode(&buf, toTypedObjs(tt.appList))
			require.NoError(t, err)

			output := buf.String()
			for _, col := range tt.wantCols {
				assert.Contains(t, output, col)
			}
			for _, row := range tt.wantRows {
				assert.Contains(t, output, row)
			}
		})
	}
}

func TestTableCodec_Encode_InvalidType(t *testing.T) {
	codec := &apps.TableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not a slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []TypedObject[App]")
}

func TestTableCodec_Decode_Unsupported(t *testing.T) {
	codec := &apps.TableCodec{}
	err := codec.Decode(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support decoding")
}

func TestTableCodec_Format(t *testing.T) {
	tests := []struct {
		name string
		wide bool
		want string
	}{
		{name: "standard", wide: false, want: "table"},
		{name: "wide", wide: true, want: "wide"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec := &apps.TableCodec{Wide: tt.wide}
			assert.Equal(t, tt.want, string(codec.Format()))
		})
	}
}
