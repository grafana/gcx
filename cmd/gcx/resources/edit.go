package resources

import (
	"bytes"
	"fmt"
	"os"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/local"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type editOpts struct {
	IO cmdio.Options

	// flags is the bound flag set, kept so Validate can reject flags that
	// cannot round-trip through the editor (--json, --jq).
	flags *pflag.FlagSet
}

func (opts *editOpts) setup(flags *pflag.FlagSet) {
	// OutputFormat doubles as the editor temp-file extension, the encoder
	// for the buffer handed to the editor, and the decode format for the
	// round-trip read-back — pin the default so agent mode does not flip it
	// to the agents codec (not decodable by the FSReader, and large
	// resources would open as a spill-summary envelope). An explicit
	// -o json|yaml from the user still wins.
	opts.IO.PinDefaultFormat("json")

	// Validate rejects -o agents (below), so never advertise it in the
	// format menu (usage string and unknown-format error listings).
	opts.IO.HideFormat("agents")

	// Bind all the flags
	opts.IO.BindFlags(flags)
	opts.flags = flags

	// Validate rejects every use of --json/--jq (below), so hide both from
	// help — advertising flags the command always refuses is the same
	// dishonesty as advertising the rejected agents format.
	_ = flags.MarkHidden("json")
	_ = flags.MarkHidden("jq")
}

func (opts *editOpts) Validate() error {
	// Typed exit-2 rejections run BEFORE the shared IO.Validate so mixed
	// invalid invocations get the same exit code as solo ones (the shared
	// errors are untyped exit 1 today, repo-wide).
	//
	// The agents display codec cannot round-trip: the FSReader has no
	// "agents" decoder, so the edited buffer could never be read back —
	// the command would fail only after the user finished editing.
	// UsageError classifies the rejection as invalid usage (exit 2,
	// DESIGN.md taxonomy).
	if opts.IO.OutputFormat == "agents" {
		return &fail.UsageError{Message: "output format 'agents' cannot be used with edit: the edited file could not be read back. Use -o json or -o yaml"}
	}

	// --json (field selection/discovery) and --jq (transformation) shape
	// the encoded buffer handed to the editor, which the command then
	// decodes back as the resource — a field-selected or jq-transformed
	// document cannot round-trip. Reject upfront, mirroring the agents
	// rejection above.
	if opts.flags != nil {
		if f := opts.flags.Lookup("json"); f != nil && f.Changed {
			return &fail.UsageError{Message: "--json cannot be used with edit: field-selected output cannot round-trip through the editor. Use -o json or -o yaml"}
		}
		if f := opts.flags.Lookup("jq"); f != nil && f.Changed {
			return &fail.UsageError{Message: "--jq cannot be used with edit: transformed output cannot round-trip through the editor. Use -o json or -o yaml"}
		}
	}

	return opts.IO.Validate()
}

func editCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &editOpts{}

	cmd := &cobra.Command{
		Use:   "edit RESOURCE_SELECTOR",
		Args:  cobra.ExactArgs(1),
		Short: "Edit resources from Grafana",
		Long: `Edit resources from Grafana using the default editor.

This command allows the edition of any resource that can be accessed by this CLI tool.

It will open the default editor as configured by the EDITOR environment variable, or fall back to 'vi' for Linux or 'notepad' for Windows.
The editor will be started in the shell set by the SHELL environment variable. If undefined, '/bin/bash' is used for Linux or 'cmd' for Windows.

The edition will be cancelled if no changes are written to the file or if the file after edition is empty.
`,
		Example: `
	# Edit a dashboard
	gcx resources edit dashboard/foo

	# Edit a dashboard in JSON
	gcx resources edit -o json dashboard/foo

	# Using an alternative editor
	EDITOR=nvim gcx resources edit dashboard/foo
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			// Interactive-class contract: agent mode must never block on an
			// interactive program. With no EDITOR/VISUAL configured the
			// fallback editor (vi/notepad) would hang against piped stdio,
			// so fail fast with the alternative. An explicitly configured
			// EDITOR is honored — setting it to a non-interactive command
			// is legitimate automation.
			if agent.IsAgentMode() && os.Getenv("EDITOR") == "" && os.Getenv("VISUAL") == "" {
				return gcxerrors.DetailedError{
					Summary: "edit is interactive and no EDITOR is configured",
					Details: "agent mode cannot drive the fallback editor (vi/notepad); it would block on a non-TTY pipe",
					Suggestions: []string{
						"Use 'gcx resources pull' to write the resource to disk, modify it, then 'gcx resources push'",
						"Set EDITOR to a non-interactive command if scripted editing is intended",
					},
				}
			}

			ctx := cmd.Context()
			edit := editorFromEnv()

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			// edit never dry-runs, so the guard is inert; pass an empty config.
			pusher, err := remote.NewDefaultPusher(ctx, cfg, remote.GuardConfig{})
			if err != nil {
				return err
			}

			reader := local.FSReader{
				Decoders:           format.Codecs(),
				MaxConcurrentReads: 1,
				StopOnError:        true,
			}

			// Fetch the resource
			res, err := FetchResources(ctx, FetchRequest{
				Config:             cfg,
				StopOnError:        true,
				ExpectSingleTarget: true,
			}, args)
			if err != nil {
				return err
			}

			// Will contain the initial state of the resource to edit
			buffer := &bytes.Buffer{}

			if opts.IO.OutputFormat == "yaml" {
				buffer.WriteString(`# Please edit the resource below. Lines beginning with a '#' will be ignored,
# and an empty file will cancel the edit.

`)
			}

			list := res.Resources.AsList()
			if len(list) != 1 {
				return fmt.Errorf("expected exactly one resource, got %d", len(list))
			}

			obj := list[0].ToUnstructured()
			if err := opts.IO.Encode(buffer, &obj); err != nil {
				return err
			}

			original := buffer.Bytes()
			cleanup, edited, err := edit.OpenInTempFile(ctx, buffer, opts.IO.OutputFormat)
			if err != nil {
				return err
			}
			defer cleanup()

			if len(edited) == 0 {
				cmdio.Info(cmd.OutOrStdout(), "Edit cancelled: empty file.")
				return nil
			}

			if bytes.Equal(original, edited) {
				cmdio.Info(cmd.OutOrStdout(), "Edit cancelled: no changes were made.")
				return nil
			}

			tmpRes := resources.NewResources()
			if err := reader.ReadBytes(ctx, tmpRes, edited, opts.IO.OutputFormat); err != nil {
				return err
			}

			if _, err := pusher.Push(ctx, remote.PushRequest{
				Resources:      tmpRes,
				MaxConcurrency: 1,
				StopOnError:    true,
			}); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Edited!")

			return nil
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}
