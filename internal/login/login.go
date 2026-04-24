package login

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
)

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
	CloudToken   string
	CloudAPIURL  string
	UseOAuth     bool
	Yes          bool
	// UseCloudInstanceSelector is only used internally to mark the case in which
	// a user explicitly left the server empty to be directed to the cloud
	// instance selector
	UseCloudInstanceSelector bool

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

	// NewAuthFlow constructs the OAuth PKCE flow. Must be non-nil when
	// UseOAuth is true; otherwise Run returns an error. Callers typically
	// pass a factory that wraps auth.NewFlow.
	NewAuthFlow func(server string, opts auth.Options) AuthFlow

	// ValidateFn overrides connectivity validation for testing.
	// Returns the Grafana version string on success. When nil, the real
	// Validate() is used.
	ValidateFn func(ctx context.Context, opts Options, restCfg config.NamespacedRESTConfig) (string, error)

	// DetectFn overrides target detection for testing. When nil,
	// DetectTarget with httputils.NewDefaultClient(ctx) is used.
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
	AuthMethod     string // "oauth", "token", or "basic"
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

// AuthFlow is the interface implemented by auth.Flow (and test stubs).
// It exists so internal/login can reference the flow without importing a
// concrete browser-dependent type, and without depending on cmd/.
type AuthFlow interface {
	Run(ctx context.Context) (*auth.Result, error)
}

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
	if opts.Server != "" && !strings.HasPrefix(opts.Server, "http://") && !strings.HasPrefix(opts.Server, "https://") {
		opts.Server = "https://" + opts.Server
	}

	// Step 2: detect target
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
	cloudCfg, err := resolveCloudAuth(*opts, target)
	if err != nil {
		return Result{}, err
	}

	// Step 6: Build temp context and validate connectivity
	tempCtx := config.Context{
		Name:    contextName,
		Grafana: grafanaCfg,
		Cloud:   cloudCfg,
	}
	restCfg, err := config.NewNamespacedRESTConfig(ctx, tempCtx)
	if err != nil {
		return Result{}, fmt.Errorf("TLS configuration: %w", err)
	}

	var grafanaVersion string
	if !opts.ForceSave {
		validateFn := opts.ValidateFn
		if validateFn == nil {
			validateFn = Validate
		}
		v, err := validateFn(ctx, *opts, restCfg)
		if err != nil {
			// Non-interactive callers with --yes get a hard fail — they did not
			// opt in to "save anyway". The debug prompt is an interactive-only
			// escape hatch that requires explicit confirmation.
			if opts.Yes || agent.IsAgentMode() {
				return Result{}, err
			}
			return Result{}, &ErrNeedClarification{
				Field: "save-unvalidated",
				Question: fmt.Sprintf(
					"Connectivity validation failed:\n  %s\n\nSave the context anyway? This is useful for debugging but the context may not work.",
					err.Error(),
				),
				Choices: []string{"yes", "no"},
			}
		}
		grafanaVersion = v
	}

	// Step 7: Persist to config (write only after all validation passes)
	if err := persistContext(ctx, *opts, contextName, tempCtx); err != nil {
		return Result{}, err
	}

	// Step 8: Return result
	return Result{
		ContextName:    contextName,
		AuthMethod:     authMethod,
		IsCloud:        target == TargetCloud,
		HasCloudToken:  cloudCfg != nil && cloudCfg.Token != "",
		GrafanaVersion: grafanaVersion,
		StackSlug:      resolveStackSlug(opts.Server),
	}, nil
}

// detectTarget calls DetectFn or falls back to the real DetectTarget.
func detectTarget(ctx context.Context, opts Options) (Target, error) {
	if opts.DetectFn != nil {
		return opts.DetectFn(ctx, opts.Server)
	}
	return DetectTarget(ctx, opts.Server, httputils.NewDefaultClient(ctx))
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

	grafanaCfg := &config.GrafanaConfig{
		Server: opts.Server,
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
		flow := opts.NewAuthFlow(opts.Server, auth.Options{Writer: w})
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

// resolveCloudAuth builds CloudConfig for Cloud targets (step 5).
// If CloudToken is empty and this is a Cloud target, returns ErrNeedInput
// unless Yes or agent mode is set (which allows skipping step 5: the CAP
// token is optional — its absence just disables Cloud management features,
// it does not block login).
func resolveCloudAuth(opts Options, target Target) (*config.CloudConfig, error) {
	if target != TargetCloud {
		return nil, nil //nolint:nilnil // nil CloudConfig means "no Cloud auth"; caller checks for nil.
	}

	if opts.CloudToken != "" {
		cc := &config.CloudConfig{
			Token:  opts.CloudToken,
			APIUrl: opts.CloudAPIURL,
		}
		if slug := resolveStackSlug(opts.Server); slug != "" {
			cc.Stack = slug
		}
		return cc, nil
	}

	// Cloud target with no token: skip if Yes or agent mode (D9, D10)
	if opts.Yes || agent.IsAgentMode() {
		return nil, nil //nolint:nilnil // nil CloudConfig means "Cloud auth skipped"; valid non-error state.
	}

	return nil, &ErrNeedInput{
		Fields:   []string{"cloud-token"},
		Optional: true,
		Hint:     "Provide a Grafana Cloud API token to enable Cloud management features, or press Enter to skip.",
	}
}

// persistContext loads the existing config (tolerating ErrNotExist), upserts the
// context, and writes it back. On re-auth (context exists), only token fields and
// AuthMethod are mutated; other fields are preserved (D20, AC-009).
func persistContext(ctx context.Context, opts Options, contextName string, tempCtx config.Context) error {
	source := opts.ConfigSource
	if source == nil {
		source = config.StandardLocation()
	}

	cfg, err := config.Load(ctx, source)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("loading config: %w", err)
	}
	if errors.Is(err, os.ErrNotExist) {
		cfg = config.Config{}
	}

	existing := cfg.Contexts[contextName]

	// Server-mismatch guard: if the existing context points at a different
	// server than the incoming one, require explicit confirmation before
	// overwriting. Bypassed only when AllowOverride is set (user confirmed via
	// the interactive ErrNeedClarification prompt or passed --allow-server-override).
	// --yes alone does not bypass this guard; changing which server a context
	// targets is a potentially destructive operation that requires an explicit signal.
	if existing != nil && existing.Grafana != nil && tempCtx.Grafana != nil {
		oldServer := existing.Grafana.Server
		newServer := tempCtx.Grafana.Server
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
	if existing != nil {
		mergeAuthIntoExisting(existing, tempCtx)
		cfg.CurrentContext = contextName // make current on success, same as new-context path
	} else {
		cfg.SetContext(contextName, true, tempCtx)
	}

	if err := config.Write(ctx, source, cfg); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// mergeAuthIntoExisting updates only auth-related fields on an existing context,
// preserving all other user-configured fields (OrgID, Datasources, Providers, etc.).
func mergeAuthIntoExisting(existing *config.Context, incoming config.Context) {
	if existing.Grafana == nil {
		existing.Grafana = &config.GrafanaConfig{}
	}
	g := existing.Grafana
	src := incoming.Grafana

	if src == nil {
		return
	}

	// Always update the server (may have changed scheme or path).
	g.Server = src.Server
	g.AuthMethod = src.AuthMethod

	// Clear all auth fields then repopulate with incoming values so that
	// switching from OAuth to token (or vice-versa) leaves no stale credentials.
	g.APIToken = src.APIToken
	g.OAuthToken = src.OAuthToken
	g.OAuthRefreshToken = src.OAuthRefreshToken
	g.OAuthTokenExpiresAt = src.OAuthTokenExpiresAt
	g.OAuthRefreshExpiresAt = src.OAuthRefreshExpiresAt
	g.ProxyEndpoint = src.ProxyEndpoint

	// Update Cloud config if present in the incoming context.
	if incoming.Cloud != nil {
		if existing.Cloud == nil {
			existing.Cloud = &config.CloudConfig{}
		}
		existing.Cloud.Token = incoming.Cloud.Token
		if incoming.Cloud.APIUrl != "" {
			existing.Cloud.APIUrl = incoming.Cloud.APIUrl
		}
	}
}
