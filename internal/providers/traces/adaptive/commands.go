package traces

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	auth "github.com/grafana/gcx/internal/auth/adaptive"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/gcx/internal/terminal"
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
	cmd.AddCommand(h.configCommand())
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

func (h *tracesHelper) newPolicyCRUD(ctx context.Context) (*adapter.TypedCRUD[Policy], error) {
	crud, _, err := NewPolicyTypedCRUD(ctx, h.loader)
	return crud, err
}

// ===========================================================================
// config
// ===========================================================================

func (h *tracesHelper) configCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage Adaptive Traces tenant configuration.",
	}
	cmd.AddCommand(
		h.configGetCommand(),
		h.configUpdateCommand(),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// config get
// ---------------------------------------------------------------------------

type configGetOpts struct {
	IO cmdio.Options
}

func (o *configGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &configTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func (h *tracesHelper) configGetCommand() *cobra.Command {
	opts := &configGetOpts{}
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get Adaptive Traces tenant configuration.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			cfg, err := client.GetConfig(ctx)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), cfg)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// NewConfigTableCodec creates a table codec for config. Exported for testing.
func NewConfigTableCodec() *configTableCodec {
	return &configTableCodec{}
}

type configTableCodec struct{}

func (c *configTableCodec) Format() format.Format { return "table" }

func (c *configTableCodec) Encode(w io.Writer, v any) error {
	cfg, ok := v.(*ReadonlyTenantConfig)
	if !ok {
		return errors.New("invalid data type for table codec: expected *ReadonlyTenantConfig")
	}

	t := style.NewTable("SETTING", "VALUE")
	t.Row("disable_anomaly_policies", strconv.FormatBool(cfg.DisableAnomalyPolicies))
	t.Row("span_name_semconv_transform_enabled", strconv.FormatBool(cfg.SpanNameSemconvTransformEnabled))
	t.Row("span_name_semconv_version", cfg.SpanNameSemconvVersion)
	if cfg.AnomalyRateLimitBytesPerSec > 0 {
		t.Row("anomaly_rate_limit_bytes_per_sec", strconv.FormatFloat(cfg.AnomalyRateLimitBytesPerSec, 'f', -1, 64))
	}

	return t.Render(w)
}

func (c *configTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// config update
// ---------------------------------------------------------------------------

type configUpdateOpts struct {
	IO   cmdio.Options
	File string
}

func (o *configUpdateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the config definition (use - for stdin)")
}

func (o *configUpdateOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return nil
}

func (h *tracesHelper) configUpdateCommand() *cobra.Command {
	opts := &configUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update Adaptive Traces tenant configuration.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := readConfigFromFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			updated, err := client.UpdateConfig(ctx, cfg)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Updated tenant configuration")
			return opts.IO.Encode(cmd.OutOrStdout(), updated)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// readConfigFromFile reads and decodes a TenantConfig from a file path or stdin ("-").
func readConfigFromFile(filePath string, stdin io.Reader) (*TenantConfig, error) {
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

	data, err := io.ReadAll(io.LimitReader(reader, maxPolicyFileSize))
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}

	var cfg TenantConfig
	yamlCodec := format.NewYAMLCodec()
	if err := yamlCodec.Decode(strings.NewReader(string(data)), &cfg); err != nil {
		return nil, fmt.Errorf("decoding input: %w", err)
	}

	return &cfg, nil
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

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "MESSAGE", "TAGS", "APPLIED", "DISMISSED", "STALE", "CREATED AT", "ACTIONS")
	} else {
		t = style.NewTable("ID", "MESSAGE", "TAGS", "APPLIED", "DISMISSED", "STALE", "CREATED AT")
	}

	for _, r := range recs {
		tags := strings.Join(r.Tags, ",")
		if c.Wide {
			t.Row(r.ID, r.Message, tags, strconv.FormatBool(r.Applied), strconv.FormatBool(r.Dismissed), strconv.FormatBool(r.Stale), r.CreatedAt, strconv.Itoa(len(r.Actions)))
		} else {
			t.Row(r.ID, r.Message, tags, strconv.FormatBool(r.Applied), strconv.FormatBool(r.Dismissed), strconv.FormatBool(r.Stale), r.CreatedAt)
		}
	}

	return t.Render(w)
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
				cmdio.Info(cmd.ErrOrStderr(), "[dry-run] Would apply recommendation %q", id)
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

			cmdio.Success(cmd.ErrOrStderr(), "Applied recommendation %q", id)
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
				cmdio.Info(cmd.ErrOrStderr(), "[dry-run] Would dismiss recommendation %q", id)
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

			cmdio.Success(cmd.ErrOrStderr(), "Dismissed recommendation %q", id)
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
	IO    cmdio.Options
	Limit int64
}

func (o *policiesListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &policyTableCodec{})
	o.IO.RegisterCustomCodec("wide", &policyTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of policies to return (0 for no limit)")
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

			crud, err := h.newPolicyCRUD(ctx)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx, opts.Limit)
			if err != nil {
				return err
			}

			policies := make([]Policy, len(typedObjs))
			for i := range typedObjs {
				policies[i] = typedObjs[i].Spec
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

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "NAME", "TYPE", "EXPIRES AT", "CREATED BY", "CREATED AT")
	} else {
		t = style.NewTable("ID", "NAME", "TYPE", "EXPIRES AT")
	}

	for _, p := range policies {
		if c.Wide {
			t.Row(p.ID, p.Name, p.Type, p.ExpiresAt, p.VersionCreatedBy, p.VersionCreatedAt)
		} else {
			t.Row(p.ID, p.Name, p.Type, p.ExpiresAt)
		}
	}

	return t.Render(w)
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

			crud, err := h.newPolicyCRUD(ctx)
			if err != nil {
				return err
			}

			typedObj, err := crud.Get(ctx, args[0])
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), &typedObj.Spec)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// policies create / update (shared opts)
// ---------------------------------------------------------------------------

type policyFileOpts struct {
	IO   cmdio.Options
	File string
}

func (o *policyFileOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the policy definition (use - for stdin)")
}

func (o *policyFileOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return nil
}

func (h *tracesHelper) policiesCreateCommand() *cobra.Command {
	opts := &policyFileOpts{}
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

			crud, err := h.newPolicyCRUD(ctx)
			if err != nil {
				return err
			}

			typedObj := &adapter.TypedObject[Policy]{Spec: *policy}
			created, err := crud.Create(ctx, typedObj)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Created policy %q (id=%s)", created.Spec.Name, created.Spec.ID)
			return opts.IO.Encode(cmd.OutOrStdout(), &created.Spec)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func (h *tracesHelper) policiesUpdateCommand() *cobra.Command {
	opts := &policyFileOpts{}
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

			crud, err := h.newPolicyCRUD(ctx)
			if err != nil {
				return err
			}

			typedObj := &adapter.TypedObject[Policy]{Spec: *policy}
			updated, err := crud.Update(ctx, args[0], typedObj)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Updated policy %q (id=%s)", updated.Spec.Name, updated.Spec.ID)
			return opts.IO.Encode(cmd.OutOrStdout(), &updated.Spec)
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

func (o *policiesDeleteOpts) Validate() error {
	return nil
}

func (h *tracesHelper) policiesDeleteCommand() *cobra.Command {
	opts := &policiesDeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete <id>...",
		Short: "Delete one or more Adaptive Traces sampling policies.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !opts.Force {
				if terminal.IsPiped() {
					return errors.New("stdin is not a terminal, use --force to skip confirmation")
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Delete %d policy(ies)? [y/N] ", len(args))
				reader := bufio.NewReader(cmd.InOrStdin())
				answer, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading confirmation: %w", err)
				}
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					cmdio.Info(cmd.ErrOrStderr(), "Aborted.")
					return nil
				}
			}

			ctx := cmd.Context()

			crud, err := h.newPolicyCRUD(ctx)
			if err != nil {
				return err
			}

			var errs []error
			for _, id := range args {
				if err := crud.Delete(ctx, id); err != nil {
					errs = append(errs, fmt.Errorf("deleting policy %q: %w", id, err))
				} else {
					cmdio.Success(cmd.ErrOrStderr(), "Deleted policy %q", id)
				}
			}

			return errors.Join(errs...)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// maxPolicyFileSize is the maximum size of a policy file (10 MB).
const maxPolicyFileSize = 10 << 20

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

// ReadPolicyFromReader decodes a Policy from an io.Reader (YAML or JSON). Exported for testing.
func ReadPolicyFromReader(reader io.Reader) (*Policy, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxPolicyFileSize))
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}

	var policy Policy
	yamlCodec := format.NewYAMLCodec()
	if err := yamlCodec.Decode(strings.NewReader(string(data)), &policy); err != nil {
		return nil, fmt.Errorf("decoding input: %w", err)
	}

	return &policy, nil
}
