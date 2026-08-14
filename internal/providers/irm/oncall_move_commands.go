package irm

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// An escalation chain is an ordered sequence of steps, and routes match from
// the top down with first-match semantics. Order is therefore part of the
// meaning of both objects. A caller sets the order on create through the
// position field of the spec, and changes it later through these move verbs.

// moveResult is the structured result of a move. It embeds the shared
// SingleMutation shape — so the type and schema_version discriminators stay
// in place — and adds the position, which is the whole point of the verb and
// which a caller reads back from the document.
type moveResult struct {
	cmdio.SingleMutation `json:",inline" yaml:",inline"`

	Position int `json:"position" yaml:"position"`
}

// moveTextCodec renders a moveResult as the human one-line summary.
// Output-only: decoding is unsupported.
type moveTextCodec struct {
	label string
}

func (c *moveTextCodec) Format() format.Format { return "text" }

func (c *moveTextCodec) Decode(io.Reader, any) error {
	return errors.New("text format does not support decoding")
}

func (c *moveTextCodec) Encode(w io.Writer, v any) error {
	r, ok := v.(moveResult)
	if !ok {
		return fmt.Errorf("text codec: unsupported value type %T (expected moveResult)", v)
	}
	cmdio.Success(w, "Moved %s %s to position %d", c.label, r.Target.ID, r.Position)
	return nil
}

type moveOpts struct {
	IO       cmdio.Options
	Position int
}

func (o *moveOpts) setup(flags *pflag.FlagSet, label string) {
	o.IO.RegisterCustomCodec("text", &moveTextCodec{label: label})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.Position, "position", -1, "Zero-based target position (required)")
}

func (o *moveOpts) Validate() error {
	if o.Position < 0 {
		return fmt.Errorf("--position is required and must be zero or greater, got %d", o.Position)
	}
	return nil
}

// newMoveCommand builds a `move <id> --position <n>` verb for an ordered
// resource. kind is the machine-facing resource kind carried in the result;
// label is the human noun used in the one-line message.
func newMoveCommand(
	loader OnCallConfigLoader, kind, label, short, long string,
	moveFn func(ctx context.Context, client OnCallAPI, id string, position int) error,
) *cobra.Command {
	opts := &moveOpts{}
	cmd := &cobra.Command{
		Use:   "move <id>",
		Short: short,
		Long:  long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, _, err := loader.LoadOnCallClient(ctx)
			if err != nil {
				return err
			}

			if err := moveFn(ctx, client, args[0], opts.Position); err != nil {
				return err
			}

			mutation := cmdio.NewSingleMutation("moved", cmdio.MutationTarget{Kind: kind, ID: args[0]})
			changed := true
			mutation.Changed = &changed
			return opts.IO.Encode(cmd.OutOrStdout(), moveResult{
				SingleMutation: mutation,
				Position:       opts.Position,
			})
		},
	}
	opts.setup(cmd.Flags(), label)
	return cmd
}

func newEscalationPolicyMoveCommand(loader OnCallConfigLoader) *cobra.Command {
	return newMoveCommand(loader, "EscalationPolicy", "escalation policy",
		"Move an escalation step to a position in its chain.",
		`Move an escalation step to a position in its chain.

An escalation chain runs its steps in order, so the position decides when a
step fires. The position is zero-based: 0 is the first step. The backend
renumbers the other steps of the chain.

Set the order at create time through the position field of the spec. Use this
command to change the order afterwards.`,
		func(ctx context.Context, client OnCallAPI, id string, position int) error {
			return client.MoveEscalationPolicy(ctx, id, position)
		})
}

func newRouteMoveCommand(loader OnCallConfigLoader) *cobra.Command {
	return newMoveCommand(loader, "Route", "route",
		"Move a route to a position in its integration.",
		`Move a route to a position in its integration.

Routes match from the top down, and the first match wins, so the position
decides which route handles an alert. The position is zero-based: 0 is the
first route. The backend renumbers the other routes of the integration.

Set the order at create time through the position field of the spec. Use this
command to change the order afterwards.`,
		func(ctx context.Context, client OnCallAPI, id string, position int) error {
			return client.MoveRoute(ctx, id, position)
		})
}
