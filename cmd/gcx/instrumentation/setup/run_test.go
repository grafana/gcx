//nolint:testpackage // White-box test: exercises unexported run(), opts, runner, and fakeClient directly.
package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	instrumentation "github.com/grafana/gcx/internal/providers/instrumentation"
	instroutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClient is a test double for clientInterface. It records which methods
// were called and the arguments passed to SetK8SInstrumentation.
type fakeClient struct {
	setupCalled bool
	getCalled   bool
	setCalled   bool

	// setCluster is the Cluster value passed to SetK8SInstrumentation.
	setCluster instrumentation.Cluster

	// Errors to return for each method. nil means success.
	setupErr error
	getErr   error
	setErr   error

	// getResponse is returned by GetK8SInstrumentation.
	getResponse *instrumentation.GetK8SInstrumentationResponse
}

func (f *fakeClient) SetupK8sDiscovery(_ context.Context, _ instrumentation.BackendURLs, _ instrumentation.PromHeaders) error {
	f.setupCalled = true
	return f.setupErr
}

func (f *fakeClient) GetK8SInstrumentation(_ context.Context, _ string) (*instrumentation.GetK8SInstrumentationResponse, error) {
	f.getCalled = true
	return f.getResponse, f.getErr
}

func (f *fakeClient) SetK8SInstrumentation(_ context.Context, _ string, k8s instrumentation.Cluster, _ instrumentation.BackendURLs) error {
	f.setCalled = true
	f.setCluster = k8s
	return f.setErr
}

// newFakeClient returns a fakeClient whose GetK8SInstrumentation returns the
// given cluster as the current declared state.
func newFakeClient(current instrumentation.Cluster) *fakeClient {
	return &fakeClient{
		getResponse: &instrumentation.GetK8SInstrumentationResponse{Cluster: current},
	}
}

// acceptDefaults is a promptFn that returns the current value unchanged for
// every flag — simulating a user who presses Enter at every prompt.
func acceptDefaults(_ string, current bool) (bool, error) {
	return current, nil
}

// prompts returns a promptFn that answers flags in the given order.
// flags are matched by call order, not by name.
func prompts(answers ...bool) func(string, bool) (bool, error) {
	idx := 0
	return func(_ string, _ bool) (bool, error) {
		if idx >= len(answers) {
			return false, nil
		}
		v := answers[idx]
		idx++
		return v, nil
	}
}

func TestRun(t *testing.T) { //nolint:maintidx // intentionally large table-driven test; complexity from test case variety, not logical branching
	clusterName := "prod-cluster"

	// Helper to build a runner from a fakeClient and optional overrides.
	makeRunner := func(client clientInterface, isTTY bool, pFn func(string, bool) (bool, error)) *runner {
		return &runner{
			client:   client,
			urls:     instrumentation.BackendURLs{},
			fm:       instrumentation.FleetManagement{URL: "https://fleet.test", Username: "42"},
			promHdrs: instrumentation.PromHeaders{},
			token:    "<TOKEN>",
			stdout:   &bytes.Buffer{},
			stderr:   &bytes.Buffer{},
			isTTY:    isTTY,
			promptFn: pFn,
		}
	}

	tests := []struct {
		name string
		opts opts
		// current is the server's declared cluster state.
		current instrumentation.Cluster
		isTTY   bool
		// promptFn answers; nil means no prompts expected.
		promptAnswers []bool
		// orgSlug is threaded into runner.orgSlug to test URL substitution.
		orgSlug string

		// Expected outcomes.
		wantErr            bool
		wantSetCalled      bool
		wantSetupCalled    bool
		wantGetCalled      bool
		wantStderrContains []string
		wantStdoutContains []string
		// wantCluster is checked when wantSetCalled is true.
		wantCluster instrumentation.Cluster
	}{
		// (a) Idempotent re-run: cluster already matches defaults;
		//     second --use-defaults run must produce "no changes".
		{
			name: "(a) idempotent re-run reports no changes",
			opts: opts{useDefaults: true},
			current: instrumentation.Cluster{
				CostMetrics:   boolPtr(true),  //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				ClusterEvents: boolPtr(true),  //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				EnergyMetrics: boolPtr(false), //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				NodeLogs:      boolPtr(false), //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
			},
			wantSetupCalled:    true,
			wantGetCalled:      true,
			wantSetCalled:      false,
			wantStderrContains: []string{"SetupK8sDiscovery: ok", "no changes"},
			wantStdoutContains: []string{"helm upgrade", "fleet.test", "42"},
		},

		// (b) --use-defaults applied to unconfigured cluster: defaults are written
		//     and the mutation summary lists the changed flags.
		{
			name:            "(b) --use-defaults applies defaults to unconfigured cluster",
			opts:            opts{useDefaults: true},
			current:         instrumentation.Cluster{}, // all nil / false
			wantSetupCalled: true,
			wantGetCalled:   true,
			wantSetCalled:   true,
			wantCluster: instrumentation.Cluster{
				Name:          clusterName,
				Selection:     "SELECTION_INCLUDED",
				CostMetrics:   boolPtr(true),  //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				ClusterEvents: boolPtr(true),  //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				EnergyMetrics: boolPtr(false), //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				NodeLogs:      boolPtr(false), //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
			},
			wantStderrContains: []string{
				"SetupK8sDiscovery: ok",
				"flag costMetrics: false → true",
				"flag clusterEvents: false → true",
			},
			wantStdoutContains: []string{"helm upgrade"},
		},

		// (c) --use-defaults --energy-metrics=false --node-logs: per-flag overrides take precedence over
		//     defaults. energy is explicitly set false (overrides default false,
		//     net effect same). logs becomes true (--node-logs overrides default false).
		//     Uses *Set fields to simulate cmd.Flags().Changed() behavior.
		{
			name: "(c) --use-defaults --energy-metrics=false --node-logs: override precedence",
			opts: opts{
				useDefaults:      true,
				energyMetrics:    false, // --energy-metrics=false
				energyMetricsSet: true,  // cmd.Flags().Changed("energy-metrics") == true
				nodeLogs:         true,  // --node-logs (=true)
				nodeLogsSet:      true,  // cmd.Flags().Changed("node-logs") == true
			},
			current:         instrumentation.Cluster{}, // all nil / false
			wantSetupCalled: true,
			wantGetCalled:   true,
			wantSetCalled:   true,
			wantCluster: instrumentation.Cluster{
				Name:          clusterName,
				Selection:     "SELECTION_INCLUDED",
				CostMetrics:   boolPtr(true),  //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				ClusterEvents: boolPtr(true),  //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				EnergyMetrics: boolPtr(false), //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				NodeLogs:      boolPtr(true),  //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
			},
			wantStderrContains: []string{
				"flag costMetrics: false → true",
				"flag clusterEvents: false → true",
				"flag nodeLogs: false → true",
			},
			wantStdoutContains: []string{"helm upgrade"},
		},

		// (d) --print-helm-only: no server calls made at all.
		{
			name:               "(d) --print-helm-only makes no server calls",
			opts:               opts{printHelmOnly: true},
			wantSetupCalled:    false,
			wantGetCalled:      false,
			wantSetCalled:      false,
			wantStdoutContains: []string{"helm upgrade", "fleet.test", "42"},
		},

		// (e) stderr mutation summary lists every server call and flag change
		//     under --use-defaults against a partially-configured cluster.
		{
			name: "(e) stderr lists all mutations under --use-defaults",
			opts: opts{useDefaults: true},
			current: instrumentation.Cluster{
				// cost is already false, events is already true — only cost will change.
				CostMetrics:   boolPtr(false), //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				ClusterEvents: boolPtr(true),  //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				EnergyMetrics: boolPtr(false), //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				NodeLogs:      boolPtr(false), //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
			},
			wantSetupCalled: true,
			wantGetCalled:   true,
			wantSetCalled:   true,
			wantCluster: instrumentation.Cluster{
				Name:          clusterName,
				Selection:     "SELECTION_INCLUDED",
				CostMetrics:   boolPtr(true),  //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				ClusterEvents: boolPtr(true),  //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				EnergyMetrics: boolPtr(false), //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				NodeLogs:      boolPtr(false), //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
			},
			wantStderrContains: []string{
				"SetupK8sDiscovery: ok",
				"flag costMetrics: false → true",
			},
			wantStdoutContains: []string{"helm upgrade"},
		},

		// (f) Interactive prompts default to current declared values.
		//     When the user accepts all defaults, no flag changes occur.
		{
			name:  "(f) interactive prompts default to current declared values",
			opts:  opts{useDefaults: false},
			isTTY: true,
			current: instrumentation.Cluster{
				Name:          clusterName,    // non-empty → configured cluster path in interactiveDefaults
				CostMetrics:   boolPtr(true),  //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				ClusterEvents: boolPtr(false), //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				EnergyMetrics: boolPtr(true),  //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				NodeLogs:      boolPtr(false), //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
			},
			// acceptDefaults returns each prompt's current value unchanged.
			promptAnswers:      nil, // signals: use acceptDefaults
			wantSetupCalled:    true,
			wantGetCalled:      true,
			wantSetCalled:      false,
			wantStderrContains: []string{"SetupK8sDiscovery: ok", "no changes"},
			wantStdoutContains: []string{"helm upgrade"},
		},

		// (g) Unconfigured cluster + TTY + !--yes: prompt defaults must be
		//     the recommended set (cost=true, events=true, energy=false,
		//     logs=false), not the cluster's nil/false values.
		//     acceptDefaults returns whatever default it receives unchanged, so
		//     if interactiveDefaults() correctly uses the recommended set for
		//     Name=="", the write will flip cost and events from false→true,
		//     proving the defaults came from the recommended set rather than
		//     the zero/nil cluster values.
		{
			name:            "(g) interactive prompts use defaults for unconfigured cluster",
			opts:            opts{useDefaults: false},
			isTTY:           true,
			current:         instrumentation.Cluster{}, // unconfigured: Name==""
			promptAnswers:   nil,                       // acceptDefaults: echoes prompt defaults back
			wantSetupCalled: true,
			wantGetCalled:   true,
			wantSetCalled:   true, // cost+events defaulted true → changed from nil/false
			wantCluster: instrumentation.Cluster{
				Name:          clusterName,
				Selection:     "SELECTION_INCLUDED",
				CostMetrics:   boolPtr(true),  //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				ClusterEvents: boolPtr(true),  //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				EnergyMetrics: boolPtr(false), //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				NodeLogs:      boolPtr(false), //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
			},
			wantStderrContains: []string{
				"SetupK8sDiscovery: ok",
				"flag costMetrics: false → true",
				"flag clusterEvents: false → true",
			},
			wantStdoutContains: []string{"helm upgrade"},
		},

		// (h) org slug is substituted into the access-policies URL in
		//     the human-mode output block.
		{
			name:            "(h) org slug substituted in human-mode output",
			opts:            opts{useDefaults: true},
			orgSlug:         "igor-org",
			current:         instrumentation.Cluster{},
			wantSetupCalled: true,
			wantGetCalled:   true,
			wantSetCalled:   true,
			wantCluster: instrumentation.Cluster{
				Name:          clusterName,
				Selection:     "SELECTION_INCLUDED",
				CostMetrics:   boolPtr(true),  //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				ClusterEvents: boolPtr(true),  //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				EnergyMetrics: boolPtr(false), //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
				NodeLogs:      boolPtr(false), //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
			},
			wantStdoutContains: []string{
				"helm upgrade",
				"https://grafana.com/orgs/igor-org/access-policies",
				"$(cat ~/.grafana-cloud-token)",
			},
		},

		// (i) --print-helm-only with org slug shows URL in stdout
		//     without making any server calls (human mode via --print-helm-only).
		{
			name:    "(i) --print-helm-only with org slug shows URL",
			opts:    opts{printHelmOnly: true},
			orgSlug: "igor-org",
			wantStdoutContains: []string{
				"helm upgrade",
				"https://grafana.com/orgs/igor-org/access-policies",
				"$(cat ~/.grafana-cloud-token)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// TestRun exercises human-mode output paths. Force human mode so the
			// test is deterministic regardless of the CI / agent environment.
			setAgentMode(t, false)

			var stdout, stderr bytes.Buffer

			// For --print-helm-only, no client is needed (structural guarantee).
			var client clientInterface
			if !tt.opts.printHelmOnly {
				client = newFakeClient(tt.current)
			}

			// Determine prompt function.
			pFn := acceptDefaults
			if tt.promptAnswers != nil {
				pFn = prompts(tt.promptAnswers...)
			}

			rn := makeRunner(client, tt.isTTY, pFn)
			rn.stdout = &stdout
			rn.stderr = &stderr
			rn.orgSlug = tt.orgSlug

			err := run(context.Background(), &tt.opts, clusterName, rn)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Verify server call expectations.
			if !tt.opts.printHelmOnly {
				fc, ok := client.(*fakeClient)
				require.True(t, ok, "client must be *fakeClient")
				assert.Equal(t, tt.wantSetupCalled, fc.setupCalled, "SetupK8sDiscovery called")
				assert.Equal(t, tt.wantGetCalled, fc.getCalled, "GetK8SInstrumentation called")
				assert.Equal(t, tt.wantSetCalled, fc.setCalled, "SetK8SInstrumentation called")
				if tt.wantSetCalled {
					assert.Equal(t, tt.wantCluster, fc.setCluster, "SetK8SInstrumentation cluster arg")
				}
			} else {
				// --print-helm-only: client is nil; assert no Set was attempted
				// (run() must short-circuit before accessing client).
				assert.Nil(t, rn.client)
			}

			// Verify stderr content.
			for _, want := range tt.wantStderrContains {
				assert.Contains(t, stderr.String(), want, "stderr should contain %q", want)
			}

			// Verify stdout content.
			for _, want := range tt.wantStdoutContains {
				assert.Contains(t, stdout.String(), want, "stdout should contain %q", want)
			}
		})
	}
}

// setAgentMode sets GCX_AGENT_MODE and re-runs agent detection, then restores
// the previous state via t.Cleanup. This is needed because agent.IsAgentMode()
// caches its result at init() time and requires agent.ResetForTesting() to
// re-read env vars set via t.Setenv.
func setAgentMode(t *testing.T, enabled bool) {
	t.Helper()
	v := "false"
	if enabled {
		v = "1"
	}
	// t.Setenv cleans up the env var after the test; we also need to reset the
	// cached agent mode state.
	t.Setenv("GCX_AGENT_MODE", v)
	agent.ResetForTesting()
	t.Cleanup(func() {
		// agent.ResetForTesting re-reads env; after t.Setenv restores the original
		// value, we must also re-run detection so the cache is consistent.
		agent.ResetForTesting()
	})
}

// TestPrintHelmCommand_OrgSlug verifies access-policies URL behaviour for
// printHelmCommand:
//
//   - Human mode with a known org slug: output contains the substituted URL.
//   - Human mode: output contains the $(cat ...) file-source pattern.
//   - Human mode with empty slug: output falls back to <your-org> placeholder.
//   - Agent mode: stdout contains a single JSON object with helmCommand,
//     accessPoliciesURL (slug substituted), and requiredScopes.
//   - Non-agent mode (regression): stdout contains raw shell text ("helm upgrade").
func TestPrintHelmCommand_OrgSlug(t *testing.T) {
	const helmCmd = "helm upgrade --install grafana-cloud grafana/grafana-cloud-onboarding"

	t.Run("human mode contains substituted URL", func(t *testing.T) {
		// Force human mode: agent.IsAgentMode() is cached at init(); setAgentMode
		// calls agent.ResetForTesting() so the change takes effect immediately.
		setAgentMode(t, false)

		var buf bytes.Buffer
		require.NoError(t, printHelmCommand(&buf, helmCmd, "igor-org"))
		out := buf.String()

		assert.Contains(t, out, "https://grafana.com/orgs/igor-org/access-policies",
			"human mode must include the substituted access-policies URL")
	})

	t.Run("human mode contains file-source pattern", func(t *testing.T) {
		setAgentMode(t, false)

		var buf bytes.Buffer
		require.NoError(t, printHelmCommand(&buf, helmCmd, "igor-org"))
		out := buf.String()

		assert.Contains(t, out, "$(cat ~/.grafana-cloud-token)",
			"human mode must include file-source pattern for token")
	})

	t.Run("human mode empty slug falls back to placeholder", func(t *testing.T) {
		setAgentMode(t, false)

		var buf bytes.Buffer
		require.NoError(t, printHelmCommand(&buf, helmCmd, ""))
		out := buf.String()

		assert.Contains(t, out, "https://grafana.com/orgs/<your-org>/access-policies",
			"empty slug must fall back to <your-org> placeholder in URL")
		assert.NotContains(t, out, "https://grafana.com/orgs//access-policies",
			"empty slug must not produce bare // in URL")
	})

	t.Run("agent mode emits JSON envelope with discriminators and all three fields", func(t *testing.T) {
		setAgentMode(t, true)

		var buf bytes.Buffer
		require.NoError(t, printHelmCommand(&buf, helmCmd, "igor-org"))
		out := strings.TrimSpace(buf.String())

		// Must parse as a single JSON object.
		var env instroutput.SetupHelmEnvelope
		require.NoError(t, json.Unmarshal([]byte(out), &env),
			"agent mode output must be a single valid JSON object")

		assert.Equal(t, instroutput.SetupHelmEnvelopeType, env.Type,
			"type field must carry the collision-resistant discriminator")
		assert.Equal(t, "1", env.SchemaVersion, "schema_version field must be versioned")
		assert.Equal(t, helmCmd, env.HelmCommand, "helmCommand field must be the raw helm command")
		assert.Equal(t, "https://grafana.com/orgs/igor-org/access-policies", env.AccessPoliciesURL,
			"accessPoliciesURL field must contain the substituted URL")
		assert.Equal(t, instroutput.SetupRequiredScopes, env.RequiredScopes,
			"requiredScopes field must match SetupRequiredScopes constant")

		// The discriminators must serialize first so agents can dispatch on a
		// line prefix without parsing the full document.
		assert.True(t, strings.HasPrefix(out, `{"type":"gcx.instrumentation.setup","schema_version":"1",`),
			"discriminators must be the first fields on the line, got: %s", out)
	})

	t.Run("non-agent --print-helm-only regression: raw shell text", func(t *testing.T) {
		// Regression contract: non-agent mode must not
		// emit JSON; stdout must contain the raw helm command text.
		setAgentMode(t, false)

		var buf bytes.Buffer
		require.NoError(t, printHelmCommand(&buf, helmCmd, "igor-org"))
		out := buf.String()

		assert.Contains(t, out, "helm upgrade",
			"non-agent mode must contain raw helm command text")
		// Must not be a JSON object.
		assert.NotEqual(t, '{', []byte(strings.TrimSpace(out))[0],
			"non-agent mode must not emit a JSON object")
	})
}

// TestFlagParsing_NoVariantsDropped verifies the --no-*
// paired flags are removed from setup; only the canonical --feat=true|false
// idiom is accepted.
//
// These tests operate at the flag-parsing layer (pflag.FlagSet) rather than
// through the full Cobra command tree to avoid the fleet.ConfigLoader
// dependency, while still exercising the exact flags declared by opts.setup.
func TestFlagParsing_NoVariantsDropped(t *testing.T) {
	// parseSetupFlags is a helper that builds a fresh FlagSet, registers the
	// setup flags on it, and parses the given args. Returns the populated opts
	// and any parse error.
	parseSetupFlags := func(args []string) (*opts, error) {
		o := &opts{}
		fs := pflag.NewFlagSet("setup-test", pflag.ContinueOnError)
		o.setup(fs)
		err := fs.Parse(args)
		if err == nil {
			// Mirror what Command.RunE does: capture Changed state.
			o.costMetricsSet = fs.Changed("cost-metrics")
			o.clusterEventsSet = fs.Changed("cluster-events")
			o.energyMetricsSet = fs.Changed("energy-metrics")
			o.nodeLogsSet = fs.Changed("node-logs")
		}
		return o, err
	}

	t.Run("--no-cost-metrics is rejected with unknown flag", func(t *testing.T) {
		// AC: gcx instrumentation setup <cluster> --use-defaults --no-cost-metrics
		// must exit non-zero with "unknown flag: --no-cost-metrics".
		_, err := parseSetupFlags([]string{"--use-defaults", "--no-cost-metrics"})
		require.Error(t, err, "parsing --no-cost-metrics should fail")
		assert.True(t,
			strings.Contains(err.Error(), "unknown flag") || strings.Contains(err.Error(), "no-cost-metrics"),
			"error message should reference unknown flag or the flag name, got: %v", err,
		)
	})

	t.Run("--cost-metrics=false sets costMetrics.enabled=false", func(t *testing.T) {
		// AC: gcx instrumentation setup <cluster> --use-defaults --cost-metrics=false
		// must parse successfully and resolve costMetrics.enabled=false (overrides default true).
		o, err := parseSetupFlags([]string{"--use-defaults", "--cost-metrics=false"})
		require.NoError(t, err, "parsing --cost-metrics=false should succeed")

		// Verify the Changed flag was captured.
		assert.True(t, o.costMetricsSet, "costMetricsSet must be true after --cost-metrics=false")
		assert.False(t, o.costMetrics, "costMetrics value must be false")

		// Verify resolveYes honours the explicit override.
		cluster := resolveYes(o)
		require.NotNil(t, cluster.CostMetrics)
		assert.False(t, *cluster.CostMetrics,
			"resolveYes with --cost-metrics=false must produce costMetrics.enabled=false")
	})

	t.Run("--cost-metrics --cost-metrics=false applies last value false (no error)", func(t *testing.T) {
		// AC: --cost-metrics --cost-metrics=false → Cobra applies last value (false);
		// no mutually-exclusive error (the --no-* form is gone).
		o, err := parseSetupFlags([]string{"--use-defaults", "--cost-metrics", "--cost-metrics=false"})
		require.NoError(t, err, "parsing --cost-metrics --cost-metrics=false should succeed")

		// Validate must not return an error (no paired-flag validation any more).
		require.NoError(t, o.Validate())

		// pflag applies the last value; assert it is false.
		assert.True(t, o.costMetricsSet, "costMetricsSet must be true")
		assert.False(t, o.costMetrics, "last value wins: costMetrics should be false")

		cluster := resolveYes(o)
		require.NotNil(t, cluster.CostMetrics)
		assert.False(t, *cluster.CostMetrics,
			"resolveYes should honour the last-wins false value")
	})
}
