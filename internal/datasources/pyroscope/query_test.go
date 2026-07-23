//nolint:testpackage // White-box tests cover query option validation.
package pyroscope

import (
	"testing"

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
