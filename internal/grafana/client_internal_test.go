package grafana

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
)

func TestGetVersionUsesOnlySelectedGrafanaAuth(t *testing.T) {
	type observation struct {
		authorization string
		clientCerts   int
	}
	observed := make(chan observation, 4)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- observation{
			authorization: r.Header.Get("Authorization"),
			clientCerts:   len(r.TLS.PeerCertificates),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"12.0.0","commit":"test","database":"ok"}`))
	}))
	server.TLS = &tls.Config{ClientAuth: tls.RequestClientCert}
	server.StartTLS()
	t.Cleanup(server.Close)

	certData := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: server.TLS.Certificates[0].Certificate[0],
	})
	keyDER, err := x509.MarshalPKCS8PrivateKey(server.TLS.Certificates[0].PrivateKey)
	if err != nil {
		t.Fatalf("marshal client key: %v", err)
	}
	keyData := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	tests := []struct {
		name              string
		grafana           config.GrafanaConfig
		runtimeToken      string
		wantAuthorization string
		wantClientCerts   int
	}{
		{
			name: "token ignores stale Basic and mTLS identity",
			grafana: config.GrafanaConfig{
				AuthMethod: "token",
				APIToken:   "selected-token",
				User:       "stale-user",
				Password:   "stale-password",
			},
			wantAuthorization: "Bearer selected-token",
		},
		{
			name: "Basic ignores stale token and mTLS identity",
			grafana: config.GrafanaConfig{
				AuthMethod: "basic",
				APIToken:   "stale-token",
				User:       "selected-user",
				Password:   "selected-password",
			},
			wantAuthorization: "Basic c2VsZWN0ZWQtdXNlcjpzZWxlY3RlZC1wYXNzd29yZA==",
		},
		{
			name: "OAuth health sends no stale direct credential or mTLS identity",
			grafana: config.GrafanaConfig{
				AuthMethod:        "oauth",
				APIToken:          "stale-token",
				User:              "stale-user",
				Password:          "stale-password",
				ProxyEndpoint:     "https://oauth-proxy.invalid",
				OAuthToken:        "selected-oauth",
				OAuthRefreshToken: "selected-refresh",
			},
		},
		{
			name: "mTLS health sends client identity but no stale HTTP credential",
			grafana: config.GrafanaConfig{
				AuthMethod: "mtls",
				APIToken:   "stale-token",
				User:       "stale-user",
				Password:   "stale-password",
			},
			wantClientCerts: 1,
		},
		{
			name: "environment token overrides persisted OAuth",
			grafana: config.GrafanaConfig{
				AuthMethod:        "oauth",
				ProxyEndpoint:     "https://oauth-proxy.invalid",
				OAuthToken:        "persisted-oauth",
				OAuthRefreshToken: "persisted-refresh",
			},
			runtimeToken:      "runtime-token",
			wantAuthorization: "Bearer runtime-token",
		},
		{
			name: "environment token overrides persisted Basic",
			grafana: config.GrafanaConfig{
				AuthMethod: "basic",
				User:       "persisted-user",
				Password:   "persisted-password",
			},
			runtimeToken:      "runtime-token",
			wantAuthorization: "Bearer runtime-token",
		},
		{
			name: "environment token overrides persisted mTLS",
			grafana: config.GrafanaConfig{
				AuthMethod: "mtls",
			},
			runtimeToken:      "runtime-token",
			wantAuthorization: "Bearer runtime-token",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			grafanaCfg := tc.grafana
			grafanaCfg.Server = server.URL
			grafanaCfg.StackID = 12345
			grafanaCfg.TLS = &config.TLS{
				Insecure: true,
				CertData: certData,
				KeyData:  keyData,
			}

			resolved := &config.Context{Name: "selected", Grafana: &grafanaCfg}
			if tc.runtimeToken != "" {
				t.Setenv("GRAFANA_TOKEN", tc.runtimeToken)
				if err := config.ParseEnvIntoContext(resolved); err != nil {
					t.Fatalf("ParseEnvIntoContext() error = %v", err)
				}
			}

			_, raw, err := GetVersion(context.Background(), resolved)
			if err != nil {
				t.Fatalf("GetVersion() error = %v", err)
			}
			if raw != "12.0.0" {
				t.Fatalf("GetVersion() raw = %q, want %q", raw, "12.0.0")
			}
			got := <-observed
			if got.authorization != tc.wantAuthorization {
				t.Errorf("Authorization = %q, want %q", got.authorization, tc.wantAuthorization)
			}
			if got.clientCerts != tc.wantClientCerts {
				t.Errorf("client certificates = %d, want %d", got.clientCerts, tc.wantClientCerts)
			}
		})
	}
}
