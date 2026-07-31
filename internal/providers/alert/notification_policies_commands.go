package alert

import (
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// notificationPoliciesCommands returns the notification-policies command group.
func notificationPoliciesCommands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "notification-policies",
		Short:   "Manage the Grafana alerting notification policy tree.",
		Aliases: []string{"notification-policy", "policies"},
	}
	cmd.AddCommand(
		newNotificationPoliciesGetCommand(loader),
		newNotificationPoliciesSetCommand(loader),
		newNotificationPoliciesResetCommand(loader),
		newNotificationPoliciesExportCommand(loader),
	)
	return cmd
}

type notificationPoliciesGetOpts struct {
	IO cmdio.Options
}

func (o *notificationPoliciesGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

func newNotificationPoliciesGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &notificationPoliciesGetOpts{}
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get the notification policy tree.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}
			policy, err := client.GetNotificationPolicy(ctx)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), policy)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type notificationPoliciesSetOpts struct {
	IO    cmdio.Options
	File  string
	Force bool
}

func (o *notificationPoliciesSetOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the policy tree (JSON/YAML, use - for stdin)")
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
	// The set result is a SingleMutation document through the codec system:
	// the default text codec prints the familiar one-line success; agent
	// mode and explicit -o json/yaml get the structured document.
	o.IO.RegisterCustomCodec("text", &singleMutationTextCodec{line: func(cmdio.SingleMutation) string {
		return "Notification policy updated"
	}})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newNotificationPoliciesSetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &notificationPoliciesSetOpts{}
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Replace the entire notification policy tree.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			var policy NotificationPolicy
			if err := providers.ReadFileOrStdin(opts.File, cmd.InOrStdin(), &policy); err != nil {
				return err
			}
			// The confirmation exchange is a diagnostic, not the result —
			// stderr keeps the prompt and "Aborted." out of the stdout
			// document.
			ok, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				"Replace notification policy tree? This overwrites the entire existing tree.")
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}
			if err := client.SetNotificationPolicy(ctx, policy); err != nil {
				return err
			}
			result := cmdio.NewSingleMutation("updated", cmdio.MutationTarget{Kind: "notification-policy"})
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type notificationPoliciesResetOpts struct {
	IO    cmdio.Options
	Force bool
}

func (o *notificationPoliciesResetOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
	// The reset result is a SingleMutation document through the codec
	// system: the default text codec prints the familiar one-line success;
	// agent mode and explicit -o json/yaml get the structured document.
	o.IO.RegisterCustomCodec("text", &singleMutationTextCodec{line: func(cmdio.SingleMutation) string {
		return "Notification policy reset to default"
	}})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newNotificationPoliciesResetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &notificationPoliciesResetOpts{}
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset the notification policy tree to its default.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			// The confirmation exchange is a diagnostic, not the result —
			// stderr keeps the prompt and "Aborted." out of the stdout
			// document.
			ok, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				"Reset notification policy tree to default? This replaces the entire tree.")
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}
			if err := client.ResetNotificationPolicy(ctx); err != nil {
				return err
			}
			result := cmdio.NewSingleMutation("reset", cmdio.MutationTarget{Kind: "notification-policy"})
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type notificationPoliciesExportOpts struct {
	Format string
}

func newNotificationPoliciesExportCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &notificationPoliciesExportOpts{}
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export the notification policy tree in provisioning format.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateExportFormat(opts.Format); err != nil {
				return err
			}
			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}
			data, err := client.ExportNotificationPolicy(ctx, opts.Format)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
	cmd.Flags().StringVar(&opts.Format, "format", "yaml", "Export format: yaml, json, or hcl")
	return cmd
}
