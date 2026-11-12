package metrics

import (
	"errors"
	"fmt"
	"strings"
)

// ruleV2 is the Adaptive Metrics v2 wire shape of a rule. The CLI, manifests
// and output keep the MetricRule shape; conversion happens at the client edge.
type ruleV2 struct {
	Metric      string         `json:"metric"`
	MatchType   string         `json:"match_type,omitempty"`
	Action      string         `json:"action"`
	Functions   []string       `json:"functions,omitempty"`
	Interval    string         `json:"interval,omitempty"`
	Delay       string         `json:"delay,omitempty"`
	LabelPolicy *labelPolicyV2 `json:"label_policy,omitempty"`
}

type labelPolicyV2 struct {
	Mode   string   `json:"mode"`
	Labels []string `json:"labels"`
}

const (
	actionAggregate = "aggregate"
	actionDrop      = "drop"
	labelModeKeep   = "keep"
	labelModeDrop   = "drop"
)

// toRuleV2 converts a MetricRule. Fields v2 does not model (managed_by) are
// dropped. A rule that sets both keep and drop labels has no v2 form and is
// rejected here with the message the v1 API used.
func toRuleV2(r MetricRule) (ruleV2, error) {
	out := ruleV2{Metric: r.Metric, MatchType: r.MatchType}
	if r.Drop {
		if len(r.Aggregations) > 0 || len(r.KeepLabels) > 0 || len(r.DropLabels) > 0 {
			return ruleV2{}, errors.New("cannot drop and aggregate metric at the same time")
		}
		out.Action = actionDrop
		out.Interval = r.AggregationInterval
		out.Delay = r.AggregationDelay
		return out, nil
	}

	out.Action = actionAggregate
	out.Functions = r.Aggregations
	out.Interval = r.AggregationInterval
	out.Delay = r.AggregationDelay
	switch {
	case len(r.KeepLabels) > 0 && len(r.DropLabels) > 0:
		return ruleV2{}, errors.New("cannot aggregate on both dropped and kept labels")
	case len(r.KeepLabels) > 0:
		out.LabelPolicy = &labelPolicyV2{Mode: labelModeKeep, Labels: r.KeepLabels}
	case len(r.DropLabels) > 0:
		out.LabelPolicy = &labelPolicyV2{Mode: labelModeDrop, Labels: r.DropLabels}
	default:
		return ruleV2{}, errors.New("no labels to aggregate on")
	}
	return out, nil
}

func fromRuleV2(r ruleV2) MetricRule {
	out := MetricRule{Metric: r.Metric, MatchType: r.MatchType}
	if r.Action == actionDrop {
		out.Drop = true
		return out
	}
	out.Aggregations = r.Functions
	out.AggregationInterval = r.Interval
	out.AggregationDelay = r.Delay
	if r.LabelPolicy != nil {
		switch r.LabelPolicy.Mode {
		case labelModeKeep:
			out.KeepLabels = r.LabelPolicy.Labels
		case labelModeDrop:
			out.DropLabels = r.LabelPolicy.Labels
		}
	}
	return out
}

// validateRuleRef rejects segment and metric values that cannot be addressed
// safely as URL path elements.
func validateRuleRef(segment, metric string, needMetric bool) error {
	if segment != "" && (strings.TrimSpace(segment) == "" || segment == "." || segment == "..") {
		return fmt.Errorf("invalid segment ID %q", segment)
	}
	if needMetric && (strings.TrimSpace(metric) == "" || metric == "." || metric == "..") {
		return fmt.Errorf("invalid metric name %q", metric)
	}
	return nil
}
