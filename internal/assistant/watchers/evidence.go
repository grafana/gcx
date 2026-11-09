package watchers

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
)

// UnsupportedConfiguration reports categories only: values and unknown field
// names may contain sensitive material and must never enter diagnostics.
func (w *Watcher) UnsupportedConfiguration() []string {
	var out []string
	if w == nil {
		return out
	}
	if w.AutoStop != nil {
		out = append(out, w.AutoStop.unsupported...)
	}
	out = append(out, w.Actions.unsupported...)
	for _, action := range []*ChatAction{w.Actions.Slack, w.Actions.MSTeams} {
		if action == nil {
			continue
		}
		out = append(out, action.unsupported...)
		if action.Target != nil {
			out = append(out, action.Target.unsupported...)
		}
	}
	if action := w.Actions.MSTeams; action != nil && action.Target != nil {
		target := action.Target
		if target.Type != "" && target.Type != "channel" || target.UserID != "" || target.TeamID != "" {
			out = append(out, "notification targeting")
		}
	}
	if a := w.Actions.Webhook; a != nil {
		out = append(out, a.unsupported...)
		if a.SecureFields != nil {
			out = append(out, a.SecureFields.unsupported...)
		}
		if a.AuthorizationScheme != "" && a.AuthorizationScheme != "Bearer" {
			out = append(out, "webhook authorization settings")
		}
	}
	if a := w.Actions.Alerting; a != nil {
		out = append(out, a.unsupported...)
	}
	if a := w.Actions.Investigation; a != nil {
		out = append(out, a.unsupported...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (e *Enrollment) UnsupportedConfiguration() []string {
	if e == nil {
		return nil
	}
	out := slices.Clone(e.unsupported)
	if e.ScheduleCron != "" && strings.TrimSpace(e.ScheduleCron) != "0 9 * * 1" || e.ScheduleTimezone != "" && e.ScheduleTimezone != "UTC" {
		out = append(out, "automatic recalibration scheduling")
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (w *Watcher) UnmarshalJSON(data []byte) error {
	type plain Watcher
	var item plain
	if err := json.Unmarshal(data, &item); err != nil {
		return err
	}
	if item.Labels == nil && item.Actions.Alerting != nil {
		item.Labels = maps.Clone(item.Actions.Alerting.Labels)
	}
	if item.AutoStop == nil && item.Temporary != nil {
		item.AutoStop = &AutoStop{PauseAt: item.Temporary.EndsAt}
	}
	*w = Watcher(item)
	return nil
}

func unknownConfiguration(data []byte, category string, known ...string) []string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return nil
	}
	for _, key := range known {
		delete(fields, key)
	}
	for _, value := range fields {
		var decoded any
		if json.Unmarshal(value, &decoded) == nil && meaningful(decoded) {
			return []string{category}
		}
	}
	return nil
}

func meaningful(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case float64:
		return v != 0
	case []any:
		return slices.ContainsFunc(v, meaningful)
	case map[string]any:
		for _, item := range v {
			if meaningful(item) {
				return true
			}
		}
	}
	return false
}

func (v *Actions) UnmarshalJSON(data []byte) error {
	type plain Actions
	var item plain
	if err := json.Unmarshal(data, &item); err != nil {
		return err
	}
	item.unsupported = unknownConfiguration(data, "notification settings", "slack", "msteams", "webhook", "irm", "alerting", "investigation")
	*v = Actions(item)
	return nil
}

func (v *ChatAction) UnmarshalJSON(data []byte) error {
	type plain ChatAction
	var item plain
	if err := json.Unmarshal(data, &item); err != nil {
		return err
	}
	item.unsupported = unknownConfiguration(data, "notification settings", "enabled", "target", "minSeverity")
	*v = ChatAction(item)
	return nil
}

func (v *ChatTarget) UnmarshalJSON(data []byte) error {
	type plain ChatTarget
	var item plain
	if err := json.Unmarshal(data, &item); err != nil {
		return err
	}
	item.unsupported = unknownConfiguration(data, "notification targeting", "type", "channelId", "teamId", "userId")
	*v = ChatTarget(item)
	return nil
}

func (v *WebhookAction) UnmarshalJSON(data []byte) error {
	type plain WebhookAction
	var item plain
	if err := json.Unmarshal(data, &item); err != nil {
		return err
	}
	item.unsupported = unknownConfiguration(data, "webhook settings", "enabled", "minSeverity", "authorizationScheme", "secureFields", "url", "urlDisplay", "secureSettings")
	*v = WebhookAction(item)
	return nil
}

func (v *AlertingAction) UnmarshalJSON(data []byte) error {
	type plain AlertingAction
	var item plain
	if err := json.Unmarshal(data, &item); err != nil {
		return err
	}
	item.unsupported = unknownConfiguration(data, "Alerting settings", "enabled", "minSeverity", "labels", "routingTree")
	*v = AlertingAction(item)
	return nil
}

func (v *InvestigationAction) UnmarshalJSON(data []byte) error {
	type plain InvestigationAction
	var item plain
	if err := json.Unmarshal(data, &item); err != nil {
		return err
	}
	item.unsupported = unknownConfiguration(data, "investigation settings", "enabled", "teamNames")
	*v = InvestigationAction(item)
	return nil
}

func (v *Enrollment) UnmarshalJSON(data []byte) error {
	type plain Enrollment
	var item plain
	if err := json.Unmarshal(data, &item); err != nil {
		return err
	}
	item.unsupported = unknownConfiguration(data, "automatic recalibration settings", "enabled", "scheduleCron", "scheduleTimezone", "watcherId", "automationId", "nextRunAt", "lastRunAt", "lastRunStatus")
	*v = Enrollment(item)
	return nil
}

func (v *AutoStop) UnmarshalJSON(data []byte) error {
	type plain AutoStop
	var item plain
	if err := json.Unmarshal(data, &item); err != nil {
		return err
	}
	item.unsupported = unknownConfiguration(data, "automatic stop settings", "pauseAt", "archive")
	*v = AutoStop(item)
	return nil
}

func (v *SecureFields) UnmarshalJSON(data []byte) error {
	type plain SecureFields
	var item plain
	if err := json.Unmarshal(data, &item); err != nil {
		return err
	}
	item.unsupported = unknownConfiguration(data, "webhook secret settings", "url", "hmacSecret", "authorizationCredentials")
	*v = SecureFields(item)
	return nil
}
