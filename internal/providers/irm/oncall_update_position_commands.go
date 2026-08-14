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
// meaning of both objects. On create the backend reads the position field of
// the spec as an insertion point, so these update-position verbs are the way
// to set a known index.

// updatePositionResult is the structured result of a position update. The
// embedded SingleMutation keeps the type and schema_version discriminators in
// place, and the added position is the whole point of the verb, which a caller
// reads back from the document.
//
// The embedded field is anonymous and carries no tag, so encoding/json
// flattens it into the parent document.
type updatePositionResult struct {
	cmdio.SingleMutation

	// Position is the index that the caller asked for. The
	// move_to_position endpoint answers with an empty body, so gcx cannot
	// read the applied index back. The two values still agree, because the
	// backend applies the exact index or it fails: an index past the end of
	// the list gives HTTP 400 "Invalid position", and Validate rejects a
	// negative index before the call. See OrderedModelViewSet in
	// engine/common/ordered_model/viewset.py of grafana/oncall.
	Position int `json:"position" yaml:"position"`
}

// updatePositionTextCodec renders an updatePositionResult as the human
// one-line summary. Output-only: decoding is unsupported.
type updatePositionTextCodec struct {
	label string
}

func (c *updatePositionTextCodec) Format() format.Format { return "text" }

func (c *updatePositionTextCodec) Decode(io.Reader, any) error {
	return errors.New("text format does not support decoding")
}

func (c *updatePositionTextCodec) Encode(w io.Writer, v any) error {
	r, ok := v.(updatePositionResult)
	if !ok {
		return fmt.Errorf("text codec: unsupported value type %T (expected updatePositionResult)", v)
	}
	cmdio.Success(w, "Updated the position of %s %s to %d", c.label, r.Target.ID, r.Position)
	return nil
}

type updatePositionOpts struct {
	IO       cmdio.Options
	Position int
}

func (o *updatePositionOpts) setup(flags *pflag.FlagSet, label string) {
	o.IO.RegisterCustomCodec("text", &updatePositionTextCodec{label: label})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	// MarkFlagRequired states the requirement only in the runtime error, so
	// the flag help carries the marker for --help and for the reference page.
	flags.IntVar(&o.Position, "position", 0, "Zero-based target position (required)")
}

func (o *updatePositionOpts) Validate() error {
	if o.Position < 0 {
		return fmt.Errorf("--position must be zero or greater, got %d", o.Position)
	}
	return nil
}

// newUpdatePositionCommand builds an `update-position <id> --position <n>`
// verb for an ordered resource. kind is the machine-facing resource kind
// carried in the result; label is the human noun used in the one-line message.
func newUpdatePositionCommand(
	loader OnCallConfigLoader, kind, label, short, long string,
	moveFn func(ctx context.Context, client OnCallAPI, id string, position int) error,
) *cobra.Command {
	opts := &updatePositionOpts{}
	cmd := &cobra.Command{
		Use:   "update-position <id>",
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

			// Changed stays nil: the command never reads the current
			// position, so it cannot tell a real change from a no-op.
			mutation := cmdio.NewSingleMutation("updated-position", cmdio.MutationTarget{Kind: kind, ID: args[0]})
			return opts.IO.Encode(cmd.OutOrStdout(), updatePositionResult{
				SingleMutation: mutation,
				Position:       opts.Position,
			})
		},
	}
	opts.setup(cmd.Flags(), label)
	// Cobra rejects an omitted --position before RunE runs.
	_ = cmd.MarkFlagRequired("position")
	return cmd
}

func newEscalationPolicyUpdatePositionCommand(loader OnCallConfigLoader) *cobra.Command {
	return newUpdatePositionCommand(loader, "EscalationPolicy", "escalation policy",
		"Update the position of an escalation step in its chain.",
		`Update the position of an escalation step in its chain.

An escalation chain runs its steps in order, so the position decides when a
step fires. The position is zero-based: 0 is the first step. The backend
renumbers the other steps of the chain.

The position field of the spec behaves differently on create: the backend
reads it as an insertion point, and it moves the step that holds that position,
and every later step, one place down. Use this command to set a known index.`,
		func(ctx context.Context, client OnCallAPI, id string, position int) error {
			return client.MoveEscalationPolicy(ctx, id, position)
		})
}

func newRouteUpdatePositionCommand(loader OnCallConfigLoader) *cobra.Command {
	return newUpdatePositionCommand(loader, "Route", "route",
		"Update the position of a route in its integration.",
		`Update the position of a route in its integration.

Routes match from the top down, and the first match wins, so the position
decides which route handles an alert. The position is zero-based: 0 is the
first route. The backend renumbers the other routes of the integration.

The position field of the spec behaves differently on create: the backend
reads it as an insertion point, and it moves the route that holds that
position, and every later route, one place down. Use this command to set a
known index.`,
		func(ctx context.Context, client OnCallAPI, id string, position int) error {
			return client.MoveRoute(ctx, id, position)
		})
}
