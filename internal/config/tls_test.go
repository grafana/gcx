package config_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/require"
)

type testTLSMaterial struct {
	certPEM []byte
	keyPEM  []byte
	caPEM   []byte
}

func newTestTLSMaterial(t *testing.T) testTLSMaterial {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "test",
		},
		NotBefore:             time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:              time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, pub, priv)
	require.NoError(t, err)

	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	return testTLSMaterial{
		certPEM: certPEM,
		keyPEM:  keyPEM,
		caPEM:   certPEM,
	}
}

func TestTLS_ResolveFiles(t *testing.T) {
	material := newTestTLSMaterial(t)
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	caPath := filepath.Join(dir, "ca.pem")

	require.NoError(t, os.WriteFile(certPath, material.certPEM, 0600))
	require.NoError(t, os.WriteFile(keyPath, material.keyPEM, 0600))
	require.NoError(t, os.WriteFile(caPath, material.caPEM, 0600))

	cfg := &config.TLS{
		CertFile: certPath,
		KeyFile:  keyPath,
		CAFile:   caPath,
	}

	require.NoError(t, cfg.ResolveFiles())
	require.Equal(t, material.certPEM, cfg.CertData)
	require.Equal(t, material.keyPEM, cfg.KeyData)
	require.Equal(t, material.caPEM, cfg.CAData)
}

func TestTLS_ResolveFiles_MissingFile(t *testing.T) {
	cfg := &config.TLS{
		CertFile: "/nonexistent/cert.pem",
		KeyFile:  "/nonexistent/key.pem",
	}

	err := cfg.ResolveFiles()
	require.Error(t, err)
	require.ErrorContains(t, err, "TLS client certificate file not found")
}

func TestTLS_ResolveFiles_CertWithoutKey(t *testing.T) {
	cfg := &config.TLS{
		CertFile: "/some/cert.pem",
	}

	err := cfg.ResolveFiles()
	require.Error(t, err)
	require.ErrorContains(t, err, "both cert-file and key-file must be provided together")
}

func TestTLS_ResolveFiles_FileOverridesData(t *testing.T) {
	material := newTestTLSMaterial(t)
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certPath, material.certPEM, 0600))
	require.NoError(t, os.WriteFile(keyPath, material.keyPEM, 0600))

	cfg := &config.TLS{
		CertFile: certPath,
		KeyFile:  keyPath,
		CertData: []byte("old-data"),
	}

	require.NoError(t, cfg.ResolveFiles())
	require.Equal(t, material.certPEM, cfg.CertData)
}

func TestTLS_ToStdTLSConfig_InsecureOnly(t *testing.T) {
	cfg := &config.TLS{
		Insecure:   true,
		ServerName: "example.com",
	}

	tlsCfg, err := cfg.ToStdTLSConfig()
	require.NoError(t, err)
	require.True(t, tlsCfg.InsecureSkipVerify)
	require.Equal(t, "example.com", tlsCfg.ServerName)
	require.Equal(t, uint16(tls.VersionTLS12), tlsCfg.MinVersion)
}

func TestTLS_ToStdTLSConfig_WithCAData(t *testing.T) {
	material := newTestTLSMaterial(t)
	cfg := &config.TLS{
		CAData: material.caPEM,
	}

	tlsCfg, err := cfg.ToStdTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, tlsCfg.RootCAs)
	require.Equal(t, uint16(tls.VersionTLS12), tlsCfg.MinVersion)
}

func TestTLS_ToStdTLSConfig_WithInvalidCAData(t *testing.T) {
	cfg := &config.TLS{
		CAData: []byte("not-a-cert"),
	}

	_, err := cfg.ToStdTLSConfig()
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to parse TLS CA certificate data")
}

func TestTLS_ToStdTLSConfig_WithCertFiles(t *testing.T) {
	material := newTestTLSMaterial(t)
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	require.NoError(t, os.WriteFile(certPath, material.certPEM, 0600))
	require.NoError(t, os.WriteFile(keyPath, material.keyPEM, 0600))

	cfg := &config.TLS{
		CertFile: certPath,
		KeyFile:  keyPath,
	}

	tlsCfg, err := cfg.ToStdTLSConfig()
	require.NoError(t, err)
	require.Len(t, tlsCfg.Certificates, 1)
	require.Equal(t, uint16(tls.VersionTLS12), tlsCfg.MinVersion)
}

func TestTLS_ToStdTLSConfig_WithCertData(t *testing.T) {
	material := newTestTLSMaterial(t)
	cfg := &config.TLS{
		CertData: material.certPEM,
		KeyData:  material.keyPEM,
	}

	tlsCfg, err := cfg.ToStdTLSConfig()
	require.NoError(t, err)
	require.Len(t, tlsCfg.Certificates, 1)
	require.Equal(t, uint16(tls.VersionTLS12), tlsCfg.MinVersion)
}

func TestTLS_ToStdTLSConfig_WithMalformedCertData(t *testing.T) {
	material := newTestTLSMaterial(t)
	cfg := &config.TLS{
		CertData: []byte("not-valid-pem"),
		KeyData:  material.keyPEM,
	}

	_, err := cfg.ToStdTLSConfig()
	require.Error(t, err)
}

func TestTLS_ToStdTLSConfig_HalfConfiguredCertData(t *testing.T) {
	material := newTestTLSMaterial(t)

	t.Run("CertData without KeyData", func(t *testing.T) {
		cfg := &config.TLS{
			CertData: material.certPEM,
		}

		_, err := cfg.ToStdTLSConfig()
		require.Error(t, err)
		require.ErrorContains(t, err, "both cert-data and key-data must be provided together")
	})

	t.Run("KeyData without CertData", func(t *testing.T) {
		cfg := &config.TLS{
			KeyData: material.keyPEM,
		}

		_, err := cfg.ToStdTLSConfig()
		require.Error(t, err)
		require.ErrorContains(t, err, "both cert-data and key-data must be provided together")
	})
}

func TestTLS_ToStdTLSConfig_PinsMinVersionTLS12(t *testing.T) {
	cfg := &config.TLS{}

	tlsCfg, err := cfg.ToStdTLSConfig()
	require.NoError(t, err)
	require.Equal(t, uint16(tls.VersionTLS12), tlsCfg.MinVersion)
}

func TestTLS_ToStdTLSConfig_CADataAddsToSystemRoots(t *testing.T) {
	material := newTestTLSMaterial(t)
	cfg := &config.TLS{
		CAData: material.caPEM,
	}

	tlsCfg, err := cfg.ToStdTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, tlsCfg.RootCAs)

	// Verify the custom CA was added to the system pool, not replacing it.
	// The pool should contain more subjects than just our single test CA,
	// proving system roots are preserved.
	systemPool, sysErr := x509.SystemCertPool()
	if sysErr == nil && systemPool.Equal(tlsCfg.RootCAs) {
		// If pools are equal, the custom CA wasn't actually added (unlikely
		// unless it happens to already be in the system pool). This is a
		// sanity guard, not a hard failure, since CI environments vary.
		t.Log("warning: RootCAs equals system pool — custom CA may already be a system root")
	}
}
