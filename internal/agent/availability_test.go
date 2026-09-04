package agent_test

import (
	"testing"

	"github.com/grafana/gcx/internal/agent"
)

func TestIsCloudOnlyPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"exact group match", "gcx slo", true},
		{"descendant of group", "gcx slo report list", true},
		{"synthetic monitoring", "gcx synthetic-monitoring checks list", true},
		{"frontend group", "gcx frontend apps list", true},
		{"cloud stacks", "gcx cloud stacks list", true},
		{"adaptive subtree", "gcx metrics adaptive rules list", true},
		{"logs adaptive subtree", "gcx logs adaptive", true},
		{"traces adaptive subtree", "gcx traces adaptive recommendations", true},
		{"metrics billing", "gcx metrics billing", true},
		{"metrics billing descendant", "gcx metrics billing query", true},
		{"profiles adaptive", "gcx profiles adaptive", true},
		{"setup group", "gcx setup", true},
		{"setup status", "gcx setup status", true},
		// Instrumentation subtrees: the Hub-facing subtrees are cloud-only,
		// but `check`, `explain`, and `list-explanations` run entirely
		// locally and must be available on self-hosted too.
		{"instrumentation setup is cloud-only", "gcx instrumentation setup", true},
		{"instrumentation status is cloud-only", "gcx instrumentation status", true},
		{"instrumentation clusters is cloud-only", "gcx instrumentation clusters list", true},
		{"instrumentation services is cloud-only", "gcx instrumentation services list", true},
		{"instrumentation check is local (not cloud-only)", "gcx instrumentation check", false},
		{"instrumentation explain is local (not cloud-only)", "gcx instrumentation explain", false},
		{"instrumentation list-explanations is local (not cloud-only)", "gcx instrumentation list-explanations", false},
		{"signal query not cloud-only", "gcx metrics query", false},
		{"profiles query not cloud-only", "gcx profiles query", false},
		{"metrics group root not cloud-only", "gcx metrics", false},
		{"resources available everywhere", "gcx resources get", false},
		{"alert available everywhere", "gcx alert rules list", false},
		{"root", "gcx", false},
		{"prefix-collision is not a match", "gcx k6s", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := agent.IsCloudOnlyPath(tt.path); got != tt.want {
				t.Errorf("IsCloudOnlyPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
