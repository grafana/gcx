//nolint:testpackage // White-box tests cover query option validation.
package pyroscope

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPyroscopeQueryOptsValidateSelectors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name: "span ID",
			args: []string{"--span-id", "00f067aa0ba902b7"},
		},
		{
			name: "multiple span IDs",
			args: []string{"--span-id", "00f067aa0ba902b7", "--span-id", "5a4fe264a9c987fe"},
		},
		{
			name: "trace selector",
			args: []string{"--trace-id", "4bf92f3577b34da6a3ce929d0e0e4736"},
		},
		{
			name: "trace selector with pprof",
			args: []string{"--trace-id", "4bf92f3577b34da6a3ce929d0e0e4736", "-o", "pprof"},
		},
		{
			name:    "error on empty with pprof",
			args:    []string{"--error-on-empty", "-o", "pprof"},
			wantErr: "--error-on-empty is not supported with -o pprof",
		},
		{
			name: "trace selector with stacktrace selector",
			args: []string{"--trace-id", "4bf92f3577b34da6a3ce929d0e0e4736", "--stacktrace-selector", "main.run"},
		},
		{
			name:    "short span ID",
			args:    []string{"--span-id", "00f067aa"},
			wantErr: "16-character hex span ID",
		},
		{
			name:    "non-hex span ID",
			args:    []string{"--span-id", "00f067aa0ba902bg"},
			wantErr: "16-character hex span ID",
		},
		{
			name:    "short trace selector",
			args:    []string{"--trace-id", "00f067aa0ba902b7"},
			wantErr: "32-character hex trace ID",
		},
		{
			name:    "non-hex trace selector",
			args:    []string{"--trace-id", "4bf92f3577b34da6a3ce929d0e0e473g"},
			wantErr: "32-character hex trace ID",
		},
		{
			name:    "span and stacktrace selectors",
			args:    []string{"--span-id", "00f067aa0ba902b7", "--stacktrace-selector", "main.run"},
			wantErr: "--span-id and --stacktrace-selector cannot be used together",
		},
		{
			name:    "span and profile selectors",
			args:    []string{"--span-id", "00f067aa0ba902b7", "--profile-id", "550e8400-e29b-41d4-a716-446655440000"},
			wantErr: "--span-id and --profile-id cannot be used together",
		},
		{
			name:    "trace and span selectors",
			args:    []string{"--trace-id", "4bf92f3577b34da6a3ce929d0e0e4736", "--span-id", "00f067aa0ba902b7"},
			wantErr: "--trace-id and --span-id cannot be used together",
		},
		{
			name:    "trace and profile selectors",
			args:    []string{"--trace-id", "4bf92f3577b34da6a3ce929d0e0e4736", "--profile-id", "550e8400-e29b-41d4-a716-446655440000"},
			wantErr: "--trace-id and --profile-id cannot be used together",
		},
		{
			name:    "span ID with pprof",
			args:    []string{"--span-id", "00f067aa0ba902b7", "-o", "pprof"},
			wantErr: "--span-id is not supported with -o pprof",
		},
		{
			name: "trace selector with dot",
			args: []string{"--trace-id", "4bf92f3577b34da6a3ce929d0e0e4736", "-o", "dot"},
		},
		{
			name:    "span ID with dot",
			args:    []string{"--span-id", "00f067aa0ba902b7", "-o", "dot"},
			wantErr: "--span-id is not supported with -o dot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &pyroscopeQueryOpts{}
			flags := pflag.NewFlagSet(t.Name(), pflag.ContinueOnError)
			opts.setup(flags)
			args := append([]string{"--profile-type", "process_cpu:cpu:nanoseconds:cpu:nanoseconds"}, tt.args...)
			require.NoError(t, flags.Parse(args))

			err := opts.Validate(flags)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestQueryCmd_ErrorOnEmptyPprofValidationRunsBeforeConfigIO(t *testing.T) {
	cmd := QueryCmd(&providers.ConfigLoader{})
	cmd.SetArgs([]string{"--profile-type", "process_cpu:cpu:nanoseconds:cpu:nanoseconds", "--error-on-empty", "-o", "pprof"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--error-on-empty is not supported with -o pprof")
}

// TestQueryCmd_DotV1FallbackErrorOnEmpty covers --error-on-empty through the
// dot-format v1-fallback path specifically: queryDotV1Fallback itself no
// longer renders or applies --error-on-empty (that moved to the shared
// queryLinkFinisher tail every render branch routes through), so this needs
// a full command run rather than calling queryDotV1Fallback directly.
func TestQueryCmd_DotV1FallbackErrorOnEmpty(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
			return
		case "/api/datasources/uid/pyro-uid":
			w.Header().Set("Content-Type", "application/json")
			_, err := w.Write([]byte(`{"uid":"pyro-uid","type":"grafana-pyroscope-datasource"}`))
			assert.NoError(t, err)
			return
		}
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		if strings.Contains(string(body), `"format":"dot"`) {
			w.WriteHeader(http.StatusBadRequest)
			_, err := w.Write([]byte(`{"message":"dot format is only supported with the v2 query backend"}`))
			assert.NoError(t, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, err = w.Write([]byte(`{"flamegraph":{"names":[],"levels":[],"total":"0","maxSelf":"0"}}`))
		assert.NoError(t, err)
	}))
	defer srv.Close()

	f, err := os.CreateTemp(t.TempDir(), "gcx-pyro-config-*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(`
contexts:
  default:
    grafana:
      server: "` + srv.URL + `"
      token: "test-token"
      org-id: 1
      tls:
        insecure-skip-verify: true
current-context: default
`)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(f.Name())

	for _, tt := range []struct {
		name         string
		errorOnEmpty bool
		wantErr      bool
	}{
		{name: "enabled", errorOnEmpty: true, wantErr: true},
		{name: "disabled", errorOnEmpty: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := QueryCmd(loader)
			root := &cobra.Command{Use: "test"}
			root.AddCommand(cmd)
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			args := []string{
				"query", "-d", "pyro-uid", "-o", "dot",
				"--profile-type", "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
				`{service_name="frontend"}`,
			}
			if tt.errorOnEmpty {
				args = append(args, "--error-on-empty")
			}
			root.SetArgs(args)

			err := root.Execute()
			if tt.wantErr {
				require.ErrorIs(t, err, dsquery.ErrNoResult)
				return
			}
			require.NoError(t, err)
			assert.Contains(t, stdout.String(), "(no profile data)")
		})
	}
}
