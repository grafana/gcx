package cloud

import (
	"context"
	"path/filepath"
	"testing"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/config"
)

// TestSaveCloudConfigPreservesStack verifies that re-authenticating (which
// writes a fresh CloudConfig with only auth fields) does not drop a previously
// configured non-auth Stack selection.
func TestSaveCloudConfigPreservesStack(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	source := config.ExplicitConfigFile(path)

	seed := config.Config{}
	seed.SetContext(config.DefaultContextName, true, config.Context{
		Cloud: &config.CloudConfig{
			Token:    "old-token",
			Stack:    "mystack",
			OAuthUrl: "https://old.example",
		},
	})
	if err := config.Write(ctx, source, seed); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	configOpts := &cmdconfig.Options{ConfigFile: path}
	newCloud := &config.CloudConfig{
		Token:    "new-token",
		OAuthUrl: "https://grafana.com",
		APIUrl:   "https://grafana.com",
	}
	if err := saveCloudConfig(ctx, configOpts, newCloud); err != nil {
		t.Fatalf("saveCloudConfig: %v", err)
	}

	got, err := config.Load(ctx, source)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	cloud := got.Contexts[config.DefaultContextName].Cloud
	if cloud.Stack != "mystack" {
		t.Errorf("Stack not preserved: got %q, want %q", cloud.Stack, "mystack")
	}
	if cloud.Token != "new-token" {
		t.Errorf("Token not updated: got %q, want %q", cloud.Token, "new-token")
	}
}
