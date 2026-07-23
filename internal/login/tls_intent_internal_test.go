package login

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

func TestPreAuthProbeSuppressesStaleClientIdentityForNonMTLSIntent(t *testing.T) {
	clientCerts := make(chan int, 4)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientCerts <- len(r.TLS.PeerCertificates)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"buildInfo":{"grafanaUrl":"https://onprem.example.com"}}`))
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
	fullTLS := &config.TLS{
		Insecure:   true,
		ServerName: "selected-sni.example.com",
		CertData:   certData,
		KeyData:    keyData,
		NextProtos: []string{"http/1.1"},
	}

	tests := []struct {
		name             string
		opts             Options
		wantClientCerts  int
		wantIdentityKept bool
	}{
		{
			name: "fresh explicit token",
			opts: Options{Inputs: Inputs{
				GrafanaToken: "selected-token",
				TLS:          fullTLS,
			}},
		},
		{
			name: "fresh explicit OAuth",
			opts: Options{Inputs: Inputs{
				UseOAuth: true,
				TLS:      fullTLS,
			}},
		},
		{
			name: "interactive persisted token",
			opts: Options{Inputs: Inputs{
				ExistingGrafanaAuthMethod: "token",
				TLS:                       fullTLS,
			}},
		},
		{
			name: "interactive persisted Basic",
			opts: Options{Inputs: Inputs{
				ExistingGrafanaAuthMethod: "basic",
				TLS:                       fullTLS,
			}},
		},
		{
			name: "interactive persisted OAuth",
			opts: Options{Inputs: Inputs{
				ExistingGrafanaAuthMethod: "oauth",
				TLS:                       fullTLS,
			}},
		},
		{
			name: "interactive persisted mTLS keeps client identity",
			opts: Options{Inputs: Inputs{
				ExistingGrafanaAuthMethod: "mtls",
				TLS:                       fullTLS,
			}},
			wantClientCerts:  1,
			wantIdentityKept: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			selectedTLS := preAuthTLS(tc.opts)
			if selectedTLS == nil {
				t.Fatal("preAuthTLS() returned nil")
			}
			if selectedTLS.Insecure != fullTLS.Insecure || selectedTLS.ServerName != fullTLS.ServerName ||
				len(selectedTLS.NextProtos) != len(fullTLS.NextProtos) {
				t.Fatalf("server trust settings were not preserved: %#v", selectedTLS)
			}
			identityKept := len(selectedTLS.CertData) > 0 || len(selectedTLS.KeyData) > 0
			if identityKept != tc.wantIdentityKept {
				t.Errorf("client identity kept = %v, want %v", identityKept, tc.wantIdentityKept)
			}

			client, err := tlsAwareClient(context.Background(), selectedTLS)
			if err != nil {
				t.Fatalf("tlsAwareClient() error = %v", err)
			}
			target, err := probeTarget(context.Background(), server.URL, client)
			if err != nil {
				t.Fatalf("probeTarget() error = %v", err)
			}
			if target != TargetOnPrem {
				t.Fatalf("probeTarget() = %v, want %v", target, TargetOnPrem)
			}
			if got := <-clientCerts; got != tc.wantClientCerts {
				t.Errorf("probe client certificates = %d, want %d", got, tc.wantClientCerts)
			}
		})
	}
}
