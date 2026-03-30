package traces

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/adaptive/auth"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Commands returns the traces command group for the adaptive provider.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "traces",
		Short: "Manage Adaptive Traces resources.",
	}
	h := &tracesHelper{loader: loader}
	cmd.AddCommand(h.policiesCommand())
	cmd.AddCommand(h.recommendationsCommand())
	return cmd
}

type tracesHelper struct {
	loader *providers.ConfigLoader
}

func (h *tracesHelper) newClient(ctx context.Context) (*Client, error) {
	signalAuth, err := auth.ResolveSignalAuth(ctx, h.loader, "traces")
	if err != nil {
		return nil, err
	}
	return NewClient(signalAuth.BaseURL, signalAuth.TenantID, signalAuth.APIToken, signalAuth.HTTPClient), nil
}

func (h *tracesHelper) recommendationsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recommendations",
		Short: "Manage Adaptive Traces recommendations.",
	}
	cmd.AddCommand(
		h.recommendationsShowCommand(),
		h.recommendationsApplyCommand(),
		h.recommendationsDismissCommand(),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// recommendations show
// ---------------------------------------------------------------------------

type recommendationsShowOpts struct {
	IO cmdio.Options
}

func (o *recommendationsShowOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &recommendationTableCodec{})
	o.IO.RegisterCustomCodec("wide", &recommendationTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func (h *tracesHelper) recommendationsShowCommand() *cobra.Command {
	opts := &recommendationsShowOpts{}
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show Adaptive Traces recommendations.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			recs, err := client.ListRecommendations(ctx)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), recs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type recommendationTableCodec struct {
	Wide bool
}

func (c *recommendationTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *recommendationTableCodec) Encode(w io.Writer, v any) error {
	recs, ok := v.([]Recommendation)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Recommendation")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)

	if c.Wide {
		fmt.Fprintln(tw, "ID\tMESSAGE\tTAGS\tAPPLIED\tDISMISSED\tSTALE\tCREATED AT\tACTIONS")
	} else {
		fmt.Fprintln(tw, "ID\tMESSAGE\tTAGS\tAPPLIED\tDISMISSED\tSTALE\tCREATED AT")
	}

	for _, r := range recs {
		tags := strings.Join(r.Tags, ",")
		if c.Wide {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%v\t%v\t%v\t%s\t%d\n",
				r.ID, r.Message, tags, r.Applied, r.Dismissed, r.Stale, r.CreatedAt, len(r.Actions))
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%v\t%v\t%v\t%s\n",
				r.ID, r.Message, tags, r.Applied, r.Dismissed, r.Stale, r.CreatedAt)
		}
	}

	return tw.Flush()
}

func (c *recommendationTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// recommendations apply
// ---------------------------------------------------------------------------

type recommendationsApplyOpts struct {
	DryRun bool
}

func (o *recommendationsApplyOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview what would be applied without making changes")
}

func (o *recommendationsApplyOpts) Validate() error {
	return nil
}

//nolint:dupl // apply and dismiss are distinct commands with identical structure.
func (h *tracesHelper) recommendationsApplyCommand() *cobra.Command {
	opts := &recommendationsApplyOpts{}
	cmd := &cobra.Command{
		Use:   "apply <id>",
		Short: "Apply an Adaptive Traces recommendation.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			id := args[0]

			if opts.DryRun {
				cmdio.Info(cmd.OutOrStdout(), "[dry-run] Would apply recommendation %q", id)
				return nil
			}

			ctx := cmd.Context()

			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			if err := client.ApplyRecommendation(ctx, id); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Applied recommendation %q", id)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// recommendations dismiss
// ---------------------------------------------------------------------------

type recommendationsDismissOpts struct {
	DryRun bool
}

func (o *recommendationsDismissOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview what would be dismissed without making changes")
}

func (o *recommendationsDismissOpts) Validate() error {
	return nil
}

//nolint:dupl // dismiss and apply are distinct commands with identical structure.
func (h *tracesHelper) recommendationsDismissCommand() *cobra.Command {
	opts := &recommendationsDismissOpts{}
	cmd := &cobra.Command{
		Use:   "dismiss <id>",
		Short: "Dismiss an Adaptive Traces recommendation.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			id := args[0]

			if opts.DryRun {
				cmdio.Info(cmd.OutOrStdout(), "[dry-run] Would dismiss recommendation %q", id)
				return nil
			}

			ctx := cmd.Context()

			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			if err := client.DismissRecommendation(ctx, id); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Dismissed recommendation %q", id)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ===========================================================================
// policies
// ===========================================================================

func (h *tracesHelper) policiesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policies",
		Short: "Manage Adaptive Traces sampling policies.",
	}
	cmd.AddCommand(
		h.policiesListCommand(),
		h.policiesGetCommand(),
		h.policiesCreateCommand(),
		h.policiesUpdateCommand(),
		h.policiesDeleteCommand(),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// policies list
// ---------------------------------------------------------------------------

type policiesListOpts struct {
	IO cmdio.Options
}

func (o *policiesListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &policyTableCodec{})
	o.IO.RegisterCustomCodec("wide", &policyTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func (h *tracesHelper) policiesListCommand() *cobra.Command {
	opts := &policiesListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Adaptive Traces sampling policies.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			policies, err := client.ListPolicies(ctx)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), policies)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// NewPolicyTableCodec creates a table codec for policies. Exported for testing.
func NewPolicyTableCodec(wide bool) *policyTableCodec {
	return &policyTableCodec{Wide: wide}
}

type policyTableCodec struct {
	Wide bool
}

func (c *policyTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *policyTableCodec) Encode(w io.Writer, v any) error {
	policies, ok := v.([]Policy)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Policy")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)

	if c.Wide {
		fmt.Fprintln(tw, "ID\tNAME\tTYPE\tEXPIRES AT\tCREATED BY\tCREATED AT")
	} else {
		fmt.Fprintln(tw, "ID\tNAME\tTYPE\tEXPIRES AT")
	}

	for _, p := range policies {
		if c.Wide {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				p.ID, p.Name, p.Type, p.ExpiresAt, p.VersionCreatedBy, p.VersionCreatedAt)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
				p.ID, p.Name, p.Type, p.ExpiresAt)
		}
	}

	return tw.Flush()
}

func (c *policyTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// policies get
// ---------------------------------------------------------------------------

type policiesGetOpts struct {
	IO cmdio.Options
}

func (o *policiesGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func (h *tracesHelper) policiesGetCommand() *cobra.Command {
	opts := &policiesGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get an Adaptive Traces sampling policy by ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			policy, err := client.GetPolicy(ctx, args[0])
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), policy)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// policies create
// ---------------------------------------------------------------------------

type policiesCreateOpts struct {
	IO   cmdio.Options
	File string
}

func (o *policiesCreateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the policy definition (use - for stdin)")
}

func (o *policiesCreateOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return nil
}

func (h *tracesHelper) policiesCreateCommand() *cobra.Command {
	opts := &policiesCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an Adaptive Traces sampling policy from a file.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			policy, err := readPolicyFromFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			created, err := client.CreatePolicy(ctx, policy)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Created policy %q (id=%s)", created.Name, created.ID)
			return opts.IO.Encode(cmd.OutOrStdout(), created)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// policies update
// ---------------------------------------------------------------------------

type policiesUpdateOpts struct {
	IO   cmdio.Options
	File string
}

func (o *policiesUpdateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the policy definition (use - for stdin)")
}

func (o *policiesUpdateOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return nil
}

func (h *tracesHelper) policiesUpdateCommand() *cobra.Command {
	opts := &policiesUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update an Adaptive Traces sampling policy by ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			policy, err := readPolicyFromFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			updated, err := client.UpdatePolicy(ctx, args[0], policy)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Updated policy %q (id=%s)", updated.Name, updated.ID)
			return opts.IO.Encode(cmd.OutOrStdout(), updated)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// policies delete
// ---------------------------------------------------------------------------

type policiesDeleteOpts struct {
	Force bool
}

func (o *policiesDeleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
}

func (h *tracesHelper) policiesDeleteCommand() *cobra.Command {
	opts := &policiesDeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete <id>...",
		Short: "Delete one or more Adaptive Traces sampling policies.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !opts.Force {
				fmt.Fprintf(cmd.OutOrStdout(), "Delete %d policy(ies)? [y/N] ", len(args))
				reader := bufio.NewReader(cmd.InOrStdin())
				answer, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading confirmation: %w", err)
				}
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					cmdio.Info(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}

			ctx := cmd.Context()

			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			for _, id := range args {
				if err := client.DeletePolicy(ctx, id); err != nil {
					return fmt.Errorf("deleting policy %q: %w", id, err)
				}
				cmdio.Success(cmd.OutOrStdout(), "Deleted policy %q", id)
			}

			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// readPolicyFromFile reads and decodes a Policy from a file path or stdin ("-").
func readPolicyFromFile(filePath string, stdin io.Reader) (*Policy, error) {
	var reader io.Reader
	if filePath == "-" {
		reader = stdin
	} else {
		f, err := os.Open(filePath)
		if err != nil {
			return nil, fmt.Errorf("opening file %s: %w", filePath, err)
		}
		defer f.Close()
		reader = f
	}

	return ReadPolicyFromReader(reader)
}

// ReadPolicyFromReader decodes a Policy from an io.Reader (JSON or YAML). Exported for testing.
func ReadPolicyFromReader(reader io.Reader) (*Policy, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}

	var policy Policy
	if err := json.Unmarshal(data, &policy); err != nil {
		// Try YAML if JSON fails.
		yamlCodec := format.NewYAMLCodec()
		if yamlErr := yamlCodec.Decode(strings.NewReader(string(data)), &policy); yamlErr != nil {
			return nil, fmt.Errorf("decoding input (tried JSON and YAML): json=%w, yaml=%w", err, yamlErr)
		}
	}

	return &policy, nil
}
