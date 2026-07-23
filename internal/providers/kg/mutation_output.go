package kg

// This file wires kg's mutation commands into the shared cmdio mutation
// result family (the pre-GA agent output contract): every finite mutation
// writes exactly one structured document to stdout through the codec system.
// The "text" codec each command registers reproduces the styled one-liner the
// command has always printed, keeping default human stdout byte-identical,
// while agent mode (agents codec) and explicit -o json/yaml receive the
// structured value.

import (
	"context"
	"errors"
	"io"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// textLineCodec adapts a render function into a "text" format codec. The
// render function receives the command's result value and writes the human
// rendering — possibly nothing, when the command never printed to stdout —
// to w.
type textLineCodec struct {
	render func(w io.Writer, v any) error
}

func (c *textLineCodec) Format() format.Format { return "text" }

func (c *textLineCodec) Encode(w io.Writer, v any) error { return c.render(w, v) }

func (c *textLineCodec) Decode(io.Reader, any) error {
	return errors.New("text format does not support decoding")
}

// singleMutationText returns a text codec rendering a cmdio.SingleMutation
// result with the given line function — the exact styled one-liner the
// command printed before the migration, so default human output stays
// byte-identical.
func singleMutationText(line func(w io.Writer, m cmdio.SingleMutation)) format.Codec { //nolint:ireturn // codec registration requires the interface
	return &textLineCodec{render: func(w io.Writer, v any) error {
		m, ok := v.(cmdio.SingleMutation)
		if !ok {
			return errors.New("invalid data type for text codec: expected SingleMutation")
		}
		line(w, m)
		return nil
	}}
}

// guardedDeleteOpts is the shared option set for kg's confirm-guarded named
// deletes (prom-rules, model-rules, suppressions): a --force bypass plus the
// codec-driven mutation result.
type guardedDeleteOpts struct {
	IO    cmdio.Options
	Force bool
}

// setup binds --force and the output flags, registering line as the exact
// human success one-liner.
func (o *guardedDeleteOpts) setup(flags *pflag.FlagSet, line func(w io.Writer, m cmdio.SingleMutation)) {
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
	o.IO.RegisterCustomCodec("text", singleMutationText(line))
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

// runGuardedDelete implements the shared flow of kg's named-config delete
// commands: validate output options, confirm the destructive action (prompt
// and abort note on stderr, never stdout), run del against a fresh client,
// and emit the single mutation result document through the codec system.
func runGuardedDelete(cmd *cobra.Command, opts *guardedDeleteOpts, loader RESTConfigLoader,
	kind, name, prompt string, del func(ctx context.Context, client *Client) error,
) error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force, prompt)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}
	cfg, err := loader.LoadGrafanaConfig(cmd.Context())
	if err != nil {
		return err
	}
	client, err := NewClient(cfg)
	if err != nil {
		return err
	}
	if err := del(cmd.Context(), client); err != nil {
		return err
	}
	return opts.IO.Encode(cmd.OutOrStdout(),
		cmdio.NewSingleMutation("deleted", cmdio.MutationTarget{Kind: kind, Name: name}))
}
