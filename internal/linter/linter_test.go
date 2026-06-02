package linter_test

import (
	"context"
	"testing"

	"github.com/grafana/gcx/internal/linter"
	"github.com/stretchr/testify/require"
)

func TestLinter_Rules_onlyBuiltins(t *testing.T) {
	req := require.New(t)

	engine, err := linter.New(linter.Debug(t.Output()))
	req.NoError(err)

	rules, err := engine.Rules(context.TODO())
	req.NoError(err)

	req.NotEmpty(rules, "There are builtin rules")
}

func TestLinter_Lint_noInputs(t *testing.T) {
	req := require.New(t)

	engine, err := linter.New(linter.Debug(t.Output()))
	req.NoError(err)

	report, err := engine.Lint(context.TODO())
	req.NoError(err)

	req.Empty(report.Violations)
}

func TestLinter_Lint_fileInputs(t *testing.T) {
	req := require.New(t)

	engine, err := linter.New(
		linter.Debug(t.Output()),
		linter.InputPaths([]string{
			"./testdata/dashboards/valid.json",
			"./testdata/dashboards/missing-panel-title.json",
		}),
	)
	req.NoError(err)

	report, err := engine.Lint(context.TODO())
	req.NoError(err)

	req.Len(report.Violations, 1)

	violation := report.Violations[0]

	req.Equal("panel-title-description", violation.Rule)
	req.Equal("dashboard", violation.ResourceType)
	req.Equal("idiomatic", violation.Category)
	req.Equal("warning", violation.Severity)
	req.Equal("./testdata/dashboards/missing-panel-title.json", violation.Location.File)
	req.Equal("panel 4 has no title", violation.Details)
	req.Equal("Panels should have a title and description.", violation.Description)
	req.ElementsMatch([]linter.RelatedResource{
		{
			Description: "documentation",
			Reference:   "https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/panel-title-description.md",
		},
	}, violation.RelatedResources)
}

func TestLinter_Lint_directoryInputs(t *testing.T) {
	req := require.New(t)

	engine, err := linter.New(
		linter.Debug(t.Output()),
		linter.InputPaths([]string{
			"./testdata/dashboards/",
		}),
	)
	req.NoError(err)

	report, err := engine.Lint(context.TODO())
	req.NoError(err)

	req.Len(report.Violations, 1)

	violation := report.Violations[0]

	req.Equal("panel-title-description", violation.Rule)
	req.Equal("dashboard", violation.ResourceType)
	req.Equal("idiomatic", violation.Category)
	req.Equal("warning", violation.Severity)
	req.Equal("testdata/dashboards/missing-panel-title.json", violation.Location.File)
	req.Equal("panel 4 has no title", violation.Details)
	req.Equal("Panels should have a title and description.", violation.Description)
	req.ElementsMatch([]linter.RelatedResource{
		{
			Description: "documentation",
			Reference:   "https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/panel-title-description.md",
		},
	}, violation.RelatedResources)
}

func TestLinter_Lint_disableResource(t *testing.T) {
	req := require.New(t)

	engine, err := linter.New(
		linter.Debug(t.Output()),
		linter.InputPaths([]string{
			"./testdata/alertrules/",
			"./testdata/dashboards/",
		}),
		linter.DisabledResources([]string{"dashboard"}),
	)
	req.NoError(err)

	report, err := engine.Lint(context.TODO())
	req.NoError(err)

	req.Len(report.Violations, 1)
	req.Equal("alertrule", report.Violations[0].ResourceType)
}

func TestLinter_Lint_disableRule(t *testing.T) {
	req := require.New(t)

	engine, err := linter.New(
		linter.Debug(t.Output()),
		linter.InputPaths([]string{
			"./testdata/alertrules/",
			"./testdata/dashboards/",
		}),
		linter.DisabledRules([]string{"panel-title-description"}),
	)
	req.NoError(err)

	report, err := engine.Lint(context.TODO())
	req.NoError(err)

	req.Len(report.Violations, 1)
	req.Equal("alert-runbook-link", report.Violations[0].Rule)
}

func TestLinter_Lint_disableAll(t *testing.T) {
	req := require.New(t)

	engine, err := linter.New(
		linter.Debug(t.Output()),
		linter.InputPaths([]string{
			"./testdata/alertrules/",
			"./testdata/dashboards/",
		}),
		linter.DisableAll(),
	)
	req.NoError(err)

	report, err := engine.Lint(context.TODO())
	req.NoError(err)

	req.Empty(report.Violations)
}

func TestLinter_Lint_disableAll_enableRule(t *testing.T) {
	req := require.New(t)

	engine, err := linter.New(
		linter.Debug(t.Output()),
		linter.InputPaths([]string{
			"./testdata/alertrules/",
			"./testdata/dashboards/",
		}),
		linter.DisableAll(),
		linter.EnabledRules([]string{"alert-runbook-link"}),
	)
	req.NoError(err)

	report, err := engine.Lint(context.TODO())
	req.NoError(err)

	req.Len(report.Violations, 1)
	req.Equal("alert-runbook-link", report.Violations[0].Rule)
}

func TestLinter_RestrictedCapabilities_RejectsHTTPSend(t *testing.T) {
	engine, err := linter.New(
		linter.WithCustomRules([]string{"./testdata/custom-rules/http_send_rule.rego"}),
	)
	require.NoError(t, err)

	_, err = engine.Lint(context.TODO())
	require.Error(t, err, "http.send must be rejected by restricted capabilities")
}

func TestLinter_RestrictedCapabilities_RejectsNetBuiltin(t *testing.T) {
	engine, err := linter.New(
		linter.WithCustomRules([]string{"./testdata/custom-rules/net_rule.rego"}),
	)
	require.NoError(t, err)

	_, err = engine.Lint(context.TODO())
	require.Error(t, err, "net.lookup_ip_addr must be rejected by restricted capabilities")
}

func TestLinter_RestrictedCapabilities_RejectsOPARuntime(t *testing.T) {
	engine, err := linter.New(
		linter.WithCustomRules([]string{"./testdata/custom-rules/opa_runtime_rule.rego"}),
	)
	require.NoError(t, err)

	_, err = engine.Lint(context.TODO())
	require.Error(t, err, "opa.runtime must be rejected by restricted capabilities")
}

func TestLinter_RestrictedCapabilities_AllowsSafeBuiltins(t *testing.T) {
	engine, err := linter.New(
		linter.WithCustomRules([]string{"./testdata/custom-rules/valid_rule.rego"}),
	)
	require.NoError(t, err)

	_, err = engine.Lint(context.TODO())
	require.NoError(t, err, "rules using only safe builtins must compile and run")
}

func TestLinter_RestrictedCapabilities_BuiltinRulesUnaffected(t *testing.T) {
	engine, err := linter.New(
		linter.InputPaths([]string{
			"./testdata/dashboards/valid.json",
			"./testdata/dashboards/missing-panel-title.json",
		}),
	)
	require.NoError(t, err)

	report, err := engine.Lint(context.TODO())
	require.NoError(t, err, "built-in rules must compile and run with restricted capabilities")
	require.Len(t, report.Violations, 1, "bundled rules should still find violations")
}

func TestLinter_Lint_disableAll_enableResource(t *testing.T) {
	req := require.New(t)

	engine, err := linter.New(
		linter.Debug(t.Output()),
		linter.InputPaths([]string{
			"./testdata/alertrules/",
			"./testdata/dashboards/",
		}),
		linter.DisableAll(),
		linter.EnabledResources([]string{"alertrule"}),
	)
	req.NoError(err)

	report, err := engine.Lint(context.TODO())
	req.NoError(err)

	req.Len(report.Violations, 1)
	req.Equal("alertrule", report.Violations[0].ResourceType)
}
