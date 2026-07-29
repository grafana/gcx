package stacks

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/gcxerrors"
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
	o.IO.DefaultFormat("yaml")
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

// stackSlugRe matches valid Grafana Cloud stack slugs. GCOM accepts lowercase
// characters only, and the issue-#950 repro confirmed hyphens are rejected;
// the slug becomes the <slug>.grafana.net subdomain. Length is not bounded
// here — GCOM does not document a limit, so anything past this format check
// is left to the server (surfaced via the 409 InvalidArgument mapping).
var stackSlugRe = regexp.MustCompile(`^[a-z0-9]+$`)

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

func (o *createOpts) Validate() error {
	if o.Name == "" || o.Slug == "" {
		return &gcxerrors.DetailedError{
			Summary:  "Invalid command usage",
			Details:  "--name and --slug are required",
			ExitCode: new(gcxerrors.ExitUsageError),
			Suggestions: []string{
				"Provide both flags: gcx cloud stacks create --name <name> --slug <slug> --region <region>",
			},
		}
	}
	if !stackSlugRe.MatchString(o.Slug) {
		return &gcxerrors.DetailedError{
			Summary:  "Invalid command usage",
			Details:  fmt.Sprintf("invalid stack slug %q: stack slugs may only contain lowercase letters (a-z) and digits (0-9); the slug becomes your instance URL <slug>.grafana.net", o.Slug),
			ExitCode: new(gcxerrors.ExitUsageError),
			Suggestions: []string{
				"Retry with a lowercase alphanumeric slug, e.g. --slug mygcxeval",
			},
			DocsLink: docs.CloudAPI,
		}
	}
	if err := validateLabels(o.Labels); err != nil {
		return err
	}
	return o.IO.Validate()
}

// validateLabels wraps a malformed --labels value in the standard usage-error
// shape so it fails with the same class and exit code as the other flag
// mistakes on stacks commands.
func validateLabels(labels []string) error {
	if _, err := labelsFromFlag(labels); err != nil {
		return &gcxerrors.DetailedError{
			Summary:  "Invalid command usage",
			Details:  err.Error(),
			ExitCode: new(gcxerrors.ExitUsageError),
			Suggestions: []string{
				"Pass labels as key=value, e.g. --labels env=prod --labels team=platform",
			},
		}
	}
	return nil
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &stackTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Name, "name", "", "Stack name (required)")
	flags.StringVar(&o.Slug, "slug", "", "Stack slug / subdomain (lowercase letters and digits only; required)")
	flags.StringVar(&o.Region, "region", "", "Region slug (e.g. us, eu). Use 'gcx cloud stacks list-regions' to list.")
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

This provisions new infrastructure and may incur costs. The stack name, slug,
and region cannot be changed after creation - double-check before running.
Use --dry-run to preview the request first.

Stack slugs may only contain lowercase letters and digits: the slug becomes
the stack's <slug>.grafana.net subdomain.`,
		Annotations: map[string]string{
			agent.AnnotationRequiredScope: "stacks:write",
			agent.AnnotationTokenCost:     "small",
			agent.AnnotationLLMHint:       "This command creates a new Grafana Cloud stack, which provisions infrastructure and may incur costs. Always confirm the stack name, slug, and region with the user before executing. Prefer --dry-run first. Stack slugs may only contain lowercase letters and digits (they become <slug>.grafana.net).",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
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
				// The preview is the command's result: it goes through the
				// codec so agent mode and explicit -o json/yaml receive one
				// structured document while the default table codec keeps the
				// classic human rendering.
				return opts.IO.Encode(cmd.OutOrStdout(),
					newDryRunPreview("created", http.MethodPost, instancesPath, req))
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

func (o *updateOpts) Validate() error {
	if o.DeleteProtection && o.NoDeleteProtection {
		return &gcxerrors.DetailedError{
			Summary:  "Invalid command usage",
			Details:  "--delete-protection and --no-delete-protection are mutually exclusive",
			ExitCode: new(gcxerrors.ExitUsageError),
			Suggestions: []string{
				"Pass only one of --delete-protection or --no-delete-protection",
			},
		}
	}
	if err := validateLabels(o.Labels); err != nil {
		return err
	}
	return o.IO.Validate()
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

This modifies a live stack. Note that the slug and region cannot be changed.
Use --dry-run to preview the request first.`,
		Args: cobra.ExactArgs(1),
		Annotations: map[string]string{
			agent.AnnotationRequiredScope: "stacks:write",
			agent.AnnotationTokenCost:     "small",
			agent.AnnotationLLMHint:       "This command modifies a live Grafana Cloud stack. Changing the name or disabling delete protection can have downstream effects. Always confirm the intended changes with the user and prefer --dry-run first.",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
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
				// See create: the preview is the result and flows through the
				// codec system.
				return opts.IO.Encode(cmd.OutOrStdout(),
					newDryRunPreview("updated", http.MethodPost, instancesPath+"/"+slug, req))
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
	IO     cmdio.Options
	Force  bool
	DryRun bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview the operation without executing it")
	// The delete result is a SingleMutation document through the codec
	// system: the default text codec reproduces the familiar human lines
	// (dry-run preview or styled success line); agent mode and explicit
	// -o json/yaml get the structured document.
	o.IO.RegisterCustomCodec("text", &deleteTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newDeleteCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete <stack-slug>",
		Short: "Delete a Grafana Cloud stack.",
		Long: `Delete a Grafana Cloud stack.

This permanently deletes a stack and all its data (dashboards, alerts,
datasources, metrics, logs, traces). This cannot be undone.
Use --dry-run to preview the operation first.`,
		Args: cobra.ExactArgs(1),
		Annotations: map[string]string{
			agent.AnnotationRequiredScope: "stacks:delete",
			agent.AnnotationTokenCost:     "small",
			agent.AnnotationLLMHint:       "This command permanently deletes a Grafana Cloud stack and all its data (dashboards, alerts, datasources, metrics, logs, traces). This action is irreversible. Always confirm with the user by name before executing. Prefer --dry-run first. Never run this command without explicit user confirmation.",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			slug := args[0]
			result := cmdio.NewSingleMutation("deleted", cmdio.MutationTarget{Kind: "stack", Name: slug})

			if opts.DryRun {
				result.DryRun = true
				return opts.IO.Encode(cmd.OutOrStdout(), result)
			}

			if err := confirmStackDelete(cmd, slug, opts.Force); err != nil {
				return err
			}

			ctx := cmd.Context()
			cfg, err := loader.LoadCloudTokenConfig(ctx)
			if err != nil {
				return err
			}

			if err := cfg.Client.DeleteStack(ctx, slug); err != nil {
				return fmt.Errorf("failed to delete stack: %w", err)
			}

			changed := true
			result.Changed = &changed
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// confirmStackDelete handles the slug-typing confirmation for stack deletion.
// Unlike other destructive commands that use providers.ConfirmDestructive with
// a simple y/N prompt, stack deletion requires typing the slug name because
// the operation is irreversible and destroys all data.
//
// The bypass chain (--force, AutoApprove, agent mode guard) is delegated to
// providers.CheckDestructiveBypass so it stays in sync with ConfirmDestructive.
func confirmStackDelete(cmd *cobra.Command, slug string, force bool) error {
	bypass, err := providers.CheckDestructiveBypass(force)
	if err != nil {
		return err
	}
	if bypass {
		return nil
	}

	fmt.Fprintf(cmd.ErrOrStderr(),
		"WARNING: This will permanently delete stack %q and ALL its data.\n"+
			"Type the stack slug to confirm: ", slug)

	scanner := bufio.NewScanner(cmd.InOrStdin())
	scanner.Scan()

	confirmation := strings.TrimSpace(scanner.Text())
	if confirmation != slug {
		return fmt.Errorf("confirmation did not match: expected %q, got %q", slug, confirmation)
	}

	return nil
}

// ---------------------------------------------------------------------------
// list-regions
// ---------------------------------------------------------------------------

type regionsOpts struct {
	IO cmdio.Options
}

func (o *regionsOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &regionTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newListRegionsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &regionsOpts{}
	cmd := &cobra.Command{
		Use:   "list-regions",
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
