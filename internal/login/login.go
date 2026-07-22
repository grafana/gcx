package login

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/credentials"
	"github.com/grafana/gcx/internal/httputils"
)

// cloudTokenHint returns the guidance shown on the interactive Cloud Access
// Policy (CAP) token prompt and in the ErrNeedInput hint: where to create a
// token, which scopes to grant, and the skip affordance (issue #820).
//
// When the stack server URL is known (always, at this prompt) it deep-links to
// the in-stack Access Policies app at <server>/a/grafana-auth-app. Otherwise it
// falls back to the org-level grafana.com page with a <your-org> placeholder
// (the org slug is not resolvable here — resolving it needs the very token we
// are asking for).
func cloudTokenHint(server string) string {
	create := "Create one at https://grafana.com/orgs/<your-org>/access-policies"
	if server != "" {
		create = "Create one at " + strings.TrimRight(server, "/") + "/a/grafana-auth-app"
	}
	return create + " (Access Policies → Create access policy).\n" +
		"Recommended scopes: stacks:read (required — resolves your stack). Then add per product:\n" +
		"  metrics:write, logs:write, traces:write — Synthetic Monitoring\n" +
		"  fleet-management:read — Fleet\n" +
		"  stacks:write — create or update stacks\n" +
		"Docs: https://grafana.com/docs/grafana-cloud/security-and-account-management/authentication-and-permissions/access-policies/create-access-policies\n" +
		"Press Enter to skip (Cloud management features will be unavailable)."
}

// Target identifies whether the login destination is Grafana Cloud or on-premises.
type Target int

const (
	TargetUnknown Target = iota
	TargetCloud
	TargetOnPrem
)

// Inputs carries the user-facing values that shape a login: server URL,
// target classification, authentication tokens, context name, and UX flags.
// All fields are directly populated from CLI flags or interactive prompts;
// none carry internal state or injection hooks.
type Inputs struct {
	Server       string
	ContextName  string
	Target       Target
	GrafanaToken string
	// ExistingGrafanaAuthMethod records a previously persisted explicit auth
	// method for pre-auth request safety. It never supplies a credential or
	// selects the final login method; it only prevents target detection from
	// presenting a stale client certificate when the existing method is known
	// to be token, OAuth, or Basic.
	ExistingGrafanaAuthMethod string
	CloudToken                string
	CloudAPIURL               string
	CloudOAuthURL             string
	// CloudCredentialKind controls which CloudEntry field receives CloudToken.
	// It is deliberately independent from CloudTokenTrusted: credential type and
	// validation policy are separate concerns. The zero value means CAP so
	// existing programmatic callers that only set CloudToken keep their behavior.
	CloudCredentialKind CloudCredentialKind
	// CloudTokenTrusted skips the optional GCOM stack validation. It is set for
	// credentials obtained from OAuth and for an existing credential the user
	// explicitly chose to keep. Freshly pasted CAP tokens leave it false.
	CloudTokenTrusted bool
	// OAuth-only metadata. These fields are ignored for CAP credentials.
	CloudOAuthTokenExpiresAt string
	CloudOAuthScopes         []string
	OrgID                    int
	UseOAuth                 bool
	// OAuthCallbackPort fixes the local port for the OAuth callback server.
	// Zero means auto-pick from the default range. Useful when only specific
	// ports are forwarded between a remote dev host and the user's browser.
	OAuthCallbackPort int
	Yes               bool
	// UseCloudInstanceSelector is only used internally to mark the case in which
	// a user explicitly left the server empty to be directed to the cloud
	// instance selector
	UseCloudInstanceSelector bool

	// TLS carries client-side TLS settings (mTLS cert/key, custom CA).
	// When non-nil, these settings are used for target detection, connectivity
	// validation, and persisted into the new/updated context.
	// On re-auth of an existing context, the CLI pre-populates this from the
	// stored grafana.tls.* block so mTLS keeps working without re-specifying certs.
	TLS *config.TLS
	// PreserveStoredTLS keeps process-environment TLS overrides runtime-only.
	// Detection and validation use TLS, while persistence restores StoredTLS.
	// A token/OAuth login fails before network use when that would create a
	// credential that the next invocation cannot resolve. Programmatic callers
	// retain the historical behavior unless they opt in.
	PreserveStoredTLS bool
	StoredTLS         *config.TLS
	// PreserveStoredProxyEndpoint is the proxy equivalent for an ephemeral
	// GRAFANA_PROXY_ENDPOINT override. RuntimeProxyEndpoint is the effective
	// override and StoredProxyEndpoint is the durable value. Bearer logins fail
	// closed when the two destinations differ; non-bearer mTLS remains usable
	// with runtime-only transport settings.
	PreserveStoredProxyEndpoint bool
	RuntimeProxyEndpoint        string
	StoredProxyEndpoint         string

	// Writer receives human-facing OAuth progress output. When nil, the
	// internal/login package discards writes (NC-001: the package is UI-free
	// and never touches os.Stderr on its own). CLI callers should pass
	// cmd.ErrOrStderr().
	Writer io.Writer
}

// Hooks carries injection seams that decouple Run from filesystem,
// network, and browser side effects. Each hook has a safe default behaviour
// when left nil (real config, live HTTP detection, real connectivity check).
// Tests supply stubs to exercise Run deterministically.
type Hooks struct {
	// ConfigSource determines where the config file is read from and
	// written to. Nil falls back to config.StandardLocation().
	ConfigSource config.Source

	// CloudMutationSafety carries shared-reference evidence from a complete
	// layered read into the selected raw-owner write. Without it, reloading one
	// file can undercount contexts in other layers and mutate their shared Cloud
	// credential in place.
	CloudMutationSafety config.CloudMutationSafety

	// LoginMutationGuard pins the selected raw owner's pre-auth revision so the
	// persistence reload cannot silently adopt a context changed during OAuth or
	// connectivity validation.
	LoginMutationGuard config.LoginMutationGuard

	// NewAuthFlow constructs the OAuth PKCE flow. Must be non-nil when
	// UseOAuth is true; otherwise Run returns an error. Callers typically
	// pass a factory that wraps auth.NewFlow.
	NewAuthFlow func(server string, opts auth.Options) AuthFlow

	// NewCloudAuthFlow constructs the direct GCOM OAuth PKCE flow used by the
	// optional Cloud follow-up. Nil selects auth.NewGCOMFlow. The seam keeps the
	// command path deterministic in tests without putting browser logic in cmd/.
	NewCloudAuthFlow func(opts auth.GCOMOptions) CloudAuthFlow

	// ValidateFn overrides connectivity validation for testing.
	// Returns the Grafana version string on success. When nil, the real
	// Validate() is used.
	ValidateFn func(ctx context.Context, opts Options, restCfg config.NamespacedRESTConfig) (string, error)

	// DetectFn overrides target detection for testing. When nil,
	// DetectTarget is called with a TLS-aware HTTP client (built from
	// opts.TLS) or a default client when no TLS is configured.
	DetectFn func(ctx context.Context, server string) (Target, error)
}

// RetryState carries plumbing used by the CLI layer when Run returns a
// sentinel (ErrNeedInput / ErrNeedClarification) and is re-invoked after
// the caller resolves the missing value. These fields are never set on
// the first invocation and should be treated as internal protocol between
// Run and its retry-loop caller.
type RetryState struct {
	// StagedContext carries partially-resolved state across sentinel
	// retries. The CLI allocates it once as &config.Context{} before the
	// Run() retry loop; Run() populates StagedContext.Grafana and
	// StagedContext.Cloud as steps complete. On subsequent Run() calls,
	// already-populated fields are reused instead of re-running the
	// underlying step (e.g. OAuth).
	//
	// Safe to leave nil — Run() works without it (but sentinels will
	// re-run earlier steps on retry).
	StagedContext *config.Context

	// AllowOverride, when true, bypasses the server-mismatch guard in
	// persistContext. Set by the CLI after the user confirms via an
	// ErrNeedClarification{Field: "allow-override"} interactive prompt, or
	// when the caller passes --allow-server-override. --yes alone does NOT
	// set this; server-identity changes require an explicit opt-in.
	AllowOverride bool

	// ForceSave, when true, bypasses connectivity validation and persists
	// the context anyway. Set by the CLI after the user confirms via an
	// ErrNeedClarification{Field: "save-unvalidated"} prompt. Intended as
	// a debug escape hatch when the health check fails for reasons the
	// user knows to be safe (e.g. Grafana Cloud hiding the version string
	// from anonymous callers).
	ForceSave bool
}

// Options is the top-level input to Run. It embeds three semantic groupings:
//
//   - Inputs: user-facing values (server, tokens, flags).
//   - Hooks: injection seams for testing (ConfigSource, ValidateFn, …).
//   - RetryState: cross-invocation plumbing for the sentinel retry loop.
//
// Fields are promoted via embedding, so callers may either initialise the
// sub-structs explicitly (clearer for mixed inputs) or read/write fields
// flatly on an existing Options value (e.g. `opts.Server = "…"`).
type Options struct {
	Inputs
	Hooks
	RetryState
}

// Result is returned by Run on success and carries enough data for callers to
// render a post-login summary and persist auth-method metadata.
type Result struct {
	ContextName    string
	AuthMethod     string // "oauth", "token", "basic", or "mtls"
	IsCloud        bool
	HasCloudToken  bool
	GrafanaVersion string
	StackSlug      string   // non-empty for known Grafana Cloud domains
	Capabilities   []string // reserved for future use
}

// ErrNeedInput is returned when Run requires a value that the caller must
// supply (e.g. via an interactive prompt or a flag) before retrying.
//
//nolint:errname // spec-defined sentinel name; renaming would break the public contract
type ErrNeedInput struct {
	Fields   []string
	Optional bool
	Hint     string
}

func (e *ErrNeedInput) Error() string {
	return "missing required input: " + strings.Join(e.Fields, ", ")
}

// ErrNeedClarification is returned when Run cannot determine a setting
// unambiguously and needs the caller to ask the user to choose.
//
//nolint:errname // spec-defined sentinel name; renaming would break the public contract
type ErrNeedClarification struct {
	Question string
	Choices  []string
	Field    string
}

func (e *ErrNeedClarification) Error() string {
	return fmt.Sprintf("clarification needed for %s: %s", e.Field, e.Question)
}

// RuntimeOnlyBearerDestinationError is returned when a token or OAuth login
// would use proxy/TLS destination settings that are present only in the process
// environment. Saving the credential without those settings would create a
// context whose next invocation rejects its own destination-bound credential.
//
// The CLI turns this UI-free sentinel into exact persistence commands for the
// selected config owner and stack.
type RuntimeOnlyBearerDestinationError struct {
	// OAuthIssuerProxyMismatch identifies the post-OAuth case where the issuer
	// selected a proxy endpoint different from GRAFANA_PROXY_ENDPOINT. Persisting
	// the environment value cannot repair that conflict; the caller must remove
	// the override and let the issuer-selected endpoint become authoritative.
	OAuthIssuerProxyMismatch bool
	RuntimeProxyEndpoint     string
	OAuthIssuerProxyEndpoint string
}

func (e *RuntimeOnlyBearerDestinationError) Error() string {
	if e.OAuthIssuerProxyMismatch {
		return "GRAFANA_PROXY_ENDPOINT conflicts with the proxy endpoint selected by the OAuth issuer; unset the environment override and retry"
	}
	return "runtime-only Grafana proxy/TLS settings cannot be used to save a token or OAuth credential; persist GRAFANA_PROXY_ENDPOINT and GRAFANA_TLS_* settings in the selected config, or unset the overrides and retry"
}

// AuthFlow is the interface implemented by auth.Flow (and test stubs).
// It exists so internal/login can reference the flow without importing a
// concrete browser-dependent type, and without depending on cmd/.
type AuthFlow interface {
	Run(ctx context.Context) (*auth.Result, error)
}

// CloudAuthFlow is implemented by auth.GCOMFlow.
type CloudAuthFlow interface {
	Run(ctx context.Context) (*auth.GCOMResult, error)
}

// CloudCredentialKind identifies which Cloud auth mechanism produced a token.
type CloudCredentialKind string

const (
	CloudCredentialCAP   CloudCredentialKind = "cap"
	CloudCredentialOAuth CloudCredentialKind = "oauth"
)

// Run orchestrates the full login lifecycle:
//
//  1. Validate server is set
//  2. Detect target (Cloud vs OnPrem)
//  3. Resolve Grafana auth (token or OAuth)
//  4. Derive context name
//  5. Resolve Cloud API token (Cloud targets only)
//  6. Build REST config and run connectivity validation
//  7. Persist context to config
//  8. Return Result
//
// Run takes opts by pointer so that resolved values (notably Target after
// auto-detection) propagate back to the caller and remain available across
// the CLI sentinel-retry flow. Callers that retry after ErrNeedInput /
// ErrNeedClarification should reuse the same Options value.
//
//nolint:gocyclo // The ordered login state machine is easier to audit when its validation and persistence gates remain explicit.
func Run(ctx context.Context, opts *Options) (Result, error) {
	// Step 1: check if the server is set
	if opts.Server == "" && !opts.UseCloudInstanceSelector {
		return Result{}, &ErrNeedInput{Fields: []string{"server"}}
	}
	if opts.UseCloudInstanceSelector {
		opts.UseOAuth = true
		opts.Target = TargetCloud
	}

	// Normalize: missing scheme → default to https. Users who meant http://
	// must pass the full URL explicitly; defaulting to https is safer.
	opts.Server = NormalizeServerURL(opts.Server)
	if err := validateRuntimeOnlyBearerDestination(*opts, ""); err != nil {
		return Result{}, err
	}

	// Step 2: detect target (using TLS-aware client when mTLS is configured)
	target := opts.Target
	if target == TargetUnknown {
		detected, err := detectTarget(ctx, *opts)
		if err != nil {
			return Result{}, fmt.Errorf("target detection failed: %w", err)
		}
		target = detected
	}

	// Still unknown after detection: need clarification unless --yes or agent mode
	if target == TargetUnknown {
		if opts.Yes || agent.IsAgentMode() {
			target = TargetOnPrem
		} else {
			return Result{}, &ErrNeedClarification{
				Field:    "target",
				Question: "Is this a Grafana Cloud instance or an on-premises Grafana?",
				Choices:  []string{"cloud", "on-prem"},
			}
		}
	}

	// Propagate the resolved target back to opts so that (a) subsequent
	// sentinel-retry iterations skip re-detection and (b) the CLI prompt
	// layer can branch on target (e.g. drop the OAuth option for on-prem).
	opts.Target = target

	// Step 3: Grafana auth
	authMethod, grafanaCfg, err := resolveGrafanaAuth(ctx, *opts, target)
	if err != nil {
		return Result{}, err
	}
	if err := validateRuntimeOnlyBearerDestination(*opts, authMethod, grafanaCfg); err != nil {
		return Result{}, err
	}

	// set the server if the user used the interactive instance selector
	if opts.Server == "" {
		opts.Server = grafanaCfg.Server
	}

	// Step 4: derive context name
	contextName := opts.ContextName
	if contextName == "" {
		contextName = config.ContextNameFromServerURL(opts.Server)
	}

	// Step 5: Cloud API token (Cloud targets only)
	cloudEntry, stackSlug, err := resolveCloudAuth(*opts, target)
	if err != nil {
		return Result{}, err
	}

	// Step 6: Build temp context and validate connectivity. The temp context
	// carries detached resolved views (Grafana, CloudEntry); persistContext
	// gives them their homes on stack and cloud entries.
	tempCtx := config.Context{
		Name:       contextName,
		Grafana:    grafanaCfg,
		CloudEntry: cloudEntry,
	}
	restCfg, err := config.NewNamespacedRESTConfig(ctx, tempCtx)
	if err != nil {
		return Result{}, fmt.Errorf("TLS configuration: %w", err)
	}
	// Connectivity validation can refresh a just-issued OAuth generation when
	// its access expiry is inside the proactive-refresh window. PersistContext
	// runs only after validation, so retain that rotation in the staged Grafana
	// config instead of later writing the consumed pre-validation generation.
	restCfg.SetOnRefresh(func(previousRefreshToken, token, refreshToken, expiresAt, refreshExpiresAt string) error {
		if tempCtx.Grafana == nil || tempCtx.Grafana.OAuthRefreshToken != previousRefreshToken {
			return fmt.Errorf("%w while validating newly issued OAuth credentials", auth.ErrTokenGenerationChanged)
		}
		tempCtx.Grafana.OAuthToken = token
		tempCtx.Grafana.OAuthRefreshToken = refreshToken
		tempCtx.Grafana.OAuthTokenExpiresAt = expiresAt
		tempCtx.Grafana.OAuthRefreshExpiresAt = refreshExpiresAt
		return nil
	})

	// Persist the cloud stack ID discovered while building the REST config so
	// subsequent commands resolve the namespace locally and skip the /bootdata
	// round-trip. On-prem (org) namespaces yield 0 and are left unset. Done
	// before validation so it also applies on the --force save-anyway path.
	if sid := restCfg.StackID(); sid != 0 {
		tempCtx.Grafana.StackID = sid
	}

	var grafanaVersion string
	if !opts.ForceSave {
		validateFn := opts.ValidateFn
		if validateFn == nil {
			validateFn = Validate
		}
		v, err := validateFn(ctx, *opts, restCfg)
		var capErr *GCOMStackError
		switch {
		case err == nil:
			grafanaVersion = v

		case errors.As(err, &capErr):
			// The Cloud Access Policy (CAP) token is optional: its absence does
			// not block login (resolveCloudAuth skips it under --yes/agent mode),
			// so a present-but-rejected token must not block login either. A
			// GCOMStackError means every earlier Validate step (health, K8s
			// discovery, version) passed — the Grafana auth is sound and only the
			// CAP check failed. Warn and continue, persisting the token anyway:
			// the CAP validation itself can be wrong for non-prod stacks (GCOM
			// root / slug derivation), and the user can re-run with a corrected
			// token without losing core access in the meantime.
			warnCloudTokenUnvalidated(opts.Writer, capErr)

		case opts.Yes || agent.IsAgentMode():
			// Non-interactive callers with --yes get a hard fail — they did not
			// opt in to "save anyway". The debug prompt is an interactive-only
			// escape hatch that requires explicit confirmation.
			return Result{}, err

		default:
			return Result{}, &ErrNeedClarification{
				Field: "save-unvalidated",
				Question: fmt.Sprintf(
					"Connectivity validation failed:\n  %s\n\nSave the context anyway? This is useful for debugging but the context may not work.",
					err.Error(),
				),
				Choices: []string{"yes", "no"},
			}
		}
	}
	if opts.PreserveStoredTLS && tempCtx.Grafana != nil {
		tempCtx.Grafana.TLS = opts.StoredTLS
	}
	if opts.PreserveStoredProxyEndpoint && authMethod != "oauth" && tempCtx.Grafana != nil {
		tempCtx.Grafana.ProxyEndpoint = opts.StoredProxyEndpoint
	}

	// Step 7: Persist to config (write only after all validation passes)
	if err := persistContext(ctx, *opts, contextName, tempCtx, stackSlug); err != nil {
		return Result{}, err
	}

	// Step 8: Return result
	return Result{
		ContextName:    contextName,
		AuthMethod:     authMethod,
		IsCloud:        target == TargetCloud,
		HasCloudToken:  cloudEntry != nil && (cloudEntry.Token != "" || cloudEntry.OAuthToken != ""),
		GrafanaVersion: grafanaVersion,
		StackSlug:      resolveStackSlug(opts.Server),
	}, nil
}

// validateRuntimeOnlyBearerDestination prevents a successful login from
// persisting a token/OAuth credential against a different destination than the
// runtime-only proxy/TLS settings that authorized the invocation. Without this
// gate the next process would apply the same environment, reject the new
// keychain generation as destination-mismatched, and leave a seemingly
// successful login unusable.
//
// Before authMethod is resolved, explicit token/OAuth intent is enough to
// reject known mismatches before target detection or a browser flow. After
// OAuth resolves, the second call also compares the issuer-provided proxy that
// will actually be persisted.
func validateRuntimeOnlyBearerDestination(opts Options, authMethod string, resolved ...*config.GrafanaConfig) error {
	if authMethod == "" && opts.GrafanaToken == "" && !opts.UseOAuth {
		return nil
	}
	if authMethod != "" && authMethod != "token" && authMethod != "oauth" {
		return nil
	}

	server := opts.Server
	var resolvedDestination *config.GrafanaConfig
	if len(resolved) > 0 {
		resolvedDestination = resolved[0]
	}
	if resolvedDestination != nil && resolvedDestination.Server != "" {
		server = resolvedDestination.Server
	}
	durable := &config.GrafanaConfig{Server: server}
	runtime := &config.GrafanaConfig{Server: server}

	if opts.PreserveStoredTLS {
		durable.TLS = opts.StoredTLS
		runtime.TLS = opts.TLS
	} else {
		durable.TLS = opts.TLS
		runtime.TLS = opts.TLS
	}
	if opts.PreserveStoredProxyEndpoint {
		runtime.ProxyEndpoint = opts.RuntimeProxyEndpoint
		switch {
		case authMethod == "oauth" && resolvedDestination != nil:
			durable.ProxyEndpoint = resolvedDestination.ProxyEndpoint
		case authMethod == "" && opts.UseOAuth && opts.GrafanaToken == "":
			// OAuth supplies and persists its own proxy endpoint. Before the
			// browser flow returns, defer this component to the post-auth check;
			// TLS is already fully knowable and remains enforced here.
			durable.ProxyEndpoint = opts.RuntimeProxyEndpoint
		default:
			durable.ProxyEndpoint = opts.StoredProxyEndpoint
		}
	} else {
		proxyEndpoint := opts.RuntimeProxyEndpoint
		if resolvedDestination != nil {
			proxyEndpoint = resolvedDestination.ProxyEndpoint
		}
		durable.ProxyEndpoint = proxyEndpoint
		runtime.ProxyEndpoint = proxyEndpoint
	}

	if config.GrafanaBearerCredentialDestinationMatches(durable, runtime) {
		return nil
	}
	if authMethod == "oauth" && resolvedDestination != nil && opts.PreserveStoredProxyEndpoint {
		proxyAligned := *durable
		proxyAligned.ProxyEndpoint = runtime.ProxyEndpoint
		if config.GrafanaBearerCredentialDestinationMatches(&proxyAligned, runtime) {
			return &RuntimeOnlyBearerDestinationError{
				OAuthIssuerProxyMismatch: true,
				RuntimeProxyEndpoint:     runtime.ProxyEndpoint,
				OAuthIssuerProxyEndpoint: durable.ProxyEndpoint,
			}
		}
	}
	return &RuntimeOnlyBearerDestinationError{}
}

// NormalizeServerURL trims surrounding whitespace and defaults a schemeless
// Grafana server to HTTPS. An explicit HTTP scheme is retained: callers that
// deliberately connect without TLS must continue to say so.
func NormalizeServerURL(raw string) string {
	raw = strings.TrimSpace(raw)
	lower := strings.ToLower(raw)
	if raw != "" && !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return "https://" + raw
	}
	return raw
}

// detectTarget calls DetectFn or falls back to the real DetectTarget.
// When TLS settings are present, builds a TLS-aware HTTP client for the probe.
//
// Cert-load failures (e.g. malformed cert-file path) are returned as hard
// errors rather than degrading to TargetUnknown. This is intentional: a broken
// TLS config should fail fast here rather than producing a confusing
// "auth rejected" error downstream during validation.
func detectTarget(ctx context.Context, opts Options) (Target, error) {
	if opts.DetectFn != nil {
		return opts.DetectFn(ctx, opts.Server)
	}
	client, err := tlsAwareClient(ctx, preAuthTLS(opts))
	if err != nil {
		return TargetUnknown, fmt.Errorf("TLS configuration: %w", err)
	}
	return DetectTarget(ctx, opts.Server, client)
}

// preAuthTLS returns the TLS view authorized for requests that run before
// login has built a fully resolved Context. Explicit token/OAuth intent, and a
// persisted explicit non-mTLS method, retain CA/SNI/ALPN trust settings but do
// not present a potentially stale client identity to the probed destination.
func preAuthTLS(opts Options) *config.TLS {
	if opts.TLS == nil {
		return nil
	}
	method := strings.ToLower(strings.TrimSpace(opts.ExistingGrafanaAuthMethod))
	if opts.GrafanaToken != "" || opts.UseOAuth || (method != "" && method != "mtls") {
		return opts.TLS.ServerTrustOnly()
	}
	return opts.TLS
}

// tlsAwareClient returns a TLS-aware *http.Client when tlsCfg is non-nil and
// non-empty, or a default client otherwise. Used by the login flow for target
// detection and connectivity validation against mTLS servers.
func tlsAwareClient(ctx context.Context, tlsCfg *config.TLS) (*http.Client, error) {
	if tlsCfg == nil || tlsCfg.IsEmpty() {
		return httputils.NewDefaultClient(ctx), nil
	}
	stdTLS, err := tlsCfg.ToStdTLSConfig()
	if err != nil {
		return nil, err
	}
	return httputils.NewDefaultClientWithTLS(ctx, stdTLS), nil
}

// resolveGrafanaAuth determines how to authenticate against Grafana (step 4).
// Priority: explicit GrafanaToken → UseOAuth flag → ErrNeedInput.
// OAuth is attempted only when UseOAuth is set; the caller (CLI) is responsible
// for setting UseOAuth based on user intent or interactive prompts.
//
// For on-prem targets, OrgID defaults to 1 when unset. This keeps fresh
// on-prem logins from tripping over config.GrafanaConfig.validateNamespace
// (which attempts DiscoverStackID against /bootdata and hard-fails on OSS).
func resolveGrafanaAuth(ctx context.Context, opts Options, target Target) (string, *config.GrafanaConfig, error) {
	// Cache hit: StagedContext already has Grafana resolved (previous
	// retry), reuse without re-running OAuth/token auth.
	if opts.StagedContext != nil && opts.StagedContext.Grafana != nil {
		return opts.StagedContext.Grafana.AuthMethod, opts.StagedContext.Grafana, nil
	}

	authTLS := preAuthTLS(opts)
	grafanaCfg := &config.GrafanaConfig{
		Server:        opts.Server,
		ProxyEndpoint: opts.RuntimeProxyEndpoint,
		OrgID:         int64(opts.OrgID),
		TLS:           opts.TLS,
	}

	var method string
	switch {
	case opts.GrafanaToken != "":
		grafanaCfg.APIToken = opts.GrafanaToken
		grafanaCfg.AuthMethod = "token"
		method = "token"

	case opts.UseOAuth:
		if opts.NewAuthFlow == nil {
			return "", nil, errors.New("OAuth requested but no auth flow factory provided")
		}
		// The internal/login package is UI-free (NC-001) — it never touches
		// process streams directly. Callers that want OAuth output surfaced
		// to the user must supply a Writer explicitly (the CLI passes
		// cmd.ErrOrStderr()). When unset, discard silently rather than
		// leaking to os.Stderr.
		w := opts.Writer
		if w == nil {
			w = io.Discard
		}
		flow := opts.NewAuthFlow(opts.Server, auth.Options{Writer: w, Port: opts.OAuthCallbackPort})
		result, err := flow.Run(ctx)
		if err != nil {
			return "", nil, fmt.Errorf("OAuth flow failed: %w", err)
		}
		if grafanaCfg.Server == "" {
			grafanaCfg.Server = result.InstanceEndpoint
		}
		grafanaCfg.OAuthToken = result.Token
		grafanaCfg.OAuthRefreshToken = result.RefreshToken
		grafanaCfg.OAuthTokenExpiresAt = result.ExpiresAt
		grafanaCfg.OAuthRefreshExpiresAt = result.RefreshExpiresAt
		grafanaCfg.ProxyEndpoint = result.APIEndpoint
		grafanaCfg.AuthMethod = "oauth"
		method = "oauth"
		// Wrap up the OAuth step with a clear success line before any
		// subsequent prompts (e.g. the optional Cloud API token). This runs
		// once: retries hit the StagedContext cache above and skip OAuth.
		announceOAuthLogin(w, result)

	case authTLS != nil && (len(authTLS.CertData) > 0 || authTLS.CertFile != ""):
		// mTLS-only auth: the client certificate authenticates at the transport
		// layer (e.g. Teleport proxy). No Grafana token or OAuth needed.
		// Note: we check only for cert presence here, not cert+key pairing.
		// TLS.ResolveFiles() enforces "both cert-file and key-file must be
		// provided together" downstream, producing a clear error if the key
		// is missing.
		grafanaCfg.AuthMethod = "mtls"
		method = "mtls"

	default:
		return "", nil, &ErrNeedInput{Fields: []string{"grafana-auth"}}
	}

	// Default OrgID=1 for fresh on-prem logins. Without this, validateNamespace
	// calls DiscoverStackID (hits /bootdata) which fails on OSS Grafana and
	// produces a confusing hard error on an otherwise valid setup. Cloud
	// logins leave OrgID=0 so StackID discovery runs normally.
	if target == TargetOnPrem && grafanaCfg.OrgID == 0 {
		grafanaCfg.OrgID = 1
	}

	// Populate cache so subsequent retries skip this step.
	if opts.StagedContext != nil {
		opts.StagedContext.Grafana = grafanaCfg
	}

	return method, grafanaCfg, nil
}

// resolveCloudAuth builds the cloud auth entry for Cloud targets (step 5),
// alongside the stack slug to record on the stack entry when derivable.
// If CloudToken is empty and this is a Cloud target, returns ErrNeedInput
// unless Yes or agent mode is set (which allows skipping step 5: the CAP
// token is optional — its absence just disables Cloud management features,
// it does not block login).
func resolveCloudAuth(opts Options, target Target) (*config.CloudEntry, string, error) {
	if target != TargetCloud {
		return nil, "", nil
	}

	slug := resolveStackSlug(opts.Server)

	if opts.CloudToken != "" {
		return cloudEntryForToken(opts), slug, nil
	}

	// Cloud target with no token: skip if Yes or agent mode (D9, D10).
	// Still persist the stack slug when derivable so datasource auto-discovery
	// works on stacks with multiple signal datasources.
	if opts.Yes || agent.IsAgentMode() {
		return nil, slug, nil
	}

	// About to prompt for the optional Cloud API token: frame it as a distinct,
	// skippable step so it doesn't read as a continuation of OAuth.
	announceCloudTokenStep(opts.Writer)
	return nil, "", &ErrNeedInput{
		Fields:   []string{"cloud-token"},
		Optional: true,
		Hint:     cloudTokenHint(opts.Server),
	}
}

// ResolveCloudEndpoints resolves the OAuth origin and API destination as one
// coherent pair. Explicit/sticky values in Options win. When only one endpoint
// is supplied it represents the Cloud environment for both operations; callers
// that intentionally use distinct endpoints must supply both. Otherwise the
// pair is derived from the stack environment, then defaults to production.
func ResolveCloudEndpoints(opts Options) (string, string) {
	oauthURL := opts.CloudOAuthURL
	apiURL := opts.CloudAPIURL

	switch {
	case oauthURL != "" && apiURL == "":
		apiURL = oauthURL
	case apiURL != "" && oauthURL == "":
		oauthURL = apiURL
	case oauthURL == "" && apiURL == "":
		if root, ok := config.GCOMRootFromServerURL(opts.Server); ok {
			oauthURL, apiURL = root, root
		} else {
			oauthURL, apiURL = "https://grafana.com", "https://grafana.com"
		}
	}

	return config.NormalizeCloudURL(oauthURL), config.NormalizeCloudURL(apiURL)
}

// cloudEntryForToken builds the cloud auth entry for a Cloud target that has
// a resolved token. OAuth-issued tokens land in the oauth-token field, pasted
// CAP tokens in the token field. OAuth metadata and the exact endpoint pair
// used to mint/use the token are persisted together.
func cloudEntryForToken(opts Options) *config.CloudEntry {
	oauthURL, apiURL := ResolveCloudEndpoints(opts)
	if opts.CloudCredentialKind == CloudCredentialOAuth {
		entry := &config.CloudEntry{
			OAuthToken:          opts.CloudToken,
			OAuthTokenExpiresAt: opts.CloudOAuthTokenExpiresAt,
			OAuthScopes:         append([]string(nil), opts.CloudOAuthScopes...),
			OAuthUrl:            oauthURL,
			APIUrl:              apiURL,
		}
		return entry
	}
	return &config.CloudEntry{
		Token:    opts.CloudToken,
		OAuthUrl: oauthURL,
		APIUrl:   apiURL,
	}
}

// announceOAuthLogin surfaces a clear success message once the interactive OAuth
// PKCE flow completes, before any subsequent prompts. It writes to w (the
// caller-supplied progress writer); a nil writer discards, keeping
// internal/login free of process streams (NC-001).
func announceOAuthLogin(w io.Writer, result *auth.Result) {
	if w == nil {
		w = io.Discard
	}
	endpoint := result.InstanceEndpoint
	if endpoint == "" {
		endpoint = result.APIEndpoint
	}
	switch {
	case endpoint != "" && result.Email != "":
		fmt.Fprintf(w, "\n✔ Signed in to %s as %s\n", endpoint, result.Email)
	case endpoint != "":
		fmt.Fprintf(w, "\n✔ Signed in to %s\n", endpoint)
	default:
		fmt.Fprintln(w, "\n✔ Signed in")
	}
}

// announceCloudTokenStep frames the upcoming optional Cloud API token prompt as
// a separate, skippable step. It writes to w (the caller-supplied progress
// writer); a nil writer discards (NC-001).
func announceCloudTokenStep(w io.Writer) {
	if w == nil {
		w = io.Discard
	}
	fmt.Fprintln(w, "\nOptional: log in to Grafana Cloud to enable Cloud management features.")
}

// warnCloudTokenUnvalidated surfaces a non-fatal advisory when a Cloud token is
// present (from a pasted CAP token or the browser OAuth login) but its GCOM
// stack check failed. Because Cloud auth is optional, login proceeds; this
// explains why Cloud management features may not work. The wording is kept
// auth-method-neutral so it reads correctly for both the token and OAuth paths.
// It writes to w (the caller-supplied progress writer); a nil writer discards,
// keeping internal/login free of process streams (NC-001).
func warnCloudTokenUnvalidated(w io.Writer, e *GCOMStackError) {
	if w == nil {
		w = io.Discard
	}
	msg := fmt.Sprintf("Warning: could not verify Grafana Cloud access for stack %q", e.Slug)
	if e.Status != 0 {
		msg += fmt.Sprintf(" (GCOM returned %d)", e.Status)
	}
	fmt.Fprintln(w, msg+". Logging in anyway; some Cloud management features may be unavailable.")
}

// persistContext loads the existing config (tolerating ErrNotExist), upserts
// the stack entry, cloud entry, and context, and writes it back. The stack is
// named after the context (1:1); the cloud entry reuses the context's existing
// binding, else one named after its API URL host. On re-auth (context exists),
// only token fields and AuthMethod are mutated; other fields are preserved
// (D20, AC-009).
func persistContext(ctx context.Context, opts Options, contextName string, tempCtx config.Context, stackSlug string) error {
	source := opts.ConfigSource
	if source == nil {
		source = config.StandardLocation()
	}

	cfg, err := config.LoadLoginMutationGuarded(ctx, source, opts.LoginMutationGuard)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("loading config: %w", err)
	}
	// Keep the source-absent revision marker returned by Load. Write uses it to
	// avoid replacing a config another process creates while login is running.
	// Load resolves only the file's current context. A login may target another
	// existing context; resolve its deferred keychain references before merging
	// auth so Write can replace and delete the exact old credential generation.
	cfg.ResolveContext(contextName)

	existing := cfg.Contexts[contextName]
	// An unbound context may already have a same-named raw stack containing
	// provider/resource settings. Bind and reuse that owned entry before the
	// destination guard; replacing it with an empty stack would lose unrelated
	// configuration and would bypass the existing-server confirmation.
	if existing != nil && existing.Stack == "" && cfg.Stacks[existing.Name] != nil {
		existing.Stack = existing.Name
		cfg.Resolve()
	}

	// Server-mismatch guard: if the existing context points at a different
	// server than the incoming one, require explicit confirmation before
	// overwriting. Bypassed only when AllowOverride is set (user confirmed via
	// the interactive ErrNeedClarification prompt or passed --allow-server-override).
	// --yes alone does not bypass this guard; changing which server a context
	// targets is a potentially destructive operation that requires an explicit signal.
	if existing != nil && existing.Grafana != nil && tempCtx.Grafana != nil {
		oldServer := NormalizeServerURL(existing.Grafana.Server)
		newServer := NormalizeServerURL(tempCtx.Grafana.Server)
		if oldServer != "" && newServer != "" && oldServer != newServer &&
			!opts.AllowOverride {
			return &ErrNeedClarification{
				Field: "allow-override",
				Question: fmt.Sprintf(
					"Context %q already exists with server %s.\nOverride with %s?",
					contextName, oldServer, newServer,
				),
				Choices: []string{"yes", "no"},
			}
		}
	}

	// Re-auth mode: preserve existing context fields, update only auth.
	if existing == nil {
		cfg.SetContext(contextName, true, config.Context{})
		existing = cfg.Contexts[contextName]
	} else {
		cfg.CurrentContext = contextName // make current on success, same as new-context path
	}
	if err := mergeAuthIntoExisting(&cfg, existing, tempCtx, opts.OrgID, stackSlug, opts.CloudMutationSafety); err != nil {
		return err
	}
	if err := opts.LoginMutationGuard.VerifyCurrentSources(); err != nil {
		return err
	}

	if err := config.Write(ctx, source, cfg); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// mergeAuthIntoExisting updates only auth-related fields on the context's
// stack and cloud entries, preserving all other user-configured fields
// (OrgID, Datasources, Providers, etc.). Missing entries are created: the
// stack named after the context, the cloud entry via EnsureCloudEntry.
func mergeAuthIntoExisting(
	cfg *config.Config,
	existing *config.Context,
	incoming config.Context,
	explicitOrgID int,
	stackSlug string,
	cloudSafety config.CloudMutationSafety,
) error {
	if incoming.Grafana != nil {
		if err := mergeGrafanaAuthIntoStack(cfg, existing, incoming.Grafana, explicitOrgID, stackSlug); err != nil {
			return err
		}
	}

	// Update the cloud entry if the incoming context carries cloud auth.
	if incoming.CloudEntry != nil {
		existing.Cloud = cfg.EnsureCloudEntryWithSafety(existing.Cloud, *incoming.CloudEntry, existing.Name, cloudSafety)
	}

	cfg.Resolve()
	return nil
}

// mergeGrafanaAuthIntoStack writes the incoming grafana auth onto the
// context's stack entry, creating a stack named after the context when it has
// none.
func mergeGrafanaAuthIntoStack(cfg *config.Config, existing *config.Context, src *config.GrafanaConfig, explicitOrgID int, stackSlug string) error {
	if existing.Stack == "" {
		if cfg.Stacks[existing.Name] == nil {
			cfg.SetStack(existing.Name, config.StackConfig{})
		}
		existing.Stack = existing.Name
		cfg.Resolve()
	}
	stack := existing.StackEntry
	if stack == nil {
		// Tolerant login can repair a sole/explicit config whose context names a
		// missing stack. Materialize that exact referenced owner before applying
		// auth instead of dereferencing a dangling resolved view.
		cfg.SetStack(existing.Stack, config.StackConfig{})
		cfg.Resolve()
		stack = existing.StackEntry
	}
	if stack.Grafana == nil {
		stack.Grafana = &config.GrafanaConfig{}
		cfg.Resolve()
	}
	g := stack.Grafana
	finishDestinationMutation := cfg.PrepareSecretPathMutation("stacks." + existing.Stack + ".grafana.server")

	// Update every credential destination before assigning incoming secrets.
	// The completion callback clears old Grafana and SM generations whose
	// server/proxy/TLS/username authority changed, without clearing the newly
	// authenticated values below.
	g.Server = src.Server
	g.User = src.User
	g.ProxyEndpoint = src.ProxyEndpoint
	g.TLS = src.TLS
	if err := finishDestinationMutation(); err != nil {
		return err
	}
	g.AuthMethod = src.AuthMethod

	// Clear all auth fields then repopulate with incoming values so that
	// switching from OAuth to token (or vice-versa) leaves no stale credentials.
	g.Password = src.Password
	g.APIToken = src.APIToken
	g.OAuthToken = src.OAuthToken
	g.OAuthRefreshToken = src.OAuthRefreshToken
	g.OAuthTokenExpiresAt = src.OAuthTokenExpiresAt
	g.OAuthRefreshExpiresAt = src.OAuthRefreshExpiresAt
	markGrafanaAuthMutation(cfg, existing.Stack)

	// Carry the freshly discovered cloud stack ID through re-auth so re-logins
	// keep it current. Left untouched when discovery yielded nothing (0).
	if src.StackID != 0 {
		g.StackID = src.StackID
	}

	if explicitOrgID != 0 {
		g.OrgID = int64(explicitOrgID)
	} else if g.OrgID == 0 {
		// Fresh contexts carry the on-prem OrgID=1 default from
		// resolveGrafanaAuth; re-auth keeps the user's existing value.
		g.OrgID = src.OrgID
	}

	if stackSlug != "" {
		stack.Slug = stackSlug
	}
	return nil
}

func markGrafanaAuthMutation(cfg *config.Config, stackName string) {
	owner := credentials.StackOwner(stackName)
	for _, field := range []credentials.Field{
		credentials.FieldGrafanaPassword,
		credentials.FieldGrafanaToken,
		credentials.FieldOAuthToken,
		credentials.FieldOAuthRefreshToken,
	} {
		cfg.MarkSecretMutation(owner, field)
	}
}
