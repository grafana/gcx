package linter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/linter/builtins"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/local"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/topdown"
	"golang.org/x/sync/errgroup"
)

type Option func(l *Linter) error

func InputPaths(inputPaths []string) Option {
	return func(l *Linter) error {
		l.inputPaths = inputPaths
		return nil
	}
}

func WithRuleBundle(ruleBundle *bundle.Bundle) Option {
	return func(l *Linter) error {
		l.ruleBundles = append(l.ruleBundles, ruleBundle)
		return nil
	}
}

func WithCustomRules(paths []string) Option {
	return func(l *Linter) error {
		l.customRulesPaths = paths
		return nil
	}
}

func Debug(stream io.Writer) Option {
	return func(l *Linter) error {
		l.debugStream = stream
		return nil
	}
}

func MaxConcurrency(maxConcurrency int) Option {
	return func(l *Linter) error {
		l.maxConcurrency = maxConcurrency
		return nil
	}
}

func DisableAll() Option {
	return func(l *Linter) error {
		l.disableAll = true
		l.enableAll = false
		return nil
	}
}

func DisabledResources(resources []string) Option {
	return func(l *Linter) error {
		l.disabledResources = resources
		return nil
	}
}

func DisabledCategories(categories []string) Option {
	return func(l *Linter) error {
		l.disabledCategories = categories
		return nil
	}
}

func DisabledRules(rules []string) Option {
	return func(l *Linter) error {
		l.disabledRules = rules
		return nil
	}
}

func EnableAll() Option {
	return func(l *Linter) error {
		l.disableAll = false
		l.enableAll = true
		return nil
	}
}

func EnabledResources(resources []string) Option {
	return func(l *Linter) error {
		l.enabledResources = resources
		return nil
	}
}

func EnabledCategories(categories []string) Option {
	return func(l *Linter) error {
		l.enabledCategories = categories
		return nil
	}
}

func EnabledRules(rules []string) Option {
	return func(l *Linter) error {
		l.enabledRules = rules
		return nil
	}
}

func ResourceReader(reader resourceReader) Option {
	return func(l *Linter) error {
		l.resourceReader = reader
		return nil
	}
}

type resourceReader interface {
	Read(ctx context.Context, dst *resources.Resources, filters resources.Filters, paths []string) error
}

type Linter struct {
	debugStream      io.Writer
	resourceReader   resourceReader
	inputPaths       []string
	ruleBundles      []*bundle.Bundle
	customRulesPaths []string
	maxConcurrency   int

	disableAll         bool
	disabledResources  []string
	disabledCategories []string
	disabledRules      []string
	enableAll          bool
	enabledResources   []string
	enabledCategories  []string
	enabledRules       []string
}

func New(opts ...Option) (*Linter, error) {
	linter := &Linter{
		resourceReader: &local.FSReader{
			Decoders:    format.Codecs(),
			StopOnError: true,
		},
		maxConcurrency: 1,
		ruleBundles: []*bundle.Bundle{
			&BuiltinBundle,
		},
		disableAll:         false,
		disabledResources:  []string{},
		disabledCategories: []string{},
		disabledRules:      []string{},
		enableAll:          true,
		enabledResources:   []string{},
		enabledCategories:  []string{},
		enabledRules:       []string{},
	}

	for _, opt := range opts {
		if err := opt(linter); err != nil {
			return nil, err
		}
	}

	return linter, nil
}

func (linter *Linter) Rules(ctx context.Context) ([]Rule, error) {
	preparedQuery, err := linter.prepare(ctx)
	if err != nil {
		return nil, err
	}

	mods := preparedQuery.Modules()
	rules := make([]Rule, 0, len(mods))

	updateFromAnnotations := func(r *Rule, annotations []*ast.Annotations) {
		if len(annotations) == 0 {
			return
		}

		annotation := annotations[0]
		r.Description = annotation.Description

		if severity, ok := annotation.Custom["severity"]; ok {
			//nolint:forcetypeassert
			r.Severity = severity.(string)
		}

		for _, related := range annotation.RelatedResources {
			r.RelatedResources = append(r.RelatedResources, RelatedResource{
				Reference:   related.Ref.String(),
				Description: related.Description,
			})
		}
	}

	// Builtin rules
	for _, module := range mods {
		parts := unquotedPath(module.Package.Path)

		// 0   1     2        3        4
		// pkg.rules.resource.category.rule

		if len(parts) != 5 {
			continue
		}

		if parts[0] != "gcx" || parts[1] != "rules" {
			continue
		}

		rule := Rule{
			Resource: parts[2],
			Category: parts[3],
			Name:     parts[4],
			Builtin:  true,
			Severity: "error",
		}

		updateFromAnnotations(&rule, module.Annotations)

		rules = append(rules, rule)
	}

	// custom rules
	for _, module := range mods {
		parts := unquotedPath(module.Package.Path)

		// 0      1   2     3        4        5
		// custom.pkg.rules.resource.category.rule

		if len(parts) != 6 {
			continue
		}

		if parts[0] != "custom" || parts[2] != "rules" {
			continue
		}

		rule := Rule{
			Resource: parts[3],
			Category: parts[4],
			Name:     parts[5],
			Builtin:  false,
			Severity: "error",
		}

		updateFromAnnotations(&rule, module.Annotations)

		rules = append(rules, rule)
	}

	return rules, nil
}

func (linter *Linter) prepare(ctx context.Context) (rego.PreparedEvalQuery, error) {
	regoOpts := []func(*rego.Rego){
		// Matches the report-generation statement in `./bundle/gcx/main/main.rego`
		rego.Query("lint := data.gcx.main.lint"),
		rego.ParsedBundle("internal", linter.createDataBundle()),
	}

	// Add a few built-ins
	regoOpts = append(regoOpts, builtins.All()...)

	if linter.debugStream != nil {
		regoOpts = append(regoOpts, rego.EnablePrintStatements(true))
		regoOpts = append(regoOpts, rego.PrintHook(topdown.NewPrintHook(linter.debugStream)))
	}

	if len(linter.customRulesPaths) != 0 {
		regoOpts = append(regoOpts, rego.Load(linter.customRulesPaths, excludeTestFilter()))
	}

	for _, ruleBundle := range linter.ruleBundles {
		var bundleName string
		if metadataName, ok := ruleBundle.Manifest.Metadata["name"].(string); ok {
			bundleName = metadataName
		}

		regoOpts = append(regoOpts, rego.ParsedBundle(bundleName, ruleBundle))
	}

	return rego.New(regoOpts...).PrepareForEval(ctx)
}

func (linter *Linter) createDataBundle() *bundle.Bundle {
	params := map[string]any{
		"disable_all":         linter.disableAll,
		"disabled_resources":  linter.disabledResources,
		"disabled_categories": linter.disabledCategories,
		"disabled_rules":      linter.disabledRules,

		"enable_all":         linter.enableAll,
		"enabled_resources":  linter.enabledResources,
		"enabled_categories": linter.enabledCategories,
		"enabled_rules":      linter.enabledRules,
	}

	return &bundle.Bundle{
		Manifest: bundle.Manifest{
			Roots:    &[]string{"internal", "eval"},
			Metadata: map[string]any{"name": "internal"},
		},
		Data: map[string]any{
			"internal": params,
		},
	}
}

func (linter *Linter) Lint(ctx context.Context) (Report, error) {
	preparedQuery, err := linter.prepare(ctx)
	if err != nil {
		return Report{}, err
	}

	inputs, err := linter.parseInputs(ctx)
	if err != nil {
		return Report{}, err
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(linter.maxConcurrency)

	reports := make([]Report, len(inputs.AsList()))
	for i, input := range inputs.AsList() {
		g.Go(func() error {
			resultSet, err := preparedQuery.Eval(ctx, rego.EvalInput(input.ToUnstructured().Object))
			if err != nil {
				return fmt.Errorf("could not lint %s: %w", input.SourcePath(), err)
			}

			report, err := resultSetToReport(input.SourcePath(), resultSet)
			if err != nil {
				return err
			}

			reports[i] = report

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return Report{}, err
	}

	finalReport := Report{}
	for _, report := range reports {
		finalReport.Violations = append(finalReport.Violations, report.Violations...)
	}

	finalReport.Summary = Summary{
		FilesScanned:  inputs.Len(),
		FilesFailed:   len(finalReport.ViolationsFileCount()),
		NumViolations: len(finalReport.Violations),
	}

	return finalReport, nil
}

func (linter *Linter) parseInputs(ctx context.Context) (*resources.Resources, error) {
	inputs := resources.NewResources()
	filters := resources.Filters{}

	for _, inputPath := range linter.inputPaths {
		if err := linter.resourceReader.Read(ctx, inputs, filters, []string{inputPath}); err != nil {
			return nil, err
		}
	}

	return inputs, nil
}

func resultSetToReport(filename string, resultSet rego.ResultSet) (Report, error) {
	if len(resultSet) != 1 {
		return Report{}, fmt.Errorf("expected 1 item in resultset, got %d", len(resultSet))
	}

	r := Report{}

	if binding, ok := resultSet[0].Bindings["lint"]; ok {
		if err := jsonRoundTrip(binding, &r); err != nil {
			return Report{}, fmt.Errorf("failed generating report from bindings: %v %w", binding, err)
		}
	}

	for i := range r.Violations {
		r.Violations[i].Location.File = filename
	}

	return r, nil
}

// jsonRoundTrip converts any value to JSON and back again.
// Useful cheat to map `map[string]any` to actual structs or the other way around.
func jsonRoundTrip(from any, to any) error {
	payload, err := json.Marshal(from)
	if err != nil {
		return err
	}

	return json.Unmarshal(payload, to)
}

// UnquotedPath returns a slice of strings from a path without quotes.
// e.g. data.dashboard["rule-name"] -> ["dashboard", "rule-name"], note that the data is not included.
func unquotedPath(path ast.Ref) []string {
	ret := make([]string, 0, len(path)-1)
	for _, ref := range path[1:] {
		ret = append(ret, strings.Trim(ref.String(), `"`))
	}

	return ret
}
