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
)

var errBootdataNonOK = errors.New("bootdata request failed")

// stackIDCache memoizes successful /bootdata stack-ID discoveries per server for
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
// callers recover the discovered stack ID without a second /bootdata round-trip.
func (n *NamespacedRESTConfig) StackID() int64 {
	info, err := authlib.ParseNamespace(n.Namespace)
	if err != nil {
		return 0
	}
	return info.StackID
}

// resolveNamespace returns the Kubernetes namespace for a Grafana context.
// A configured StackID is authoritative and resolves locally with no network
// call. Otherwise the cloud stack ID is discovered via /bootdata (memoized per
// server), with OrgID as the on-prem fallback when discovery fails.
func resolveNamespace(ctx context.Context, cfg GrafanaConfig) string {
	// A configured StackID is authoritative; Validate already guards mismatches
	// against discovery, so trust it here and skip the network round-trip.
	if cfg.StackID != 0 {
		return authlib.CloudNamespaceFormatter(cfg.StackID)
	}

	// No StackID configured: discover it (cached). Discovery takes precedence
	// over OrgID, matching prior behavior.
	if discoveredStackID, err := discoverStackIDCached(ctx, cfg); err == nil {
		return authlib.CloudNamespaceFormatter(discoveredStackID)
	}

	if cfg.OrgID != 0 {
		return authlib.OrgNamespaceFormatter(cfg.OrgID)
	}
	return authlib.CloudNamespaceFormatter(cfg.StackID)
}

// discoverStackIDCached wraps DiscoverStackID with a process-lifetime positive
// cache keyed by server URL. Failures are not cached so a transient error does
// not pin a missing stack ID for the rest of the process.
func discoverStackIDCached(ctx context.Context, cfg GrafanaConfig) (int64, error) {
	key := strings.TrimSuffix(cfg.Server, "/")

	stackIDCacheMu.Lock()
	cached, ok := stackIDCache[key]
	stackIDCacheMu.Unlock()
	if ok {
		return cached, nil
	}

	id, err := DiscoverStackID(ctx, cfg)
	if err != nil {
		return 0, err
	}

	stackIDCacheMu.Lock()
	stackIDCache[key] = id
	stackIDCacheMu.Unlock()
	return id, nil
}

// DiscoverStackID attempts to discover a Grafana Cloud stack namespace via the /bootdata endpoint.
// It returns the parsed stack ID when the response matches the expected format.
func DiscoverStackID(ctx context.Context, cfg GrafanaConfig) (int64, error) {
	bootdataURL, err := buildBootdataURL(cfg.Server)
	if err != nil {
		return 0, err
	}

	client, err := newBootdataHTTPClient(cfg)
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

func newBootdataHTTPClient(cfg GrafanaConfig) (*http.Client, error) {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}

	if cfg.TLS != nil {
		tlsCfg, err := cfg.TLS.ToStdTLSConfig()
		if err != nil {
			return nil, fmt.Errorf("TLS configuration: %w", err)
		}
		transport.TLSClientConfig = tlsCfg
	}

	return &http.Client{
		Timeout:   5 * time.Second,
		Transport: transport,
	}, nil
}
