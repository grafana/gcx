//nolint:testpackage // tests require access to unexported fakeAppsClient
package apps

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instoutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
)

func TestGetCmd_JSONOutputShape(t *testing.T) {
	client := &fakeAppsClient{
		getResponses: []getResponse{{namespaces: buildNamespaces(true, "grotshop", "checkout")}},
		discoverItems: []instrumentation.DiscoveryItem{
			{ClusterName: "c1", Namespace: "grotshop", Name: "svc"},
		},
	}

	cmd := newGetCmd(client)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"-o", "json", "c1", "grotshop"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outStr := strings.TrimSpace(out.String())
	if strings.HasPrefix(outStr, "[") {
		t.Errorf("apps get must return single JSON object, not array; got: %s", outStr)
	}
	if !strings.HasPrefix(outStr, "{") {
		t.Errorf("apps get must return single JSON object starting with '{'; got: %s", outStr)
	}
}

// TestGetCmd_Discovered covers all four (configured/not, discovered/not) combinations.
func TestGetCmd_Discovered(t *testing.T) {
	discoveredItems := []instrumentation.DiscoveryItem{
		{ClusterName: "c1", Namespace: "grotshop", Name: "svc"},
	}

	tests := []struct {
		name           string
		args           []string
		namespaces     []instrumentation.App
		discoverItems  []instrumentation.DiscoveryItem
		wantDiscovered *bool // nil = check not applicable (error path)
		wantExitCode   *int  // nil = expect exit 0
		wantErrSummary string
		wantJSON       bool
	}{
		{
			// Configured AND discovered → exit 0, discovered:true
			name:           "configured + discovered — exit 0 discovered true",
			args:           []string{"-o", "json", "c1", "grotshop"},
			namespaces:     buildNamespaces(true, "grotshop"),
			discoverItems:  discoveredItems,
			wantDiscovered: boolp(true), //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
		},
		{
			// Configured but NOT discovered → exit 0, discovered:false
			name:           "configured + not discovered — exit 0 discovered false",
			args:           []string{"-o", "json", "c1", "grotshop"},
			namespaces:     buildNamespaces(true, "grotshop"),
			discoverItems:  nil,
			wantDiscovered: boolp(false), //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
		},
		{
			// NOT configured but discovered → exit 0, discovered:true (show empty view)
			name:           "not configured + discovered — exit 0 discovered true",
			args:           []string{"-o", "json", "c1", "grotshop"},
			namespaces:     nil,
			discoverItems:  discoveredItems,
			wantDiscovered: boolp(true), //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
		},
		{
			// Neither configured nor discovered → exit 1, Resource not found
			name:           "not configured + not discovered — exit 1",
			args:           []string{"-o", "json", "c1", "grotshop"},
			namespaces:     nil,
			discoverItems:  nil,
			wantExitCode:   intptr(fail.ExitGeneralError),
			wantErrSummary: "Resource not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeAppsClient{
				getResponses:  []getResponse{{namespaces: tc.namespaces}},
				discoverItems: tc.discoverItems,
			}

			cmd := newGetCmd(client)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(tc.args)

			err := cmd.Execute()

			if tc.wantExitCode != nil {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				var de *fail.DetailedError
				if !errors.As(err, &de) {
					t.Fatalf("expected *fail.DetailedError, got %T: %v", err, err)
				}
				if de.Summary != tc.wantErrSummary {
					t.Errorf("expected summary %q, got %q", tc.wantErrSummary, de.Summary)
				}
				if de.ExitCode == nil || *de.ExitCode != *tc.wantExitCode {
					t.Errorf("expected exit code %d, got %v", *tc.wantExitCode, de.ExitCode)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.wantDiscovered != nil {
				var view instoutput.AppView
				if err2 := json.Unmarshal(out.Bytes(), &view); err2 != nil {
					t.Fatalf("failed to parse JSON output: %v\noutput: %s", err2, out.String())
				}
				if view.Discovered != *tc.wantDiscovered {
					t.Errorf("expected discovered=%v, got %v\noutput: %s", *tc.wantDiscovered, view.Discovered, out.String())
				}
			}
		})
	}
}

func TestGetCmd(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		namespaces    []instrumentation.App
		discoverItems []instrumentation.DiscoveryItem
		wantOutput    string
		wantErr       string
	}{
		{
			name:       "found namespace — declared and discovered",
			args:       []string{"c1", "grotshop"},
			namespaces: buildNamespaces(true, "grotshop", "checkout"),
			discoverItems: []instrumentation.DiscoveryItem{
				{ClusterName: "c1", Namespace: "grotshop", Name: "svc"},
			},
			wantOutput: "grotshop",
		},
		{
			// namespace not in declared list AND not discovered → exit 1 Resource not found
			name:          "namespace not found — neither declared nor discovered",
			args:          []string{"c1", "missing"},
			namespaces:    buildNamespaces(true, "grotshop"),
			discoverItems: nil,
			wantErr:       `Resource not found`,
		},
		{
			// namespace not in declared list AND not discovered (empty cluster)
			name:          "empty cluster — not discovered — not found",
			args:          []string{"c1", "grotshop"},
			namespaces:    nil,
			discoverItems: nil,
			wantErr:       `Resource not found`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeAppsClient{
				getResponses:  []getResponse{{namespaces: tc.namespaces}},
				discoverItems: tc.discoverItems,
			}

			cmd := newGetCmd(client)
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
			if tc.wantOutput != "" && !bytes.Contains(out.Bytes(), []byte(tc.wantOutput)) {
				t.Errorf("expected output to contain %q, got:\n%s", tc.wantOutput, out.String())
			}
		})
	}
}

// intptr returns a pointer to an int literal.
func intptr(i int) *int { return &i } //nolint:modernize // intptr(x) cannot simplify to new(x) — new(int) creates *0, not *x
