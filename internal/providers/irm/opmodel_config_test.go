//nolint:testpackage // white-box test exercises the private authenticated config loader
package irm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	fail "github.com/grafana/gcx/internal/gcxerrors"
)

func TestLoadOpModelUsesAuthenticatedGrafanaProxy(t *testing.T) {
	t.Parallel()

	var gotPath, gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.EscapedPath()
		gotAuthorization = req.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"container":"INC-1","revision":"1"}`))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configBody := fmt.Sprintf(`current-context: test
contexts:
  test:
    grafana:
      server: %s
      stack-id: 123
      token: test-token
`, server.URL)
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loader := &configLoader{}
	loader.SetConfigFile(configPath)
	api, err := loader.LoadOpModel(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("LoadOpModel() error = %v", err)
	}
	if _, err := api.GetContainer(context.Background(), getContainerRequest{Container: "INC-1"}); err != nil {
		t.Fatalf("GetContainer() error = %v", err)
	}

	if gotPath != "/api/plugins/grafana-irm-app/resources/opmodel/apis/irm.grafana.app/v1alpha1/containers/INC-1" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuthorization != "Bearer test-token" {
		t.Errorf("Authorization = %q", gotAuthorization)
	}
}

func TestRequireSharingOAuthUsesEffectiveCredentials(t *testing.T) {
	tests := map[string]struct {
		fields   string
		envToken string
		allowed  bool
	}{
		"oauth":                                 {fields: "auth-method: oauth\n      oauth-token: test-oauth-token\n      proxy-endpoint: http://localhost:9091", allowed: true},
		"legacy oauth":                          {fields: "oauth-token: test-oauth-token\n      proxy-endpoint: http://localhost:9091", allowed: true},
		"refresh only oauth":                    {fields: "auth-method: oauth\n      oauth-refresh-token: test-refresh-token\n      proxy-endpoint: http://localhost:9091", allowed: true},
		"service account":                       {fields: "auth-method: token\n      token: test-service-token"},
		"legacy service account":                {fields: "token: test-service-token"},
		"basic":                                 {fields: "auth-method: basic\n      user: deric\n      password: test-password"},
		"service account overrides stale oauth": {fields: "auth-method: token\n      token: test-service-token\n      oauth-token: test-oauth-token\n      proxy-endpoint: http://localhost:9091"},
		"environment token overrides oauth":     {fields: "auth-method: oauth\n      oauth-token: test-oauth-token\n      proxy-endpoint: http://localhost:9091", envToken: "test-service-token"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Setenv("GRAFANA_TOKEN", tc.envToken)
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			body := "current-context: test\ncontexts:\n  test:\n    grafana:\n      server: http://localhost:3000\n      stack-id: 123\n      " + tc.fields + "\n"
			if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			loader := &configLoader{}
			loader.SetConfigFile(configPath)
			err := loader.RequireSharingOAuth(context.Background())
			if tc.allowed {
				if err != nil {
					t.Fatalf("OAuth rejected: %v", err)
				}
				return
			}
			var detailed *fail.DetailedError
			if !errors.As(err, &detailed) || detailed.ExitCode == nil || *detailed.ExitCode != fail.ExitAuthFailure || !strings.Contains(detailed.Summary, "OAuth") {
				t.Fatalf("expected actionable OAuth authentication error, got %v", err)
			}
		})
	}
}
