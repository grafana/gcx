package mcpservers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/mcpserver"
	assistantmcp "github.com/grafana/gcx/internal/assistant/mcpservers"
	"github.com/grafana/gcx/internal/deeplink"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"sigs.k8s.io/yaml"
)

var openURL = deeplink.OpenWithStatus //nolint:gochecknoglobals // Test seam for browser-open failure handling.

// newClient builds the MCP-servers client and returns the namespace from the
// resolved Grafana config, so callers needing envelope parity with the
// resources path can build a matching mcpserver.TypedCRUD without
// re-resolving config.
func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*assistantmcp.Client, string, error) {
	cfg, err := loader.LoadGrafanaConfig(cmd.Context())
	if err != nil {
		return nil, "", err
	}
	base, err := assistanthttp.NewClient(cfg)
	if err != nil {
		return nil, "", err
	}
	return assistantmcp.NewClient(base), cfg.Namespace, nil
}

// newCRUDAndClient builds the shared TypedCRUD[MCPServer] the create/update/
// delete commands route their data access through (CONSTITUTION §37-41), plus
// the raw client retained only for the non-CRUD OAuth validate/initiate step.
// Both share one resolved config so no extra round trip is spent.
func newCRUDAndClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*adapter.TypedCRUD[mcpserver.MCPServer], *assistantmcp.Client, error) {
	client, namespace, err := newClient(cmd, loader)
	if err != nil {
		return nil, nil, err
	}
	return mcpserver.NewTypedCRUDForClient(client, namespace), client, nil
}

// resolveServerRef resolves a user-supplied <id-or-name> reference to a single
// server through crud.List, so ref resolution stays on the TypedCRUD path
// rather than the raw client. An exact server-ID match wins outright; otherwise
// the ref is matched against the display name (case-insensitive) or the
// composite metadata.name, and an ambiguous name surfaces as an error listing
// candidates instead of a silent pick.
func resolveServerRef(ctx context.Context, crud *adapter.TypedCRUD[mcpserver.MCPServer], ref string) (*adapter.TypedObject[mcpserver.MCPServer], error) {
	objs, err := crud.List(ctx, 0)
	if err != nil {
		return nil, err
	}
	for i := range objs {
		if objs[i].Spec.ServerID() == ref {
			return &objs[i], nil
		}
	}
	var matches []*adapter.TypedObject[mcpserver.MCPServer]
	for i := range objs {
		o := &objs[i]
		if strings.EqualFold(o.Spec.Name, ref) || o.Name == ref {
			matches = append(matches, o)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("%w: %s", assistantmcp.ErrNotFound, ref)
	case 1:
		return matches[0], nil
	default:
		servers := make([]assistantmcp.Server, 0, len(matches))
		for _, m := range matches {
			servers = append(servers, assistantmcp.Server{ID: m.Spec.ServerID(), Name: m.Spec.Name, Scope: m.Spec.Scope, URL: m.Spec.URL})
		}
		return nil, assistantmcp.AmbiguousReferenceError{Ref: ref, Matches: servers}
	}
}

// findByNaturalKey returns the server matching m's (scope, name, url) natural
// key via crud.List, reporting found=false when none exists. Used by create's
// --if-not-exists idempotent pre-check without leaving the TypedCRUD path.
func findByNaturalKey(ctx context.Context, crud *adapter.TypedCRUD[mcpserver.MCPServer], m mcpserver.MCPServer) (*adapter.TypedObject[mcpserver.MCPServer], bool, error) {
	objs, err := crud.List(ctx, 0)
	if err != nil {
		return nil, false, err
	}
	for i := range objs {
		o := &objs[i]
		if strings.EqualFold(o.Spec.Scope, m.Scope) && strings.EqualFold(o.Spec.Name, m.Name) && o.Spec.URL == m.URL {
			return o, true, nil
		}
	}
	return nil, false, nil
}

// manifestFromInput converts the CLI's ServerInput (built from flags/--file)
// into the MCPServer manifest domain type the TypedCRUD adapter operates on.
// The CLI's --header is inline-value only, so each becomes an overwrite header;
// the manifest's fromEnv/fromFile write intents are reachable only via the
// resources push path, not these commands. A nil input.Headers (no --header
// flags supplied) is preserved as nil so callers can distinguish it from an
// explicit empty list.
func manifestFromInput(input assistantmcp.ServerInput) mcpserver.MCPServer {
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	m := mcpserver.MCPServer{
		Name:         input.Name,
		Description:  input.Description,
		URL:          input.URL,
		Scope:        input.Scope,
		Enabled:      enabled,
		Applications: input.Applications,
		Config:       input.Config,
	}
	if input.Headers != nil {
		m.Headers = manifestHeaders(input.Headers)
	}
	return m
}

// manifestHeaders converts the CLI's inline-value-only ServerInput headers into
// manifest headers; each becomes an overwrite header.
func manifestHeaders(inputs []assistantmcp.Header) []mcpserver.MCPServerHeader {
	headers := make([]mcpserver.MCPServerHeader, 0, len(inputs))
	for _, h := range inputs {
		headers = append(headers, mcpserver.MCPServerHeader{Name: h.Name, Value: h.Value})
	}
	return headers
}

// applyUpdate overlays a partial CLI update onto the current server manifest.
// scope, name, and url form the immutable natural-key identity (ADR-021
// Decision 3), so an attempt to change any of them via update is a clear
// error rather than a silent no-op — delete and recreate to change identity. Headers follow the CLI write-intent model: no --header flags
// (nil) preserves every current header; any --header flags become the full
// desired list.
func applyUpdate(current mcpserver.MCPServer, input assistantmcp.ServerInput) (mcpserver.MCPServer, error) {
	desired := current
	if input.Name != "" && !strings.EqualFold(input.Name, current.Name) {
		return desired, identityChangeError("name", current.Name)
	}
	if input.URL != "" && input.URL != current.URL {
		return desired, identityChangeError("url", current.Name)
	}
	if input.Scope != "" && !strings.EqualFold(input.Scope, current.Scope) {
		return desired, identityChangeError("scope", current.Name)
	}
	if input.Description != "" {
		desired.Description = input.Description
	}
	if input.Enabled != nil {
		desired.Enabled = *input.Enabled
	}
	if len(input.Applications) > 0 {
		desired.Applications = input.Applications
	}
	if len(input.Config) > 0 {
		desired.Config = input.Config
	}
	if input.Headers == nil {
		desired.Headers = current.Headers
	} else {
		desired.Headers = manifestHeaders(input.Headers)
	}
	return desired, nil
}

func identityChangeError(field, name string) error {
	return fmt.Errorf(
		"cannot change %s via update: scope, name, and url are the immutable identity of MCP server %q — delete and recreate it to change them",
		field, name,
	)
}

// displayServer renders a manifest returned by the adapter back into the flat
// Server view create/update/delete emit. Header value presence is best-effort:
// the adapter never carries stored secret values (they are redacted on read),
// so ValueConfigured reflects only whether this invocation supplied a value.
func displayServer(m mcpserver.MCPServer) *assistantmcp.Server {
	headers := make([]assistantmcp.ServerHeader, 0, len(m.Headers))
	for _, h := range m.Headers {
		headers = append(headers, assistantmcp.ServerHeader{
			Name:            h.Name,
			ValueConfigured: h.Value != "" || h.FromEnv != "" || h.FromFile != "",
		})
	}
	return &assistantmcp.Server{
		ID:            m.ServerID(),
		Name:          m.Name,
		Description:   m.Description,
		Type:          assistantmcp.IntegrationTypeMCP,
		Enabled:       m.Enabled,
		Scope:         m.Scope,
		URL:           m.URL,
		Applications:  m.Applications,
		CustomHeaders: headers,
		Configuration: m.Config,
	}
}

// isEnvelopeFormat reports whether the resolved output format must produce
// the exact {apiVersion, kind, metadata, spec} envelope gcx resources
// get/pull emits, so gcx assistant mcp-servers get/list and gcx resources
// get mcpservers are byte-identical for JSON and YAML. The agents format is
// a JSON-family format (it is the agent-mode default), so it carries the
// same envelope — a machine consumer must see one shape regardless of
// whether it asked with -o json or inherited the agents default.
// Table/wide/text output keeps the flat, human-friendly Server view.
func isEnvelopeFormat(f string) bool {
	return f == string(format.JSON) || f == string(format.YAML) || f == "agents"
}

// envelopeList is the JSON/YAML shape gcx resources get emits for a
// multi-item result (cmd/gcx/resources.printItems) -- an "items" array of
// bare envelope maps, never a bare array.
type envelopeList struct {
	Items []map[string]any `json:"items" yaml:"items"`
}

// mcpServerEnvelopes converts already-fetched client Servers into the same
// unstructured {apiVersion, kind, metadata, spec} envelope maps the adapter
// produces, without any extra network calls.
func mcpServerEnvelopes(client *assistantmcp.Client, namespace string, servers []assistantmcp.Server) ([]map[string]any, error) {
	crud := mcpserver.NewTypedCRUDForClient(client, namespace)
	items := make([]map[string]any, 0, len(servers))
	for _, s := range servers {
		u, err := crud.ToUnstructured(mcpserver.ServerToMCPServer(s))
		if err != nil {
			return nil, err
		}
		items = append(items, u.Object)
	}
	return items, nil
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
			client, namespace, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			return runList(cmd, client, namespace, opts)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// runList fetches a single bounded page of MCP servers and, when more
// integrations may exist beyond it, prints a STDERR hint. The hint never
// presents the underlying integration total as an MCP-server count -- it
// reads "showing first N -- use --limit for more", never "N of TOTAL",
// because MCP servers are narrowed client-side and the total spans all
// assistant integrations.
//
// For --output json/yaml, the page is rendered as the same {"items": [...]}
// envelope shape gcx resources get mcpservers emits instead of the flat
// Server view; other formats are unaffected.
func runList(cmd *cobra.Command, client *assistantmcp.Client, namespace string, opts *listOpts) error {
	result, err := client.ListBounded(cmd.Context(), assistantmcp.ListOptions{Limit: opts.Limit, Offset: opts.Offset})
	if err != nil {
		return err
	}
	if result.HasMore {
		cmdio.EmitHint(cmd.ErrOrStderr(), fmt.Sprintf("showing first %d — use --limit for more", result.Limit), "")
	}
	if isEnvelopeFormat(opts.IO.OutputFormat) {
		items, err := mcpServerEnvelopes(client, namespace, result.Servers)
		if err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), envelopeList{Items: items})
	}
	return opts.IO.Encode(cmd.OutOrStdout(), result.Servers)
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
			client, namespace, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			return runGet(cmd, client, namespace, opts, args[0])
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// runGet resolves ref (an ID or human name, per client.Get) to the underlying
// server. For --output json/yaml, the result is rendered as the same
// {apiVersion, kind, metadata, spec} envelope gcx resources get mcpservers/
// <name> emits instead of the flat Server view; other formats are
// unaffected.
func runGet(cmd *cobra.Command, client *assistantmcp.Client, namespace string, opts *getOpts, ref string) error {
	server, err := client.Get(cmd.Context(), ref)
	if err != nil {
		return err
	}
	if isEnvelopeFormat(opts.IO.OutputFormat) {
		crud := mcpserver.NewTypedCRUDForClient(client, namespace)
		u, err := crud.ToUnstructured(mcpserver.ServerToMCPServer(*server))
		if err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), u.Object)
	}
	return opts.IO.Encode(cmd.OutOrStdout(), server)
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
			crud, client, err := newCRUDAndClient(cmd, loader)
			if err != nil {
				return err
			}

			manifest := manifestFromInput(input)
			if manifest.Scope == "" {
				manifest.Scope = "user"
			}
			return runCreate(cmd, crud, client, opts, manifest)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// runCreate runs the natural-key existence check unconditionally before
// deciding whether to create. Unlike the adapter's CreateFn (which upserts on a
// natural-key match so gcx resources push stays idempotent), the human create
// command must FAIL on an existing (scope, name, url) match — routing a bare
// create into the adapter's upsert would silently strip stored headers absent
// from the CLI input. --if-not-exists opts into an idempotent no-op instead
// of the error.
func runCreate(cmd *cobra.Command, crud *adapter.TypedCRUD[mcpserver.MCPServer], client *assistantmcp.Client, opts *createOpts, manifest mcpserver.MCPServer) error {
	existing, found, err := findByNaturalKey(cmd.Context(), crud, manifest)
	if err != nil {
		return err
	}
	if found {
		if opts.IfNotExists {
			result := &assistantmcp.MutationResult{Operation: "unchanged", Server: displayServer(existing.Spec)}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		}
		return fmt.Errorf(
			"MCP server %q (scope %q) already exists at %s; use `gcx assistant mcp-servers update` to modify it, or --if-not-exists to no-op",
			manifest.Name, manifest.Scope, manifest.URL,
		)
	}

	obj := &adapter.TypedObject[mcpserver.MCPServer]{Spec: manifest}
	obj.SetName(manifest.GetResourceName())
	created, err := crud.Create(cmd.Context(), obj)
	if err != nil {
		return err
	}

	result := &assistantmcp.MutationResult{Operation: "created", Server: displayServer(created.Spec)}
	return finishMutation(cmd, client, &opts.IO, result)
}

// finishMutation completes a create/update after the mutation has already
// persisted: the best-effort OAuth requirement check (attach the auth URL,
// then open it or hint at it), the result document, and the exit code. An
// OAuth-step failure must not suppress the result document or masquerade as
// a failed mutation — previously it did both, exiting non-zero with nothing
// on stdout although the server was created/updated. The failure summary is
// carried in-band on the result document's `error` field (a consumer reading
// only stdout must see why the exit code is ExitPartialFailure, and the
// authUrl stays present when it is known); the document is emitted, the same
// summary goes to stderr as a typed warning, and the exit code travels via
// EmittedError, saying "the mutation succeeded but a follow-up step did not"
// without a second stdout document.
func finishMutation(cmd *cobra.Command, client *assistantmcp.Client, io *cmdio.Options, result *assistantmcp.MutationResult) error {
	authErr := maybeAttachAuthURL(cmd, client, result)
	if authErr == nil {
		maybeOpenAuthURL(cmd, result)
	} else {
		result.Error = fmt.Sprintf(
			"MCP server %s, but the OAuth requirement check failed: %v — if the server needs OAuth, authorize it in Grafana's Assistant settings",
			result.Operation, authErr)
	}
	if err := io.Encode(cmd.OutOrStdout(), result); err != nil {
		return err
	}
	if authErr != nil {
		cmdio.EmitWarn(cmd.ErrOrStderr(), result.Error)
		return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, authErr)
	}
	return nil
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

Partial updates are merged with the current server before saving. Scope, name,
and url are the server's immutable identity: they cannot be changed via update —
delete and recreate the server to change them. Headers follow an explicit
write-intent model: a --header with a value overwrites (or creates) that header;
if you pass no --header flags at all, every existing header is preserved
unchanged; but once you pass any --header flags, they become the full desired
header list, so any existing header you don't list is removed.`,
		Example: `  gcx assistant mcp-servers update GitHub --disabled
  gcx assistant mcp-servers update SharedTools --description "Shared internal MCP tools"
  gcx assistant mcp-servers update SharedTools --header "X-API-Key=<token>"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			input, err := opts.buildInput()
			if err != nil {
				return err
			}
			crud, client, err := newCRUDAndClient(cmd, loader)
			if err != nil {
				return err
			}
			return runUpdate(cmd, crud, client, opts, args[0], input)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// runUpdate resolves the ref, overlays the partial CLI update onto the
// current manifest, persists it through the TypedCRUD path, and finishes via
// finishMutation (result document first, OAuth-step failure as partial
// failure).
func runUpdate(cmd *cobra.Command, crud *adapter.TypedCRUD[mcpserver.MCPServer], client *assistantmcp.Client, opts *updateOpts, ref string, input assistantmcp.ServerInput) error {
	current, err := resolveServerRef(cmd.Context(), crud, ref)
	if err != nil {
		return err
	}
	desired, err := applyUpdate(current.Spec, input)
	if err != nil {
		return err
	}

	obj := &adapter.TypedObject[mcpserver.MCPServer]{Spec: desired}
	obj.SetName(desired.GetResourceName())
	updated, err := crud.Update(cmd.Context(), desired.GetResourceName(), obj)
	if err != nil {
		return err
	}

	result := &assistantmcp.MutationResult{Operation: "updated", Server: displayServer(updated.Spec)}
	return finishMutation(cmd, client, &opts.IO, result)
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
				// Declining the prompt must not look like success: exit 0
				// with empty stdout is indistinguishable from a completed
				// delete for a piping consumer. Follow the cancelled-exit
				// convention (ExitCancelled) with an explicit summary.
				return cancelledDeleteError(args[0])
			}
			crud, client, err := newCRUDAndClient(cmd, loader)
			if err != nil {
				return err
			}
			return runDelete(cmd, crud, client, opts, args[0])
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// cancelledDeleteError reports a declined confirmation prompt as an explicit
// cancellation (exit ExitCancelled), matching the IRM cancelled-exit
// convention, instead of the old silent exit 0 with empty stdout.
func cancelledDeleteError(ref string) error {
	exitCancelled := gcxerrors.ExitCancelled
	return &gcxerrors.DetailedError{
		Summary:  "delete cancelled",
		Details:  fmt.Sprintf("Confirmation prompt was declined; MCP server %q was not deleted.", ref),
		ExitCode: &exitCancelled,
	}
}

// runDelete resolves the <id-or-name> ref to a single server via
// resolveServerRef (which disambiguates past any (scope, name) collision using
// the exact server ID) and then deletes by that resolved server ID. It must NOT
// route back through crud.Delete(current.Name): the composite metadata.name
// ({scope}-{slug(name)}) is not unique when servers collide on (scope, name),
// so the adapter's name-based DeleteFn would re-hit the same ambiguity
// resolveServerRef already resolved past — failing a delete the ID uniquely
// identified. The generic name-based collision detection stays for
// gcx resources delete mcpservers/<name>; only this CLI path changes.
func runDelete(cmd *cobra.Command, crud *adapter.TypedCRUD[mcpserver.MCPServer], client *assistantmcp.Client, opts *deleteOpts, ref string) error {
	current, err := resolveServerRef(cmd.Context(), crud, ref)
	if err != nil {
		return err
	}
	if _, err := client.Delete(cmd.Context(), current.Spec.ServerID()); err != nil {
		return err
	}
	result := &assistantmcp.MutationResult{Operation: "deleted", Server: displayServer(current.Spec)}
	return opts.IO.Encode(cmd.OutOrStdout(), result)
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

// maybeOpenAuthURL delivers the OAuth authorization URL to the consumer. The
// browser open routes through the deeplink guard (internal/deeplink.Open):
// that single shared guard skips the launch in agent mode and emits the typed
// stderr hint itself — no bespoke agent-mode branch here. Machine consumers
// always receive the URL in-band via the stdout result document's authUrl
// field regardless; the reported open status only keeps the human stderr
// guidance accurate.
func maybeOpenAuthURL(cmd *cobra.Command, result *assistantmcp.MutationResult) {
	if result == nil || result.AuthURL == "" {
		return
	}
	opened, err := openURL(result.AuthURL)
	switch {
	case err != nil:
		cmdio.Warning(cmd.ErrOrStderr(), "Could not open browser: %v", err)
		cmdio.Info(cmd.ErrOrStderr(), "Open the OAuth authorization URL manually: %s", result.AuthURL)
	case opened:
		cmdio.Info(cmd.ErrOrStderr(), "Opening OAuth authorization URL: %s", result.AuthURL)
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
