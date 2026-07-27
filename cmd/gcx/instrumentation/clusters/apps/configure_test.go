//nolint:testpackage,modernize // tests require access to unexported fakeAppsClient; boolp(true) != new(bool) (creates *false)
package apps

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/providers/instrumentation/rmw"
)

func TestConfigureCmd(t *testing.T) {
	tests := []struct {
		name               string
		args               []string
		initial            []instrumentation.App
		wantErr            string
		wantNS             *instrumentation.App
		wantOtherPreserved string
		wantOtherApps      []instrumentation.AppOverride
		wantNoSet          bool  // true when SetAppInstrumentation must NOT be called (no-op)
		wantChanged        *bool // nil means "don't check"; pointer to expected value
	}{
		{
			name:    "no flags → error",
			args:    []string{"c1", "grotshop"},
			initial: nil,
			wantErr: "requires either --use-defaults --yes or one or more --<feat> flags",
		},
		{
			name:    "--use-defaults without --yes → error",
			args:    []string{"c1", "grotshop", "--use-defaults"},
			initial: nil,
			wantErr: "--yes is required",
		},
		{
			name:    "--use-defaults with feature flag → mutually exclusive error",
			args:    []string{"c1", "grotshop", "--use-defaults", "--tracing"},
			initial: nil,
			wantErr: "mutually exclusive",
		},
		{
			name:    "--use-defaults --yes: applies all-on defaults",
			args:    []string{"c1", "grotshop", "--use-defaults", "--yes"},
			initial: nil,
			wantNS: &instrumentation.App{
				Name:            "grotshop",
				Autoinstrument:  boolp(true),
				Tracing:         boolp(true),
				Logging:         boolp(true),
				ProcessMetrics:  boolp(true),
				ExtendedMetrics: boolp(true),
				Profiling:       boolp(true),
			},
		},
		{
			name:    "configure new namespace — autoinstrument set true",
			args:    []string{"c1", "grotshop", "--tracing"},
			initial: nil,
			wantNS: &instrumentation.App{
				Name:           "grotshop",
				Autoinstrument: boolp(true),
				Tracing:        boolp(true),
			},
		},
		{
			name: "configure with --tracing=false sets tracing=false",
			args: []string{"c1", "grotshop", "--tracing=false"},
			initial: []instrumentation.App{
				{Name: "grotshop", Autoinstrument: boolp(true), Tracing: boolp(true)},
			},
			wantNS: &instrumentation.App{
				Name:           "grotshop",
				Autoinstrument: boolp(true),
				Tracing:        boolp(false),
			},
		},
		{
			// other namespaces including their apps[] overrides must be
			// byte-equal after configure on a different namespace.
			name: "preserves checkout namespace with apps[] overrides",
			args: []string{"c1", "grotshop", "--tracing"},
			initial: []instrumentation.App{
				{Name: "grotshop", Autoinstrument: boolp(true)},
				{
					Name:           "checkout",
					Autoinstrument: boolp(true),
					Apps: []instrumentation.AppOverride{
						{Name: "payment-svc", Selection: "SELECTION_INCLUDED"},
					},
				},
			},
			wantNS: &instrumentation.App{
				Name:           "grotshop",
				Autoinstrument: boolp(true),
				Tracing:        boolp(true),
			},
			wantOtherPreserved: "checkout",
			wantOtherApps: []instrumentation.AppOverride{
				{Name: "payment-svc", Selection: "SELECTION_INCLUDED"},
			},
		},
		{
			// Idempotent re-run: existing state already matches desired state.
			// SetAppInstrumentation must NOT be called; changed: false.
			name: "idempotent re-run same flags — no write, changed:false",
			args: []string{"c1", "grotshop", "--tracing", "--logging"},
			initial: []instrumentation.App{
				{
					Name:           "grotshop",
					Autoinstrument: boolp(true),
					Tracing:        boolp(true),
					Logging:        boolp(true),
				},
			},
			wantNoSet:   true,
			wantChanged: boolp(false),
		},
		{
			// Partial idempotent: only one flag changes — write must happen; changed: true.
			name: "flip one flag — write happens, changed:true",
			args: []string{"c1", "grotshop", "--logging"},
			initial: []instrumentation.App{
				{
					Name:           "grotshop",
					Autoinstrument: boolp(true),
					Tracing:        boolp(true),
					Logging:        boolp(false),
				},
			},
			wantNS: &instrumentation.App{
				Name:           "grotshop",
				Autoinstrument: boolp(true),
				Logging:        boolp(true),
			},
			wantChanged: boolp(true),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Supply three identical GET responses to cover all paths:
			//   - pre-check GET (RMW mode: before deciding to write)
			//   - rmw.Update's first GET
			//   - rmw.Update's re-check GET
			// use-defaults mode only needs one GET, extra responses don't hurt.
			responses := []getResponse{
				{namespaces: tc.initial},
				{namespaces: tc.initial},
				{namespaces: tc.initial},
			}
			client := &fakeAppsClient{getResponses: responses}

			cmd := newConfigureCmd(client)
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

			if tc.wantNoSet {
				if len(client.setCalls) != 0 {
					t.Errorf("expected SetAppInstrumentation NOT to be called (no-op), but got %d call(s)", len(client.setCalls))
				}
			} else {
				if len(client.setCalls) == 0 {
					t.Fatal("expected SetAppInstrumentation to be called")
				}
				assertConfigureNSState(t, client.setCalls[len(client.setCalls)-1].namespaces, tc.wantNS, tc.wantOtherPreserved, tc.wantOtherApps)
			}

			if tc.wantChanged != nil {
				// Output is JSON in agent mode ("changed":true / "changed":false)
				// or human-readable text in non-agent mode ("done" / "no changes").
				// Accept both formats to be environment-independent.
				outStr := out.String()
				var wantStr, humanStr string
				if *tc.wantChanged {
					wantStr, humanStr = `"changed":true`, "done"
				} else {
					wantStr, humanStr = `"changed":false`, "no changes"
				}
				if !strings.Contains(outStr, wantStr) && !strings.Contains(outStr, humanStr) {
					t.Errorf("output %q does not indicate changed=%v", outStr, *tc.wantChanged)
				}
			}
		})
	}
}

// TestConfigureCmd_ConflictError tests concurrent configure produces a
// ConflictError with the correct command/namespace fields in the error message.
func TestConfigureCmd_ConflictError(t *testing.T) {
	client := &fakeAppsClient{
		getResponses: []getResponse{
			// Pre-check GET: tracing=false (state differs from desired, proceed to rmw).
			{namespaces: []instrumentation.App{
				{Name: "grotshop", Autoinstrument: boolp(true), Tracing: boolp(false)},
			}},
			// rmw first GET: tracing=false.
			{namespaces: []instrumentation.App{
				{Name: "grotshop", Autoinstrument: boolp(true), Tracing: boolp(false)},
			}},
			// rmw re-check GET (pre-write): tracing=true — simulates a concurrent change.
			{namespaces: []instrumentation.App{
				{Name: "grotshop", Autoinstrument: boolp(true), Tracing: boolp(true)},
			}},
		},
	}

	cmd := newConfigureCmd(client)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"c1", "grotshop", "--tracing"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	if !rmw.IsConflictError(err) {
		t.Fatalf("expected ConflictError, got %T: %v", err, err)
	}
	errStr := err.Error()
	for _, want := range []string{"apps configure", "grotshop", "cannot write"} {
		if !strings.Contains(errStr, want) {
			t.Errorf("error missing %q: %q", want, errStr)
		}
	}
}

// TestAppListEqual tests the internal appListEqual helper.
func TestAppListEqual(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []instrumentation.App
		wantEq   bool
		wantDiff string
	}{
		{"empty lists", nil, nil, true, ""},
		{"same single ns", buildNamespaces(true, "ns1"), buildNamespaces(true, "ns1"), true, ""},
		{
			"added namespace in b",
			buildNamespaces(true, "ns1"),
			buildNamespaces(true, "ns1", "ns2"),
			false, "added namespace: ns2",
		},
		{
			"removed namespace in b",
			buildNamespaces(true, "ns1", "ns2"),
			buildNamespaces(true, "ns1"),
			false, "removed namespace: ns2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eq, diff := appListEqual(tc.a, tc.b)
			if eq != tc.wantEq {
				t.Errorf("equal: want %v, got %v (diff=%q)", tc.wantEq, eq, diff)
			}
			if tc.wantDiff != "" && !strings.Contains(diff, tc.wantDiff) {
				t.Errorf("diff: want %q in %q", tc.wantDiff, diff)
			}
		})
	}
}

// TestConfigureCmd_Discovered verifies that the MutationResult returned by
// apps configure includes the discovered field reflecting RunK8sDiscovery state
// at the time of the call. Tested directly on the MutationResult by
// invoking configure in agent mode (GCX_AGENT_MODE=true) so JSON output is emitted.
func TestConfigureCmd_Discovered(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		initial        []instrumentation.App
		discoverItems  []instrumentation.DiscoveryItem
		wantDiscovered bool
	}{
		{
			name:    "namespace discovered after write",
			args:    []string{"c1", "grotshop", "--tracing"},
			initial: nil,
			discoverItems: []instrumentation.DiscoveryItem{
				{ClusterName: "c1", Namespace: "grotshop", Name: "svc"},
			},
			wantDiscovered: true,
		},
		{
			name:           "namespace not discovered after write",
			args:           []string{"c1", "grotshop", "--tracing"},
			initial:        nil,
			discoverItems:  nil,
			wantDiscovered: false,
		},
		{
			// Idempotent (no-op) path: discovered field still present.
			name: "idempotent configure — namespace discovered",
			args: []string{"c1", "grotshop", "--tracing"},
			initial: []instrumentation.App{
				{Name: "grotshop", Autoinstrument: boolp(true), Tracing: boolp(true)},
			},
			discoverItems: []instrumentation.DiscoveryItem{
				{ClusterName: "c1", Namespace: "grotshop", Name: "svc"},
			},
			wantDiscovered: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Enable agent mode so MutationResult.Emit() outputs JSON.
			// agent.IsAgentMode() uses a cached init()-time value, so ResetForTesting()
			// must be called after t.Setenv to re-run detection from the new env.
			t.Setenv("GCX_AGENT_MODE", "true")
			agent.ResetForTesting()
			t.Cleanup(func() { agent.ResetForTesting() }) // restore after test

			responses := []getResponse{
				{namespaces: tc.initial},
				{namespaces: tc.initial},
				{namespaces: tc.initial},
			}
			client := &fakeAppsClient{
				getResponses:  responses,
				discoverItems: tc.discoverItems,
			}

			cmd := newConfigureCmd(client)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(tc.args)

			if err := cmd.Execute(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// In agent mode, MutationResult.Emit() outputs JSON.
			outStr := strings.TrimSpace(out.String())
			if !strings.HasPrefix(outStr, "{") {
				t.Fatalf("expected JSON output in agent mode, got: %q", outStr)
			}
			var result map[string]any
			if err := json.Unmarshal([]byte(outStr), &result); err != nil {
				t.Fatalf("failed to parse JSON output: %v\noutput: %s", err, outStr)
			}
			disc, ok := result["discovered"]
			if !ok {
				t.Fatalf("expected 'discovered' field in JSON output: %s", outStr)
			}
			if disc != tc.wantDiscovered {
				t.Errorf("discovered: want %v, got %v\noutput: %s", tc.wantDiscovered, disc, outStr)
			}
		})
	}
}

// assertConfigureNSState verifies the target namespace state and other-namespace preservation.
func assertConfigureNSState(t *testing.T, setNSs []instrumentation.App, wantNS *instrumentation.App, wantOther string, wantOtherApps []instrumentation.AppOverride) {
	t.Helper()
	if wantNS == nil {
		return
	}

	found := false
	for _, ns := range setNSs {
		if ns.Name != wantNS.Name {
			continue
		}
		found = true
		if wantNS.Autoinstrument != nil && (ns.Autoinstrument == nil || *ns.Autoinstrument != *wantNS.Autoinstrument) {
			t.Errorf("autoinstrument: want %v, got %v", *wantNS.Autoinstrument, ns.Autoinstrument)
		}
		if wantNS.Tracing != nil && (ns.Tracing == nil || *ns.Tracing != *wantNS.Tracing) {
			t.Errorf("tracing: want %v, got %v", *wantNS.Tracing, ns.Tracing)
		}
		if wantNS.Logging != nil && (ns.Logging == nil || *ns.Logging != *wantNS.Logging) {
			t.Errorf("logging: want %v, got %v", *wantNS.Logging, ns.Logging)
		}
		if wantNS.ProcessMetrics != nil && (ns.ProcessMetrics == nil || *ns.ProcessMetrics != *wantNS.ProcessMetrics) {
			t.Errorf("process-metrics: want %v, got %v", *wantNS.ProcessMetrics, ns.ProcessMetrics)
		}
		if wantNS.ExtendedMetrics != nil && (ns.ExtendedMetrics == nil || *ns.ExtendedMetrics != *wantNS.ExtendedMetrics) {
			t.Errorf("extended-metrics: want %v, got %v", *wantNS.ExtendedMetrics, ns.ExtendedMetrics)
		}
		if wantNS.Profiling != nil && (ns.Profiling == nil || *ns.Profiling != *wantNS.Profiling) {
			t.Errorf("profiling: want %v, got %v", *wantNS.Profiling, ns.Profiling)
		}
	}
	if !found {
		t.Errorf("namespace %q not found in set payload", wantNS.Name)
	}

	if wantOther == "" {
		return
	}
	for _, ns := range setNSs {
		if ns.Name != wantOther {
			continue
		}
		if len(ns.Apps) != len(wantOtherApps) {
			t.Errorf("other namespace apps: want %d overrides, got %d", len(wantOtherApps), len(ns.Apps))
			return
		}
		for i, ov := range ns.Apps {
			if ov.Name != wantOtherApps[i].Name || ov.Selection != wantOtherApps[i].Selection {
				t.Errorf("other namespace apps[%d]: want %+v, got %+v", i, wantOtherApps[i], ov)
			}
		}
	}
}
