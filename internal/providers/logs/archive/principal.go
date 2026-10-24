package archive

import (
	"github.com/grafana/gcx/internal/docs"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
)

func (h *archiveHelper) principalCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "principal",
		Short: "Show the Grafana identity that archive buckets must grant access to.",
	}
	cmd.AddCommand(h.principalGetCommand())
	return cmd
}

type principalGetOpts struct {
	IO cmdio.Options
}

func (o *principalGetOpts) setup(cmd *cobra.Command) {
	registerSingleTable(&o.IO, principalTable())
	o.IO.DefaultFormat(cmdio.FormatTable)
	o.IO.BindFlags(cmd.Flags())
}

func (o *principalGetOpts) Validate() error {
	return o.IO.Validate()
}

func (h *archiveHelper) principalGetCommand() *cobra.Command {
	opts := &principalGetOpts{}
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Show the identity and region an archive bucket must be set up for.",
		Long: "Show the cloud provider and region of this stack's Grafana Cloud cell, and the Grafana principal " +
			"(an AWS IAM ARN, a GCP service account or an Azure application ID) to grant access to an archive bucket.\n\n" +
			"An archive bucket must be on the same cloud provider as the cell and, for S3 and GCS, in the same region. " +
			"For Azure, the application must also be authorized in your Azure AD tenant; see " + docs.AdaptiveLogsArchive + ". " +
			"Use this before 'gcx logs archive destinations create'.",
		Example: `  # Show the principal to grant and the region the bucket must be in
  gcx logs archive principal get

  # Print the AWS IAM ARN to grant, on an AWS cell
  gcx logs archive principal get --jq '.aws.arn'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			details, err := client.CellDetails(ctx)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), *details)
		},
	}
	opts.setup(cmd)
	return cmd
}

func principalTable() cmdio.Table[CellDetails] {
	return cmdio.Table[CellDetails]{
		Columns: []cmdio.Column[CellDetails]{
			{Header: "CLOUD PROVIDER", Content: func(c CellDetails) string { return cmdio.OrDash(c.CloudProvider) }},
			{Header: "REGION", Content: func(c CellDetails) string { return cmdio.OrDash(c.Region) }},
			{Header: "DESTINATION TYPE", Content: func(c CellDetails) string { return cmdio.OrDash(c.DestinationType()) }},
			{Header: "PRINCIPAL", Content: func(c CellDetails) string { return cmdio.OrDash(c.Principal()) }},
		},
	}
}
