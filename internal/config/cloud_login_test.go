package config_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestMergeCloudIntoSwitchingAuthMethodClearsTheOther(t *testing.T) {
	// An entry holds one credential: an OAuth login over a CAP-token entry
	// clears the CAP token (and vice versa), so a stale credential never
	// shadows the fresh one.
	fromOAuth := config.MergeCloudInto(
		&config.CloudEntry{Token: "cap-token"},
		&config.CloudEntry{OAuthToken: "oauth-token", OAuthTokenExpiresAt: "2099-01-01T00:00:00Z"},
	)
	assert.Empty(t, fromOAuth.Token)
	assert.Equal(t, "oauth-token", fromOAuth.OAuthToken)
	assert.Equal(t, "2099-01-01T00:00:00Z", fromOAuth.OAuthTokenExpiresAt)

	fromCAP := config.MergeCloudInto(
		&config.CloudEntry{OAuthToken: "oauth-token", OAuthTokenExpiresAt: "2099-01-01T00:00:00Z"},
		&config.CloudEntry{Token: "cap-token"},
	)
	assert.Equal(t, "cap-token", fromCAP.Token)
	assert.Empty(t, fromCAP.OAuthToken)
	assert.Empty(t, fromCAP.OAuthTokenExpiresAt)
}

func TestCloudEntryResolveToken(t *testing.T) {
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)

	tests := []struct {
		name    string
		entry   config.CloudEntry
		want    string
		wantErr string
	}{
		{
			name:  "access policy token wins",
			entry: config.CloudEntry{Token: "cap", OAuthToken: "oauth"},
			want:  "cap",
		},
		{
			name:  "oauth token used when no CAP token",
			entry: config.CloudEntry{OAuthToken: "oauth", OAuthTokenExpiresAt: future},
			want:  "oauth",
		},
		{
			name:  "oauth token without expiry is used",
			entry: config.CloudEntry{OAuthToken: "oauth"},
			want:  "oauth",
		},
		{
			name:    "expired oauth token names the fix",
			entry:   config.CloudEntry{Name: "grafana-com", OAuthToken: "oauth", OAuthTokenExpiresAt: past},
			wantErr: "gcx cloud login",
		},
		{
			name:  "no credential",
			entry: config.CloudEntry{APIUrl: "https://grafana.com"},
			want:  "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.entry.ResolveToken()
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
