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

// Options holds all inputs required by Run. Injection fields (ConfigSource,
// NewAuthFlow, Writer, ValidateFn, DetectFn) allow unit tests to run without
// real filesystem, browser, or network access.
type Options struct {
	Server       string
	ContextName  string
	Target       Target
	GrafanaToken string
	CloudToken   string
	CloudAPIURL  string
	UseOAuth     bool
	Yes          bool
	Cloud        bool
	ConfigSource config.Source
	NewAuthFlow  func(server string, opts auth.Options) AuthFlow
	Writer       io.Writer

	// ValidateFn overrides connectivity validation for testing.
	// Returns the Grafana version string on success. When nil, the real Validate() is used.
	ValidateFn func(ctx context.Context, opts Options, restCfg config.NamespacedRESTConfig) (string, error)

	// DetectFn overrides target detection for testing.
	// When nil, DetectTarget with httputils.NewDefaultClient(ctx) is used.
	DetectFn func(ctx context.Context, server string) (Target, error)

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
	// ErrNeedClarification{Field: "allow-override"} prompt, or implicitly
	// when opts.Yes is true.
	AllowOverride bool

	// ForceSave, when true, bypasses connectivity validation and persists
	// the context anyway. Set by the CLI after the user confirms via an
	// ErrNeedClarification{Field: "save-unvalidated"} prompt. Intended as
	// a debug escape hatch when the health check fails for reasons the
	// user knows to be safe (e.g. Grafana Cloud hiding the version string
	// from anonymous callers).
	ForceSave bool
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
//  2. Derive context name
//  3. Detect target (Cloud vs OnPrem)
//  4. Resolve Grafana auth (token or OAuth)
//  5. Resolve Cloud API token (Cloud targets only)
//  6. Build REST config and run connectivity validation
//  7. Persist context to config
//  8. Return Result
func Run(ctx context.Context, opts Options) (Result, error) {
	// Step 1: server must be set
	if opts.Server == "" {
		return Result{}, &ErrNeedInput{Fields: []string{"server"}}
	}

	// Normalize: missing scheme → default to https. Users who meant http://
	// must pass the full URL explicitly; defaulting to https is safer.
	if !strings.HasPrefix(opts.Server, "http://") && !strings.HasPrefix(opts.Server, "https://") {
		opts.Server = "https://" + opts.Server
	}

	// Step 2: derive context name
	contextName := opts.ContextName
	if contextName == "" {
		contextName = config.ContextNameFromServerURL(opts.Server)
	}

	// Step 3: detect target
	target := opts.Target
	if target == TargetUnknown {
		detected, err := detectTarget(ctx, opts)
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

	// Step 4: Grafana auth
	authMethod, grafanaCfg, err := resolveGrafanaAuth(ctx, opts, target)
	if err != nil {
		return Result{}, err
	}

	// Step 5: Cloud API token (Cloud targets only)
	cloudCfg, err := resolveCloudAuth(opts, target)
	if err != nil {
		return Result{}, err
	}

	// Step 6: Build temp context and validate connectivity
	tempCtx := config.Context{
		Name:    contextName,
		Grafana: grafanaCfg,
		Cloud:   cloudCfg,
	}
	restCfg := config.NewNamespacedRESTConfig(ctx, tempCtx)

	var grafanaVersion string
	if !opts.ForceSave {
		validateFn := opts.ValidateFn
		if validateFn == nil {
			validateFn = Validate
		}
		v, err := validateFn(ctx, opts, restCfg)
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
	if err := persistContext(ctx, opts, contextName, tempCtx); err != nil {
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
func resolveGrafanaAuth(ctx context.Context, opts Options, _ Target) (string, *config.GrafanaConfig, error) {
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
		w := opts.Writer
		if w == nil {
			w = os.Stderr
		}
		flow := opts.NewAuthFlow(opts.Server, auth.Options{Writer: w})
		result, err := flow.Run(ctx)
		if err != nil {
			return "", nil, fmt.Errorf("OAuth flow failed: %w", err)
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

	// Populate cache so subsequent retries skip this step.
	if opts.StagedContext != nil {
		opts.StagedContext.Grafana = grafanaCfg
	}

	return method, grafanaCfg, nil
}

// resolveCloudAuth builds CloudConfig for Cloud targets (step 5).
// If CloudToken is empty and this is a Cloud target, returns ErrNeedInput
// unless Yes or agent mode is set (which allows skipping step 2).
func resolveCloudAuth(opts Options, target Target) (*config.CloudConfig, error) {
	if target != TargetCloud {
		return nil, nil //nolint:nilnil // nil CloudConfig means "no Cloud auth"; caller checks for nil.
	}

	if opts.CloudToken != "" {
		return &config.CloudConfig{
			Token:  opts.CloudToken,
			APIUrl: opts.CloudAPIURL,
		}, nil
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
	// overwriting. Bypass when Yes (--yes) or AllowOverride (user confirmed
	// via the ErrNeedClarification{Field:"allow-override"} sentinel).
	if existing != nil && existing.Grafana != nil && tempCtx.Grafana != nil {
		oldServer := existing.Grafana.Server
		newServer := tempCtx.Grafana.Server
		if oldServer != "" && newServer != "" && oldServer != newServer &&
			!opts.Yes && !opts.AllowOverride {
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
