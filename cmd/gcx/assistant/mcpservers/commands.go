package mcpservers

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	assistantmcp "github.com/grafana/gcx/internal/assistant/mcpservers"
	"github.com/grafana/gcx/internal/deeplink"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"sigs.k8s.io/yaml"
)

var openURL = deeplink.Open //nolint:gochecknoglobals // Test seam for browser-open failure handling.

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*assistantmcp.Client, error) {
	cfg, err := loader.LoadGrafanaConfig(cmd.Context())
	if err != nil {
		return nil, err
	}
	base, err := assistanthttp.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return assistantmcp.NewClient(base), nil
}

func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "mcp-servers",
		Aliases: []string{"mcp-server"},
		Short:   "Manage Assistant MCP server integrations.",
		Long: `Manage remote MCP server integrations in the current Grafana stack's Assistant settings.

MCP servers can be scoped to the current user ("user", shown as "Just me" in
Grafana) or to the stack tenant ("tenant", shown as "Everybody" in Grafana).
Tenant-scoped servers are shared and must be configured with a non-empty
authentication header such as Authorization, X-API-Key, or X-Grafana-API-Key.

OAuth-based MCP servers, such as GitHub Copilot, are user-scoped. When Grafana
reports that OAuth is required after create or update, gcx initiates the
Assistant OAuth flow and opens the authorization URL in a browser.`,
		Example: `  # List configured MCP servers as text table output
  gcx assistant mcp-servers list

  # Add a user-scoped OAuth MCP server and open the authorization URL
  gcx assistant mcp-servers create --name GitHub --url https://api.githubcopilot.com/mcp

  # Add a tenant-scoped header-auth MCP server
  gcx assistant mcp-servers create --name SharedTools --url https://mcp.example.com/mcp \
    --scope tenant --header "Authorization=Bearer <token>"`,
	}
	cmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newCreateCommand(loader),
		newUpdateCommand(loader),
		newDeleteCommand(loader),
	)
	return cmd
}

type listOpts struct {
	IO     cmdio.Options
	Limit  int
	Offset int
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &ListTableCodec{})
	o.IO.RegisterCustomCodec("table", &ListTableCodec{FormatName: "table"})
	o.IO.RegisterCustomCodec("wide", &ListTableCodec{Wide: true})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.Limit, "limit", 50, "Maximum number of integrations to request")
	flags.IntVar(&o.Offset, "offset", 0, "Number of integrations to skip")
}

func (o *listOpts) Validate() error {
	if o.Limit < 0 {
		return errors.New("--limit must be non-negative")
	}
	if o.Offset < 0 {
		return errors.New("--offset must be non-negative")
	}
	return o.IO.Validate()
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Assistant MCP servers.",
		Long: `List Assistant MCP server integrations.

The default output format is text table output. Use --output wide to include
scope and applications, --output table for the legacy table alias, or --output
json, yaml, or agents for machine-readable output.`,
		Example: `  gcx assistant mcp-servers list
  gcx assistant mcp-servers list --output text
  gcx assistant mcp-servers list --output wide
  gcx assistant mcp-servers list --output json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			servers, err := client.List(cmd.Context(), assistantmcp.ListOptions{Limit: opts.Limit, Offset: opts.Offset})
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), servers)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func (o *getOpts) Validate() error {
	return o.IO.Validate()
}

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <id-or-name>",
		Short: "Get an Assistant MCP server.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			server, err := client.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), server)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type createOpts struct {
	inputFlags

	IO          cmdio.Options
	IfNotExists bool
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	o.bind(flags)
	flags.BoolVar(&o.IfNotExists, "if-not-exists", false, "Return an existing server with the same name, URL, and scope instead of failing")
}

func (o *createOpts) Validate() error {
	input, err := o.buildInput()
	if err != nil {
		return err
	}
	if err := input.Validate(true); err != nil {
		return err
	}
	if input.Scope == "tenant" {
		return assistantmcp.ValidateTenantAuthHeaders(input.Headers)
	}
	return nil
}

func newCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an Assistant MCP server.",
		Long: `Create an Assistant MCP server integration.

By default, servers are user-scoped. Use --scope tenant for a shared server.
Tenant-scoped servers require at least one non-empty authentication header, such
as Authorization, X-API-Key, or X-Grafana-API-Key. OAuth-based servers should be
created with user scope; gcx opens the OAuth authorization URL when Grafana
reports that OAuth is required.`,
		Example: `  gcx assistant mcp-servers create --name GitHub --url https://api.githubcopilot.com/mcp

  gcx assistant mcp-servers create --name SharedTools --url https://mcp.example.com/mcp \
    --scope tenant --header "Authorization=Bearer <token>"

  gcx assistant mcp-servers create --file server.yaml --if-not-exists`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := opts.Validate(); err != nil {
				return err
			}
			input, err := opts.buildInput()
			if err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			if opts.IfNotExists {
				result, found, err := existingResult(cmd, client, input)
				if err != nil {
					return err
				}
				if found {
					return opts.IO.Encode(cmd.OutOrStdout(), result)
				}
			}
			result, err := client.Create(cmd.Context(), input)
			if err != nil {
				return err
			}
			if err := maybeAttachAuthURL(cmd, client, result); err != nil {
				return err
			}
			maybeOpenAuthURL(cmd, result)
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type updateOpts struct {
	inputFlags

	IO cmdio.Options
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	o.bind(flags)
}

func (o *updateOpts) Validate() error {
	return o.IO.Validate()
}

func newUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update <id-or-name>",
		Short: "Update an Assistant MCP server.",
		Long: `Update an Assistant MCP server integration.

Partial updates are merged with the current server before saving. Existing
tenant-scoped servers can be updated without re-supplying hidden header values.
Changing a user-scoped server to tenant scope requires a non-empty
authentication header.`,
		Example: `  gcx assistant mcp-servers update GitHub --disabled
  gcx assistant mcp-servers update SharedTools --description "Shared internal MCP tools"
  gcx assistant mcp-servers update LocalTools --scope tenant --header "X-API-Key=<token>"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			input, err := opts.buildInput()
			if err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			result, err := client.Update(cmd.Context(), args[0], input)
			if err != nil {
				return err
			}
			if err := maybeAttachAuthURL(cmd, client, result); err != nil {
				return err
			}
			maybeOpenAuthURL(cmd, result)
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type deleteOpts struct {
	IO    cmdio.Options
	Force bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.BoolVar(&o.Force, "force", false, "Delete without confirmation")
}

func (o *deleteOpts) Validate() error {
	return o.IO.Validate()
}

func newDeleteCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete <id-or-name>",
		Short: "Delete an Assistant MCP server.",
		Long: `Delete an Assistant MCP server integration.

The command prompts for confirmation by default. Use --force to bypass the
prompt. GCX_AUTO_APPROVE also bypasses the prompt for non-interactive workflows,
while agent mode still requires explicit --force for destructive operations.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				fmt.Sprintf("Delete MCP server %q?", args[0]))
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
			result, err := client.Delete(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// inputFlags holds the flags shared by create and update for building a
// ServerInput. Both commands embed it so flag wiring and merge logic live in
// one place.
type inputFlags struct {
	File         string
	Name         string
	Description  string
	URL          string
	Enabled      bool
	Disabled     bool
	Scope        string
	Headers      []string
	Applications []string
}

func (in *inputFlags) bind(flags *pflag.FlagSet) {
	flags.StringVarP(&in.File, "file", "f", "", "Read MCP server input from a YAML or JSON file")
	flags.StringVar(&in.Name, "name", "", "MCP server display name")
	flags.StringVar(&in.Description, "description", "", "MCP server description")
	flags.StringVar(&in.URL, "url", "", "Remote MCP server URL")
	flags.BoolVar(&in.Enabled, "enabled", false, "Enable the MCP server")
	flags.BoolVar(&in.Disabled, "disabled", false, "Disable the MCP server")
	flags.StringVar(&in.Scope, "scope", "", "MCP server scope: user or tenant")
	flags.StringArrayVar(&in.Headers, "header", nil, "Custom header as NAME=VALUE (repeatable; tenant scope requires an auth header)")
	flags.StringArrayVar(&in.Applications, "application", nil, "Assistant application allowed to use this server (repeatable)")
}

func (in *inputFlags) buildInput() (assistantmcp.ServerInput, error) {
	input := assistantmcp.ServerInput{}
	if in.Enabled && in.Disabled {
		return input, errors.New("cannot use both --enabled and --disabled")
	}
	if in.File != "" {
		loaded, err := loadInputFile(in.File)
		if err != nil {
			return input, err
		}
		input = loaded
	}
	if in.Name != "" {
		input.Name = in.Name
	}
	if in.Description != "" {
		input.Description = in.Description
	}
	if in.URL != "" {
		input.URL = in.URL
	}
	if in.Scope != "" {
		input.Scope = in.Scope
	}
	if len(in.Applications) > 0 {
		input.Applications = in.Applications
	}
	if in.Disabled {
		enabled := false
		input.Enabled = &enabled
	} else if in.Enabled {
		enabled := true
		input.Enabled = &enabled
	}
	if len(in.Headers) > 0 {
		headers := make([]assistantmcp.Header, 0, len(in.Headers))
		for _, raw := range in.Headers {
			header, err := assistantmcp.ParseHeader(raw)
			if err != nil {
				return input, err
			}
			headers = append(headers, header)
		}
		input.Headers = headers
	}
	return input, nil
}

func loadInputFile(path string) (assistantmcp.ServerInput, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return assistantmcp.ServerInput{}, fmt.Errorf("failed to read %s: %w", path, err)
	}
	var input assistantmcp.ServerInput
	if err := yaml.Unmarshal(data, &input); err != nil {
		return assistantmcp.ServerInput{}, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	return input, nil
}

func maybeOpenAuthURL(cmd *cobra.Command, result *assistantmcp.MutationResult) {
	if result == nil || result.AuthURL == "" {
		return
	}
	cmdio.Info(cmd.ErrOrStderr(), "Opening OAuth authorization URL: %s", result.AuthURL)
	if err := openURL(result.AuthURL); err != nil {
		cmdio.Warning(cmd.ErrOrStderr(), "Could not open browser: %v", err)
		cmdio.Info(cmd.ErrOrStderr(), "Open the OAuth authorization URL manually: %s", result.AuthURL)
	}
}

func maybeAttachAuthURL(cmd *cobra.Command, client *assistantmcp.Client, result *assistantmcp.MutationResult) error {
	if result == nil || result.Server == nil || result.AuthURL != "" {
		return nil
	}
	validation, err := client.ValidateByID(cmd.Context(), result.Server.ID)
	if err != nil {
		return err
	}
	if validation.Status != assistantmcp.ValidationStatusOAuthRequired {
		return nil
	}
	oauth, err := client.InitiateOAuthByID(cmd.Context(), result.Server.ID, result.Server.Scope)
	if err != nil {
		return err
	}
	result.AuthURL = oauth.AuthURL
	return nil
}

func existingResult(cmd *cobra.Command, client *assistantmcp.Client, input assistantmcp.ServerInput) (*assistantmcp.MutationResult, bool, error) {
	existing, err := client.Find(cmd.Context(), input)
	if err != nil {
		if errors.Is(err, assistantmcp.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}

	// --if-not-exists is an idempotent check: the server already exists and is
	// returned unchanged. Do not validate, initiate OAuth, or open a browser
	// here -- those side effects would surprise automation expecting a no-op.
	// Run an explicit create/update (without --if-not-exists) to (re)trigger
	// the OAuth flow for an existing server.
	result := &assistantmcp.MutationResult{Operation: "unchanged", Server: existing}
	return result, true, nil
}

type ListTableCodec struct {
	Wide       bool
	FormatName format.Format
}

func (c *ListTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	if c.FormatName != "" {
		return c.FormatName
	}
	return "text"
}

func (c *ListTableCodec) Encode(dst io.Writer, value any) error {
	servers, ok := value.([]assistantmcp.Server)
	if !ok {
		return fmt.Errorf("expected []mcpservers.Server, got %T", value)
	}
	headers := []string{"ID", "NAME", "ENABLED", "URL"}
	if c.Wide {
		headers = append(headers, "SCOPE", "APPLICATIONS")
	}
	table := style.NewTable(headers...)
	for _, server := range servers {
		row := []string{server.ID, server.Name, strconv.FormatBool(server.Enabled), server.URL}
		if c.Wide {
			row = append(row, server.Scope, strings.Join(server.Applications, ", "))
		}
		table.Row(row...)
	}
	return table.Render(dst)
}

func (c *ListTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
