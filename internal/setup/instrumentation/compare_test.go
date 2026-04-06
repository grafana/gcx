package instrumentation_test

import (
	"testing"

	"github.com/grafana/gcx/internal/setup/instrumentation"
	"github.com/stretchr/testify/assert"
)

func TestCompare(t *testing.T) {
	tests := []struct {
		name      string
		local     *instrumentation.AppSpec
		remote    *instrumentation.AppSpec
		wantEmpty bool
		wantNS    []string // remote-only namespace names
		wantApps  []instrumentation.RemoteOnlyApp
	}{
		{
			name:      "nil remote returns empty diff",
			local:     &instrumentation.AppSpec{Namespaces: []instrumentation.NamespaceConfig{{Name: "default"}}},
			remote:    nil,
			wantEmpty: true,
		},
		{
			name:      "nil local with empty remote returns empty diff",
			local:     nil,
			remote:    &instrumentation.AppSpec{},
			wantEmpty: true,
		},
		{
			name:  "nil local with remote namespace returns remote-only namespace",
			local: nil,
			remote: &instrumentation.AppSpec{
				Namespaces: []instrumentation.NamespaceConfig{{Name: "monitoring"}},
			},
			wantEmpty: false,
			wantNS:    []string{"monitoring"},
		},
		{
			name: "exact match returns empty diff",
			local: &instrumentation.AppSpec{
				Namespaces: []instrumentation.NamespaceConfig{
					{Name: "default", Apps: []instrumentation.AppConfig{{Name: "web"}}},
				},
			},
			remote: &instrumentation.AppSpec{
				Namespaces: []instrumentation.NamespaceConfig{
					{Name: "default", Apps: []instrumentation.AppConfig{{Name: "web"}}},
				},
			},
			wantEmpty: true,
		},
		{
			name: "local superset of remote returns empty diff",
			local: &instrumentation.AppSpec{
				Namespaces: []instrumentation.NamespaceConfig{
					{Name: "default", Apps: []instrumentation.AppConfig{{Name: "web"}, {Name: "api"}}},
					{Name: "monitoring"},
				},
			},
			remote: &instrumentation.AppSpec{
				Namespaces: []instrumentation.NamespaceConfig{
					{Name: "default", Apps: []instrumentation.AppConfig{{Name: "web"}}},
				},
			},
			wantEmpty: true,
		},
		{
			name: "remote superset of local returns diff with remote-only namespace",
			local: &instrumentation.AppSpec{
				Namespaces: []instrumentation.NamespaceConfig{
					{Name: "default"},
				},
			},
			remote: &instrumentation.AppSpec{
				Namespaces: []instrumentation.NamespaceConfig{
					{Name: "default"},
					{Name: "monitoring"},
				},
			},
			wantEmpty: false,
			wantNS:    []string{"monitoring"},
		},
		{
			name: "remote has extra app in shared namespace returns remote-only app",
			local: &instrumentation.AppSpec{
				Namespaces: []instrumentation.NamespaceConfig{
					{Name: "default", Apps: []instrumentation.AppConfig{{Name: "web"}}},
				},
			},
			remote: &instrumentation.AppSpec{
				Namespaces: []instrumentation.NamespaceConfig{
					{Name: "default", Apps: []instrumentation.AppConfig{{Name: "web"}, {Name: "api"}}},
				},
			},
			wantEmpty: false,
			wantApps: []instrumentation.RemoteOnlyApp{
				{Namespace: "default", App: "api"},
			},
		},
		{
			name:      "both nil returns empty diff",
			local:     nil,
			remote:    nil,
			wantEmpty: true,
		},
		{
			name: "remote namespace absent from local even with apps",
			local: &instrumentation.AppSpec{
				Namespaces: []instrumentation.NamespaceConfig{
					{Name: "default"},
				},
			},
			remote: &instrumentation.AppSpec{
				Namespaces: []instrumentation.NamespaceConfig{
					{Name: "monitoring", Apps: []instrumentation.AppConfig{{Name: "prometheus"}}},
				},
			},
			wantEmpty: false,
			wantNS:    []string{"monitoring"},
			// When the whole namespace is remote-only, its apps are not listed separately.
		},
		{
			name: "mixed: remote-only namespace and remote-only app in shared namespace",
			local: &instrumentation.AppSpec{
				Namespaces: []instrumentation.NamespaceConfig{
					{Name: "default", Apps: []instrumentation.AppConfig{{Name: "web"}}},
				},
			},
			remote: &instrumentation.AppSpec{
				Namespaces: []instrumentation.NamespaceConfig{
					{Name: "default", Apps: []instrumentation.AppConfig{{Name: "web"}, {Name: "db"}}},
					{Name: "ops"},
				},
			},
			wantEmpty: false,
			wantNS:    []string{"ops"},
			wantApps: []instrumentation.RemoteOnlyApp{
				{Namespace: "default", App: "db"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := instrumentation.Compare(tt.local, tt.remote)

			if tt.wantEmpty {
				assert.True(t, diff.IsEmpty(), "expected empty diff, got namespaces=%v apps=%v", diff.Namespaces, diff.Apps)
				return
			}

			assert.False(t, diff.IsEmpty(), "expected non-empty diff")

			// Check remote-only namespaces.
			gotNS := make([]string, len(diff.Namespaces))
			for i, n := range diff.Namespaces {
				gotNS[i] = n.Namespace
			}
			assert.ElementsMatch(t, tt.wantNS, gotNS)

			// Check remote-only apps.
			assert.ElementsMatch(t, tt.wantApps, diff.Apps)
		})
	}
}

func TestDiff_IsEmpty(t *testing.T) {
	tests := []struct {
		name      string
		diff      instrumentation.Diff
		wantEmpty bool
	}{
		{
			name:      "zero value diff is empty",
			diff:      instrumentation.Diff{},
			wantEmpty: true,
		},
		{
			name: "diff with namespaces is not empty",
			diff: instrumentation.Diff{
				Namespaces: []instrumentation.RemoteOnlyNamespace{{Namespace: "ops"}},
			},
			wantEmpty: false,
		},
		{
			name: "diff with apps is not empty",
			diff: instrumentation.Diff{
				Apps: []instrumentation.RemoteOnlyApp{{Namespace: "default", App: "api"}},
			},
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantEmpty, tt.diff.IsEmpty())
		})
	}
}
