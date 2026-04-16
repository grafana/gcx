package alert

import (
	"fmt"

	cmdio "github.com/grafana/gcx/internal/output"
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
	File string
}

func newNotificationPoliciesSetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &notificationPoliciesSetOpts{}
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Replace the entire notification policy tree.",
		RunE: func(cmd *cobra.Command, args []string) error {
			var policy NotificationPolicy
			if err := readProvisioningInput(opts.File, cmd.InOrStdin(), &policy); err != nil {
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
			if err := client.SetNotificationPolicy(ctx, policy); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Notification policy updated")
			return nil
		},
	}
	cmd.Flags().StringVarP(&opts.File, "filename", "f", "", "File containing the policy tree (JSON/YAML, use - for stdin)")
	return cmd
}

func newNotificationPoliciesResetCommand(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset the notification policy tree to its default.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
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
			fmt.Fprintln(cmd.OutOrStdout(), "Notification policy reset to default")
			return nil
		},
	}
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
