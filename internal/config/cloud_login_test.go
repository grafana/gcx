package config_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/config"
)

// TestSaveCloudConfigPreservesStack verifies that re-authenticating (which
// writes fresh cloud auth fields) refreshes the context's existing cloud entry
// in place and does not drop the previously configured stack selection.
func TestSaveCloudConfigPreservesStack(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	source := config.ExplicitConfigFile(path)

	seed := config.Config{}
	seed.SetStack("default", config.StackConfig{Slug: "mystack"})
	seed.SetCloudEntry("grafana-com", config.CloudEntry{
		Token:    "old-token",
		OAuthUrl: "https://old.example",
	})
	seed.SetContext(config.DefaultContextName, true, config.Context{
		Stack: "default",
		Cloud: "grafana-com",
	})
	if err := config.Write(ctx, source, seed); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	newCloud := &config.CloudEntry{
		Token:    "new-token",
		OAuthUrl: "https://grafana.com",
		APIUrl:   "https://grafana.com",
	}
	contextName, entryName, err := config.SaveCloudConfig(ctx, source, "", newCloud)
	if err != nil {
		t.Fatalf("SaveCloudConfig: %v", err)
	}
	if contextName != config.DefaultContextName {
		t.Errorf("context name: got %q, want %q", contextName, config.DefaultContextName)
	}
	if entryName != "grafana-com" {
		t.Errorf("entry name: got %q, want %q (existing ref must be refreshed in place)", entryName, "grafana-com")
	}

	got, err := config.Load(ctx, source)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	cur := got.Contexts[config.DefaultContextName]
	if cur.Cloud != "grafana-com" {
		t.Errorf("cloud ref not preserved: got %q, want %q", cur.Cloud, "grafana-com")
	}
	if cur.CloudEntry == nil || cur.CloudEntry.Token != "new-token" {
		t.Errorf("Token not updated: got %+v, want token %q", cur.CloudEntry, "new-token")
	}
	if got := cur.ResolveStackSlug(); got != "mystack" {
		t.Errorf("stack slug not preserved: got %q, want %q", got, "mystack")
	}
}
