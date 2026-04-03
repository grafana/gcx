package faro

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/grafana-app-sdk/logging"
	"k8s.io/client-go/rest"
)

const faroPluginSettingsPath = "/api/plugins/grafana-kowalski-app/settings"

// DiscoverFaroAPIURL queries the Grafana Faro plugin settings to discover
// the direct Faro API endpoint URL (jsonData.api_endpoint).
func DiscoverFaroAPIURL(ctx context.Context, cfg config.NamespacedRESTConfig) (string, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return "", fmt.Errorf("faro: create HTTP client for discovery: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.Host+faroPluginSettingsPath, nil)
	if err != nil {
		return "", fmt.Errorf("faro: create discovery request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("faro: discover API URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("faro: plugin settings returned HTTP %d (is grafana-kowalski-app installed?)", resp.StatusCode)
	}

	var body struct {
		JSONData struct {
			APIEndpoint string `json:"api_endpoint"`
		} `json:"jsonData"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("faro: decode plugin settings: %w", err)
	}

	if body.JSONData.APIEndpoint == "" {
		return "", errors.New("faro: api_endpoint not configured in Faro plugin settings")
	}

	return body.JSONData.APIEndpoint, nil
}

// GenerateBundleID creates a bundle ID matching the faro-cli pattern: {timestamp}-{randomHex5}.
func GenerateBundleID() string {
	ts := time.Now().UnixMilli()
	b := make([]byte, 3) // 3 bytes = 6 hex chars, trimmed to 5
	_, _ = rand.Read(b)
	return fmt.Sprintf("%d-%s", ts, hex.EncodeToString(b)[:5])
}

// UploadSourcemap uploads a sourcemap file to the direct Faro API.
// Auth uses Bearer {stackId}:{token} format per the faro-bundler-plugins convention.
func UploadSourcemap(ctx context.Context, faroAPIURL string, stackID int, token string, appID string, bundleID string, reader io.Reader, contentType string) error {
	endpoint := fmt.Sprintf("%s/api/v1/app/%s/sourcemaps/%s",
		strings.TrimRight(faroAPIURL, "/"), appID, bundleID)

	log := logging.FromContext(ctx)
	log.Info("Uploading sourcemap", "app_id", appID, "bundle_id", bundleID, "endpoint", endpoint)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, reader)
	if err != nil {
		return fmt.Errorf("faro: create upload request: %w", err)
	}

	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %d:%s", stackID, token))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("faro: upload sourcemap: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("faro: upload sourcemap: status %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}
