package alert

// Alert rule state constants. The Grafana alerting API only returns these three states.
const (
	StateFiring   = "firing"
	StatePending  = "pending"
	StateInactive = "inactive"
)

// ErrorResponse is the error response body returned by the alerting API.
type ErrorResponse struct {
	Error string `json:"error"`
}

// RulesResponse is the response from /api/prometheus/grafana/api/v1/rules.
type RulesResponse struct {
	Status string    `json:"status"`
	Data   RulesData `json:"data"`
}

// RulesData contains the groups and totals from the rules response.
type RulesData struct {
	Groups []RuleGroup    `json:"groups"`
	Totals map[string]int `json:"totals,omitempty"`
}

// GetResourceName returns the rule group name.
func (g RuleGroup) GetResourceName() string { return g.Name }

// SetResourceName restores the rule group name.
func (g *RuleGroup) SetResourceName(name string) { g.Name = name }

// RuleGroup represents an alert rule group.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type RuleGroup struct {
	Name           string         `json:"name"`
	File           string         `json:"file"`
	FolderUID      string         `json:"folderUid"`
	Rules          []RuleStatus   `json:"rules"`
	Totals         map[string]int `json:"totals,omitempty"`
	Interval       int            `json:"interval"`
	LastEvaluation string         `json:"lastEvaluation"`
	EvaluationTime float64        `json:"evaluationTime"`
}

// GetResourceName returns the rule UID.
func (r RuleStatus) GetResourceName() string { return r.UID }

// SetResourceName restores the rule UID.
func (r *RuleStatus) SetResourceName(name string) { r.UID = name }

// RuleStatus represents an alert rule with its current status.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type RuleStatus struct {
	State                 string                `json:"state"`
	Name                  string                `json:"name"`
	UID                   string                `json:"uid"`
	FolderUID             string                `json:"folderUid"`
	Health                string                `json:"health"`
	Type                  string                `json:"type"`
	Query                 string                `json:"query"`
	LastEvaluation        string                `json:"lastEvaluation"`
	EvaluationTime        float64               `json:"evaluationTime"`
	IsPaused              bool                  `json:"isPaused"`
	Labels                map[string]string     `json:"labels"`
	Annotations           map[string]string     `json:"annotations"`
	QueriedDatasourceUIDs []string              `json:"queriedDatasourceUIDs,omitempty"`
	NotificationSettings  *NotificationSettings `json:"notificationSettings,omitempty"`
}

// NotificationSettings contains notification configuration for an alert rule.
type NotificationSettings struct {
	Receiver      string `json:"receiver,omitempty"`
	GroupInterval string `json:"group_interval,omitempty"`
}
