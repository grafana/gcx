// Package setup implements the "gcx instrumentation setup <cluster>"
// onboarding command.
package setup

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	instrumentation "github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/providers/instrumentation/helm"
	instroutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
)

// clientInterface abstracts the three instrumentation API calls made by setup.
// The concrete implementation is *instrumentation.Client; tests inject a fake.
type clientInterface interface {
	SetupK8sDiscovery(ctx context.Context, urls instrumentation.BackendURLs, promHeaders instrumentation.PromHeaders) error
	GetK8SInstrumentation(ctx context.Context, clusterName string) (*instrumentation.GetK8SInstrumentationResponse, error)
	SetK8SInstrumentation(ctx context.Context, clusterName string, k8s instrumentation.Cluster, urls instrumentation.BackendURLs) error
}

// runner holds all injected dependencies for the setup orchestration.
// Explicit fields (rather than closure captures) make dependencies visible and
// allow table-driven tests to vary individual components.
type runner struct {
	// client is nil when --print-helm-only is set (structural no-call guarantee).
	client   clientInterface
	urls     instrumentation.BackendURLs
	fm       instrumentation.FleetManagement // used by helm.Format
	promHdrs instrumentation.PromHeaders
	// token is substituted into the helm command as the access-policy password.
	token string
	// orgSlug is the Grafana Cloud org slug used to build the access-policies URL
	// in human-mode output and the agent-mode JSON envelope.
	// When empty the literal placeholder <your-org> is substituted (fallback).
	orgSlug string
	stdout  io.Writer
	stderr  io.Writer
	// isTTY reports whether stdin is an interactive terminal. When true and
	// --use-defaults is not set, setup prompts for each K8s flag interactively.
	isTTY bool
	// promptFn is called once per K8s flag in interactive mode. name is the
	// human-readable flag name (e.g. "costMetrics"); current is its declared
	// value. Returns the desired value. Injected to enable test doubles.
	promptFn func(name string, current bool) (bool, error)
}

// run executes the setup workflow. All dependencies are injected through r so
// the function is fully testable without an HTTP server.
//
// Flow:
//  1. --print-helm-only: print helm command and return immediately; no server
//     calls are made (structural guarantee).
//  2. SetupK8sDiscovery (idempotent server-side).
//  3. GetK8SInstrumentation to capture current declared state.
//  4. Resolve desired flag values: interactive prompts when TTY && !--use-defaults,
//     or defaults + per-flag overrides under --use-defaults.
//  5. SetK8SInstrumentation only when at least one flag differs.
//  6. Emit mutation summary to stderr.
//  7. Print helm command to stdout.
func run(ctx context.Context, o *opts, cluster string, r *runner) error {
	// Step 1: --print-helm-only short-circuit.
	// client is nil on this path — structural guarantee no server calls occur.
	// Under agent mode, the helm command is wrapped in a JSON envelope.
	if o.printHelmOnly {
		return printHelmCommand(r.stdout, helm.Format(cluster, r.fm, r.token), r.orgSlug)
	}

	// Step 2: SetupK8sDiscovery is idempotent — always call.
	if err := r.client.SetupK8sDiscovery(ctx, r.urls, r.promHdrs); err != nil {
		return fmt.Errorf("setup: %w", err)
	}
	fmt.Fprintln(r.stderr, "SetupK8sDiscovery: ok")

	// Step 3: Capture current declared K8s configuration.
	resp, err := r.client.GetK8SInstrumentation(ctx, cluster)
	if err != nil {
		return fmt.Errorf("setup: %w", err)
	}
	current := resp.Cluster

	// Step 4: Resolve desired flag values.
	desired, err := resolveDesired(o, r, current)
	if err != nil {
		return err
	}
	desired.Name = cluster
	desired.Selection = "SELECTION_INCLUDED"

	// Step 5+6: Compare, conditionally write, and emit summary.
	anyChanged := emitFlagSummary(r.stderr, current, desired)
	if anyChanged {
		if err := r.client.SetK8SInstrumentation(ctx, cluster, desired, r.urls); err != nil {
			return fmt.Errorf("setup: %w", err)
		}
	} else {
		fmt.Fprintln(r.stderr, "no changes")
	}

	// Step 7: Print helm command to stdout.
	return printHelmCommand(r.stdout, helm.Format(cluster, r.fm, r.token), r.orgSlug)
}

// printHelmCommand writes the helm command to w, enriched with access-policies
// guidance.
//
// In agent mode, emits a single JSON object with helmCommand, accessPoliciesURL,
// and requiredScopes.
//
// In human mode, emits the raw helm command followed by the required-scopes
// list and the access-policies URL. The recommended file-source pattern is also
// appended so users avoid placing the token in shell history.
//
// orgSlug is the Grafana Cloud org slug used to build the URL. When empty,
// the literal placeholder <your-org> is substituted (fallback spec).
func printHelmCommand(w io.Writer, cmd string, orgSlug string) error {
	policyURL := instroutput.AccessPoliciesURL(orgSlug)

	if agent.IsAgentMode() {
		env := instroutput.SetupHelmEnvelope{
			HelmCommand:       cmd,
			AccessPoliciesURL: policyURL,
			RequiredScopes:    instroutput.SetupRequiredScopes,
		}
		data, err := json.Marshal(env)
		if err != nil {
			return fmt.Errorf("setup: marshal helm command: %w", err)
		}
		_, err = fmt.Fprintln(w, string(data))
		return err
	}

	// Human mode: emit the helm command, then the access-policies guidance block.
	if _, err := fmt.Fprintln(w, cmd); err != nil {
		return err
	}

	// Access policy guidance block.
	fmt.Fprintln(w, "\nYou will need a Cloud Access Policy token with these scopes:")
	for _, scope := range instroutput.SetupRequiredScopes {
		fmt.Fprintf(w, "  - %s\n", scope)
	}
	fmt.Fprintf(w, "\nCreate the policy at %s\n", policyURL)

	// Recommended file-source pattern to avoid token in shell history.
	fmt.Fprintf(w, "\nRecommended: store your token in ~/.grafana-cloud-token (chmod 600) then re-run with:\n")
	fmt.Fprintf(w, "  --set grafanaCloud.fleetManagement.auth.password=$(cat ~/.grafana-cloud-token)\n")
	return nil
}

// resolveDesired determines desired Cluster flag values from opts and (when
// interactive) from user prompts via r.promptFn.
func resolveDesired(o *opts, r *runner, current instrumentation.Cluster) (instrumentation.Cluster, error) {
	switch {
	case o.useDefaults:
		return resolveYes(o), nil
	case r.isTTY:
		return resolveInteractive(r, current)
	default:
		return instrumentation.Cluster{}, errors.New(
			"setup: stdin is not a TTY; use --use-defaults for non-interactive mode")
	}
}

// resolveYes applies defaults with per-flag override precedence.
//
// The cluster's PRIOR state is intentionally NOT consulted — setup is
// "apply this configuration", not RMW-preserve like clusters enable/disable.
// Idempotence is achieved when the prior state already matches the
// resolved set, not by incorporating prior state into the resolution.
//
// Per-flag overrides use the *Set fields (populated by cmd.Flags().Changed in
// Command) to distinguish "flag explicitly provided" from "flag not mentioned".
// This is required because pflag cannot distinguish --flag=false from an absent
// flag on plain bool fields — Changed() is the only reliable test.
func resolveYes(o *opts) instrumentation.Cluster {
	// Recommended defaults.
	cost, events, energy, logs := true, true, false, false

	// Per-flag overrides take precedence over defaults.
	// Use *Set fields (not raw value) to detect explicit --feat=false.
	if o.costMetricsSet {
		cost = o.costMetrics
	}
	if o.clusterEventsSet {
		events = o.clusterEvents
	}
	if o.energyMetricsSet {
		energy = o.energyMetrics
	}
	if o.nodeLogsSet {
		logs = o.nodeLogs
	}

	return instrumentation.Cluster{
		CostMetrics:   boolPtr(cost),   //nolint:modernize // boolPtr allocates and sets; new() only allocates.
		ClusterEvents: boolPtr(events), //nolint:modernize
		EnergyMetrics: boolPtr(energy), //nolint:modernize
		NodeLogs:      boolPtr(logs),   //nolint:modernize
	}
}

// resolveInteractive calls r.promptFn for each K8s flag with context-sensitive
// defaults:
//   - Unconfigured cluster (current.Name == ""): defaults to recommended
//     values (cost=true, events=true, energy=false, logs=false).
//   - Configured cluster: defaults to the cluster's current declared values.
func resolveInteractive(r *runner, current instrumentation.Cluster) (instrumentation.Cluster, error) {
	// choose prompt defaults based on configured vs unconfigured state.
	// An empty Name in the wire response signals a cluster that has never been Set.
	defCost, defEvents, defEnergy, defLogs := interactiveDefaults(current)

	cost, err := r.promptFn("costMetrics", defCost)
	if err != nil {
		return instrumentation.Cluster{}, fmt.Errorf("setup: prompt costMetrics: %w", err)
	}
	events, err := r.promptFn("clusterEvents", defEvents)
	if err != nil {
		return instrumentation.Cluster{}, fmt.Errorf("setup: prompt clusterEvents: %w", err)
	}
	energy, err := r.promptFn("energyMetrics", defEnergy)
	if err != nil {
		return instrumentation.Cluster{}, fmt.Errorf("setup: prompt energyMetrics: %w", err)
	}
	logs, err := r.promptFn("nodeLogs", defLogs)
	if err != nil {
		return instrumentation.Cluster{}, fmt.Errorf("setup: prompt nodeLogs: %w", err)
	}
	return instrumentation.Cluster{
		CostMetrics:   boolPtr(cost),   //nolint:modernize // boolPtr allocates and sets; new() only allocates.
		ClusterEvents: boolPtr(events), //nolint:modernize
		EnergyMetrics: boolPtr(energy), //nolint:modernize
		NodeLogs:      boolPtr(logs),   //nolint:modernize
	}, nil
}

// interactiveDefaults returns the prompt default values for interactive mode.
// For an unconfigured cluster (Name==""), the recommended set is used.
// For a configured cluster, the current declared values are the defaults.
// Return order: cost, events, energy, logs.
func interactiveDefaults(current instrumentation.Cluster) (bool, bool, bool, bool) {
	if current.Name == "" {
		// Unconfigured: default to recommended values.
		return true, true, false, false
	}
	return derefBool(current.CostMetrics),
		derefBool(current.ClusterEvents),
		derefBool(current.EnergyMetrics),
		derefBool(current.NodeLogs)
}

// emitFlagSummary writes one diff line per flag that changed between current
// and desired, and returns true when at least one flag changed. Iteration order
// is stable: costMetrics, clusterEvents, energyMetrics, nodeLogs.
func emitFlagSummary(w io.Writer, current, desired instrumentation.Cluster) bool {
	type flagSpec struct {
		name string
		cur  *bool
		des  *bool
	}
	specs := []flagSpec{
		{"costMetrics", current.CostMetrics, desired.CostMetrics},
		{"clusterEvents", current.ClusterEvents, desired.ClusterEvents},
		{"energyMetrics", current.EnergyMetrics, desired.EnergyMetrics},
		{"nodeLogs", current.NodeLogs, desired.NodeLogs},
	}
	changed := false
	for _, s := range specs {
		if derefBool(s.cur) != derefBool(s.des) {
			fmt.Fprintf(w, "flag %s: %v → %v\n", s.name, derefBool(s.cur), derefBool(s.des))
			changed = true
		}
	}
	return changed
}

// defaultPromptFn returns a promptFn that reads y/n answers line-by-line from
// in and writes questions to errOut. Used by Command() for real interactive
// terminal sessions. Tests inject their own promptFn instead.
func defaultPromptFn(in io.Reader, errOut io.Writer) func(name string, current bool) (bool, error) {
	scanner := bufio.NewScanner(in)
	return func(name string, current bool) (bool, error) {
		def := "n"
		if current {
			def = "y"
		}
		fmt.Fprintf(errOut, "%s [y/n, default %s]: ", name, def)
		if !scanner.Scan() {
			// EOF or error → use current value as-is.
			return current, scanner.Err()
		}
		switch strings.ToLower(strings.TrimSpace(scanner.Text())) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			// Unrecognized input → keep current value.
			return current, nil
		}
	}
}

// boolPtr returns a pointer to b.
//
//nolint:modernize // boolPtr allocates and sets the value; new(bool) only allocates (zero value).
func boolPtr(b bool) *bool { return &b }

// derefBool dereferences b, returning false for nil.
func derefBool(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}
