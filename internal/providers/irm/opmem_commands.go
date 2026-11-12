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

func newOpMemCmd(loader OpMemClientLoader) *cobra.Command {
	shared := &opMemSharedOpts{}
	cmd := &cobra.Command{
		Use:   "opmem",
		Short: "Work the incident operational model.",
		Long: `opmem means operational memory. It records the incident operational model:
what a response team notices (observations), claims (impacts and hypotheses),
does (actions), and how observations inform claims (evidence links).

Everything lives in a container identified by an alert-group id or incident id,
as one append-only event stream. Nothing is ever deleted. You can read a
container's current state, read its raw events, and save a model draft into it.

Requests run as the signed-in Grafana user. The --actor flags on upsert-model
record who drafted the content, for example the agent working for that user.
The server stores them as given and does not check them against the login.

Writes apply to current server state without revision preconditions. After an
ambiguous failure, inspect state before retrying a write.

Experimental: talks to the opmem prototype through the authenticated IRM
plugin resource proxy. The target and tenancy come from the active Grafana
context.`,
	}
	shared.bind(cmd.PersistentFlags(), defaultOpMemTimeout)

	cmd.AddCommand(newOpMemContainersCmd(loader, shared))
	return markExperimental(cmd)
}

type opMemSharedOpts struct {
	Timeout time.Duration
}

func (o *opMemSharedOpts) bind(flags *pflag.FlagSet, timeout time.Duration) {
	flags.DurationVar(&o.Timeout, "timeout", timeout, "Deadline for each individual request (e.g. 5s, 30s)")
}

func (o *opMemSharedOpts) validate() error {
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

type opMemActorOpts struct {
	Kind string
	ID   string
	Name string
}

func (o *opMemActorOpts) bind(flags *pflag.FlagSet) {
	flags.StringVar(&o.Kind, "actor-kind", "", "Who drafted the content: human or agent (required). Requests run as the signed-in Grafana user; the server stores the actor as given and does not check it against the login")
	flags.StringVar(&o.ID, "actor-id", "", "A user id, or the name of the agent working for the signed-in user (required)")
	flags.StringVar(&o.Name, "actor-name", "", "Optional human-readable display name for the actor")
}

func (o *opMemActorOpts) validate() error {
	if err := requireOpMemFlag("actor-kind", o.Kind); err != nil {
		return err
	}
	if err := validateEnumFlag("actor-kind", o.Kind, actorKindVocab); err != nil {
		return err
	}
	return requireOpMemFlag("actor-id", o.ID)
}

func (o *opMemActorOpts) actor() opMemActorInput {
	return opMemActorInput{Kind: o.Kind, ID: o.ID, DisplayName: o.Name}
}

func requireOpMemFlag(name, value string) error {
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

func requireOpMemArg(name, value string) error {
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

func setupOpMemReadIO(io *cmdio.Options, flags *pflag.FlagSet) {
	io.RegisterCustomCodec("yaml", format.NewOrderedYAMLCodec())
	io.BindFlags(flags)
}

func withOpMem(cmd *cobra.Command, loader OpMemClientLoader, shared *opMemSharedOpts, fn func(ctx context.Context, api OpMemAPI) error) error {
	api, err := loader.LoadOpMem(cmd.Context(), shared.Timeout)
	if err != nil {
		return err
	}
	return fn(cmd.Context(), api)
}

type opMemValidatable interface{ Validate() error }

func validateOpMemInputs(shared *opMemSharedOpts, opts opMemValidatable) error {
	if err := shared.validate(); err != nil {
		return err
	}
	return opts.Validate()
}

func validateOpMemIDInputs(shared *opMemSharedOpts, argName, id string, opts opMemValidatable) error {
	if err := shared.validate(); err != nil {
		return err
	}
	if err := requireOpMemArg(argName, id); err != nil {
		return err
	}
	return opts.Validate()
}

func opMemUsage(message string) error {
	code := fail.ExitUsageError
	return &fail.DetailedError{Summary: message, ExitCode: &code, Suggestions: []string{"Check the flag values against the command help (--help)"}}
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
