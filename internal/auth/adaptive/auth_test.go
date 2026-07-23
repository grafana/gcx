package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	auth "github.com/grafana/gcx/internal/auth/adaptive"
	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveSignalAuthRejectsAutoLocalCacheMissBeforeNetwork(t *testing.T) {
	homeDir := t.TempDir()
	workDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())
	t.Setenv(config.ConfigFileEnvVar, "")
	t.Chdir(workDir)

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	write := func(path, content string) {
		t.Helper()
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600))
	}
	write(filepath.Join(homeDir, ".config", "gcx", "config.yaml"), `
version: 1
cloud:
  grafana-com:
    token: user-cloud-token
    api-url: `+server.URL+`
    oauth-url: `+server.URL+`
contexts:
  user:
    cloud: grafana-com
current-context: user
`)
	write(filepath.Join(workDir, config.LocalConfigFileName), `
version: 1
stacks:
  repository:
    slug: victim-stack
    grafana:
      server: `+server.URL+`
      tls:
        insecure-skip-verify: true
contexts:
  repository:
    stack: repository
    cloud: grafana-com
current-context: repository
`)

	_, err := auth.ResolveSignalAuth(context.Background(), &providers.ConfigLoader{}, "logs")
	require.ErrorContains(t, err, "auto-discovered repository config")
	assert.Contains(t, err.Error(), "--config")
	assert.Zero(t, requests.Load(), "auto-local cache miss must fail before GCOM or adaptive API traffic")
}

func TestResolveSignalAuthUsesCurrentGCOMDestinationInsteadOfStoredCache(t *testing.T) {
	var gcomRequests, staleRequests atomic.Int32
	stale := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		staleRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(stale.Close)

	currentURL := "https://logs-current.example.invalid"
	gcom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		gcomRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(cloud.StackInfo{
			HLInstanceURL: currentURL,
			HLInstanceID:  4242,
		}); err != nil {
			t.Errorf("encode GCOM response: %v", err)
		}
	}))
	t.Cleanup(gcom.Close)

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte(strings.TrimSpace(`
version: 1
stacks:
  default:
    slug: current-stack
    providers:
      adaptive:
        logs-tenant-url: `+stale.URL+`
        logs-tenant-id: "9999"
cloud:
  grafana-com:
    token: current-cloud-token
    api-url: `+gcom.URL+`
    oauth-url: `+gcom.URL+`
contexts:
  default:
    stack: default
    cloud: grafana-com
current-context: default
`)+"\n"), 0o600))

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(configFile)
	got, err := auth.ResolveSignalAuth(context.Background(), loader, "logs")
	require.NoError(t, err)
	assert.Equal(t, currentURL, got.BaseURL)
	assert.Equal(t, 4242, got.TenantID)
	assert.Equal(t, "current-cloud-token", got.APIToken)
	assert.Equal(t, int32(1), gcomRequests.Load())
	assert.Zero(t, staleRequests.Load())
}

func TestExtractSignalInfo(t *testing.T) {
	stack := cloud.StackInfo{
		HMInstancePromURL: "https://prometheus-prod-01-eu-west-0.grafana.net",
		HMInstancePromID:  12345,
		HLInstanceURL:     "https://logs-prod-eu-west-0.grafana.net",
		HLInstanceID:      67890,
		HTInstanceURL:     "https://tempo-prod-eu-west-0.grafana.net",
		HTInstanceID:      11111,
	}

	tests := []struct {
		name       string
		signal     string
		wantURL    string
		wantID     int
		wantErrMsg string
	}{
		{
			name:    "metrics",
			signal:  "metrics",
			wantURL: "https://prometheus-prod-01-eu-west-0.grafana.net",
			wantID:  12345,
		},
		{
			name:    "logs",
			signal:  "logs",
			wantURL: "https://logs-prod-eu-west-0.grafana.net",
			wantID:  67890,
		},
		{
			name:    "traces",
			signal:  "traces",
			wantURL: "https://tempo-prod-eu-west-0.grafana.net",
			wantID:  11111,
		},
		{
			name:       "unknown signal",
			signal:     "profiles",
			wantErrMsg: "unknown signal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, id, err := auth.ExtractSignalInfo(stack, tt.signal)
			if tt.wantErrMsg != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if url != tt.wantURL {
				t.Errorf("URL = %q, want %q", url, tt.wantURL)
			}
			if id != tt.wantID {
				t.Errorf("ID = %d, want %d", id, tt.wantID)
			}
		})
	}
}

func TestExtractSignalInfoMissingURL(t *testing.T) {
	stack := cloud.StackInfo{
		HMInstancePromID: 12345,
	}
	_, _, err := auth.ExtractSignalInfo(stack, "metrics")
	if err == nil {
		t.Fatal("expected error for missing URL, got nil")
	}
}

func TestExtractSignalInfoMissingID(t *testing.T) {
	stack := cloud.StackInfo{
		HMInstancePromURL: "https://prometheus.grafana.net",
	}
	_, _, err := auth.ExtractSignalInfo(stack, "metrics")
	if err == nil {
		t.Fatal("expected error for missing ID, got nil")
	}
}
