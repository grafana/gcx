package integrations

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	cfg, err := loader.LoadGrafanaConfig(cmd.Context())
	if err != nil {
		return nil, err
	}
	base, err := assistanthttp.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return NewClient(base), nil
}

// Commands returns the integrations command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "integrations",
		Short: "Manage Grafana Assistant integrations.",
	}

	cmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newCreateCommand(loader),
		newUpdateCommand(loader),
		newDeleteCommand(loader),
		newValidateCommand(loader),
	)
	return cmd
}

// --- list ---

type listOpts struct {
	IO          cmdio.Options
	Scope       string
	EnabledOnly bool
	Limit       int
	Offset      int
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ListTableCodec{})
	o.IO.RegisterCustomCodec("wide", &ListTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Scope, "scope", "", "Filter by scope (user or tenant)")
	flags.BoolVar(&o.EnabledOnly, "enabled-only", false, "Only return enabled integrations")
	flags.IntVar(&o.Limit, "limit", 20, "Maximum number of integrations to return")
	flags.IntVar(&o.Offset, "offset", 0, "Number of integrations to skip (for pagination)")
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List integrations.",
		Long:  "List integrations with optional scope and status filters.",
		Example: `  gcx assistant integrations list
  gcx assistant integrations list --scope=user
  gcx assistant integrations list --enabled-only --limit=50
  gcx assistant integrations list -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.Scope != "" && opts.Scope != "user" && opts.Scope != "tenant" {
				return fmt.Errorf("--scope must be user or tenant, got %q", opts.Scope)
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			integrations, _, err := client.List(cmd.Context(), ListOptions{
				Scope:       opts.Scope,
				EnabledOnly: opts.EnabledOnly,
				Limit:       opts.Limit,
				Offset:      opts.Offset,
			})
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), integrations)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- get ---

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get integration detail.",
		Long:  "Get the full detail of a specific integration by ID.",
		Example: `  gcx assistant integrations get 550e8400-e29b-41d4-a716-446655440000
  gcx assistant integrations get 550e8400-e29b-41d4-a716-446655440000 -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			integration, err := client.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), integration)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- create ---

type createOpts struct {
	IO            cmdio.Options
	Name          string
	Type          string
	Scope         string
	Enabled       bool
	Applications  []string
	Description   string
	URL           string
	Configuration string
	CustomHeaders []string
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Name, "name", "", "Integration name (required)")
	flags.StringVar(&o.Type, "type", "mcp", "Integration type")
	flags.StringVar(&o.Scope, "scope", "", "Integration scope: user or tenant (required)")
	flags.BoolVar(&o.Enabled, "enabled", true, "Whether the integration is enabled")
	flags.StringSliceVar(&o.Applications, "applications", []string{"all"}, "Target applications (assistant, loop, all)")
	flags.StringVar(&o.Description, "description", "", "Integration description")
	flags.StringVar(&o.URL, "url", "", "MCP server URL (shortcut for --configuration)")
	flags.StringVar(&o.Configuration, "configuration", "", "Integration configuration as JSON")
	flags.StringSliceVar(&o.CustomHeaders, "custom-header", nil, "Custom HTTP headers (key=value, repeatable)")
}

func (o *createOpts) Validate() error {
	if o.Name == "" {
		return errors.New("--name is required")
	}
	if o.Scope == "" {
		return errors.New("--scope is required (user or tenant)")
	}
	if o.Scope != "user" && o.Scope != "tenant" {
		return fmt.Errorf("--scope must be user or tenant, got %q", o.Scope)
	}
	if o.URL != "" && o.Configuration != "" {
		return errors.New("--url and --configuration are mutually exclusive")
	}
	return nil
}

func buildConfiguration(urlFlag, configFlag string) (json.RawMessage, error) {
	if urlFlag != "" {
		cfg := map[string]string{"url": urlFlag}
		data, err := json.Marshal(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to build configuration: %w", err)
		}
		return data, nil
	}
	if configFlag != "" {
		raw := json.RawMessage(configFlag)
		if !json.Valid(raw) {
			return nil, errors.New("--configuration is not valid JSON")
		}
		return raw, nil
	}
	return nil, nil
}

func parseHeaders(headers []string) ([]Header, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	result := make([]Header, 0, len(headers))
	for _, h := range headers {
		key, value, ok := strings.Cut(h, "=")
		if !ok {
			return nil, fmt.Errorf("invalid header format %q, expected key=value", h)
		}
		result = append(result, Header{Key: key, Value: value})
	}
	return result, nil
}

func newCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an integration.",
		Long:  "Create a new integration. Use --url for MCP server integrations or --configuration for custom JSON.",
		Example: `  gcx assistant integrations create --name="my-mcp" --scope=user --url="https://mcp.example.com"
  gcx assistant integrations create --name="team-mcp" --scope=tenant --url="https://mcp.example.com" --applications=assistant
  gcx assistant integrations create --name="custom" --scope=user --configuration='{"url":"https://mcp.example.com","socksProxyEnabled":true}'`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			config, err := buildConfiguration(opts.URL, opts.Configuration)
			if err != nil {
				return err
			}

			headers, err := parseHeaders(opts.CustomHeaders)
			if err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			enabled := opts.Enabled
			integration, err := client.Create(cmd.Context(), CreateRequest{
				Name:          opts.Name,
				Type:          opts.Type,
				Scope:         opts.Scope,
				Enabled:       &enabled,
				Applications:  opts.Applications,
				Description:   opts.Description,
				Configuration: config,
				CustomHeaders: headers,
			})
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), integration)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- update ---

type updateOpts struct {
	IO            cmdio.Options
	Name          string
	Description   string
	Applications  []string
	URL           string
	Configuration string
	CustomHeaders []string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Name, "name", "", "New integration name")
	flags.StringVar(&o.Description, "description", "", "New description")
	// --enabled is not bound to a struct field; read via flags.Changed/GetBool
	// so we can distinguish "not set" from "set to true" (the default).
	flags.Bool("enabled", true, "Whether the integration is enabled")
	flags.StringSliceVar(&o.Applications, "applications", nil, "Target applications (assistant, loop, all)")
	flags.StringVar(&o.URL, "url", "", "MCP server URL (shortcut for --configuration)")
	flags.StringVar(&o.Configuration, "configuration", "", "Integration configuration as JSON")
	flags.StringSliceVar(&o.CustomHeaders, "custom-header", nil, "Custom HTTP headers (key=value, repeatable)")
}

func (o *updateOpts) Validate() error {
	if o.URL != "" && o.Configuration != "" {
		return errors.New("--url and --configuration are mutually exclusive")
	}
	return nil
}

func newUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update an integration.",
		Long:  "Update an existing integration. Fetches the current state and applies changes.",
		Example: `  gcx assistant integrations update 550e8400-e29b-41d4-a716-446655440000 --name="renamed"
  gcx assistant integrations update 550e8400-e29b-41d4-a716-446655440000 --enabled=false
  gcx assistant integrations update 550e8400-e29b-41d4-a716-446655440000 --url="https://new-mcp.example.com"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			// Scope is immutable and required by the PUT endpoint.
			existing, err := client.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			req := UpdateRequest{
				Scope: existing.Scope,
			}

			if cmd.Flags().Changed("name") {
				req.Name = opts.Name
			}
			if cmd.Flags().Changed("description") {
				req.Description = &opts.Description
			}
			if cmd.Flags().Changed("enabled") {
				val, _ := cmd.Flags().GetBool("enabled")
				req.Enabled = &val
			}
			if cmd.Flags().Changed("applications") {
				req.Applications = opts.Applications
			}

			config, err := buildConfiguration(opts.URL, opts.Configuration)
			if err != nil {
				return err
			}
			if config != nil {
				req.Configuration = config
			}

			headers, err := parseHeaders(opts.CustomHeaders)
			if err != nil {
				return err
			}
			if headers != nil {
				for i := range headers {
					headers[i].Modified = true
				}
				req.CustomHeaders = headers
			}

			integration, err := client.Update(cmd.Context(), args[0], req)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), integration)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- delete ---

type deleteOpts struct {
	Force bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
}

func newDeleteCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete an integration.",
		Long:  "Delete an integration by ID. Prompts for confirmation unless --force is passed.",
		Example: `  gcx assistant integrations delete 550e8400-e29b-41d4-a716-446655440000
  gcx assistant integrations delete 550e8400-e29b-41d4-a716-446655440000 --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				fmt.Sprintf("Delete integration %s?", args[0]))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			if err := client.Delete(cmd.Context(), args[0]); err != nil {
				return err
			}
			cmdio.Info(cmd.ErrOrStderr(), "Deleted integration %s", args[0])
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- validate ---

type validateOpts struct {
	IO cmdio.Options
}

func (o *validateOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ValidateTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newValidateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &validateOpts{}
	cmd := &cobra.Command{
		Use:   "validate <id>",
		Short: "Validate an integration.",
		Long:  "Test connectivity for an integration and list discovered MCP tools.",
		Example: `  gcx assistant integrations validate 550e8400-e29b-41d4-a716-446655440000
  gcx assistant integrations validate 550e8400-e29b-41d4-a716-446655440000 -o yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			result, err := client.Validate(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- table codecs ---

// ListTableCodec renders []Integration as a table.
type ListTableCodec struct {
	Wide bool
}

func (c *ListTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *ListTableCodec) Encode(w io.Writer, v any) error {
	integrations, ok := v.([]Integration)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Integration")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "NAME\tID\tTYPE\tSCOPE\tENABLED\tDESCRIPTION\tAPPLICATIONS\tCREATED BY\tMODIFIED")
	} else {
		fmt.Fprintln(tw, "NAME\tID\tTYPE\tSCOPE\tENABLED\tMODIFIED")
	}

	for _, i := range integrations {
		name := truncate(i.Name, 40)
		enabled := formatEnabled(i.Enabled)
		modified := assistanthttp.FormatTime(i.ModifiedAt)

		if c.Wide {
			desc := truncate(i.Description, 30)
			apps := strings.Join(i.Applications, ",")
			if apps == "" {
				apps = "-"
			}
			createdBy := i.CreatedBy
			if createdBy == "" {
				createdBy = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				name, i.ID, i.Type, i.Scope, enabled, desc, apps, createdBy, modified)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				name, i.ID, i.Type, i.Scope, enabled, modified)
		}
	}
	return tw.Flush()
}

func (c *ListTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ValidateTableCodec renders *ValidationResult as a table.
type ValidateTableCodec struct{}

func (c *ValidateTableCodec) Format() format.Format {
	return "table"
}

func (c *ValidateTableCodec) Encode(w io.Writer, v any) error {
	result, ok := v.(*ValidationResult)
	if !ok {
		return errors.New("invalid data type for table codec: expected *ValidationResult")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)

	fmt.Fprintln(tw, "STATUS\tMESSAGE")
	msg := result.Message
	if msg == "" && result.Error != "" {
		msg = result.Error
	}
	if msg == "" {
		msg = "-"
	}
	fmt.Fprintf(tw, "%s\t%s\n", result.Status, msg)

	if err := tw.Flush(); err != nil {
		return err
	}

	if len(result.Tools) > 0 {
		fmt.Fprintln(w)
		tw = tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "TOOL\tDESCRIPTION")
		for _, t := range result.Tools {
			desc := truncate(t.Description, 60)
			fmt.Fprintf(tw, "%s\t%s\n", t.Name, desc)
		}
		return tw.Flush()
	}

	return nil
}

func (c *ValidateTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

func formatEnabled(enabled *bool) string {
	if enabled == nil {
		return "-"
	}
	if *enabled {
		return "Yes"
	}
	return "No"
}

func truncate(s string, maxLen int) string {
	if s == "" {
		return "-"
	}
	if len(s) <= maxLen {
		return s
	}
	r := []rune(s)
	if len(r) > maxLen {
		return string(r[:maxLen-3]) + "..."
	}
	return s
}
