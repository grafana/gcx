//nolint:testpackage // tests require access to unexported fakeAppsClient
package apps

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/instrumentation"
)

func TestRemoveCmd(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		initial    []instrumentation.App
		wantErr    string
		wantSetLen int // expected number of namespaces passed to Set
	}{
		{
			name:    "requires --yes flag",
			args:    []string{"c1", "grotshop"},
			initial: buildNamespaces(true, "grotshop"),
			wantErr: "requires --yes",
		},
		{
			name:       "removes target namespace",
			args:       []string{"c1", "grotshop", "--yes"},
			initial:    buildNamespaces(true, "grotshop", "checkout"),
			wantSetLen: 1, // "checkout" remains
		},
		{
			name:       "removing only namespace results in empty list",
			args:       []string{"c1", "grotshop", "--yes"},
			initial:    buildNamespaces(true, "grotshop"),
			wantSetLen: 0,
		},
		{
			name:       "no-op when namespace not present",
			args:       []string{"c1", "missing", "--yes"},
			initial:    buildNamespaces(true, "grotshop"),
			wantSetLen: 1, // "grotshop" still there
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeAppsClient{
				getResponses: []getResponse{{namespaces: tc.initial}},
			}

			cmd := newRemoveCmd(client, instrumentation.BackendURLs{})
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(tc.args)

			err := cmd.Execute()

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("expected error %q, got %q", tc.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(client.setCalls) == 0 {
				t.Fatal("expected SetAppInstrumentation to be called")
			}
			got := client.setCalls[0].namespaces
			if len(got) != tc.wantSetLen {
				t.Errorf("expected %d namespaces after remove, got %d: %+v", tc.wantSetLen, len(got), got)
			}
		})
	}
}
