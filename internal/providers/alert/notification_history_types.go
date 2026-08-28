package alert

import "time"

// Notification history status and outcome constants. The historian API only
// returns these values for status/outcome.
const (
	NotificationStatusFiring   = "firing"
	NotificationStatusResolved = "resolved"

	NotificationOutcomeSuccess = "success"
	NotificationOutcomeError   = "error"
)

// NotificationQueryRequest is the body for POST notification/query. from and to
// are always sent; the remaining fields are optional server-side filters.
type NotificationQueryRequest struct {
	Type        string         `json:"type,omitempty"`
	From        time.Time      `json:"from"`
	To          time.Time      `json:"to"`
	Limit       int64          `json:"limit,omitempty"`
	Step        int64          `json:"step,omitempty"`
	Receiver    string         `json:"receiver,omitempty"`
	RuleUID     string         `json:"ruleUID,omitempty"`
	Status      string         `json:"status,omitempty"`
	Outcome     string         `json:"outcome,omitempty"`
	GroupLabels []LabelMatcher `json:"groupLabels,omitempty"`
	Labels      []LabelMatcher `json:"labels,omitempty"`
}

// LabelMatcher filters notifications by group or alert labels. Type is one of
// "=", "!=", "=~", "!~".
type LabelMatcher struct {
	Type  string `json:"type"`
	Label string `json:"label"`
	Value string `json:"value"`
}

// NotificationQueryResponse is the response from POST notification/query. Only
// entries are modeled here; counts (for type=counts/range_counts) are out of
// scope for now and ignored on decode.
type NotificationQueryResponse struct {
	Entries []NotificationEntry `json:"entries"`
}

// NotificationEntry is one grouped notification delivery attempt. The alerts
// for an entry are fetched separately via QueryAlerts: the entry's own alerts
// field is deprecated and not populated by the API.
type NotificationEntry struct {
	Timestamp        time.Time         `json:"timestamp"`
	UUID             string            `json:"uuid"`
	Receiver         string            `json:"receiver"`
	Integration      string            `json:"integration"`
	IntegrationIndex int64             `json:"integrationIndex"`
	Status           string            `json:"status"`
	Outcome          string            `json:"outcome"`
	GroupLabels      map[string]string `json:"groupLabels,omitempty"`
	RuleUIDs         []string          `json:"ruleUIDs,omitempty"`
	AlertCount       int64             `json:"alertCount"`
	Retry            bool              `json:"retry"`
	Error            string            `json:"error,omitempty"`
	Duration         int64             `json:"duration"`
	PipelineTime     time.Time         `json:"pipelineTime,omitzero"`
	GroupKey         string            `json:"groupKey,omitempty"`
}

// NotificationAlertsRequest is the body for POST notifications/queryalerts.
type NotificationAlertsRequest struct {
	UUID  string    `json:"uuid"`
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
	Limit int64     `json:"limit,omitempty"`
}

// NotificationAlertsResponse is the response from POST notifications/queryalerts.
type NotificationAlertsResponse struct {
	Alerts []NotificationAlert `json:"alerts"`
}

// NotificationAlert is a single alert that was part of a grouped notification.
type NotificationAlert struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	StartsAt    time.Time         `json:"startsAt,omitzero"`
	EndsAt      time.Time         `json:"endsAt,omitzero"`
	Enrichments any               `json:"enrichments,omitempty"`
}
