package config_test

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/require"
)

// Self-signed test cert/key pair generated for testing only.
const testCertPEM = `-----BEGIN CERTIFICATE-----
MIIBczCCARmgAwIBAgIUdct9t5JW7uy/OqKXvsdffCRMK7wwCgYIKoZIzj0EAwIw
DzENMAsGA1UEAwwEdGVzdDAeFw0yNjA0MjIxNzM1MDBaFw0yNzA0MjIxNzM1MDBa
MA8xDTALBgNVBAMMBHRlc3QwWTATBgcqhkjOPQIBBggqhkjOPQMBBwNCAARd3mqS
HOYCZOhkwUTG2qBMN8JnJkTelG33ctDHcC7xfp8xuVKqkP5MHFd5TlIu68tWZg5o
w0F9VCslYrCGqXCgo1MwUTAdBgNVHQ4EFgQU5LzHFgGER7Gn/UjL6aVP6ZlNbSMw
HwYDVR0jBBgwFoAU5LzHFgGER7Gn/UjL6aVP6ZlNbSMwDwYDVR0TAQH/BAUwAwEB
/zAKBggqhkjOPQQDAgNIADBFAiBS4yv4UOt41GsyQVOJVdcmDmhrm98l3GKbKBgB
PKIFLwIhAJWqcaHMUuYob4/iQWhcat59ijqyr+gd9gP4brGrrHNu
-----END CERTIFICATE-----`

const testKeyPEM = `-----BEGIN PRIVATE KEY-----
MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgGNOZS+qPcRefWpz0
eab7S6cFDjBtPG+fH1ywzwOoummhRANCAARd3mqSHOYCZOhkwUTG2qBMN8JnJkTe
lG33ctDHcC7xfp8xuVKqkP5MHFd5TlIu68tWZg5ow0F9VCslYrCGqXCg
-----END PRIVATE KEY-----`

const testCAPEM = `-----BEGIN CERTIFICATE-----
MIIBczCCARmgAwIBAgIUdct9t5JW7uy/OqKXvsdffCRMK7wwCgYIKoZIzj0EAwIw
DzENMAsGA1UEAwwEdGVzdDAeFw0yNjA0MjIxNzM1MDBaFw0yNzA0MjIxNzM1MDBa
MA8xDTALBgNVBAMMBHRlc3QwWTATBgcqhkjOPQIBBggqhkjOPQMBBwNCAARd3mqS
HOYCZOhkwUTG2qBMN8JnJkTelG33ctDHcC7xfp8xuVKqkP5MHFd5TlIu68tWZg5o
w0F9VCslYrCGqXCgo1MwUTAdBgNVHQ4EFgQU5LzHFgGER7Gn/UjL6aVP6ZlNbSMw
HwYDVR0jBBgwFoAU5LzHFgGER7Gn/UjL6aVP6ZlNbSMwDwYDVR0TAQH/BAUwAwEB
/zAKBggqhkjOPQQDAgNIADBFAiBS4yv4UOt41GsyQVOJVdcmDmhrm98l3GKbKBgB
PKIFLwIhAJWqcaHMUuYob4/iQWhcat59ijqyr+gd9gP4brGrrHNu
-----END CERTIFICATE-----`

func TestTLS_ResolveFiles(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	caPath := filepath.Join(dir, "ca.pem")

	require.NoError(t, os.WriteFile(certPath, []byte(testCertPEM), 0600))
	require.NoError(t, os.WriteFile(keyPath, []byte(testKeyPEM), 0600))
	require.NoError(t, os.WriteFile(caPath, []byte(testCAPEM), 0600))

	cfg := &config.TLS{
		CertFile: certPath,
		KeyFile:  keyPath,
		CAFile:   caPath,
	}

	require.NoError(t, cfg.ResolveFiles())
	require.Equal(t, []byte(testCertPEM), cfg.CertData)
	require.Equal(t, []byte(testKeyPEM), cfg.KeyData)
	require.Equal(t, []byte(testCAPEM), cfg.CAData)
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
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certPath, []byte(testCertPEM), 0600))
	require.NoError(t, os.WriteFile(keyPath, []byte(testKeyPEM), 0600))

	cfg := &config.TLS{
		CertFile: certPath,
		KeyFile:  keyPath,
		CertData: []byte("old-data"),
	}

	require.NoError(t, cfg.ResolveFiles())
	require.Equal(t, []byte(testCertPEM), cfg.CertData)
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
}

func TestTLS_ToStdTLSConfig_WithCAData(t *testing.T) {
	cfg := &config.TLS{
		CAData: []byte(testCAPEM),
	}

	tlsCfg, err := cfg.ToStdTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, tlsCfg.RootCAs)
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
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	require.NoError(t, os.WriteFile(certPath, []byte(testCertPEM), 0600))
	require.NoError(t, os.WriteFile(keyPath, []byte(testKeyPEM), 0600))

	cfg := &config.TLS{
		CertFile: certPath,
		KeyFile:  keyPath,
	}

	tlsCfg, err := cfg.ToStdTLSConfig()
	require.NoError(t, err)
	require.Len(t, tlsCfg.Certificates, 1)
}

func TestTLS_ToStdTLSConfig_WithCertData(t *testing.T) {
	cfg := &config.TLS{
		CertData: []byte(testCertPEM),
		KeyData:  []byte(testKeyPEM),
	}

	tlsCfg, err := cfg.ToStdTLSConfig()
	require.NoError(t, err)
	require.Len(t, tlsCfg.Certificates, 1)
}

func TestTLS_ToStdTLSConfig_HalfConfiguredCertData(t *testing.T) {
	cfg := &config.TLS{
		CertData: []byte(testCertPEM),
	}

	_, err := cfg.ToStdTLSConfig()
	require.Error(t, err)
	require.ErrorContains(t, err, "both cert-data and key-data must be provided together")
}

func TestTLS_ToStdTLSConfig_PinsMinVersionTLS12(t *testing.T) {
	cfg := &config.TLS{}

	tlsCfg, err := cfg.ToStdTLSConfig()
	require.NoError(t, err)
	require.Equal(t, uint16(tls.VersionTLS12), tlsCfg.MinVersion)
}

func TestTLS_ToStdTLSConfig_CADataAddsToSystemRoots(t *testing.T) {
	cfg := &config.TLS{
		CAData: []byte(testCAPEM),
	}

	tlsCfg, err := cfg.ToStdTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, tlsCfg.RootCAs)
}
