package extsdk_test

import (
	"errors"
	"testing"

	"github.com/grafana/gcx/extsdk"
)

func TestResultErr(t *testing.T) {
	tests := []struct {
		name   string
		result extsdk.Result
		want   string
	}{
		{name: "success", result: extsdk.Result{Stdout: []byte(`{}`)}},
		{name: "error document", result: extsdk.Result{ExitCode: 4, Stdout: []byte(`{"error":{"summary":"boom","details":"it broke"}}`), Stderr: []byte("ignored")}, want: "boom: it broke"},
		{name: "stderr", result: extsdk.Result{ExitCode: 2, Stderr: []byte("Error: bad flag\n")}, want: "bad flag"},
		{name: "multi-line stderr", result: extsdk.Result{ExitCode: 2, Stderr: []byte("Error: Invalid command usage\n│\n│ unknown flag\n")}, want: "Invalid command usage"},
		{name: "no output", result: extsdk.Result{ExitCode: 1}, want: "gcx exited with status 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.result.Err()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var exit *extsdk.ExitError
			if !errors.As(err, &exit) {
				t.Fatalf("want *ExitError, got %v", err)
			}
			if exit.Code != tt.result.ExitCode || exit.Message != tt.want {
				t.Fatalf("got code %d message %q, want %d %q", exit.Code, exit.Message, tt.result.ExitCode, tt.want)
			}
		})
	}
}
