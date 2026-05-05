package stacks

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const instancesPath = "/api/instances"

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

type listOpts struct {
	IO  cmdio.Options
	Org string
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &stackTableCodec{})
	o.IO.RegisterCustomCodec("wide", &stackTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Org, "org", "", "Organisation slug (required)")
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List stacks in an organisation.",
		Annotations: map[string]string{
			agent.AnnotationRequiredScope: "stacks:read",
			agent.AnnotationTokenCost:     "large",
			agent.AnnotationLLMHint:       "List all stacks in the organisation. Use get to view details of a single stack.",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.Org == "" {
				return errors.New("--org is required")
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			cfg, err := loader.LoadCloudTokenConfig(ctx)
			if err != nil {
				return err
			}

			stacks, err := cfg.Client.ListStacks(ctx, opts.Org)
			if err != nil {
				return fmt.Errorf("failed to list stacks: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), stacks)
		},
	}
	opts.setup(cmd.Flags())
	_ = cmd.MarkFlagRequired("org")
	return cmd
}

// ---------------------------------------------------------------------------
// get
// ---------------------------------------------------------------------------

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &stackTableCodec{})
	o.IO.RegisterCustomCodec("wide", &stackTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <stack-slug>",
		Short: "Get details of a single stack.",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			agent.AnnotationRequiredScope: "stacks:read",
			agent.AnnotationTokenCost:     "small",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			cfg, err := loader.LoadCloudTokenConfig(ctx)
			if err != nil {
				return err
			}

			stack, err := cfg.Client.GetStack(ctx, args[0])
			if err != nil {
				return fmt.Errorf("failed to get stack: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), stack)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// create
// ---------------------------------------------------------------------------

type createOpts struct {
	IO               cmdio.Options
	Name             string
	Slug             string
	Region           string
	Description      string
	Labels           []string
	URL              string
	DeleteProtection bool
	DryRun           bool
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &stackTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Name, "name", "", "Stack name (required)")
	flags.StringVar(&o.Slug, "slug", "", "Stack slug / subdomain (required)")
	flags.StringVar(&o.Region, "region", "", "Region slug (e.g. us, eu). Use 'gcx stacks regions' to list.")
	flags.StringVar(&o.Description, "description", "", "Short description")
	flags.StringSliceVar(&o.Labels, "labels", nil, "Labels in key=value format (may be repeated)")
	flags.StringVar(&o.URL, "url", "", "Custom domain URL")
	flags.BoolVar(&o.DeleteProtection, "delete-protection", false, "Enable delete protection")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview the request without executing it")
}

func newCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new Grafana Cloud stack.",
		Long: `Create a new Grafana Cloud stack.

This command creates a new Grafana Cloud stack, which provisions infrastructure
and may incur costs. Always confirm the stack name, slug, and region with the
user before executing. Prefer --dry-run first.`,
		Annotations: map[string]string{
			agent.AnnotationRequiredScope: "stacks:write",
			agent.AnnotationTokenCost:     "small",
			agent.AnnotationLLMHint:       "This command creates a new Grafana Cloud stack, which provisions infrastructure and may incur costs. Always confirm the stack name, slug, and region with the user before executing. Prefer --dry-run first.",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.Name == "" || opts.Slug == "" {
				return errors.New("--name and --slug are required")
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			labels, err := labelsFromFlag(opts.Labels)
			if err != nil {
				return err
			}

			req := cloud.CreateStackRequest{
				Name:        opts.Name,
				Slug:        opts.Slug,
				Region:      opts.Region,
				Description: opts.Description,
				Labels:      labels,
				URL:         opts.URL,
			}
			if opts.DeleteProtection {
				dp := true
				req.DeleteProtection = &dp
			}

			if opts.DryRun {
				dryRunSummary(cmd.OutOrStdout(), http.MethodPost, instancesPath, req)
				return nil
			}

			ctx := cmd.Context()
			cfg, err := loader.LoadCloudTokenConfig(ctx)
			if err != nil {
				return err
			}

			stack, err := cfg.Client.CreateStack(ctx, req)
			if err != nil {
				return fmt.Errorf("failed to create stack: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), stack)
		},
	}
	opts.setup(cmd.Flags())
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("slug")
	return cmd
}

// ---------------------------------------------------------------------------
// update
// ---------------------------------------------------------------------------

type updateOpts struct {
	IO                 cmdio.Options
	Name               string
	Description        string
	Labels             []string
	DeleteProtection   bool
	NoDeleteProtection bool
	DryRun             bool
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &stackTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Name, "name", "", "New stack name")
	flags.StringVar(&o.Description, "description", "", "New description")
	flags.StringSliceVar(&o.Labels, "labels", nil, "Labels in key=value format (replaces all labels)")
	flags.BoolVar(&o.DeleteProtection, "delete-protection", false, "Enable delete protection")
	flags.BoolVar(&o.NoDeleteProtection, "no-delete-protection", false, "Disable delete protection")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview the request without executing it")
}

func newUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update <stack-slug>",
		Short: "Update a Grafana Cloud stack.",
		Long: `Update a Grafana Cloud stack.

This command modifies a live Grafana Cloud stack. Changing the name or disabling
delete protection can have downstream effects. Always confirm the intended
changes with the user and prefer --dry-run first.`,
		Args: cobra.ExactArgs(1),
		Annotations: map[string]string{
			agent.AnnotationRequiredScope: "stacks:write",
			agent.AnnotationTokenCost:     "small",
			agent.AnnotationLLMHint:       "This command modifies a live Grafana Cloud stack. Changing the name or disabling delete protection can have downstream effects. Always confirm the intended changes with the user and prefer --dry-run first.",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if opts.DeleteProtection && opts.NoDeleteProtection {
				return errors.New("--delete-protection and --no-delete-protection are mutually exclusive")
			}

			labels, err := labelsFromFlag(opts.Labels)
			if err != nil {
				return err
			}

			req := cloud.UpdateStackRequest{
				Name:   opts.Name,
				Labels: labels,
			}
			if cmd.Flags().Changed("description") {
				req.Description = &opts.Description
			}
			if opts.DeleteProtection {
				dp := true
				req.DeleteProtection = &dp
			}
			if opts.NoDeleteProtection {
				dp := false
				req.DeleteProtection = &dp
			}

			slug := args[0]

			if opts.DryRun {
				dryRunSummary(cmd.OutOrStdout(), http.MethodPost, instancesPath+"/"+slug, req)
				return nil
			}

			ctx := cmd.Context()
			cfg, err := loader.LoadCloudTokenConfig(ctx)
			if err != nil {
				return err
			}

			stack, err := cfg.Client.UpdateStack(ctx, slug, req)
			if err != nil {
				return fmt.Errorf("failed to update stack: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), stack)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// delete
// ---------------------------------------------------------------------------

type deleteOpts struct {
	Yes    bool
	DryRun bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVarP(&o.Yes, "yes", "y", false, "Skip confirmation prompt")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview the operation without executing it")
}

func newDeleteCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete <stack-slug>",
		Short: "Delete a Grafana Cloud stack.",
		Long: `Delete a Grafana Cloud stack.

This command permanently deletes a Grafana Cloud stack and ALL its data
(dashboards, alerts, datasources, metrics, logs, traces). This action is
IRREVERSIBLE. Always confirm with the user by name before executing. Prefer
--dry-run first. Never run this command without explicit user confirmation.`,
		Args: cobra.ExactArgs(1),
		Annotations: map[string]string{
			agent.AnnotationRequiredScope: "stacks:delete",
			agent.AnnotationTokenCost:     "small",
			agent.AnnotationLLMHint:       "This command permanently deletes a Grafana Cloud stack and all its data (dashboards, alerts, datasources, metrics, logs, traces). This action is irreversible. Always confirm with the user by name before executing. Prefer --dry-run first. Never run this command without explicit user confirmation.",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]

			if opts.DryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Dry run: DELETE %s/%s\n", instancesPath, slug)
				fmt.Fprintf(cmd.OutOrStdout(), "\nStack %q would be permanently deleted. No changes were made.\n", slug)
				return nil
			}

			cliOpts, err := config.LoadCLIOptions()
			if err != nil {
				return err
			}

			if !opts.Yes && !cliOpts.AutoApprove {
				fmt.Fprintf(cmd.OutOrStdout(),
					"WARNING: This will permanently delete stack %q and ALL its data.\n"+
						"Type the stack slug to confirm: ", slug)

				scanner := bufio.NewScanner(cmd.InOrStdin())
				scanner.Scan()
				confirmation := strings.TrimSpace(scanner.Text())
				if confirmation != slug {
					return fmt.Errorf("confirmation did not match: expected %q, got %q", slug, confirmation)
				}
			}

			ctx := cmd.Context()
			cfg, err := loader.LoadCloudTokenConfig(ctx)
			if err != nil {
				return err
			}

			if err := cfg.Client.DeleteStack(ctx, slug); err != nil {
				return fmt.Errorf("failed to delete stack: %w", err)
			}

			cmdio.Success(cmd.OutOrStdout(), "Stack %q deleted successfully.", slug)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// regions
// ---------------------------------------------------------------------------

type regionsOpts struct {
	IO cmdio.Options
}

func (o *regionsOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &regionTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newRegionsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &regionsOpts{}
	cmd := &cobra.Command{
		Use:   "regions",
		Short: "List available regions for stack creation.",
		Annotations: map[string]string{
			agent.AnnotationRequiredScope: "stacks:read",
			agent.AnnotationTokenCost:     "small",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			cfg, err := loader.LoadCloudTokenConfig(ctx)
			if err != nil {
				return err
			}

			regions, err := cfg.Client.ListRegions(ctx)
			if err != nil {
				return fmt.Errorf("failed to list regions: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), regions)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
