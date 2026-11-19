package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	authlib "github.com/grafana/authlib/types"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/version"
)

var (
	errBootdataNonOK = errors.New("bootdata request failed")
	errWhoamiNonOK   = errors.New("whoami request failed")
	errNotCloudStack = errors.New("whoami namespace is not a cloud stack")
)

// stackIDCache memoizes successful stack-ID discoveries per server for
// the lifetime of the process, so repeated REST-config builds in a single
// invocation don't repay the network round-trip.
//
//nolint:gochecknoglobals // process-wide positive cache; only successes stored.
var (
	stackIDCacheMu sync.Mutex
	stackIDCache   = map[string]int64{}
)

// StackID returns the Grafana Cloud stack ID encoded in the resolved namespace
// (e.g. "stacks-12345" -> 12345), or 0 when the namespace is not a cloud stack
// namespace (on-prem org namespaces, or an unresolved/empty namespace). It lets
// callers recover the discovered stack ID without a second discovery round-trip.
func (n *NamespacedRESTConfig) StackID() int64 {
	info, err := authlib.ParseNamespace(n.Namespace)
	if err != nil {
		return 0
	}
	return info.StackID
}

// resolveNamespace returns the Kubernetes namespace for a Grafana context.
// A configured StackID is authoritative and resolves locally with no network
// call. Otherwise the cloud stack ID is discovered (memoized per server), with
// OrgID as the on-prem fallback when discovery fails.
func resolveNamespace(ctx context.Context, cfg GrafanaConfig, mode grafanaAuthMode) string {
	// A configured StackID is authoritative; Validate already guards mismatches
	// against discovery, so trust it here and skip the network round-trip.
	if cfg.StackID != 0 {
		return authlib.CloudNamespaceFormatter(cfg.StackID)
	}

	// No StackID configured: discover it (cached). Discovery takes precedence
	// over OrgID, matching prior behavior.
	if discoveredStackID, err := discoverStackIDCached(ctx, cfg, mode); err == nil {
		return authlib.CloudNamespaceFormatter(discoveredStackID)
	}

	if cfg.OrgID != 0 {
		return authlib.OrgNamespaceFormatter(cfg.OrgID)
	}
	return authlib.CloudNamespaceFormatter(cfg.StackID)
}

// cachedStackID peeks the process-lifetime discovery cache for a server without
// issuing a network round-trip. ok is false when no successful discovery has
// been memoized for the server yet.
func cachedStackID(server string) (int64, bool) {
	key := strings.TrimSuffix(server, "/")
	stackIDCacheMu.Lock()
	defer stackIDCacheMu.Unlock()
	id, ok := stackIDCache[key]
	return id, ok
}

// discoverStackIDCached discovers the stack ID with a process-lifetime positive
// cache keyed by server URL. Failures are not cached so a transient error does
// not pin a missing stack ID for the rest of the process.
//
// The authenticated whoami endpoint is tried first; the unauthenticated
// /bootdata endpoint is only used when whoami is unavailable (older Grafana,
// missing or expired credentials). A whoami answer with a non-stack namespace
// is definitive (on-prem) and skips /bootdata.
func discoverStackIDCached(ctx context.Context, cfg GrafanaConfig, mode grafanaAuthMode) (int64, error) {
	key := strings.TrimSuffix(cfg.Server, "/")

	stackIDCacheMu.Lock()
	cached, ok := stackIDCache[key]
	stackIDCacheMu.Unlock()
	if ok {
		return cached, nil
	}

	id, err := discoverStackIDWhoami(ctx, cfg, mode)
	if err != nil && !errors.Is(err, errNotCloudStack) {
		id, err = DiscoverStackID(ctx, cfg)
	}
	if err != nil {
		return 0, err
	}

	stackIDCacheMu.Lock()
	stackIDCache[key] = id
	stackIDCacheMu.Unlock()
	return id, nil
}

const whoamiPath = "/apis/iam.grafana.app/v0alpha1/whoami"

// discoverStackIDWhoami resolves the cloud stack ID from the authenticated
// iam.grafana.app whoami endpoint, whose "namespace" field is "stacks-<id>" on
// Grafana Cloud. A successful response with a non-stack namespace returns an
// error wrapping errNotCloudStack.
//
// The request carries the selected credentials directly rather than going
// through the OAuth RefreshTransport: token-persistence hooks are only wired
// after the REST config is built, so a refresh here would rotate the refresh
// token without saving it. An expired access token simply fails the probe.
func discoverStackIDWhoami(ctx context.Context, cfg GrafanaConfig, mode grafanaAuthMode) (int64, error) {
	base := strings.TrimSuffix(cfg.Server, "/")
	if mode == grafanaAuthOAuth {
		if cfg.OAuthToken == "" || cfg.ProxyEndpoint == "" {
			return 0, errors.New("whoami: no OAuth access token")
		}
		base = strings.TrimSuffix(cfg.ProxyEndpoint, "/") + "/api/cli/v1/proxy"
	}

	client, err := newDiscoveryHTTPClient(cfg)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+whoamiPath, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", version.UserAgent())
	switch mode {
	case grafanaAuthOAuth:
		req.Header.Set("Authorization", "Bearer "+cfg.OAuthToken)
	case grafanaAuthToken:
		req.Header.Set("Authorization", "Bearer "+cfg.APIToken)
	case grafanaAuthBasic:
		req.SetBasicAuth(cfg.User, cfg.Password)
	case grafanaAuthMTLS, grafanaAuthUnknown:
		// Client certificate (if any) is carried by the TLS config.
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%w: status %d", errWhoamiNonOK, resp.StatusCode)
	}

	var payload struct {
		Namespace string `json:"namespace"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, err
	}
	namespace := strings.TrimSpace(payload.Namespace)
	if namespace == "" {
		// Not a whoami payload we understand; let the caller fall back.
		return 0, errors.New("whoami: empty namespace")
	}
	ns, err := authlib.ParseNamespace(namespace)
	if err != nil {
		return 0, err
	}
	if ns.StackID == 0 {
		return 0, fmt.Errorf("%w: %q", errNotCloudStack, namespace)
	}
	return ns.StackID, nil
}

// DiscoverStackID attempts to discover a Grafana Cloud stack namespace via the
// unauthenticated /bootdata endpoint (legacy fallback for whoami).
// It returns the parsed stack ID when the response matches the expected format.
func DiscoverStackID(ctx context.Context, cfg GrafanaConfig) (int64, error) {
	bootdataURL, err := buildBootdataURL(cfg.Server)
	if err != nil {
		return 0, err
	}

	client, err := newDiscoveryHTTPClient(cfg)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bootdataURL.String(), nil)
	if err != nil {
		return 0, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%w: status %d", errBootdataNonOK, resp.StatusCode)
	}

	var payload struct {
		Settings struct {
			Namespace string `json:"namespace"`
		} `json:"settings"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, err
	}

	namespace := strings.TrimSpace(payload.Settings.Namespace)
	if namespace == "" {
		return 0, errors.New("empty namespace")
	}

	ns, err := authlib.ParseNamespace(namespace)
	if err != nil {
		return 0, err
	}

	if ns.StackID == 0 {
		return 0, errors.New("discovered stack id is 0")
	}

	return ns.StackID, nil
}

func buildBootdataURL(server string) (*url.URL, error) {
	parsed, err := url.Parse(server)
	if err != nil {
		return nil, err
	}

	trimmedPath := strings.TrimSuffix(parsed.Path, "/")
	parsed.Path = trimmedPath + "/bootdata"
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return parsed, nil
}

func newDiscoveryHTTPClient(cfg GrafanaConfig) (*http.Client, error) {
	// No retry: discovery is optional (callers fall back to the org
	// namespace) and runs on most commands, so it must fail fast.
	opts := httputils.ClientOpts{Timeout: 5 * time.Second, DisableRetry: true}
	if cfg.TLS != nil {
		tlsCfg, err := cfg.TLS.ToStdTLSConfig()
		if err != nil {
			return nil, fmt.Errorf("TLS configuration: %w", err)
		}
		opts.TLSConfig = tlsCfg
	}

	return httputils.NewClient(opts), nil
}
