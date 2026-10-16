package watchers

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

type Check struct {
	ID            string            `json:"id,omitempty" yaml:"id,omitempty"`
	Enabled       *bool             `json:"enabled" yaml:"enabled"`
	DatasourceUID string            `json:"datasourceUid,omitempty" yaml:"datasourceUid,omitempty"`
	QueryType     string            `json:"queryType" yaml:"queryType"`
	Expression    string            `json:"expression,omitempty" yaml:"expression,omitempty"`
	Guidance      string            `json:"guidance,omitempty" yaml:"guidance,omitempty"`
	Parameters    map[string]any    `json:"parameters" yaml:"parameters"`
	Unsupported   *UnsupportedCheck `json:"unsupported,omitempty" yaml:"unsupported,omitempty"`
}

type UnsupportedCheck struct {
	QueryType  bool           `json:"queryType,omitempty" yaml:"queryType,omitempty"`
	Parameters map[string]any `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

func ProjectChecks(queries []map[string]any) []Check {
	checks := make([]Check, 0, len(queries))
	for _, raw := range queries {
		enabled := true
		check := Check{
			ID: stringField(raw, "id"), Enabled: &enabled,
			DatasourceUID: stringField(raw, "datasourceUid"),
			QueryType:     stringField(raw, "type"), Expression: stringField(raw, "expr"),
			Guidance: stringField(raw, "comment"), Parameters: make(map[string]any),
		}
		unsupported := &UnsupportedCheck{Parameters: make(map[string]any)}
		if value, present := raw["enabled"]; present && value != nil {
			if observed, ok := value.(bool); ok {
				check.Enabled = &observed
			} else {
				check.Enabled = nil
				unsupported.Parameters["enabled"] = value
			}
		}
		switch check.QueryType {
		case "promql", "logql", "alerts", "bigquery", "traceql_metrics", "synthetic_monitoring", "pyroscope_series", "cloudwatch_metric":
		default:
			unsupported.QueryType = true
		}
		for key, value := range raw {
			switch key {
			case "id", "type", "datasourceUid", "expr", "comment", "enabled":
				if key != "enabled" && !isString(value) {
					unsupported.Parameters[key] = value
				}
			case "role", "thresholds", "goodWhen":
				check.Parameters[key] = value
				inspectParameter(key, value, unsupported.Parameters)
			case "logBaselineSeed":
				check.Parameters["logBaseline"] = value
				inspectParameter("logBaseline", value, unsupported.Parameters)
			default:
				unsupported.Parameters[key] = value
			}
		}
		if unsupported.QueryType || len(unsupported.Parameters) > 0 {
			check.Unsupported = unsupported
		}
		checks = append(checks, check)
	}
	return checks
}

func inspectParameter(key string, value any, unsupported map[string]any) {
	switch key {
	case "role":
		if !oneOf("fast_incident", "current_health", "long_window_budget", "context")(value) {
			unsupported[key] = value
		}
	case "goodWhen":
		if !oneOf("low", "high", "absent")(value) {
			unsupported[key] = value
		}
	case "thresholds":
		inspectObject(key, value, map[string]func(any) bool{
			"comparator": oneOf("gt", "gte", "lt", "lte"), "warning": isNumber, "critical": isNumber, "source": isString,
		}, unsupported)
	case "logBaseline":
		inspectObject(key, value, map[string]func(any) bool{
			"benignLineFilters": stringArray,
			"patterns": func(value any) bool {
				patterns, ok := value.([]any)
				if !ok {
					return false
				}
				for i, pattern := range patterns {
					inspectObject(fmt.Sprintf("logBaseline.patterns[%d]", i), pattern, map[string]func(any) bool{
						"pattern": isString, "level": isString, "classification": oneOf("benign", "acknowledged", "watch"),
						"reviewAfter": func(value any) bool {
							s, ok := value.(string)
							_, err := time.Parse(time.RFC3339, s)
							return ok && err == nil
						},
					}, unsupported)
				}
				return true
			},
		}, unsupported)
	}
}

func inspectObject(path string, value any, fields map[string]func(any) bool, unsupported map[string]any) {
	object, ok := value.(map[string]any)
	if !ok {
		unsupported[path] = value
		return
	}
	for key, value := range object {
		validate, known := fields[key]
		if !known || !validate(value) {
			unsupported[path+"."+key] = value
		}
	}
}

func oneOf(values ...string) func(any) bool {
	return func(value any) bool {
		s, ok := value.(string)
		return ok && slices.Contains(values, s)
	}
}

func isString(value any) bool {
	_, ok := value.(string)
	return ok
}

func isNumber(value any) bool {
	switch value.(type) {
	case float64, float32, int, int64, json.Number:
		return true
	default:
		return false
	}
}

func stringArray(value any) bool {
	values, ok := value.([]any)
	if !ok {
		return false
	}
	for _, value := range values {
		if !isString(value) {
			return false
		}
	}
	return true
}

func stringField(raw map[string]any, key string) string {
	v, _ := raw[key].(string)
	return v
}
