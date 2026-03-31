package linter

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	resourceTypeRegex = regexp.MustCompile(`^[a-z]+$`)
	categoryRegex     = regexp.MustCompile(`^[a-z]+$`)
	nameRegex         = regexp.MustCompile(`^[a-z_]+[a-z0-9_\-]*$`)
)

type newRuleOpts struct {
	output   string
	category string
}

func (opts *newRuleOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&opts.output, "output", "o", "", "Output directory")
	flags.StringVarP(&opts.category, "category", "c", "idiomatic", "Rule category")
}

func (opts *newRuleOpts) Validate(args []string) error {
	if args[0] == "" {
		return errors.New("resource-type is required for rule")
	}

	if !resourceTypeRegex.MatchString(args[0]) {
		return errors.New("resource-type must be a single word, using lowercase letters only")
	}

	if args[1] == "" {
		return errors.New("name is required for rule")
	}

	if !nameRegex.MatchString(args[1]) {
		return errors.New("name must consist only of lowercase letters, numbers, underscores and dashes")
	}

	if !categoryRegex.MatchString(opts.category) {
		return errors.New("category must be a single word, using lowercase letters only")
	}

	return nil
}

func newCmd() *cobra.Command {
	opts := newRuleOpts{}

	cmd := &cobra.Command{
		Use:   "new RESOURCE_TYPE NAME",
		Short: "Scaffold a new linter rule",
		Long:  "Scaffold a new Rego-based linter rule with a starter template, test fixture, and expected output file. Creates files in the current directory or the path specified by --output.",
		Args:  cobra.ExactArgs(2),
		Example: `
	# Creates a new dashboard linter rule in the current directory:

	gcx dev lint new dashboard test-linter

	# Creates a new dashboard linter rule in another directory:

	gcx dev lint new dashboard test-linter -o custom-rules
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(args); err != nil {
				return err
			}

			return scaffoldCustomRule(cmd.OutOrStdout(), opts, args[0], args[1])
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

func scaffoldCustomRule(stdout io.Writer, opts newRuleOpts, resourceType string, name string) error {
	ruleDir := filepath.Join(
		opts.output, "rules", "custom", "gcx", "rules", resourceType, opts.category, name,
	)

	ruleFileName := strings.ToLower(strings.ReplaceAll(name, "-", "_")) + ".rego"
	ruleFile := filepath.Join(ruleDir, ruleFileName)
	ruleTestFileName := strings.ToLower(strings.ReplaceAll(name, "-", "_")) + "_test.rego"
	ruleTestFile := filepath.Join(ruleDir, ruleTestFileName)

	exists, err := pathExists(ruleFile)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("%s already exists", ruleFile)
	}

	if err := os.MkdirAll(ruleDir, 0770); err != nil {
		return err
	}

	render := func(content string) string {
		rendered := strings.ReplaceAll(content, "{{.ResourceType}}", resourceType)
		rendered = strings.ReplaceAll(rendered, "{{.Category}}", opts.category)
		rendered = strings.ReplaceAll(rendered, "{{.Name}}", name)

		return rendered
	}

	if err := os.WriteFile(ruleFile, []byte(render(customRuleTemplate)), 0600); err != nil {
		return err
	}

	if err := os.WriteFile(ruleTestFile, []byte(render(customRuleTestTemplate)), 0600); err != nil {
		return err
	}

	cmdio.Success(stdout, "Rule written in %s", ruleFile)

	return nil
}

const customRuleTemplate = `# METADATA
# description: Describe the rule here.
# custom:
#  severity: warning
package custom.gcx.rules.{{.ResourceType}}.{{.Category}}["{{.Name}}"]

import data.gcx.result
import data.gcx.utils

# Dashboard v1
report contains violation if {
	utils.resource_is_dashboard_v1(input)

	input.spec.timezone != "utc"

	violation := result.fail(rego.metadata.chain(), "details")
}

# Dashboard v2
report contains violation if {
	utils.resource_is_dashboard_v2(input)

	input.spec.timeSettings.timezone != "utc"

	violation := result.fail(rego.metadata.chain(), "details")
}
`

const customRuleTestTemplate = `package custom.gcx.rules.{{.ResourceType}}.{{.Category}}["{{.Name}}_test"]

import data.gcx.utils
import data.custom.gcx.rules.{{.ResourceType}}.{{.Category}}["{{.Name}}"] as rule

test_dashboard_v1_with_timezone_utc_is_accepted if {
	resource := {
	    "kind": "Dashboard",
	    "apiVersion": "dashboard.grafana.app/v1beta1",
	    "metadata": {"name": "test-dashboard"},
	    "spec": {"timezone": "utc"}
	}

	report := rule.report with input as resource

	assert_reports_match(report, set())
}

test_dashboard_v1_with_timezone_browser_is_rejected if {
	resource := {
	    "kind": "Dashboard",
	    "apiVersion": "dashboard.grafana.app/v1beta1",
	    "metadata": {"name": "test-dashboard"},
	    "spec": {"timezone": "browser"}
	}

	report := rule.report with input as resource

	assert_reports_match(report, {{
	    "category": "{{.Category}}",
	    "description": "Describe the rule here.",
	    "details": "details",
	    "related_resources": [],
	    "resource_type": "{{.ResourceType}}",
	    "rule": "{{.Name}}",
	    "severity": "warning",
	}})
}
`

func pathExists(name string) (bool, error) {
	_, err := os.Stat(name)
	if err == nil {
		return true, nil
	}

	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}

	return false, err
}
