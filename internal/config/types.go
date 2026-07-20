package config

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/grafana/gcx/internal/credentials"
)

const (
	// DefaultContextName is the name of the default context.
	DefaultContextName = "default"
)

// ConfigVersion is the current config format version. The legacy
// (pre-versioned) format is detected by shape and auto-migrated on load.
const ConfigVersion = 1

// Config holds the information needed to connect to remote Grafana instances.
type Config struct {
	// Source contains the path to the config file parsed to populate this struct.
	Source string `json:"-" yaml:"-"`

	// Sources lists all config files that were discovered and merged to produce
	// this config. Populated by LoadLayered.
	Sources []ConfigSource `json:"-" yaml:"-"`

	// Version is the config format version. Absent on legacy configs, which the
	// loader migrates to the current format automatically.
	Version int64 `json:"version,omitempty" yaml:"version,omitempty"`

	// Stacks is a map of Grafana stack configurations (connection, providers,
	// per-stack resource settings), indexed by name. Contexts reference stacks
	// by name via Context.Stack.
	Stacks map[string]*StackConfig `json:"stacks,omitempty" yaml:"stacks,omitempty"`

	// Cloud is a map of named Grafana Cloud (GCOM) auth entries. Contexts
	// reference entries by name via Context.Cloud.
	Cloud map[string]*CloudEntry `json:"cloud,omitempty" yaml:"cloud,omitempty"`

	// Resources holds global settings for the `gcx resources` commands,
	// applying to all stacks. Merged (union) with each stack's Resources.
	Resources *ResourcesConfig `json:"resources,omitempty" yaml:"resources,omitempty"`

	// Contexts is a map of context configurations, indexed by name.
	Contexts map[string]*Context `json:"contexts" yaml:"contexts"`

	// CurrentContext is the name of the context currently in use.
	CurrentContext string `json:"current-context" yaml:"current-context"`

	// Diagnostics holds optional local diagnostic settings. All features are off by default.
	Diagnostics *DiagnosticsConfig `json:"diagnostics,omitempty" yaml:"diagnostics,omitempty"`

	// keychainFields tracks which (context, field) pairs were successfully
	// resolved from the OS keychain (or migrated into it) at load time.
	// Populated by the loader; used by Write to round-trip sentinels back to
	// disk and to delete entries whose field has since been cleared. Not part
	// of the on-disk schema.
	keychainFields keychainBacked `json:"-" yaml:"-"`

	// keychainPreserve tracks (owner, field) pairs whose sentinel could not
	// be resolved at load time because the keychain was unavailable (locked
	// session, missing DBus), mapped to the original sentinel string. Their
	// in-memory value is cleared, but Write must round-trip the original
	// sentinel back to disk so a transient outage never destroys the
	// reference. Not part of the on-disk schema.
	keychainPreserve keychainPreserved `json:"-" yaml:"-"`

	// keychainStore holds the keychain backend so that sentinel resolution can
	// be deferred for non-current contexts. Populated once by Load; nil when
	// the keychain is not in use.
	keychainStore credentials.Store `json:"-" yaml:"-"`
}

// DiagnosticsConfig controls optional local diagnostic features.
type DiagnosticsConfig struct {
	// AgentInvocationLog enables logging of failed agent-mode invocations to disk.
	// Off by default. When enabled, errors from agent-driven gcx calls are written
	// to LogDir (JSONL format) for capability-gap analysis.
	AgentInvocationLog bool `json:"agent-invocation-log,omitempty" yaml:"agent-invocation-log,omitempty"`

	// LogDir overrides the output directory for agent invocation log files.
	// Default: $XDG_STATE_HOME/gcx/ (platform-specific).
	LogDir string `json:"log-dir,omitempty" yaml:"log-dir,omitempty"`

	// Telemetry controls anonymous usage telemetry: "enabled", "disabled",
	// or "log" (prints to stderr). Overridden by the GCX_TELEMETRY and
	// DO_NOT_TRACK environment variables.
	Telemetry string `json:"telemetry,omitempty" yaml:"telemetry,omitempty"`
}

func (config *Config) HasContext(name string) bool {
	return config.Contexts[name] != nil
}

// GetCurrentContext returns the current context.
// If the current context is not set, it returns an error.
func (config *Config) GetCurrentContext() *Context {
	return config.Contexts[config.CurrentContext]
}

// ResolveContext resolves keychain sentinels on the stack and cloud entries
// referenced by a named context that was not resolved during Load (i.e. a
// non-current context). This is a no-op when the referenced entries have
// already been resolved or when no keychain store is available.
func (config *Config) ResolveContext(name string) {
	if config.keychainStore == nil {
		return
	}
	ctx := config.Contexts[name]
	if ctx == nil {
		return
	}
	backed, preserve := resolveSentinelsForContext(ctx, config.keychainStore)
	config.trackKeychainResults(backed, preserve)
}

// trackKeychainResults merges sentinel-resolution results into the config's
// keychain bookkeeping.
func (config *Config) trackKeychainResults(backed keychainBacked, preserve keychainPreserved) {
	if len(backed) > 0 && config.keychainFields == nil {
		config.keychainFields = keychainBacked{}
	}
	for owner, fields := range backed {
		for field := range fields {
			config.keychainFields.mark(owner, field)
		}
	}
	if len(preserve) > 0 && config.keychainPreserve == nil {
		config.keychainPreserve = keychainPreserved{}
	}
	for owner, fields := range preserve {
		for field, sentinel := range fields {
			config.keychainPreserve.mark(owner, field, sentinel)
		}
	}
}

// SetContext adds a new context to the Grafana config.
// If a context with the same name already exists, it is overwritten.
func (config *Config) SetContext(name string, makeCurrent bool, context Context) {
	if config.Contexts == nil {
		config.Contexts = make(map[string]*Context)
	}

	config.Contexts[name] = &context

	if makeCurrent {
		config.CurrentContext = name
	}
	config.Resolve()
}

// SetStack adds or replaces a stack entry.
func (config *Config) SetStack(name string, stack StackConfig) {
	if config.Stacks == nil {
		config.Stacks = make(map[string]*StackConfig)
	}
	config.Stacks[name] = &stack
	config.Resolve()
}

// SetCloudEntry adds or replaces a cloud auth entry.
func (config *Config) SetCloudEntry(name string, entry CloudEntry) {
	if config.Cloud == nil {
		config.Cloud = make(map[string]*CloudEntry)
	}
	config.Cloud[name] = &entry
	config.Resolve()
}

// Resolve wires each context's resolved view (stack entry, cloud entry,
// Grafana/Providers pointers, dry-run union) from its name references.
// Dangling references leave nil pointers; Context.Validate reports them.
// Idempotent; must re-run after any structural mutation (merge, migration,
// SetContext/SetStack/SetCloudEntry).
func (config *Config) Resolve() {
	for name, stack := range config.Stacks {
		if stack != nil {
			stack.Name = name
		}
	}
	for name, entry := range config.Cloud {
		if entry != nil {
			entry.Name = name
		}
	}
	for name, ctx := range config.Contexts {
		if ctx == nil {
			continue
		}
		ctx.Name = name
		ctx.StackEntry = nil
		ctx.Grafana = nil
		ctx.Providers = nil
		ctx.CloudEntry = nil
		if ctx.Stack != "" {
			if stack := config.Stacks[ctx.Stack]; stack != nil {
				ctx.StackEntry = stack
				ctx.Grafana = stack.Grafana
				ctx.Providers = stack.Providers
			}
		}
		if ctx.Cloud != "" {
			ctx.CloudEntry = config.Cloud[ctx.Cloud]
		}
		ctx.assumeServerDryRun = unionDryRun(config.Resources, ctx.StackEntry)
	}
}

// unionDryRun merges the global and per-stack assume-server-dry-run lists,
// preserving order (global first) and dropping duplicates.
func unionDryRun(global *ResourcesConfig, stack *StackConfig) []string {
	var lists [][]string
	if global != nil {
		lists = append(lists, global.AssumeServerDryRun)
	}
	if stack != nil && stack.Resources != nil {
		lists = append(lists, stack.Resources.AssumeServerDryRun)
	}
	var union []string
	seen := map[string]bool{}
	for _, list := range lists {
		for _, item := range list {
			if !seen[item] {
				seen[item] = true
				union = append(union, item)
			}
		}
	}
	return union
}

// StackConfig holds the connection and provider configuration for a single
// Grafana stack. Contexts reference stacks by name via Context.Stack.
type StackConfig struct {
	Name string `json:"-" yaml:"-"`

	// Slug is the Grafana Cloud stack slug (e.g. "mystack").
	// Optional: if not set, the slug may be derived from Grafana.Server.
	Slug string `json:"slug,omitempty" yaml:"slug,omitempty"`

	Grafana *GrafanaConfig `json:"grafana,omitempty" yaml:"grafana,omitempty"`

	// Providers holds per-provider configuration, indexed by provider name.
	// Each provider has a map of string key-value pairs.
	// Secret fields are selectively redacted by providers.RedactSecrets using
	// each provider's ConfigKey metadata.
	Providers map[string]map[string]string `json:"providers,omitempty" yaml:"providers,omitempty"`

	// Resources holds per-stack settings for the `gcx resources` commands,
	// merged (union) with the global Config.Resources.
	Resources *ResourcesConfig `json:"resources,omitempty" yaml:"resources,omitempty"`
}

// CloudEntry holds Grafana Cloud (GCOM) platform credentials and environment
// configuration. Entries are named and referenced by contexts via
// Context.Cloud; several contexts typically share one entry.
type CloudEntry struct {
	Name string `json:"-" yaml:"-"`

	// Token is a Grafana Cloud access policy token used to authenticate
	// against GCOM.
	Token string `datapolicy:"secret" env:"GRAFANA_CLOUD_TOKEN" json:"token,omitempty" yaml:"token,omitempty"`

	// OAuthToken is a grafana.com OAuth access token obtained via
	// `gcx cloud login`. The grafana.com OAuth flow issues no refresh token;
	// on expiry the user re-runs `gcx cloud login`.
	OAuthToken string `datapolicy:"secret" json:"oauth-token,omitempty" yaml:"oauth-token,omitempty"`

	// OAuthTokenExpiresAt is the OAuthToken expiration time in RFC3339 format.
	OAuthTokenExpiresAt string `json:"oauth-token-expires-at,omitempty" yaml:"oauth-token-expires-at,omitempty"`

	// OAuthUrl is the base URL for the OAuth login flow run by `gcx cloud
	// login`. It is used only during login. Optional: defaults to
	// "https://grafana.com".
	OAuthUrl string `env:"GRAFANA_CLOUD_OAUTH_URL" json:"oauth-url,omitempty" yaml:"oauth-url,omitempty"`

	// APIUrl is the base URL for all Grafana Cloud API (GCOM) resource calls
	// (stacks, regions, access policies, etc.). Every client talking to GCOM
	// uses it. Optional: defaults to "https://grafana.com".
	APIUrl string `env:"GRAFANA_CLOUD_API_URL" json:"api-url,omitempty" yaml:"api-url,omitempty"`

	// Orgs lists the grafana.com org slugs this entry's credential can see.
	// Populated at login; user-editable as a fallback.
	Orgs []string `json:"orgs,omitempty" yaml:"orgs,omitempty"`

	// Stacks lists the grafana.com stack slugs in the credential's realm, for
	// stack-realm access policy tokens. These are grafana.com slugs, NOT keys
	// into the local stacks map. Absent means the realm is the whole org(s),
	// including stacks created later — login must not auto-fill it for
	// org-realm tokens.
	Stacks []string `json:"stacks,omitempty" yaml:"stacks,omitempty"`
}

// Context binds a stack and (optionally) a cloud auth entry together with
// per-context defaults such as datasource UIDs.
type Context struct {
	Name string `json:"-" yaml:"-"`

	// Stack names the entry in Config.Stacks this context targets.
	Stack string `json:"stack,omitempty" yaml:"stack,omitempty"`

	// Cloud names the entry in Config.Cloud providing GCOM auth for this
	// context. Optional: without it, cloud-dependent operations fail at
	// runtime with a hint, not at validation time.
	Cloud string `json:"cloud,omitempty" yaml:"cloud,omitempty"`

	// Datasources holds per-kind default datasource UIDs, indexed by
	// datasource kind (e.g. "prometheus", "loki").
	Datasources map[string]string `json:"datasources,omitempty" yaml:"datasources,omitempty"`

	// Resolved views, populated by Config.Resolve after decode, merge, or any
	// structural mutation. Not part of the on-disk schema. Grafana and
	// Providers share pointers with the stack entry, so mutations through them
	// are visible on the stack (and vice versa).
	StackEntry *StackConfig                 `json:"-" yaml:"-"`
	CloudEntry *CloudEntry                  `json:"-" yaml:"-"`
	Grafana    *GrafanaConfig               `json:"-" yaml:"-"`
	Providers  map[string]map[string]string `json:"-" yaml:"-"`

	// assumeServerDryRun is the resolved union of the global and per-stack
	// assume-server-dry-run lists. Populated by Config.Resolve.
	assumeServerDryRun []string

	// envStackSlug is a process-environment override for the stack slug
	// (GRAFANA_CLOUD_STACK), applied by ParseEnvIntoContext. It wins over the
	// stack entry's Slug without mutating the shared stack config.
	envStackSlug string
}

// ResourcesConfig holds settings for the `gcx resources` commands.
type ResourcesConfig struct {
	// AssumeServerDryRun lists resources ("<resource>.<group>", e.g.
	// "alertrules.rules.alerting.grafana.app") the user asserts honor server-side dry-run on
	// this stack, added to the built-in allowlist so --dry-run sends them to the server.
	AssumeServerDryRun []string `json:"assume-server-dry-run,omitempty" yaml:"assume-server-dry-run,omitempty"`
}

// AssumeServerDryRun returns the union of the global and per-stack
// assume-server-dry-run lists, resolved by Config.Resolve. Nil if unset.
func (context *Context) AssumeServerDryRun() []string {
	return context.assumeServerDryRun
}

func (context *Context) Validate(ctx context.Context) error {
	if context.Stack != "" && context.StackEntry == nil {
		return ValidationError{
			Path:    fmt.Sprintf("$.contexts.'%s'.stack", context.Name),
			Message: fmt.Sprintf("stack %q is not defined", context.Stack),
			Suggestions: []string{
				fmt.Sprintf("Add a `stacks.%s` entry, or point the context at an existing stack", context.Stack),
			},
		}
	}
	if context.Cloud != "" && context.CloudEntry == nil {
		return ValidationError{
			Path:    fmt.Sprintf("$.contexts.'%s'.cloud", context.Name),
			Message: fmt.Sprintf("cloud entry %q is not defined", context.Cloud),
			Suggestions: []string{
				fmt.Sprintf("Add a `cloud.%s` entry, or run `gcx cloud login`", context.Cloud),
			},
		}
	}

	if context.Grafana == nil || context.Grafana.IsEmpty() {
		return ValidationError{
			Path:    fmt.Sprintf("$.contexts.'%s'", context.Name),
			Message: "context references no stack with grafana config",
			Suggestions: []string{
				"Run `gcx login` to configure a stack for this context",
			},
		}
	}

	return context.Grafana.Validate(ctx, context.stackName())
}

// stackName returns the name of the stack this context references, falling
// back to the context name for detached contexts (env-only, tests).
func (context *Context) stackName() string {
	if context.Stack != "" {
		return context.Stack
	}
	return context.Name
}

// ToRESTConfig returns a REST config for the context.
func (context *Context) ToRESTConfig(ctx context.Context) (NamespacedRESTConfig, error) {
	return NewNamespacedRESTConfig(ctx, *context)
}

// IsCloud reports whether this context targets Grafana Cloud.
// Any one of the following signals is sufficient:
//   - the stack entry's Slug is explicitly set (or GRAFANA_CLOUD_STACK is)
//   - Grafana.StackID is non-zero
//   - Grafana.Server hostname belongs to a Grafana-run Cloud domain
//     (*.grafana.net, *.grafana.com, and their -dev/-ops variants)
func (context *Context) IsCloud() bool {
	if context.envStackSlug != "" {
		return true
	}
	if context.StackEntry != nil && context.StackEntry.Slug != "" {
		return true
	}
	if context.Grafana == nil {
		return false
	}
	if context.Grafana.StackID != 0 {
		return true
	}
	if context.Grafana.Server == "" {
		return false
	}
	parsed, err := url.Parse(context.Grafana.Server)
	if err != nil {
		return false
	}
	return IsGrafanaCloudHost(strings.ToLower(parsed.Hostname()))
}

// ResolveStackSlug returns the Grafana Cloud stack slug for this context.
// Resolution order: the GRAFANA_CLOUD_STACK environment override, the stack
// entry's explicit Slug, then derivation from Grafana.Server by extracting the
// subdomain from *.grafana.net or *.grafana-dev.net URLs.
// Returns "" if no source yields a slug.
func (context *Context) ResolveStackSlug() string {
	if context.envStackSlug != "" {
		return context.envStackSlug
	}
	if context.StackEntry != nil && context.StackEntry.Slug != "" {
		return context.StackEntry.Slug
	}

	if context.Grafana == nil || context.Grafana.Server == "" {
		return ""
	}

	slug, _ := StackSlugFromServerURL(context.Grafana.Server)
	return slug
}

// grafanaCloudStackSuffixes lists the Grafana-run stack URL suffixes together
// with the env tag appended to slugs for non-prod environments. This is the
// single source of truth for stack-URL suffix classification. It intentionally
// excludes the .com variants (grafana.com, grafana-dev.com, grafana-ops.com)
// because those are GCOM root domains, not stack URLs — a host of the form
// "something.grafana.com" is not a Grafana Cloud stack endpoint.
//
//nolint:gochecknoglobals // constant-like lookup table; no mutable state.
var grafanaCloudStackSuffixes = []struct {
	suffix   string
	envTag   string // appended to the context NAME (not the slug) for non-prod environments
	gcomRoot string // GCOM (grafana.com) API root that manages stacks in this environment
}{
	{".grafana.net", "", "https://grafana.com"},
	{".grafana-dev.net", "-dev", "https://grafana-dev.com"},
	{".grafana-ops.net", "-ops", "https://grafana-ops.com"},
}

// grafanaCloudRootSuffixes are the Grafana-run root domains used by probes
// (e.g. buildInfo.grafanaUrl pointing at grafana.com). These are NOT stack URL
// suffixes but do indicate Cloud-hosted infrastructure.
//
//nolint:gochecknoglobals // constant-like lookup table; no mutable state.
var grafanaCloudRootSuffixes = []string{
	".grafana.com",
	".grafana-dev.com",
	".grafana-ops.com",
}

// IsGrafanaCloudHost reports whether the given host (lowercased, without port)
// belongs to a Grafana-run Cloud domain. It matches *.grafana.net,
// *.grafana-dev.net, *.grafana-ops.net (stack URLs) and *.grafana.com,
// *.grafana-dev.com, *.grafana-ops.com (GCOM root domains used by probes).
// The caller is responsible for lowercasing the host before calling this function.
func IsGrafanaCloudHost(host string) bool {
	for _, entry := range grafanaCloudStackSuffixes {
		if strings.HasSuffix(host, entry.suffix) {
			return true
		}
	}
	for _, suffix := range grafanaCloudRootSuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// matchGrafanaCloudStack matches a server URL against the known Grafana-run
// stack suffixes. It returns, in order: the bare stack slug; the context-name
// env tag ("-dev"/"-ops", empty for prod); the GCOM API root for that
// environment; and a bool that is false for non-Grafana-Cloud or custom-domain
// URLs. Regional subdomains ("mystack.us.grafana.net") collapse to their first
// component ("mystack").
func matchGrafanaCloudStack(serverURL string) (string, string, string, bool) {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		return "", "", "", false
	}

	host := parsed.Hostname()
	for _, entry := range grafanaCloudStackSuffixes {
		s, found := strings.CutSuffix(host, entry.suffix)
		if !found {
			continue
		}
		if i := strings.Index(s, "."); i >= 0 {
			s = s[:i]
		}
		if s == "" {
			continue
		}
		return s, entry.envTag, entry.gcomRoot, true
	}

	return "", "", "", false
}

// StackSlugFromServerURL extracts the Grafana Cloud stack slug from a server URL.
// It returns the slug and true for *.grafana.net, *.grafana-dev.net, and
// *.grafana-ops.net URLs, or ("", false) for anything else. The slug is the real
// stack slug used by the GCOM and stack-scoped APIs — the per-environment naming
// suffix ("-dev"/"-ops") is NOT included here; that lives in ContextNameFromServerURL.
func StackSlugFromServerURL(serverURL string) (string, bool) {
	slug, _, _, ok := matchGrafanaCloudStack(serverURL)
	return slug, ok
}

// GCOMRootFromServerURL returns the GCOM (grafana.com) API root that manages the
// stack at serverURL: grafana.com for *.grafana.net, grafana-dev.com for
// *.grafana-dev.net, grafana-ops.com for *.grafana-ops.net. ok is false for
// non-Grafana-Cloud or custom-domain URLs.
func GCOMRootFromServerURL(serverURL string) (string, bool) {
	_, _, gcomRoot, ok := matchGrafanaCloudStack(serverURL)
	return gcomRoot, ok
}

// ContextNameFromServerURL derives a context name from a Grafana server URL.
// For Grafana Cloud URLs, it returns the stack slug with the per-environment
// suffix ("-dev"/"-ops") appended to prevent collisions between same-named
// stacks across environments. For other URLs, dots in the hostname are replaced
// with hyphens to keep the name shell-friendly. Returns DefaultContextName if
// the URL cannot be parsed.
func ContextNameFromServerURL(serverURL string) string {
	if slug, envTag, _, ok := matchGrafanaCloudStack(serverURL); ok {
		return slug + envTag
	}

	parsed, err := url.Parse(serverURL)
	if err != nil || parsed.Hostname() == "" {
		return DefaultContextName
	}

	return strings.ReplaceAll(parsed.Hostname(), ".", "-")
}

// ResolveCloudAPIURL returns the base URL for Grafana Cloud API (GCOM) resource
// calls (stacks, regions, access policies, etc.). Every client talking to GCOM
// uses it. Resolution order:
//  1. An explicit Cloud.APIUrl, prefixed with "https://" if it has no scheme.
//  2. The GCOM root derived from the stack server URL's environment, so that an
//     ops stack (*.grafana-ops.net) resolves to grafana-ops.com and a dev stack
//     to grafana-dev.com instead of prod grafana.com.
//  3. "https://grafana.com" as the default (prod, or non-Grafana-Cloud hosts).
func (context *Context) ResolveCloudAPIURL() string {
	if context.CloudEntry != nil && context.CloudEntry.APIUrl != "" {
		return NormalizeCloudURL(context.CloudEntry.APIUrl)
	}

	if context.Grafana != nil {
		if root, ok := GCOMRootFromServerURL(context.Grafana.Server); ok {
			return root
		}
	}

	return "https://grafana.com"
}

// NormalizeCloudURL prefixes a Grafana Cloud URL with "https://" when no scheme
// is present, and warns when an insecure http:// scheme is used.
func NormalizeCloudURL(raw string) string {
	if !strings.HasPrefix(raw, "https://") && !strings.HasPrefix(raw, "http://") {
		raw = "https://" + raw
	}
	if strings.HasPrefix(raw, "http://") {
		slog.Warn("Grafana Cloud URL uses http:// - credentials may be sent unencrypted", "url", raw)
	}
	return raw
}

type GrafanaConfig struct {
	// Server is the address of the Grafana server (https://hostname:port/path).
	// Required.
	Server string `env:"GRAFANA_SERVER" json:"server,omitempty" yaml:"server,omitempty"`

	// User to authenticate as with basic authentication.
	// Optional.
	User string `env:"GRAFANA_USER" json:"user,omitempty" yaml:"user,omitempty"`
	// Password to use when using with basic authentication.
	// Optional.
	Password string `datapolicy:"secret" env:"GRAFANA_PASSWORD" json:"password,omitempty" yaml:"password,omitempty"`

	// APIToken is a service account token.
	// See https://grafana.com/docs/grafana/latest/administration/service-accounts/#add-a-token-to-a-service-account-in-grafana
	// Note: if defined, the API Token takes precedence over basic auth credentials.
	// Optional.
	APIToken string `datapolicy:"secret" env:"GRAFANA_TOKEN" json:"token,omitempty" yaml:"token,omitempty"`

	// ProxyEndpoint is the assistant backend URL used as a reverse proxy for
	// OAuth-authenticated requests. Set automatically by `gcx login`.
	// This may differ from Server when cloud routing directs CLI traffic through
	// a separate endpoint (e.g. the assistant app backend).
	ProxyEndpoint string `env:"GRAFANA_PROXY_ENDPOINT" json:"proxy-endpoint,omitempty" yaml:"proxy-endpoint,omitempty"`

	// OAuthToken is the OAuth access token (gat_) obtained via `gcx login`.
	OAuthToken string `datapolicy:"secret" json:"oauth-token,omitempty" yaml:"oauth-token,omitempty"`

	// OAuthRefreshToken is the refresh token (gar_) for renewing OAuthToken.
	OAuthRefreshToken string `datapolicy:"secret" json:"oauth-refresh-token,omitempty" yaml:"oauth-refresh-token,omitempty"`

	// OAuthTokenExpiresAt is the OAuthToken expiration time in RFC3339 format.
	OAuthTokenExpiresAt string `json:"oauth-token-expires-at,omitempty" yaml:"oauth-token-expires-at,omitempty"`

	// OAuthRefreshExpiresAt is the OAuthRefreshToken expiration time in RFC3339 format.
	OAuthRefreshExpiresAt string `json:"oauth-refresh-expires-at,omitempty" yaml:"oauth-refresh-expires-at,omitempty"`

	// AuthMethod is the authentication method stored by gcx login: "oauth", "token", "basic", or "mtls".
	// Empty string is valid for legacy configs; readers should call InferredAuthMethod() in that case.
	AuthMethod string `json:"auth-method,omitempty" yaml:"auth-method,omitempty"`

	// OrgID specifies the organization targeted by this config.
	// Note: required when targeting an on-prem Grafana instance.
	// See StackID for Grafana Cloud instances.
	OrgID int64 `env:"GRAFANA_ORG_ID" json:"org-id,omitempty" yaml:"org-id,omitempty"`

	// StackID specifies the Grafana Cloud stack targeted by this config.
	// Note: required when targeting a Grafana Cloud instance.
	// See OrgID for on-prem Grafana instances.
	StackID int64 `env:"GRAFANA_STACK_ID" json:"stack-id,omitempty" yaml:"stack-id,omitempty"`

	// TLS contains TLS-related configuration settings.
	TLS *TLS `json:"tls,omitempty" yaml:"tls,omitempty"`
}

func (grafana GrafanaConfig) validateNamespace(ctx context.Context, contextName string) error {
	if grafana.OrgID != 0 {
		return nil
	}

	// A configured StackID is authoritative (matching resolveNamespace). Don't pay
	// a /bootdata round-trip on every command just to preflight it — only assert
	// against discovery when a value is already memoized this process. A wrong id
	// still surfaces at the first real API call.
	if grafana.StackID != 0 {
		if discoveredStackID, ok := cachedStackID(grafana.Server); ok && discoveredStackID != grafana.StackID {
			return grafana.stackIDMismatchError(contextName, discoveredStackID)
		}
		return nil
	}

	// No StackID configured: discover it (cached, reused by resolveNamespace).
	if _, err := discoverStackIDCached(ctx, grafana); err != nil {
		return ValidationError{
			Path:    fmt.Sprintf("$.stacks.'%s'.grafana", contextName),
			Message: fmt.Sprintf("missing stacks.%[1]s.grafana.org-id or stacks.%[1]s.grafana.stack-id", contextName),
			Suggestions: []string{
				"Specify the Grafana Org ID for on-prem Grafana",
				"Specify the Grafana Cloud Stack ID for Grafana Cloud",
				"Find your Stack ID at grafana.com under your stack's details page",
			},
		}
	}

	return nil
}

func (grafana GrafanaConfig) stackIDMismatchError(contextName string, discoveredStackID int64) error {
	return ValidationError{
		Path:    fmt.Sprintf("$.stacks.'%s'.grafana", contextName),
		Message: fmt.Sprintf("mismatched stacks.%[1]s.grafana.stack-id, discovered %d - was %d in config", contextName, discoveredStackID, grafana.StackID),
		Suggestions: []string{
			"Specify the correct Grafana Cloud Stack ID for Grafana Cloud or omit the stack-id param",
		},
	}
}

func (grafana GrafanaConfig) Validate(ctx context.Context, contextName string) error {
	if grafana.Server == "" {
		return ValidationError{
			Path:    fmt.Sprintf("$.stacks.'%s'.grafana", contextName),
			Message: "server is required",
			Suggestions: []string{
				"Set the address of the Grafana server to connect to",
			},
		}
	}

	hasProxy := grafana.ProxyEndpoint != ""
	hasOAuth := grafana.OAuthToken != ""
	if hasProxy != hasOAuth {
		return ValidationError{
			Path:    fmt.Sprintf("$.stacks.'%s'.grafana", contextName),
			Message: "incomplete OAuth config: proxy-endpoint and oauth-token must both be set",
			Suggestions: []string{
				"Run `gcx login` to complete the OAuth flow",
				"Or remove partial OAuth fields from the config",
			},
		}
	}

	if err := grafana.validateNamespace(ctx, contextName); err != nil {
		return err
	}

	return nil
}

func (grafana GrafanaConfig) IsEmpty() bool {
	return grafana == GrafanaConfig{}
}

// InferredAuthMethod returns the effective authentication method for this config.
// When AuthMethod is set, it is returned verbatim. Otherwise, the method is inferred
// from populated credential fields: OAuthToken => "oauth"; APIToken => "token";
// User or Password => "basic"; TLS with client cert => "mtls"; no credentials => "unknown".
func (grafana GrafanaConfig) InferredAuthMethod() string {
	if grafana.AuthMethod != "" {
		return grafana.AuthMethod
	}
	if grafana.OAuthToken != "" {
		return "oauth"
	}
	if grafana.APIToken != "" {
		return "token"
	}
	if grafana.User != "" || grafana.Password != "" {
		return "basic"
	}
	if grafana.TLS != nil && (len(grafana.TLS.CertData) > 0 || grafana.TLS.CertFile != "") {
		return "mtls"
	}
	return "unknown"
}

// TLS contains settings to enable transport layer security.
type TLS struct {
	// InsecureSkipTLSVerify disables the validation of the server's SSL certificate.
	// Enabling this will make your HTTPS connections insecure.
	Insecure bool `json:"insecure-skip-verify,omitempty" yaml:"insecure-skip-verify,omitempty"`

	// ServerName is passed to the server for SNI and is used in the client to check server
	// certificates against. If ServerName is empty, the hostname used to contact the
	// server is used.
	ServerName string `json:"server-name,omitempty" yaml:"server-name,omitempty"`

	// CertFile is the path to a PEM-encoded client certificate file.
	// This enables mutual TLS (mTLS) authentication with the server.
	CertFile string `env:"GRAFANA_TLS_CERT_FILE" json:"cert-file,omitempty" yaml:"cert-file,omitempty"`
	// KeyFile is the path to a PEM-encoded client certificate key file.
	KeyFile string `datapolicy:"secret" env:"GRAFANA_TLS_KEY_FILE" json:"key-file,omitempty" yaml:"key-file,omitempty"`
	// CAFile is the path to a PEM-encoded CA certificate bundle file.
	// When set, this CA is used to verify the server's certificate.
	CAFile string `env:"GRAFANA_TLS_CA_FILE" json:"ca-file,omitempty" yaml:"ca-file,omitempty"`

	// CertData holds PEM-encoded bytes (typically read from a client certificate file).
	// Note: this value is base64-encoded in the config file and will be
	// automatically decoded.
	CertData []byte `json:"cert-data,omitempty" yaml:"cert-data,omitempty"`
	// KeyData holds PEM-encoded bytes (typically read from a client certificate key file).
	// Note: this value is base64-encoded in the config file and will be
	// automatically decoded.
	KeyData []byte `datapolicy:"secret" json:"key-data,omitempty" yaml:"key-data,omitempty"`
	// CAData holds PEM-encoded bytes (typically read from a root certificates bundle).
	// Note: this value is base64-encoded in the config file and will be
	// automatically decoded.
	CAData []byte `json:"ca-data,omitempty" yaml:"ca-data,omitempty"`

	// NextProtos is a list of supported application level protocols, in order of preference.
	// Used to populate tls.Config.NextProtos.
	// To indicate to the server http/1.1 is preferred over http/2, set to ["http/1.1", "h2"] (though the server is free to ignore that preference).
	// To use only http/1.1, set to ["http/1.1"].
	NextProtos []string `json:"next-protos,omitempty" yaml:"next-protos,omitempty"`
}

// IsEmpty reports whether all TLS fields are at their zero values.
func (cfg *TLS) IsEmpty() bool {
	return !cfg.Insecure && cfg.ServerName == "" &&
		cfg.CertFile == "" && cfg.KeyFile == "" && cfg.CAFile == "" &&
		len(cfg.CertData) == 0 && len(cfg.KeyData) == 0 && len(cfg.CAData) == 0 &&
		len(cfg.NextProtos) == 0
}

func tlsFileError(description, path string, err error) error {
	if os.IsNotExist(err) {
		return ValidationError{
			Path:    path,
			Message: fmt.Sprintf("TLS %s file not found", description),
			Suggestions: []string{
				"Your client certificates may have expired — renew them and try again",
				"Verify the file path in your gcx config or GRAFANA_TLS_CERT_FILE / GRAFANA_TLS_KEY_FILE env vars",
			},
		}
	}
	return fmt.Errorf("reading TLS %s: %w", description, err)
}

// ResolveFiles reads CertFile, KeyFile, and CAFile from disk and populates
// the corresponding CertData, KeyData, and CAData fields. File-based fields
// take precedence: if both CertFile and CertData are set, CertFile wins.
func (cfg *TLS) ResolveFiles() error {
	if (cfg.CertFile != "") != (cfg.KeyFile != "") {
		return errors.New("both cert-file and key-file must be provided together")
	}
	if cfg.CertFile != "" {
		data, err := os.ReadFile(cfg.CertFile)
		if err != nil {
			return tlsFileError("client certificate", cfg.CertFile, err)
		}
		cfg.CertData = data
	}
	if cfg.KeyFile != "" {
		data, err := os.ReadFile(cfg.KeyFile)
		if err != nil {
			return tlsFileError("client key", cfg.KeyFile, err)
		}
		cfg.KeyData = data
	}
	if cfg.CAFile != "" {
		data, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return tlsFileError("CA certificate", cfg.CAFile, err)
		}
		cfg.CAData = data
	}
	return nil
}

// ToStdTLSConfig converts the TLS configuration into a standard crypto/tls
// Config. It loads client certificates from CertData/KeyData and adds custom
// CA certificates from CAData to the root CA pool.
func (cfg *TLS) ToStdTLSConfig() (*tls.Config, error) {
	if err := cfg.ResolveFiles(); err != nil {
		return nil, err
	}

	tlsCfg := &tls.Config{
		//nolint:gosec
		InsecureSkipVerify: cfg.Insecure,
		MinVersion:         tls.VersionTLS12,
		ServerName:         cfg.ServerName,
		NextProtos:         cfg.NextProtos,
	}

	hasCert := len(cfg.CertData) > 0
	hasKey := len(cfg.KeyData) > 0
	if hasCert != hasKey {
		return nil, errors.New("both cert-data and key-data must be provided together")
	}
	if hasCert && hasKey {
		cert, err := tls.X509KeyPair(cfg.CertData, cfg.KeyData)
		if err != nil {
			return nil, fmt.Errorf("loading TLS client certificate keypair: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	if len(cfg.CAData) > 0 {
		pool, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("loading system certificate pool: %w", err)
		}
		if !pool.AppendCertsFromPEM(cfg.CAData) {
			return nil, errors.New("failed to parse TLS CA certificate data")
		}
		tlsCfg.RootCAs = pool
	}

	return tlsCfg, nil
}

// Minify returns a trimmed down version of the given configuration containing
// only the current context and the relevant options it directly depends on
// (its stack, its cloud entry, and the global resources settings).
func Minify(config Config) (Config, error) {
	minified := config

	if config.CurrentContext == "" {
		return Config{}, errors.New("current-context must be defined in order to minify")
	}

	minified.Contexts = make(map[string]*Context, 1)
	minified.Stacks = nil
	minified.Cloud = nil
	cur := config.Contexts[config.CurrentContext]
	if cur != nil {
		minified.Contexts[config.CurrentContext] = cur
		if stack := config.Stacks[cur.Stack]; cur.Stack != "" && stack != nil {
			minified.Stacks = map[string]*StackConfig{cur.Stack: stack}
		}
		if entry := config.Cloud[cur.Cloud]; cur.Cloud != "" && entry != nil {
			minified.Cloud = map[string]*CloudEntry{cur.Cloud: entry}
		}
	}

	return minified, nil
}
