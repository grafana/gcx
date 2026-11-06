// opmodel_commands.go; the `gcx irm opmodel` command group: the shared
// --timeout persistent flag, the actor flag set every mutation requires, and
// the shared runners the per-noun leaf files drive.
//
// All commands are experimental: the backend is the op model prototype in
// grafana/irm, whose HTTP API is dev-gated behind -api.opmodel-enabled.
package irm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	fail "github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// newOpModelCmd mounts the opmodel noun groups under `gcx irm opmodel`.
func newOpModelCmd(loader OpModelClientLoader) *cobra.Command {
	shared := &opModelSharedOpts{}
	cmd := &cobra.Command{
		Use:    "opmodel",
		Hidden: true,
		Short:  "Work the incident operational model (experimental prototype).",
		Long: `Work the incident operational model: what a response team notices
(observations), claims (impacts and hypotheses), does (actions), and how
observations inform claims (evidence links).

Everything lives in a container; an alert-group id or incident id; as one
append-only event stream; nothing is ever deleted. Anyone (human or agent)
may create and relate entities. Assessment and recovery are independent human
judgments; agent corroboration does not replace human verification.

Writes apply to current server state without revision preconditions. Later
valid writes to the same field can replace earlier values; history is retained.
After an ambiguous failure, inspect state before retrying a mutation.

Experimental: talks to the op model prototype through the authenticated IRM
plugin resource proxy. The target and tenancy come from the active Grafana
context.`,
	}
	shared.bind(cmd.PersistentFlags())

	cmd.AddCommand(newOpModelContainersCmd(loader, shared))
	return markExperimental(cmd)
}

// opModelSharedOpts carries the persistent flags shared by every opmodel and
// shares leaf. The active Grafana context owns the target and tenancy.
type opModelSharedOpts struct {
	Timeout time.Duration
}

func (o *opModelSharedOpts) bind(flags *pflag.FlagSet) {
	flags.DurationVar(&o.Timeout, "timeout", defaultOpModelTimeout, "Deadline for each individual request (e.g. 5s, 30s). shares publish issues one request per entity, each with its own deadline")
}

func (o *opModelSharedOpts) validate() error {
	if o.Timeout <= 0 {
		exit2 := fail.ExitUsageError
		return &fail.DetailedError{
			Summary:     fmt.Sprintf("invalid --timeout %s", o.Timeout),
			Details:     "The per-request deadline must be greater than zero.",
			ExitCode:    &exit2,
			Suggestions: []string{"Re-run with --timeout 5s"},
		}
	}
	return nil
}

// opModelActorOpts is the actor flag set every mutation requires. Reads take
// no actor flags: reading judges nothing and mints nothing.
type opModelActorOpts struct {
	Kind string
	ID   string
	Name string
}

func (o *opModelActorOpts) bind(flags *pflag.FlagSet) {
	flags.StringVar(&o.Kind, "actor-kind", "", "Who performs this mutation: human or agent (required). The server enforces which gestures need a human")
	flags.StringVar(&o.ID, "actor-id", "", "The actor's stable identifier; a user id or an agent name (required)")
	flags.StringVar(&o.Name, "actor-name", "", "Optional human-readable display name for the actor")
}

func (o *opModelActorOpts) validate() error {
	if err := requireOpModelFlag("actor-kind", o.Kind); err != nil {
		return err
	}
	if err := validateEnumFlag("actor-kind", o.Kind, actorKindVocab); err != nil {
		return err
	}
	return requireOpModelFlag("actor-id", o.ID)
}

// actor renders the flag set as the API's actor body.
func (o *opModelActorOpts) actor() opModelActorInput {
	return opModelActorInput{Kind: o.Kind, ID: o.ID, DisplayName: o.Name}
}

// requireOpModelFlag rejects a missing, explicitly-empty, or whitespace-only
// required flag value with a structured usage error.
func requireOpModelFlag(name, value string) error {
	if strings.TrimSpace(value) != "" {
		return nil
	}
	exit2 := fail.ExitUsageError
	return &fail.DetailedError{
		Summary:     fmt.Sprintf("--%s is required", name),
		Details:     fmt.Sprintf("The --%s flag must carry a non-empty value.", name),
		ExitCode:    &exit2,
		Suggestions: []string{fmt.Sprintf("Re-run with --%s <value>", name)},
	}
}

// requireOpModelArg rejects a blank positional argument with a structured
// usage error naming the argument.
func requireOpModelArg(name, value string) error {
	if strings.TrimSpace(value) != "" {
		return nil
	}
	exit2 := fail.ExitUsageError
	return &fail.DetailedError{
		Summary:     fmt.Sprintf("<%s> must not be empty", name),
		Details:     fmt.Sprintf("The positional <%s> argument carried an empty or whitespace-only value.", name),
		ExitCode:    &exit2,
		Suggestions: []string{fmt.Sprintf("Re-run with a real <%s>", name)},
	}
}

// setupOpModelMutationIO wires the codec system for mutation leaves: the
// one-line text codec is the TTY default, agents codec takes over in agent
// mode (inside BindFlags), json/yaml stay available for scripts.
func setupOpModelMutationIO(io *cmdio.Options, flags *pflag.FlagSet) {
	io.RegisterCustomCodec("text", &opModelMutationTextCodec{})
	io.RegisterCustomCodec("yaml", format.NewOrderedYAMLCodec())
	io.DefaultFormat("text")
	io.BindFlags(flags)
}

// setupOpModelReadIO wires the codec system for read leaves: JSON default
// (the envelopes are typed documents), yaml available, agents codec in agent
// mode.
func setupOpModelReadIO(io *cmdio.Options, flags *pflag.FlagSet) {
	io.RegisterCustomCodec("yaml", format.NewOrderedYAMLCodec())
	io.BindFlags(flags)
}

// withOpModel loads the configured client and runs fn. Construction performs
// no network I/O, so an unreachable backend fails on the first request, not
// here. The --timeout deadline is applied per request by the client itself,
// not wrapped around fn: a multi-request verb must not fail partway through
// because the whole sequence shared one budget.
func withOpModel(cmd *cobra.Command, loader OpModelClientLoader, shared *opModelSharedOpts, fn func(ctx context.Context, api OpModelAPI) error) error {
	api, err := loader.LoadOpModel(cmd.Context(), shared.Timeout)
	if err != nil {
		return err
	}
	return fn(cmd.Context(), api)
}

// opModelValidatable is the static-validation surface every opmodel opts
// struct exposes.
type opModelValidatable interface{ Validate() error }

// validateOpModelInputs runs the shared static-validation prelude for
// flags-only mutation verbs: shared flags first, then the verb's own opts.
func validateOpModelInputs(shared *opModelSharedOpts, opts opModelValidatable) error {
	if err := shared.validate(); err != nil {
		return err
	}
	return opts.Validate()
}

// validateOpModelIDInputs is the prelude for id-targeted verbs: shared
// flags, then the positional id, then the verb's own opts.
func validateOpModelIDInputs(shared *opModelSharedOpts, argName, id string, opts opModelValidatable) error {
	if err := shared.validate(); err != nil {
		return err
	}
	if err := requireOpModelArg(argName, id); err != nil {
		return err
	}
	return opts.Validate()
}

// runOpModelMutation is the shared mutation path: load clients, run the
// verb-specific call, emit the shared mutation envelope on stdout.
func runOpModelMutation(cmd *cobra.Command, loader OpModelClientLoader, shared *opModelSharedOpts, io *cmdio.Options, fn func(ctx context.Context, api OpModelAPI) (opModelMutation, error)) error {
	return withOpModel(cmd, loader, shared, func(ctx context.Context, api OpModelAPI) error {
		m, err := fn(ctx, api)
		if err != nil {
			return err
		}
		return io.Encode(cmd.OutOrStdout(), m)
	})
}

// markExperimental marks a prototype command and its children in help and
// agent metadata. Token cost lives in the central commandAnnotations registry.
func markExperimental(cmd *cobra.Command) *cobra.Command {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[agent.AnnotationStability] = agent.StabilityExperimental
	if !strings.HasPrefix(cmd.Short, "[experimental]") {
		cmd.Short = "[experimental] " + cmd.Short
		cmd.Long = "This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.\n\n" + cmd.Long
	}
	for _, child := range cmd.Commands() {
		markExperimental(child)
	}
	return cmd
}
