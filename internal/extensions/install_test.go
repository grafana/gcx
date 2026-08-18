package extensions_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/extensions"
)

func writeExtension(t *testing.T, manifest string, binary string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, extensions.ManifestFilename), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if binary != "" {
		if err := os.WriteFile(filepath.Join(dir, "entry"), []byte(binary), 0o755); err != nil { //nolint:gosec
			t.Fatal(err)
		}
	}
	return dir
}

func TestInstallFromLocalPath(t *testing.T) {
	src := writeExtension(t, `apiVersion: extensions.gcx.grafana.com/v1alpha1
kind: Extension
metadata:
  name: demo
spec:
  version: 1.2.3
  description: a demo
  platforms:
    - os: "*"
      arch: "*"
      path: ./entry
      bin: demo
`, "#!/bin/sh\necho hi\n")

	store := &extensions.Store{Root: t.TempDir()}
	installed, err := store.Install(context.Background(), extensions.InstallOptions{Source: src})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if installed.Version != "1.2.3" || installed.Name != "demo" {
		t.Fatalf("unexpected install record: %+v", installed)
	}
	if !installed.ReportUsage {
		t.Fatal("expected telemetry reporting to default on")
	}

	info, err := os.Stat(installed.Entrypoint)
	if err != nil {
		t.Fatalf("entrypoint missing: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("entrypoint is not executable: %v", info.Mode())
	}

	got, err := store.Lookup("demo")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.Entrypoint != installed.Entrypoint {
		t.Fatalf("index entry does not match install result")
	}
}

func TestInstallVerifiesChecksum(t *testing.T) {
	payload := []byte("binary contents")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	sum := sha256.Sum256(payload)
	manifest := func(checksum string) string {
		return `apiVersion: extensions.gcx.grafana.com/v1alpha1
kind: Extension
metadata:
  name: demo
spec:
  version: 1.0.0
  platforms:
    - os: ` + runtime.GOOS + `
      arch: ` + runtime.GOARCH + `
      url: ` + srv.URL + `/demo
      sha256: ` + checksum + `
`
	}

	t.Run("matching checksum installs", func(t *testing.T) {
		store := &extensions.Store{Root: t.TempDir()}
		src := writeExtension(t, manifest(hex.EncodeToString(sum[:])), "")
		if _, err := store.Install(context.Background(), extensions.InstallOptions{Source: src}); err != nil {
			t.Fatalf("install: %v", err)
		}
	})

	t.Run("mismatched checksum fails closed", func(t *testing.T) {
		store := &extensions.Store{Root: t.TempDir()}
		src := writeExtension(t, manifest(strings.Repeat("0", 64)), "")
		_, err := store.Install(context.Background(), extensions.InstallOptions{Source: src})
		if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
			t.Fatalf("expected a checksum mismatch, got %v", err)
		}
		if _, err := store.Lookup("demo"); err == nil {
			t.Fatal("a failed install must not be recorded in the index")
		}
	})
}

func TestInstallEnforcesMinGCXVersion(t *testing.T) {
	src := writeExtension(t, `apiVersion: extensions.gcx.grafana.com/v1alpha1
kind: Extension
metadata:
  name: demo
spec:
  version: 1.0.0
  minGCXVersion: 9.9.9
  platforms:
    - os: "*"
      arch: "*"
      path: ./entry
`, "x")

	store := &extensions.Store{Root: t.TempDir()}
	_, err := store.Install(context.Background(), extensions.InstallOptions{Source: src, GCXVersion: "1.1.0"})
	if err == nil || !strings.Contains(err.Error(), "requires gcx 9.9.9") {
		t.Fatalf("expected a version constraint error, got %v", err)
	}
}

func TestUninstallRemovesFilesAndIndexEntry(t *testing.T) {
	src := writeExtension(t, `apiVersion: extensions.gcx.grafana.com/v1alpha1
kind: Extension
metadata:
  name: demo
spec:
  version: 1.0.0
  platforms:
    - os: "*"
      arch: "*"
      path: ./entry
`, "x")

	store := &extensions.Store{Root: t.TempDir()}
	installed, err := store.Install(context.Background(), extensions.InstallOptions{Source: src})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := store.Uninstall("demo"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(installed.Entrypoint); !os.IsNotExist(err) {
		t.Fatalf("entrypoint should be gone, got %v", err)
	}
	if err := store.Uninstall("demo"); err == nil {
		t.Fatal("uninstalling twice should report the extension is not installed")
	}
}
