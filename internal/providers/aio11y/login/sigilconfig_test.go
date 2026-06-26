package login_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/aio11y/login"
)

func TestWriteSigilConfig_CreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sigil", "config.env")

	err := login.WriteSigilConfig(path, map[string]string{
		"SIGIL_ENDPOINT":       "https://sigil-prod-eu-west-2.grafana.net",
		"SIGIL_AUTH_TENANT_ID": "42",
		"SIGIL_AUTH_TOKEN":     "glc_xxx",
	})
	if err != nil {
		t.Fatalf("writeSigilConfig: %v", err)
	}

	got := readEnv(t, path)
	if got["SIGIL_ENDPOINT"] != "https://sigil-prod-eu-west-2.grafana.net" {
		t.Errorf("SIGIL_ENDPOINT = %q", got["SIGIL_ENDPOINT"])
	}
	if got["SIGIL_AUTH_TENANT_ID"] != "42" {
		t.Errorf("SIGIL_AUTH_TENANT_ID = %q", got["SIGIL_AUTH_TENANT_ID"])
	}
	if got["SIGIL_AUTH_TOKEN"] != "glc_xxx" {
		t.Errorf("SIGIL_AUTH_TOKEN = %q", got["SIGIL_AUTH_TOKEN"])
	}

	// File must be 0600 because it holds credentials.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 600", perm)
	}
}

func TestWriteSigilConfig_PreservesUnrelatedAndComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.env")
	seed := "# my config\n" +
		"SIGIL_TAGS=team=infra\n" +
		"SIGIL_ENDPOINT=https://old.example.com\n" +
		"\n" +
		"export SIGIL_CONTENT_CAPTURE_MODE=metadata_only\n"
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	err := login.WriteSigilConfig(path, map[string]string{
		"SIGIL_ENDPOINT":   "https://new.example.com",
		"SIGIL_AUTH_TOKEN": "glc_new",
	})
	if err != nil {
		t.Fatalf("writeSigilConfig: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)

	got := readEnv(t, path)
	if got["SIGIL_ENDPOINT"] != "https://new.example.com" {
		t.Errorf("SIGIL_ENDPOINT not updated: %q", got["SIGIL_ENDPOINT"])
	}
	if got["SIGIL_AUTH_TOKEN"] != "glc_new" {
		t.Errorf("SIGIL_AUTH_TOKEN not appended: %q", got["SIGIL_AUTH_TOKEN"])
	}
	// Unrelated keys and comments preserved.
	if got["SIGIL_TAGS"] != "team=infra" {
		t.Errorf("SIGIL_TAGS not preserved: %q", got["SIGIL_TAGS"])
	}
	if got["SIGIL_CONTENT_CAPTURE_MODE"] != "metadata_only" {
		t.Errorf("SIGIL_CONTENT_CAPTURE_MODE not preserved: %q", got["SIGIL_CONTENT_CAPTURE_MODE"])
	}
	if !containsLine(content, "# my config") {
		t.Errorf("comment line lost:\n%s", content)
	}
}

func TestWriteSigilConfig_EmptyValueDeletes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.env")
	if err := os.WriteFile(path, []byte("SIGIL_ENDPOINT=https://x\nSIGIL_OTEL_EXPORTER_OTLP_ENDPOINT=https://otlp\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := login.WriteSigilConfig(path, map[string]string{
		"SIGIL_OTEL_EXPORTER_OTLP_ENDPOINT": "", // delete
	})
	if err != nil {
		t.Fatalf("writeSigilConfig: %v", err)
	}

	got := readEnv(t, path)
	if _, ok := got["SIGIL_OTEL_EXPORTER_OTLP_ENDPOINT"]; ok {
		t.Errorf("expected key to be deleted, still present")
	}
	if got["SIGIL_ENDPOINT"] != "https://x" {
		t.Errorf("SIGIL_ENDPOINT clobbered: %q", got["SIGIL_ENDPOINT"])
	}
}

// readEnv parses a dotenv file into a map for assertions.
func readEnv(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	out := map[string]string{}
	for _, line := range splitLines(string(raw)) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(trimmed, "export "); ok {
			trimmed = strings.TrimSpace(rest)
		}
		key, after, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(key)] = after
	}
	return out
}

func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func containsLine(content, want string) bool {
	return slices.Contains(splitLines(content), want)
}
