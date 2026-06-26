// Package login implements `gcx aio11y login`, which provisions the shared
// sigil dotenv (~/.config/sigil/config.env) from the current gcx Grafana Cloud
// context. It resolves the Sigil API endpoint, OTLP gateway endpoint, and
// tenant ID from GCOM and — by default — mints a dedicated, correctly-scoped
// Cloud Access Policy token so coding-agent plugins (Cursor, Claude Code, …)
// can send telemetry to Grafana AI Observability with a single command.
package login

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// contentCaptureModes are the values accepted by the sigil plugins for
// SIGIL_CONTENT_CAPTURE_MODE.
//
//nolint:gochecknoglobals // constant-like lookup table.
var contentCaptureModes = []string{
	"metadata_only",
	"no_tool_content",
	"full",
	"full_with_metadata_spans",
}

// sigilScopes are the Cloud Access Policy scopes the SIGIL_AUTH_TOKEN must
// carry for AI Observability export to work. Also the scopes gcx requests when
// provisioning a token automatically.
//
//nolint:gochecknoglobals // constant-like lookup table.
var sigilScopes = []string{"sigil:write", "metrics:write", "traces:write"}

// Token source labels (also surfaced in JSON output).
const (
	sourceProvisioned  = "provisioned"
	sourceCloudContext = "cloud-context"
	sourceFlag         = "flag"
	sourceDryRun       = "provision (dry-run)"
)

type options struct {
	IO                 cmdio.Options
	SigilEndpoint      string
	OTLPEndpoint       string
	Token              string
	NoProvision        bool
	TokenName          string
	TokenExpiry        string
	ContentCaptureMode string
	ConfigPath         string
	DryRun             bool
}

func (o *options) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &textCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)

	flags.StringVar(&o.Token, "token", "", "Use this token as SIGIL_AUTH_TOKEN verbatim (skips automatic provisioning)")
	flags.BoolVar(&o.NoProvision, "no-provision", false, "Don't mint a dedicated token; write the context's Cloud Access Policy token instead")
	flags.StringVar(&o.TokenName, "token-name", "", "Name for the provisioned token (default: sigil-<stack-slug>)")
	flags.StringVar(&o.TokenExpiry, "token-expiry", "", "Expiry for the provisioned token (RFC3339, e.g. 2027-01-01T00:00:00Z); default: no expiry")
	flags.StringVar(&o.ContentCaptureMode, "content-capture-mode", "", "Set SIGIL_CONTENT_CAPTURE_MODE. One of: "+joinStrings(contentCaptureModes))
	flags.StringVar(&o.SigilEndpoint, "sigil-endpoint", "", "Override the Sigil API endpoint (SIGIL_ENDPOINT) instead of reading it from GCOM")
	flags.StringVar(&o.OTLPEndpoint, "otlp-endpoint", "", "Override the OTLP gateway endpoint (SIGIL_OTEL_EXPORTER_OTLP_ENDPOINT) instead of reading it from GCOM")
	flags.StringVar(&o.ConfigPath, "config-path", "", "Override the sigil config.env path (default: $XDG_CONFIG_HOME/sigil/config.env)")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Resolve and print the configuration without minting a token or writing config.env")
}

func (o *options) validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if o.ContentCaptureMode != "" && !slices.Contains(contentCaptureModes, o.ContentCaptureMode) {
		return fmt.Errorf("invalid --content-capture-mode %q; valid values: %s", o.ContentCaptureMode, joinStrings(contentCaptureModes))
	}
	if o.TokenExpiry != "" {
		if _, err := time.Parse(time.RFC3339, o.TokenExpiry); err != nil {
			return fmt.Errorf("invalid --token-expiry %q (want RFC3339, e.g. 2027-01-01T00:00:00Z): %w", o.TokenExpiry, err)
		}
	}
	if o.Token != "" && o.NoProvision {
		return errors.New("--token and --no-provision are mutually exclusive")
	}
	return nil
}

// Command returns the `aio11y login` command.
func Command(loader *providers.ConfigLoader) *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Provision sigil credentials for coding-agent plugins from the current context.",
		Long: `Provision the shared sigil credentials file (~/.config/sigil/config.env) used by
the Grafana AI Observability coding-agent plugins (Cursor, Claude Code, Codex,
Copilot, OpenCode, pi).

Endpoints and the tenant ID are resolved from the current gcx Grafana Cloud
context:
  - SIGIL_ENDPOINT                    the stack's GCOM regionSigilUrl
  - SIGIL_AUTH_TENANT_ID              the Grafana Cloud stack instance ID
  - SIGIL_OTEL_EXPORTER_OTLP_ENDPOINT the GCOM OTLP gateway endpoint

By default gcx mints a dedicated Cloud Access Policy token scoped to
` + "`sigil:write`, `metrics:write`, `traces:write`" + ` and writes it as
SIGIL_AUTH_TOKEN. This requires the context's cloud token to have
` + "`accesspolicies:write`" + `. If it doesn't, gcx falls back to writing the
context token directly and tells you how to create a scoped one.

Re-running reuses the access policy and rotates the token. Existing optional
keys in config.env (e.g. SIGIL_TAGS) are preserved.`,
		Example: `  gcx aio11y login
  gcx aio11y login --content-capture-mode full
  gcx aio11y login --no-provision          # write the context token as-is
  gcx aio11y login --token glc_xxx         # use a token you created yourself
  gcx aio11y login --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.validate(); err != nil {
				return err
			}
			return run(cmd, opts, loader)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func run(cmd *cobra.Command, opts *options, loader *providers.ConfigLoader) error {
	ctx := cmd.Context()
	ew := cmd.ErrOrStderr()

	cloudCfg, conns, client, err := loader.LoadCloudConnections(ctx)
	if err != nil {
		return err
	}

	otlpEndpoint := opts.OTLPEndpoint
	if otlpEndpoint == "" {
		otlpEndpoint = conns.OtlpHTTPURL
	}
	if otlpEndpoint == "" {
		return errors.New("GCOM did not return an OTLP gateway endpoint for this stack; pass --otlp-endpoint")
	}

	// Prefer the Sigil endpoint GCOM reports directly; fall back to deriving it
	// from the OTLP gateway region for stacks that don't yet expose the field.
	sigilEndpoint := opts.SigilEndpoint
	if sigilEndpoint == "" {
		sigilEndpoint = cloudCfg.Stack.RegionSigilURL
	}
	if sigilEndpoint == "" {
		sigilEndpoint, err = SigilEndpointFromOTLP(otlpEndpoint)
		if err != nil {
			return fmt.Errorf("GCOM did not report a Sigil endpoint (regionSigilUrl) for this stack and it could not be derived: %w", err)
		}
	}

	tenantID := strconv.Itoa(cloudCfg.Stack.ID)

	token, tokenSource, outcome, err := resolveToken(ctx, ew, opts, cloudCfg, client)
	if err != nil {
		return err
	}

	res := &result{
		ConfigPath:         "",
		Endpoint:           sigilEndpoint,
		TenantID:           tenantID,
		OTLPEndpoint:       otlpEndpoint,
		ContentCaptureMode: opts.ContentCaptureMode,
		TokenConfigured:    token != "",
		TokenSource:        tokenSource,
	}
	if outcome != nil {
		res.AccessPolicy = outcome.PolicyName
		res.TokenName = outcome.TokenName
		res.TokenRotated = outcome.TokenRotated
		res.PolicyReused = outcome.PolicyReused
	}

	path := opts.ConfigPath
	if path == "" {
		path, err = sigilConfigPath()
		if err != nil {
			return err
		}
	}
	res.ConfigPath = path

	if !opts.DryRun {
		updates := map[string]string{
			"SIGIL_ENDPOINT":                    sigilEndpoint,
			"SIGIL_AUTH_TENANT_ID":              tenantID,
			"SIGIL_AUTH_TOKEN":                  token,
			"SIGIL_OTEL_EXPORTER_OTLP_ENDPOINT": otlpEndpoint,
		}
		if opts.ContentCaptureMode != "" {
			updates["SIGIL_CONTENT_CAPTURE_MODE"] = opts.ContentCaptureMode
		}
		if err := WriteSigilConfig(path, updates); err != nil {
			return err
		}
		res.Wrote = true
	}

	if !res.TokenConfigured && !opts.DryRun {
		cmdio.Warning(ew, "No token was written. Provide one with --token or grant the cloud token accesspolicies:write.")
	}

	return opts.IO.Encode(cmd.OutOrStdout(), res)
}

// resolveToken decides which token to write as SIGIL_AUTH_TOKEN and how.
//
//   - --token:        use it verbatim.
//   - --no-provision: use the context's cloud token.
//   - --dry-run:      report the plan without minting (no side effects).
//   - default:        mint a dedicated scoped token via GCOM, falling back to
//     the context token (with guidance) when the cloud token can't provision.
func resolveToken(ctx context.Context, ew io.Writer, opts *options, cloudCfg providers.CloudRESTConfig, client *cloud.GCOMClient) (string, string, *provisionOutcome, error) {
	switch {
	case opts.Token != "":
		return opts.Token, sourceFlag, nil, nil
	case opts.NoProvision:
		return cloudCfg.Token, sourceCloudContext, nil, nil
	case opts.DryRun:
		return "", sourceDryRun, nil, nil
	}

	region := cloudCfg.Stack.RegionSlug
	if region == "" {
		cmdio.Warning(ew, "Could not determine the stack region; skipping token provisioning and using the cloud token as-is.")
		return cloudCfg.Token, sourceCloudContext, nil, nil
	}

	slug := cloudCfg.Stack.Slug
	if slug == "" {
		slug = "stack"
	}
	tokenName := opts.TokenName
	if tokenName == "" {
		tokenName = "sigil-" + slug
	}

	outcome, err := provisionToken(ctx, client, provisionParams{
		Region:      region,
		StackID:     cloudCfg.Stack.ID,
		StackSlug:   slug,
		PolicyName:  "sigil-" + slug,
		TokenName:   tokenName,
		TokenExpiry: opts.TokenExpiry,
		Scopes:      sigilScopes,
	})
	if err == nil {
		return outcome.Token, sourceProvisioned, &outcome, nil
	}
	if isPermissionError(err) {
		// Bootstrap token lacks accesspolicies:write — degrade gracefully so
		// the user still gets a working-or-near-working config, with guidance.
		cmdio.Warning(ew, "Could not mint a dedicated token: the cloud token lacks accesspolicies:write.")
		cmdio.EmitNote(ew, "Wrote your cloud token as SIGIL_AUTH_TOKEN instead — it must have scopes: "+joinStrings(sigilScopes)+".")
		cmdio.EmitNote(ew, "To auto-provision, use a cloud token with accesspolicies:write, or create a scoped token at "+accessPoliciesURL(cloudCfg.Stack.OrgSlug)+" and pass it with --token.")
		return cloudCfg.Token, sourceCloudContext, nil, nil
	}
	return "", "", nil, err
}

// accessPoliciesURL returns the Cloud portal access-policies URL for an org.
func accessPoliciesURL(orgSlug string) string {
	if orgSlug == "" {
		orgSlug = "<your-org>"
	}
	return "https://grafana.com/orgs/" + orgSlug + "/access-policies"
}

func joinStrings(s []string) string {
	return strings.Join(s, ", ")
}

// result is the structured outcome of `aio11y login`. The token secret is
// never included — only how it was sourced.
type result struct {
	ConfigPath         string `json:"config_path" yaml:"config_path"`
	Endpoint           string `json:"endpoint" yaml:"endpoint"`
	TenantID           string `json:"tenant_id" yaml:"tenant_id"`
	OTLPEndpoint       string `json:"otlp_endpoint" yaml:"otlp_endpoint"`
	ContentCaptureMode string `json:"content_capture_mode,omitempty" yaml:"content_capture_mode,omitempty"`
	TokenConfigured    bool   `json:"token_configured" yaml:"token_configured"`
	TokenSource        string `json:"token_source" yaml:"token_source"`
	AccessPolicy       string `json:"access_policy,omitempty" yaml:"access_policy,omitempty"`
	TokenName          string `json:"token_name,omitempty" yaml:"token_name,omitempty"`
	TokenRotated       bool   `json:"token_rotated,omitempty" yaml:"token_rotated,omitempty"`
	PolicyReused       bool   `json:"policy_reused,omitempty" yaml:"policy_reused,omitempty"`
	Wrote              bool   `json:"wrote" yaml:"wrote"`
}

// textCodec renders the human-facing summary for `aio11y login`.
type textCodec struct{}

func (c *textCodec) Format() format.Format { return "text" }

func (c *textCodec) Encode(w io.Writer, v any) error {
	res, ok := v.(*result)
	if !ok {
		return errors.New("invalid data type for text codec: expected *result")
	}

	switch {
	case res.Wrote:
		cmdio.Success(w, "Saved sigil credentials to %s", res.ConfigPath)
	default:
		cmdio.Info(w, "Dry run — nothing minted or written. Resolved configuration:")
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "  SIGIL_ENDPOINT                    %s\n", res.Endpoint)
	fmt.Fprintf(w, "  SIGIL_AUTH_TENANT_ID              %s\n", res.TenantID)
	fmt.Fprintf(w, "  SIGIL_OTEL_EXPORTER_OTLP_ENDPOINT %s\n", res.OTLPEndpoint)
	if res.ContentCaptureMode != "" {
		fmt.Fprintf(w, "  SIGIL_CONTENT_CAPTURE_MODE        %s\n", res.ContentCaptureMode)
	}
	fmt.Fprintf(w, "  SIGIL_AUTH_TOKEN                  %s\n", tokenDescription(res))

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Cursor setup:")
	fmt.Fprintln(w, "  /add-plugin grafana/sigil-sdk")
	return nil
}

func (c *textCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}

// tokenDescription renders a human-readable summary of how the token was sourced.
func tokenDescription(res *result) string {
	switch res.TokenSource {
	case sourceProvisioned:
		verb := "minted"
		if res.TokenRotated {
			verb = "rotated"
		}
		policyState := "created"
		if res.PolicyReused {
			policyState = "reused"
		}
		return fmt.Sprintf("(%s token %q under %s access policy %q)", verb, res.TokenName, policyState, res.AccessPolicy)
	case sourceFlag:
		return "(set from --token)"
	case sourceCloudContext:
		return "(set from the context's cloud token)"
	case sourceDryRun:
		return "(would mint a dedicated access-policy token)"
	default:
		return "(missing)"
	}
}
