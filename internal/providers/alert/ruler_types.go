package alert

import (
	"errors"
	"fmt"

	"github.com/prometheus/common/model"
	promql "github.com/prometheus/prometheus/promql/parser"
)

// RulerRule is a single alerting or recording rule in the Prometheus/Loki
// ruler config wire format. Exactly one of Record or Alert must be set.
type RulerRule struct {
	Record        string            `json:"record,omitempty"`
	Alert         string            `json:"alert,omitempty"`
	Expr          string            `json:"expr"`
	For           string            `json:"for,omitempty"`
	KeepFiringFor string            `json:"keep_firing_for,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// RulerRuleGroup is a ruler config rule group as accepted and returned by the
// Mimir/Loki ruler API.
type RulerRuleGroup struct {
	Name     string      `json:"name"`
	Interval string      `json:"interval,omitempty"`
	Rules    []RulerRule `json:"rules"`
}

// RulerApplyInput accepts both apply input shapes in a single decode: a
// standard Prometheus rules file (`groups:` list, as used by mimirtool) and a
// bare single rule group (`name`/`interval`/`rules` at the top level).
type RulerApplyInput struct {
	Groups   []RulerRuleGroup `json:"groups,omitempty"`
	Name     string           `json:"name,omitempty"`
	Interval string           `json:"interval,omitempty"`
	Rules    []RulerRule      `json:"rules,omitempty"`
}

// RuleGroups returns the rule groups described by the input.
func (in RulerApplyInput) RuleGroups() ([]RulerRuleGroup, error) {
	if len(in.Groups) > 0 {
		if in.Name != "" || len(in.Rules) > 0 {
			return nil, errors.New("input mixes a `groups:` list with a top-level rule group")
		}
		return in.Groups, nil
	}
	if in.Name != "" || len(in.Rules) > 0 {
		return []RulerRuleGroup{{Name: in.Name, Interval: in.Interval, Rules: in.Rules}}, nil
	}
	return nil, errors.New("input contains neither a `groups:` list nor a rule group")
}

// Validate checks the rule group for structural problems before it is sent to
// the ruler. When promQL is true, each rule expression is additionally parsed
// as PromQL (skip for Loki datasources, whose expressions are LogQL).
func (g RulerRuleGroup) Validate(promQL bool) error {
	if g.Name == "" {
		return errors.New("rule group has no name")
	}
	if len(g.Rules) == 0 {
		return fmt.Errorf("rule group %q has no rules", g.Name)
	}
	if g.Interval != "" {
		if _, err := model.ParseDuration(g.Interval); err != nil {
			return fmt.Errorf("rule group %q: invalid interval %q: %w", g.Name, g.Interval, err)
		}
	}
	for i, rule := range g.Rules {
		if err := rule.validate(promQL); err != nil {
			return fmt.Errorf("rule group %q, rule %d: %w", g.Name, i, err)
		}
	}
	return nil
}

func (r RulerRule) validate(promQL bool) error {
	switch {
	case r.Record == "" && r.Alert == "":
		return errors.New("rule must set either `record` or `alert`")
	case r.Record != "" && r.Alert != "":
		return errors.New("rule must set only one of `record` and `alert`")
	}
	if r.Expr == "" {
		return errors.New("rule has an empty `expr`")
	}
	if r.Record != "" {
		if r.For != "" {
			return errors.New("recording rule must not set `for`")
		}
		if r.KeepFiringFor != "" {
			return errors.New("recording rule must not set `keep_firing_for`")
		}
		if len(r.Annotations) > 0 {
			return errors.New("recording rule must not set `annotations`")
		}
		if !model.UTF8Validation.IsValidMetricName(r.Record) {
			return fmt.Errorf("invalid recorded metric name %q", r.Record)
		}
	}
	for _, dur := range []struct{ name, value string }{
		{"for", r.For},
		{"keep_firing_for", r.KeepFiringFor},
	} {
		if dur.value == "" {
			continue
		}
		if _, err := model.ParseDuration(dur.value); err != nil {
			return fmt.Errorf("invalid `%s` duration %q: %w", dur.name, dur.value, err)
		}
	}
	if promQL {
		if _, err := promql.NewParser(promql.Options{}).ParseExpr(r.Expr); err != nil {
			return fmt.Errorf("invalid PromQL expression: %w", err)
		}
	}
	return nil
}
