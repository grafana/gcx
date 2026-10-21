package loki

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/grafana/mcp-grafana/lokiguard"
	"k8s.io/client-go/rest"
)

const (
	// policyPathCLI is the assistant API route as exposed to CLI (gat_ OAuth)
	// callers through the assistant proxy endpoint: the CLI surface lives under
	// /api/cli/v1, next to the A2A routes gcx already calls there.
	policyPathCLI = "/api/cli/v1/query-policy/loki"
	// policyPathPlugin is the same route reached through the Grafana plugin
	// proxy, which is how service-account token callers reach the assistant.
	policyPathPlugin = "/api/plugins/grafana-assistant-app/resources/api/v1/query-policy/loki"

	policyFetchTimeout = 2 * time.Second
	policyCacheTTL     = 5 * time.Minute
	policyMaxBodyBytes = 64 << 10

	// POC: GCX_LOKI_POLICY_URL forces the guard on and names the full URL to
	// fetch the policy from. Production selection is "Grafana Cloud target"
	// (config.Context.IsCloud); a local Grafana can never satisfy that.
	policyURLEnv = "GCX_LOKI_POLICY_URL"
	// GCX_LOKI_POLICY_CACHE_DIR overrides the on-disk policy cache directory.
	policyCacheDirEnv = "GCX_LOKI_POLICY_CACHE_DIR"
)

// policyMode is how a tenant wants guard decisions applied.
type policyMode string

const (
	modeEnforce policyMode = "enforce"
	modeShadow  policyMode = "shadow"
	modeOff     policyMode = "off"
)

// queryPolicy is the tenant policy served by the assistant API at
// GET /api/v1/query-policy/loki.
type queryPolicy struct {
	Mode            policyMode `json:"mode"`
	MaxBytes        int64      `json:"maxBytes"`
	MaxRangeSeconds int64      `json:"maxRangeSeconds"`
	// Source is "default" or "tenant" (an admin override exists).
	Source string `json:"source"`
}

// defaultMaxBytes is the scan budget used when the tenant policy cannot be
// fetched: generous enough not to block ordinary queries, low enough to stop a
// catch-all selector over a long window.
const defaultMaxBytes = 50 << 30 // 50 GiB

// defaultQueryPolicy is used when the tenant policy cannot be fetched.
func defaultQueryPolicy() queryPolicy {
	return queryPolicy{Mode: modeEnforce, MaxBytes: defaultMaxBytes, MaxRangeSeconds: 24 * 60 * 60, Source: "default"}
}

func (p queryPolicy) validate() error {
	switch p.Mode {
	case modeEnforce, modeShadow, modeOff:
	default:
		return fmt.Errorf("unknown policy mode %q", p.Mode)
	}
	if p.MaxBytes < 0 || p.MaxRangeSeconds < 0 {
		return errors.New("negative policy budget")
	}
	return nil
}

func (p queryPolicy) guardPolicy() lokiguard.Policy {
	return lokiguard.Policy{
		MaxBytes: p.MaxBytes,
		MaxRange: time.Duration(p.MaxRangeSeconds) * time.Second,
	}
}

// policySource says where to fetch the policy from and with which client.
type policySource struct {
	// url is empty when the target is Cloud but no policy route is reachable
	// with the selected auth method; the built-in default then applies.
	url    string
	client *http.Client
	// cacheTTL is zero to disable the disk cache.
	cacheTTL time.Duration
	cacheDir string
}

// resolveGuardTarget decides whether the guard applies to this invocation and
// where its policy comes from. It reports ok=false for non-Cloud targets,
// which are out of scope by design.
func resolveGuardTarget(cfgCtx *config.Context, cfg config.NamespacedRESTConfig) (policySource, bool) {
	if override := strings.TrimSpace(os.Getenv(policyURLEnv)); override != "" {
		// POC: plain client, so the target's credentials are never sent to the
		// override URL. No cache, so a stub can be flipped live.
		return policySource{url: override, client: &http.Client{}}, true
	}

	if cfgCtx == nil || !cfgCtx.IsCloud() || cfgCtx.Grafana == nil {
		return policySource{}, false
	}

	src := policySource{cacheTTL: policyCacheTTL, cacheDir: policyCacheDir()}
	method, err := cfgCtx.EffectiveGrafanaAuthMethod()
	if err != nil {
		return src, true // Cloud, but credentials unusable: default policy.
	}

	switch method {
	case "oauth":
		if endpoint := strings.TrimSuffix(cfgCtx.Grafana.ProxyEndpoint, "/"); endpoint != "" {
			src.url = endpoint + policyPathCLI
		}
	case "token":
		if server := strings.TrimSuffix(cfgCtx.Grafana.Server, "/"); server != "" {
			src.url = server + policyPathPlugin
		}
	}
	if src.url != "" {
		httpClient, err := rest.HTTPClientFor(&cfg.Config)
		if err != nil {
			src.url = ""
		} else {
			src.client = httpClient
		}
	}
	return src, true
}

func policyCacheDir() string {
	if dir := os.Getenv(policyCacheDirEnv); dir != "" && filepath.IsAbs(dir) {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "gcx", "loki-policy")
	}
	return filepath.Join(home, ".cache", "gcx", "loki-policy")
}

// fetch returns the effective policy and, when it had to fall back to the
// built-in default, the reason. A failure never fails the query.
func (s policySource) fetch(ctx context.Context, now time.Time) (queryPolicy, string) {
	if s.url == "" {
		return defaultQueryPolicy(), "no policy endpoint is reachable with the configured authentication"
	}

	cachePath := ""
	if s.cacheTTL > 0 && s.cacheDir != "" {
		sum := sha256.Sum256([]byte(s.url))
		cachePath = filepath.Join(s.cacheDir, hex.EncodeToString(sum[:8])+".json")
		if p, ok := readPolicyCache(cachePath, now, s.cacheTTL); ok {
			return p, ""
		}
	}

	ctx, cancel := context.WithTimeout(ctx, policyFetchTimeout)
	defer cancel()
	p, err := fetchPolicy(ctx, s.client, s.url)
	if err != nil {
		return defaultQueryPolicy(), err.Error()
	}
	if cachePath != "" {
		writePolicyCache(ctx, cachePath, p, now)
	}
	return p, ""
}

func fetchPolicy(ctx context.Context, client *http.Client, url string) (queryPolicy, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return queryPolicy{}, fmt.Errorf("build policy request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return queryPolicy{}, fmt.Errorf("fetch policy: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, policyMaxBodyBytes))
	if err != nil {
		return queryPolicy{}, fmt.Errorf("read policy: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return queryPolicy{}, fmt.Errorf("fetch policy: HTTP %d", resp.StatusCode)
	}

	// The assistant API wraps responses: {"status":"success","data":{...}}.
	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return queryPolicy{}, fmt.Errorf("decode policy: %w", err)
	}
	if envelope.Status != "success" || len(envelope.Data) == 0 {
		return queryPolicy{}, fmt.Errorf("decode policy: unexpected response status %q", envelope.Status)
	}
	var p queryPolicy
	if err := json.Unmarshal(envelope.Data, &p); err != nil {
		return queryPolicy{}, fmt.Errorf("decode policy: %w", err)
	}
	if err := p.validate(); err != nil {
		return queryPolicy{}, err
	}
	return p, nil
}

type policyCacheEntry struct {
	FetchedAt time.Time   `json:"fetchedAt"`
	Policy    queryPolicy `json:"policy"`
}

func readPolicyCache(path string, now time.Time, ttl time.Duration) (queryPolicy, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return queryPolicy{}, false
	}
	var e policyCacheEntry
	if err := json.Unmarshal(data, &e); err != nil || e.Policy.validate() != nil {
		return queryPolicy{}, false
	}
	if age := now.Sub(e.FetchedAt); age < 0 || age > ttl {
		return queryPolicy{}, false
	}
	return e.Policy, true
}

// writePolicyCache is best effort: a cache that cannot be written only costs
// one extra fetch next time.
func writePolicyCache(ctx context.Context, path string, p queryPolicy, now time.Time) {
	data, err := json.Marshal(policyCacheEntry{FetchedAt: now, Policy: p})
	if err == nil {
		err = os.MkdirAll(filepath.Dir(path), 0o700)
	}
	if err == nil {
		err = os.WriteFile(path, data, 0o600)
	}
	if err != nil {
		logging.FromContext(ctx).Debug("could not cache Loki query policy", slog.String("error", err.Error()))
	}
}
