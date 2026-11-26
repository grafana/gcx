package grafana

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestSignedInUserID(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		orgID      int64
		wantID     int64
		wantStatus int
		wantOrg    string
	}{
		{name: "signed-in user", status: http.StatusOK, body: `{"id":42,"login":"admin"}`, wantID: 42},
		{name: "org header", status: http.StatusOK, body: `{"id":7}`, orgID: 3, wantID: 7, wantOrg: "3"},
		{name: "anonymous user", status: http.StatusOK, body: `{"id":0}`, wantID: 0},
		{name: "rejected credentials", status: http.StatusUnauthorized, body: `{"message":"invalid username or password: hunter2"}`, wantStatus: http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotOrg, gotAgent string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotOrg, gotAgent = r.URL.Path, r.Header.Get(orgIDHeader), r.Header.Get("User-Agent")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(server.Close)
			cfgCtx := &config.Context{Name: "test", Grafana: &config.GrafanaConfig{
				Server:     server.URL + "/grafana/",
				AuthMethod: "basic",
				User:       "admin",
				Password:   "hunter2",
				OrgID:      tc.orgID,
			}}

			id, err := SignedInUserID(context.Background(), cfgCtx)

			if gotPath != "/grafana/api/user" {
				t.Errorf("path = %q, want /grafana/api/user", gotPath)
			}
			if gotOrg != tc.wantOrg {
				t.Errorf("%s = %q, want %q", orgIDHeader, gotOrg, tc.wantOrg)
			}
			if gotAgent == "" {
				t.Error("User-Agent not set")
			}
			if tc.wantStatus != 0 {
				var statusErr *StatusError
				if !errors.As(err, &statusErr) || statusErr.StatusCode != tc.wantStatus {
					t.Fatalf("err = %v, want StatusError %d", err, tc.wantStatus)
				}
				if strings.Contains(err.Error(), "hunter2") {
					t.Errorf("error %q echoes the response body", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("SignedInUserID() error = %v", err)
			}
			if id != tc.wantID {
				t.Errorf("id = %d, want %d", id, tc.wantID)
			}
		})
	}
}
