package resources

import (
	"bytes"
	"fmt"

	cmdconfig "github.com/grafana/grafanactl/cmd/grafanactl/config"
	"github.com/grafana/grafanactl/internal/format"
	cmdio "github.com/grafana/grafanactl/internal/output"
	"github.com/grafana/grafanactl/internal/resources"
	"github.com/grafana/grafanactl/internal/resources/local"
	"github.com/grafana/grafanactl/internal/resources/remote"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type editOpts struct {
	IO cmdio.Options
}

func (opts *editOpts) setup(flags *pflag.FlagSet) {
	// Bind all the flags
	opts.IO.BindFlags(flags)
}

func (opts *editOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}

	return nil
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
	# Editing a dashboard
	grafanactl resources dashboard/foo

	# Editing a dashboard in JSON
	grafanactl resources -o json dashboard/foo

	# Using an alternative editor
	EDITOR=nvim grafanactl resources dashboard/foo
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			edit := editorFromEnv()

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			pusher, err := remote.NewDefaultPusher(ctx, cfg)
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
