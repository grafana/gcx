package apps

import (
	"context"
	"errors"
	"fmt"
	"strings"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instoutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/grafana/gcx/internal/providers/instrumentation/rmw"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type configureOpts struct {
	IO              cmdio.Options
	useDefaults     bool
	yes             bool
	tracing         bool
	logging         bool
	processMetrics  bool
	extendedMetrics bool
	profiling       bool
}

func (o *configureOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.useDefaults, "use-defaults", false, "Apply all-on canonical defaults. Requires --yes.")
	flags.BoolVar(&o.yes, "yes", false, "Confirm the --use-defaults operation (required with --use-defaults)")
	flags.BoolVar(&o.tracing, "tracing", false, "Set distributed tracing collection. Pass --tracing=false to disable.")
	flags.BoolVar(&o.logging, "logging", false, "Set log collection. Pass --logging=false to disable.")
	flags.BoolVar(&o.processMetrics, "process-metrics", false, "Set process-level metrics collection. Pass --process-metrics=false to disable.")
	flags.BoolVar(&o.extendedMetrics, "extended-metrics", false, "Set extended Beyla metrics collection. Pass --extended-metrics=false to disable.")
	flags.BoolVar(&o.profiling, "profiling", false, "Set continuous profiling collection. Pass --profiling=false to disable.")
	// The configure result is a MutationResult document through the codec
	// system: the default text codec prints the familiar one-liner; agent
	// mode and explicit -o json/yaml get the structured document.
	instoutput.BindMutationIO(&o.IO, flags)
}

// Validate of the mutually-exclusive-mode and --yes-required-for-defaults rules
// requires inspecting flags.Changed(), which depends on the *cobra.Command
// instance. Those checks live in the RunE body where the command is in scope.
func (o *configureOpts) Validate() error { return o.IO.Validate() }

// makeConfigureCmd builds the "apps configure <cluster> <namespace>" command.
//
// Two mutually exclusive modes:
//
//  1. --use-defaults --yes: overwrites the namespace entry with all-on defaults
//     (Autoinstrument=true, all signals true).
//
//  2. One or more --<feat> flags: RMW update — only the explicitly-set flags
//     are changed; unspecified flags preserve their current value.
//
// factory is called inside RunE — after cobra has parsed all flags — to
// lazily construct the appsClient and BackendURLs.
func makeConfigureCmd(factory appClientFactory) *cobra.Command {
	opts := &configureOpts{}

	cmd := &cobra.Command{
		Use:   "configure <cluster> <namespace>",
		Short: "Configure Beyla instrumentation for a namespace",
		Long: `Configure Beyla auto-instrumentation for a namespace within the given cluster.

Two mutually exclusive modes:

  --use-defaults --yes
      Apply all-on canonical defaults, overwriting current state. Requires --yes.
      Defaults: autoinstrument=true, all signals enabled.

  --<feat>[=true|false] (one or more)
      Set listed features; unspecified features preserve their current value (RMW).
      Idempotent. No confirmation required.

Combining --use-defaults with any --<feat> flag is an error.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			client, urls, promHeaders, err := factory(ctx)
			if err != nil {
				return err
			}
			cluster := args[0]
			namespace := args[1]

			flags := cmd.Flags()
			useDefaultsSet := flags.Changed("use-defaults")
			anyFeatureSet := flags.Changed("tracing") || flags.Changed("logging") ||
				flags.Changed("process-metrics") || flags.Changed("extended-metrics") ||
				flags.Changed("profiling")

			if useDefaultsSet && anyFeatureSet {
				return errors.New("apps configure: --use-defaults and feature flags are mutually exclusive")
			}
			if !useDefaultsSet && !anyFeatureSet {
				return errors.New("apps configure: requires either --use-defaults --yes or one or more --<feat> flags")
			}

			// --use-defaults mode: overwrite with all-on defaults, requires --yes.
			if useDefaultsSet {
				if !opts.yes {
					return errors.New("apps configure --use-defaults: --yes is required to confirm this operation")
				}
				resp, err := client.GetAppInstrumentation(ctx, cluster)
				if err != nil {
					return err
				}
				defaults := defaultAppConfig(namespace)
				updated := replaceOrAppendNamespace(resp.Namespaces, namespace, defaults)
				if equal, _ := appListEqual(resp.Namespaces, updated); equal {
					// No write — discover post-state before emitting result.
					return emitConfigureResult(ctx, cmd, opts, client, promHeaders, cluster, namespace, false, false)
				}
				if err := client.SetAppInstrumentation(ctx, cluster, updated, urls); err != nil {
					return err
				}
				// Discover post-write state.
				return emitConfigureResult(ctx, cmd, opts, client, promHeaders, cluster, namespace, true, true)
			}

			// RMW mode: set listed flags, preserve unspecified.

			// Pre-check: compute resolved post-state and skip write if no-op.
			preResp, err := client.GetAppInstrumentation(ctx, cluster)
			if err != nil {
				return err
			}
			pre := preResp.Namespaces
			post := applyAppMutations(pre, namespace, flags,
				opts.tracing, opts.logging, opts.processMetrics, opts.extendedMetrics, opts.profiling)
			if equal, _ := appListEqual(pre, post); equal {
				return emitConfigureResult(ctx, cmd, opts, client, promHeaders, cluster, namespace, false, false)
			}

			getFn := func(ctx context.Context) ([]instrumentation.App, error) {
				resp, err := client.GetAppInstrumentation(ctx, cluster)
				if err != nil {
					return nil, err
				}
				return resp.Namespaces, nil
			}
			mutateFn := func(namespaces []instrumentation.App) []instrumentation.App {
				return applyAppMutations(namespaces, namespace, flags,
					opts.tracing, opts.logging, opts.processMetrics, opts.extendedMetrics, opts.profiling)
			}
			setFn := func(ctx context.Context, namespaces []instrumentation.App) error {
				return client.SetAppInstrumentation(ctx, cluster, namespaces, urls)
			}

			err = rmw.Update(ctx, getFn, mutateFn, setFn, appListEqual, 2)
			if err != nil {
				var ce rmw.ConflictError
				if errors.As(err, &ce) {
					ce.Command = "apps configure"
					ce.Namespace = namespace
					return ce
				}
				return err
			}
			// Discover post-write state.
			return emitConfigureResult(ctx, cmd, opts, client, promHeaders, cluster, namespace, true, true)
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

// emitConfigureResult probes namespace discovery, then writes the
// MutationResult document to stdout through the codec system.
//
// postWrite selects the contract for a discovery-probe failure:
//   - pre-write paths (no mutation was applied) return the probe error — the
//     error document is the honest terminal result;
//   - post-write paths (the mutation is already applied) still emit the result
//     document, just without the Discovered enrichment, and route the probe
//     failure to stderr as a warning. An applied mutation must never be
//     reported as a bare failure with no result on stdout.
func emitConfigureResult(
	ctx context.Context,
	cmd *cobra.Command,
	opts *configureOpts,
	client appsClient,
	promHeaders instrumentation.PromHeaders,
	cluster, namespace string,
	changed, postWrite bool,
) error {
	result := instoutput.NewMutationResult("configure", instoutput.Target{Cluster: cluster, Namespace: namespace})
	result.Changed = changed

	disc, discErr := client.IsNamespaceDiscovered(ctx, promHeaders, cluster, namespace)
	switch {
	case discErr == nil:
		result.Discovered = boolPtr(disc) //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
	case !postWrite:
		return fmt.Errorf("apps configure: %w", discErr)
	default:
		cmdio.EmitWarn(cmd.ErrOrStderr(),
			fmt.Sprintf("apps configure: configuration applied, but the discovery probe failed: %v", discErr))
	}

	return opts.IO.Encode(cmd.OutOrStdout(), result)
}

// newConfigureCmd is a test-facing constructor that injects a pre-built
// appsClient (with zero-value BackendURLs). Production code uses
// makeConfigureCmd(factoryFromLoader(loader)) instead.
func newConfigureCmd(client appsClient) *cobra.Command {
	return makeConfigureCmd(func(_ context.Context) (appsClient, instrumentation.BackendURLs, instrumentation.PromHeaders, error) {
		return client, instrumentation.BackendURLs{}, instrumentation.PromHeaders{}, nil
	})
}

// defaultAppConfig returns all-on canonical defaults for --use-defaults mode.
func defaultAppConfig(name string) instrumentation.App {
	t := true
	return instrumentation.App{
		Name:            name,
		Autoinstrument:  &t,
		Tracing:         &t,
		Logging:         &t,
		ProcessMetrics:  &t,
		ExtendedMetrics: &t,
		Profiling:       &t,
	}
}

// replaceOrAppendNamespace replaces the entry with the given name in namespaces,
// or appends it if not found. Returns a new slice; does not modify the input.
func replaceOrAppendNamespace(namespaces []instrumentation.App, name string, entry instrumentation.App) []instrumentation.App {
	result := make([]instrumentation.App, 0, len(namespaces)+1)
	found := false
	for _, ns := range namespaces {
		if ns.Name == name {
			result = append(result, entry)
			found = true
		} else {
			result = append(result, ns)
		}
	}
	if !found {
		result = append(result, entry)
	}
	return result
}

// applyAppMutations applies Autoinstrument=true and the given flag mutations
// to the target namespace within the full list. If the namespace is not present, a
// new entry is inserted. Other namespace entries are returned unchanged.
//
// Only flags that have been explicitly set (cobra flags.Changed) are applied.
func applyAppMutations(
	namespaces []instrumentation.App,
	target string,
	flags interface{ Changed(name string) bool },
	tracing, logging, processMetrics, extendedMetrics, profiling bool,
) []instrumentation.App {
	result := make([]instrumentation.App, 0, len(namespaces)+1)
	found := false

	for _, ns := range namespaces {
		if ns.Name != target {
			result = append(result, ns)
			continue
		}
		found = true
		result = append(result, applyMutationsToNS(ns, flags, tracing, logging, processMetrics, extendedMetrics, profiling))
	}

	if !found {
		result = append(result, newNamespaceEntry(target, flags, tracing, logging, processMetrics, extendedMetrics, profiling))
	}

	return result
}

// applyMutationsToNS applies Autoinstrument=true and flag mutations to
// an existing namespace entry. Returns a new copy; does not modify the input.
func applyMutationsToNS(
	ns instrumentation.App,
	flags interface{ Changed(name string) bool },
	tracing, logging, processMetrics, extendedMetrics, profiling bool,
) instrumentation.App {
	t := true
	ns.Autoinstrument = &t
	if flags.Changed("tracing") {
		ns.Tracing = boolPtr(tracing) //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
	}
	if flags.Changed("logging") {
		ns.Logging = boolPtr(logging) //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
	}
	if flags.Changed("process-metrics") {
		ns.ProcessMetrics = boolPtr(processMetrics) //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
	}
	if flags.Changed("extended-metrics") {
		ns.ExtendedMetrics = boolPtr(extendedMetrics) //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
	}
	if flags.Changed("profiling") {
		ns.Profiling = boolPtr(profiling) //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
	}
	return ns
}

// newNamespaceEntry builds a fresh namespace entry with Autoinstrument=true and
// the given flag mutations applied. Used when the namespace is absent
// from the current configuration.
func newNamespaceEntry(
	name string,
	flags interface{ Changed(name string) bool },
	tracing, logging, processMetrics, extendedMetrics, profiling bool,
) instrumentation.App {
	t := true
	ns := instrumentation.App{
		Name:           name,
		Autoinstrument: &t,
	}
	if flags.Changed("tracing") {
		ns.Tracing = boolPtr(tracing) //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
	}
	if flags.Changed("logging") {
		ns.Logging = boolPtr(logging) //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
	}
	if flags.Changed("process-metrics") {
		ns.ProcessMetrics = boolPtr(processMetrics) //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
	}
	if flags.Changed("extended-metrics") {
		ns.ExtendedMetrics = boolPtr(extendedMetrics) //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
	}
	if flags.Changed("profiling") {
		ns.Profiling = boolPtr(profiling) //nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
	}
	return ns
}

// boolPtr returns a pointer to the given bool value.
//
//nolint:modernize // boolPtr(x) cannot simplify to new(x) — new(T) creates *zero-value, not *x
func boolPtr(b bool) *bool { return &b }

// appListEqual compares two namespace lists for equality. Comparison is by
// namespace name key; ordering differences within each namespace's Apps[] are
// handled by rmw.AppEqual. Returns (true, "") when equal; (false, diff) when not.
func appListEqual(a, b []instrumentation.App) (bool, string) {
	// Build maps by name for order-independent comparison.
	aMap := make(map[string]instrumentation.App, len(a))
	for _, ns := range a {
		aMap[ns.Name] = ns
	}
	bMap := make(map[string]instrumentation.App, len(b))
	for _, ns := range b {
		bMap[ns.Name] = ns
	}

	var diffs []string

	// Check for removed or changed entries (in a, absent or changed in b).
	for name, aNS := range aMap {
		bNS, ok := bMap[name]
		if !ok {
			diffs = append(diffs, "removed namespace: "+name)
			continue
		}
		if equal, d := rmw.AppEqual(aNS, bNS); !equal {
			diffs = append(diffs, d)
		}
	}

	// Check for added entries (in b, absent in a).
	for name := range bMap {
		if _, ok := aMap[name]; !ok {
			diffs = append(diffs, "added namespace: "+name)
		}
	}

	if len(diffs) == 0 {
		return true, ""
	}
	return false, strings.Join(diffs, "; ")
}
