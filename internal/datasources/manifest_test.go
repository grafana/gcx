package datasources_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	dsclient "github.com/grafana/gcx/internal/datasources"
)

func TestReadManifestFile_YAMLAndStdinJSON(t *testing.T) {
	t.Run("yaml file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "ds.yaml")
		content := `apiVersion: grafana-sentry-datasource.datasource.grafana.app/v0alpha1
kind: DataSource
metadata:
  name: sentry-dev
spec:
  type: grafana-sentry-datasource
  title: Sentry
  url: https://sentry.example.io/
secure:
  authToken:
    create: tok-123
`
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		m, err := dsclient.ReadManifestFile(path, nil)
		if err != nil {
			t.Fatalf("ReadManifestFile: %v", err)
		}
		if m.Spec.Type != "grafana-sentry-datasource" {
			t.Errorf("type = %q", m.Spec.Type)
		}
		if m.Metadata.Name != "sentry-dev" {
			t.Errorf("name = %q", m.Metadata.Name)
		}
		if m.Secure["authToken"].Create != "tok-123" {
			t.Errorf("secret create = %q", m.Secure["authToken"].Create)
		}
	})

	t.Run("json stdin", func(t *testing.T) {
		json := `{"apiVersion":"grafana-sentry-datasource.datasource.grafana.app/v0alpha1",` +
			`"kind":"DataSource","metadata":{"name":"x"},` +
			`"spec":{"type":"grafana-sentry-datasource","url":"https://e"}}`
		m, err := dsclient.ReadManifestFile("-", strings.NewReader(json))
		if err != nil {
			t.Fatalf("ReadManifestFile stdin: %v", err)
		}
		if m.Spec.URL != "https://e" {
			t.Errorf("url = %q", m.Spec.URL)
		}
	})

	t.Run("missing type", func(t *testing.T) {
		_, err := dsclient.ReadManifestFile("-", strings.NewReader(`{"kind":"DataSource","spec":{}}`))
		if err == nil {
			t.Fatal("expected error for missing spec.type")
		}
	})
}

func TestResolveSecrets(t *testing.T) {
	t.Run("fromEnv set", func(t *testing.T) {
		t.Setenv("MY_TOKEN", "env-secret")
		m := &dsclient.DataSourceManifest{
			Secure: map[string]dsclient.SecureValue{"authToken": {FromEnv: "MY_TOKEN"}},
		}
		if err := m.ResolveSecrets(""); err != nil {
			t.Fatalf("ResolveSecrets: %v", err)
		}
		got := m.Secure["authToken"]
		if got.Create != "env-secret" {
			t.Errorf("create = %q, want env-secret", got.Create)
		}
		if got.FromEnv != "" {
			t.Errorf("fromEnv should be cleared, got %q", got.FromEnv)
		}
	})

	t.Run("fromEnv missing errors", func(t *testing.T) {
		m := &dsclient.DataSourceManifest{
			Secure: map[string]dsclient.SecureValue{"authToken": {FromEnv: "DEFINITELY_UNSET_VAR_XYZ"}},
		}
		if err := m.ResolveSecrets(""); err == nil {
			t.Fatal("expected error for missing env var")
		}
	})

	t.Run("fromFile", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "secret")
		if err := os.WriteFile(path, []byte("file-secret\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		m := &dsclient.DataSourceManifest{
			Secure: map[string]dsclient.SecureValue{"authToken": {FromFile: path}},
		}
		if err := m.ResolveSecrets(""); err != nil {
			t.Fatalf("ResolveSecrets: %v", err)
		}
		if got := m.Secure["authToken"].Create; got != "file-secret" {
			t.Errorf("create = %q, want file-secret (trimmed)", got)
		}
	})

	t.Run("multiple sources errors", func(t *testing.T) {
		m := &dsclient.DataSourceManifest{
			Secure: map[string]dsclient.SecureValue{"authToken": {Create: "a", FromEnv: "B"}},
		}
		if err := m.ResolveSecrets(""); err == nil {
			t.Fatal("expected error for multiple secret sources")
		}
	})

	t.Run("no source errors", func(t *testing.T) {
		m := &dsclient.DataSourceManifest{
			Secure: map[string]dsclient.SecureValue{"authToken": {}},
		}
		if err := m.ResolveSecrets(""); err == nil {
			t.Fatal("expected error for no secret source")
		}
	})

	t.Run("name-only placeholder is kept-existing (dropped)", func(t *testing.T) {
		// Simulates `get -o yaml | update -f -`: the read-back manifest carries
		// a name-only secure entry that must mean "leave the secret unchanged",
		// not "missing value".
		m := &dsclient.DataSourceManifest{
			Secure: map[string]dsclient.SecureValue{"authToken": {Name: "authToken"}},
		}
		if err := m.ResolveSecrets(""); err != nil {
			t.Fatalf("ResolveSecrets: %v", err)
		}
		if _, ok := m.Secure["authToken"]; ok {
			t.Error("name-only secure entry should be dropped from the write payload")
		}
	})

	t.Run("remove is valid", func(t *testing.T) {
		m := &dsclient.DataSourceManifest{
			Secure: map[string]dsclient.SecureValue{"authToken": {Remove: true}},
		}
		if err := m.ResolveSecrets(""); err != nil {
			t.Fatalf("ResolveSecrets: %v", err)
		}
	})

	t.Run("secrets file merges", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "secrets.yaml")
		if err := os.WriteFile(path, []byte("authToken:\n  create: sf-secret\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		m := &dsclient.DataSourceManifest{}
		if err := m.ResolveSecrets(path); err != nil {
			t.Fatalf("ResolveSecrets: %v", err)
		}
		if got := m.Secure["authToken"].Create; got != "sf-secret" {
			t.Errorf("create = %q, want sf-secret", got)
		}
	})
}

func TestDiffManifest(t *testing.T) {
	current := &dsclient.DataSourceManifest{
		Spec:   dsclient.DataSourceSpec{Type: "p", Title: "t", URL: "https://a"},
		Secure: map[string]dsclient.SecureValue{"old": {Name: "ref"}},
	}
	next := &dsclient.DataSourceManifest{
		Spec:   dsclient.DataSourceSpec{Type: "p", Title: "t", URL: "https://b", Access: "proxy"},
		Secure: map[string]dsclient.SecureValue{"authToken": {Create: "x"}},
	}

	summary := dsclient.DiffManifest(current, next)
	rendered := summary.Render()

	if !strings.Contains(rendered, "url: changed") {
		t.Errorf("expected url changed, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "access: added") {
		t.Errorf("expected access added, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "authToken: set") {
		t.Errorf("expected authToken set, got:\n%s", rendered)
	}
	// Secret values must never appear in the diff.
	if strings.Contains(rendered, "x") && strings.Contains(rendered, "create") {
		t.Errorf("diff must not disclose secret values:\n%s", rendered)
	}
}

func TestDiffManifest_CreateAllAdded(t *testing.T) {
	next := &dsclient.DataSourceManifest{
		Spec: dsclient.DataSourceSpec{Type: "p", Title: "t", URL: "https://a"},
	}
	summary := dsclient.DiffManifest(nil, next)
	if summary.Empty() {
		t.Fatal("expected non-empty summary for create")
	}
	if !strings.Contains(summary.Render(), "title: added") {
		t.Errorf("expected title added, got:\n%s", summary.Render())
	}
}

func TestMappingRoundTrip(t *testing.T) {
	m := &dsclient.DataSourceManifest{
		APIVersion: "grafana-sentry-datasource.datasource.grafana.app/v0alpha1",
		Kind:       "DataSource",
		Metadata:   dsclient.DataSourceMetadata{Name: "sentry-dev"},
		Spec: dsclient.DataSourceSpec{
			Title:    "Sentry",
			Access:   "proxy",
			URL:      "https://sentry.example.io/",
			JSONData: map[string]any{"orgSlug": "acme"},
		},
		Secure: map[string]dsclient.SecureValue{"authToken": {Create: "tok"}},
	}

	ds := m.ToDatasource()
	if ds.Type != "grafana-sentry-datasource" {
		t.Errorf("type derived from apiVersion = %q", ds.Type)
	}
	if ds.Name != "Sentry" || ds.UID != "sentry-dev" {
		t.Errorf("name=%q uid=%q", ds.Name, ds.UID)
	}
	if ds.SecureJSONData["authToken"] != "tok" {
		t.Errorf("secureJsonData = %v", ds.SecureJSONData)
	}

	// Simulate a server read: secret returned only as a field marker.
	read := &dsclient.Datasource{
		UID:              "sentry-dev",
		Name:             "Sentry",
		Type:             "grafana-sentry-datasource",
		URL:              "https://sentry.example.io/",
		JSONData:         map[string]any{"orgSlug": "acme"},
		SecureJSONFields: map[string]bool{"authToken": true},
	}
	out := dsclient.ManifestFromDatasource(read)
	if out.APIVersion != "grafana-sentry-datasource.datasource.grafana.app/v0alpha1" {
		t.Errorf("apiVersion = %q", out.APIVersion)
	}
	if out.Spec.URL != "https://sentry.example.io/" {
		t.Errorf("url = %q", out.Spec.URL)
	}
	if out.Secure["authToken"].Name != "authToken" || out.Secure["authToken"].Create != "" {
		t.Errorf("secret should be a name-only placeholder, got %+v", out.Secure["authToken"])
	}
}

func TestReadManifest_TypeFromApiVersionAndConflict(t *testing.T) {
	// spec.type derived from apiVersion when omitted.
	m, err := dsclient.ReadManifestFile("-", strings.NewReader(
		`{"apiVersion":"prometheus.datasource.grafana.app/v0alpha1","kind":"DataSource","spec":{}}`))
	if err != nil {
		t.Fatalf("ReadManifestFile: %v", err)
	}
	if m.PluginType() != "prometheus" {
		t.Errorf("PluginType = %q, want prometheus", m.PluginType())
	}

	// Conflicting spec.type vs apiVersion group is rejected.
	_, err = dsclient.ReadManifestFile("-", strings.NewReader(
		`{"apiVersion":"prometheus.datasource.grafana.app/v0alpha1","kind":"DataSource","spec":{"type":"loki"}}`))
	if err == nil {
		t.Fatal("expected conflict error for mismatched spec.type and apiVersion group")
	}
}

func TestIsDatasourceGroup(t *testing.T) {
	tests := []struct {
		group      string
		wantPlugin string
		wantOK     bool
	}{
		{"prometheus.datasource.grafana.app", "prometheus", true},
		{"yesoreyeram-infinity-datasource.datasource.grafana.app", "yesoreyeram-infinity-datasource", true},
		{"datasource.grafana.app", "", false},
		{"dashboard.grafana.app", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.group, func(t *testing.T) {
			plugin, ok := dsclient.IsDatasourceGroup(tt.group)
			if ok != tt.wantOK || plugin != tt.wantPlugin {
				t.Errorf("IsDatasourceGroup(%q) = (%q, %v), want (%q, %v)",
					tt.group, plugin, ok, tt.wantPlugin, tt.wantOK)
			}
		})
	}
}
