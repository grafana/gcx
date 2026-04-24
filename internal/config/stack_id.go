package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	authlib "github.com/grafana/authlib/types"
)

var errBootdataNonOK = errors.New("bootdata request failed")

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
