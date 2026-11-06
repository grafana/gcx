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

type opModelSharedOpts struct {
	Timeout time.Duration
}

func (o *opModelSharedOpts) bind(flags *pflag.FlagSet) {
	flags.DurationVar(&o.Timeout, "timeout", defaultOpModelTimeout, "Deadline for each individual request (e.g. 5s, 30s)")
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

func (o *opModelActorOpts) actor() opModelActorInput {
	return opModelActorInput{Kind: o.Kind, ID: o.ID, DisplayName: o.Name}
}

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

func setupOpModelMutationIO(io *cmdio.Options, flags *pflag.FlagSet) {
	io.RegisterCustomCodec("text", &opModelMutationTextCodec{})
	io.RegisterCustomCodec("yaml", format.NewOrderedYAMLCodec())
	io.DefaultFormat("text")
	io.BindFlags(flags)
}

func setupOpModelReadIO(io *cmdio.Options, flags *pflag.FlagSet) {
	io.RegisterCustomCodec("yaml", format.NewOrderedYAMLCodec())
	io.BindFlags(flags)
}

func withOpModel(cmd *cobra.Command, loader OpModelClientLoader, shared *opModelSharedOpts, fn func(ctx context.Context, api OpModelAPI) error) error {
	api, err := loader.LoadOpModel(cmd.Context(), shared.Timeout)
	if err != nil {
		return err
	}
	return fn(cmd.Context(), api)
}

type opModelValidatable interface{ Validate() error }

func validateOpModelInputs(shared *opModelSharedOpts, opts opModelValidatable) error {
	if err := shared.validate(); err != nil {
		return err
	}
	return opts.Validate()
}

func validateOpModelIDInputs(shared *opModelSharedOpts, argName, id string, opts opModelValidatable) error {
	if err := shared.validate(); err != nil {
		return err
	}
	if err := requireOpModelArg(argName, id); err != nil {
		return err
	}
	return opts.Validate()
}

func runOpModelMutation(cmd *cobra.Command, loader OpModelClientLoader, shared *opModelSharedOpts, io *cmdio.Options, fn func(ctx context.Context, api OpModelAPI) (opModelMutation, error)) error {
	return withOpModel(cmd, loader, shared, func(ctx context.Context, api OpModelAPI) error {
		m, err := fn(ctx, api)
		if err != nil {
			return err
		}
		return io.Encode(cmd.OutOrStdout(), m)
	})
}

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
