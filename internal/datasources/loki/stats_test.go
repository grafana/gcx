package loki_test

import (
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/datasources/loki"
)

// Expression resolution itself (positional arg vs --expr, both/neither
// provided) is exercised by dsquery.ExprOpts's own TestResolveExpr — statsOpts
// embeds that type directly, so no separate unit test is needed here. This
// test just confirms the CLI wiring surfaces that error end-to-end.
func TestStatsCmd_ExprFlagAndPositionalBothProvidedIsError(t *testing.T) {
	cmd := loki.StatsCmd(nil)
	cmd.SetArgs([]string{`{job="x"}`, "--expr", `{job="x"}`})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error when both a positional arg and --expr are provided")
	}
}

func TestStatsCmd_NoSelectorFoundReturnsError(t *testing.T) {
	cmd := loki.StatsCmd(nil)
	cmd.SetArgs([]string{"--expr", "vector(1)"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error for an expression with no stream selector")
	}
}

func TestStatsCmd_Construction(t *testing.T) {
	cmd := loki.StatsCmd(nil)
	if cmd.Use != "stats [EXPR]" {
		t.Errorf("Use = %q, want %q", cmd.Use, "stats [EXPR]")
	}
	if cmd.Annotations[agent.AnnotationTokenCost] != "small" {
		t.Errorf("expected %s annotation to be \"small\", got %q", agent.AnnotationTokenCost, cmd.Annotations[agent.AnnotationTokenCost])
	}
	for _, flag := range []string{"datasource", "expr", "from", "to", "since", "output"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("expected --%s flag to be registered", flag)
		}
	}
}
