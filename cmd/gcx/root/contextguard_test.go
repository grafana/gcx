package root_test

import (
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const guardTestVersion = "v0.0.0-test"

// enableStrictContext turns on strict context mode and clears the environment
// overrides that would otherwise satisfy the guard on a developer machine.
func enableStrictContext(t *testing.T) {
	t.Helper()
	t.Setenv("GCX_REQUIRE_CONTEXT", "true")
	t.Setenv("GRAFANA_SERVER", "")
}

func TestEnforceContextSelection_Disabled(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "unset", value: ""},
		{name: "false", value: "false"},
		{name: "zero", value: "0"},
		{name: "off", value: "off"},
		{name: "no", value: "no"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GCX_REQUIRE_CONTEXT", tc.value)
			t.Setenv("GRAFANA_SERVER", "")

			assert.NoError(t, root.EnforceContextSelection(guardTestVersion,
				[]string{"slo", "definitions", "list"}))
		})
	}
}

// A value that is neither recognized nor an off switch keeps the requirement
// in place: a typo must not silently disable the guard.
func TestEnforceContextSelection_UnrecognizedValueStaysOn(t *testing.T) {
	t.Setenv("GCX_REQUIRE_CONTEXT", "ture")
	t.Setenv("GRAFANA_SERVER", "")

	assert.Error(t, root.EnforceContextSelection(guardTestVersion,
		[]string{"slo", "definitions", "list"}))
}

func TestEnforceContextSelection_RequiresContext(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "provider command", args: []string{"slo", "definitions", "list"}},
		{name: "signal query", args: []string{"metrics", "query", "up"}},
		{name: "resources get", args: []string{"resources", "get", "dashboards"}},
		{name: "resources list-types", args: []string{"resources", "list-types"}},
		{name: "raw api passthrough", args: []string{"api", "/api/health"}},
		{name: "setup status", args: []string{"setup", "status"}},
		{name: "config check reaches the environment", args: []string{"config", "check"}},
		{name: "instrumentation status", args: []string{"instrumentation", "status"}},
		{name: "cloud stacks list", args: []string{"cloud", "stacks", "list"}},
		{name: "dev import", args: []string{"dev", "import", "dashboards"}},
		{name: "empty context value", args: []string{"--context=", "slo", "definitions", "list"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			enableStrictContext(t)

			err := root.EnforceContextSelection(guardTestVersion, tc.args)
			require.Error(t, err)

			detailed := fail.ErrorToDetailedError(err)
			require.NotNil(t, detailed)
			require.NotNil(t, detailed.ExitCode)
			assert.Equal(t, gcxerrors.ExitUsageError, *detailed.ExitCode)
			assert.Contains(t, strings.Join(detailed.Suggestions, "\n"), "--context <name>")
		})
	}
}

func TestEnforceContextSelection_SatisfiedByExplicitTarget(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		grafanaServer string
	}{
		{
			name: "context before the command",
			args: []string{"--context", "prospect-a", "slo", "definitions", "list"},
		},
		{
			name: "context after the command",
			args: []string{"slo", "definitions", "list", "--context", "prospect-a"},
		},
		{
			name: "context in equals form",
			args: []string{"--context=prospect-a", "slo", "definitions", "list"},
		},
		{
			// `config` binds its own --context, which shadows root's in the
			// resolved command's flag set.
			name: "subtree-bound context flag",
			args: []string{"config", "check", "--context", "prospect-a"},
		},
		{
			name: "root context flag ahead of a shadowing subtree",
			args: []string{"--context", "prospect-a", "config", "check"},
		},
		{
			name:          "server override names the destination",
			args:          []string{"slo", "definitions", "list"},
			grafanaServer: "https://prospect-a.grafana.net",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			enableStrictContext(t)
			if tc.grafanaServer != "" {
				t.Setenv("GRAFANA_SERVER", tc.grafanaServer)
			}

			assert.NoError(t, root.EnforceContextSelection(guardTestVersion, tc.args))
		})
	}
}

func TestEnforceContextSelection_ExemptCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "no arguments prints help", args: []string{}},
		{name: "version", args: []string{"version"}},
		{name: "help command", args: []string{"help"}},
		{name: "help flag on an enforced command", args: []string{"slo", "definitions", "list", "--help"}},
		{name: "group node prints help", args: []string{"slo"}},
		{name: "completion", args: []string{"completion", "bash"}},
		{name: "shell completion helper", args: []string{cobra.ShellCompRequestCmd, "slo"}},
		{name: "commands catalog", args: []string{"commands"}},
		{name: "help tree", args: []string{"help-tree"}},
		{name: "providers list", args: []string{"providers", "list"}},
		{name: "skills installer", args: []string{"agent", "skills", "list"}},
		{name: "config view", args: []string{"config", "view"}},
		{name: "config use-context", args: []string{"config", "use-context", "prospect-a"}},
		{name: "login names its own destination", args: []string{"login", "prospect-a"}},
		{name: "cloud login", args: []string{"cloud", "login"}},
		{name: "local lint", args: []string{"dev", "lint", "run", "./dashboards"}},
		{name: "scaffold", args: []string{"dev", "scaffold", "myproject"}},
		{name: "resources list-examples", args: []string{"resources", "list-examples"}},
		{name: "local instrumentation check", args: []string{"instrumentation", "check"}},
		{name: "instrumentation explain", args: []string{"instrumentation", "explain", "some-id"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			enableStrictContext(t)

			assert.NoError(t, root.EnforceContextSelection(guardTestVersion, tc.args))
		})
	}
}

// `instrumentation check` is local until --fix-plan=assistant sends the
// findings to a stack, which is the point at which it needs a named context.
func TestEnforceContextSelection_InstrumentationCheckFixPlan(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantError bool
	}{
		{name: "default is local", args: []string{"instrumentation", "check"}},
		{name: "local fix plan", args: []string{"instrumentation", "check", "--fix-plan", "local"}},
		{
			name:      "assistant fix plan reaches a stack",
			args:      []string{"instrumentation", "check", "--fix-plan", "assistant"},
			wantError: true,
		},
		{
			name: "assistant fix plan with a context",
			args: []string{"instrumentation", "check", "--fix-plan", "assistant", "--context", "prospect-a"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			enableStrictContext(t)

			err := root.EnforceContextSelection(guardTestVersion, tc.args)
			if tc.wantError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

// A provider group that defines its own PersistentPreRun would shadow root's
// hook, so enforcement must not depend on that hook running.
func TestEnforceContextSelection_CoversSubtreesWithOwnPreRun(t *testing.T) {
	enableStrictContext(t)

	for _, args := range [][]string{
		{"slo", "definitions", "list"},
		{"synthetic-monitoring", "checks", "list"},
		{"dashboards", "list"},
		{"datasources", "list"},
		{"alert", "rules", "list"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			assert.Error(t, root.EnforceContextSelection(guardTestVersion, args))
		})
	}
}

// The exempt list must keep naming real commands, so a rename or removal shows
// up here rather than silently widening or narrowing what the guard covers.
func TestContextExemptRoutesExist(t *testing.T) {
	rootCmd := root.Command(guardTestVersion)

	routes := map[string]bool{}
	agent.WalkCommands(rootCmd, func(cmd *cobra.Command) {
		names := []string{}
		for c := cmd; c.HasParent(); c = c.Parent() {
			names = append([]string{c.Name()}, names...)
		}
		if len(names) > 0 {
			routes[strings.Join(names, " ")] = true
		}
	})

	for _, route := range root.ContextPolicyRoutesForTest() {
		assert.True(t, routes[route], "route %q is in the context policy but not in the command tree", route)
	}
}
