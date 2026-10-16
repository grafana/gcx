package watchers

// ConfigurationRequest keeps omission distinct from replacement, including empty
// collections. Secrets belong only to request types, never response types.
type ConfigurationRequest struct {
	Name                   *string            `json:"name,omitempty"`
	Description            *string            `json:"description,omitempty"`
	Prompt                 *string            `json:"prompt,omitempty"`
	TriggerIntervalSeconds *int               `json:"triggerIntervalSeconds,omitempty"`
	Sensitivity            *string            `json:"sensitivity,omitempty"`
	DisableDecisionSkip    *bool              `json:"disableDecisionSkip,omitempty"`
	DatasourceUIDs         *[]string          `json:"datasourceUids,omitempty"`
	Labels                 *map[string]string `json:"labels,omitempty"`
	AutoStop               *AutoStop          `json:"autoStop,omitempty"`
	Actions                *WriteActions      `json:"actions,omitempty"`
}

type WriteActions struct {
	Slack         *ChatAction          `json:"slack,omitempty"`
	MSTeams       *ChatAction          `json:"msteams,omitempty"`
	Webhook       *WebhookWriteAction  `json:"webhook,omitempty"`
	Alerting      *AlertingAction      `json:"alerting,omitempty"`
	Investigation *InvestigationAction `json:"investigation,omitempty"`
}

type WebhookWriteAction struct {
	Enabled             *bool                  `json:"enabled,omitempty"`
	MinSeverity         *string                `json:"minSeverity,omitempty"`
	URL                 *string                `json:"url,omitempty"`
	AuthorizationScheme *string                `json:"authorizationScheme,omitempty"`
	SecureSettings      *WebhookSecureSettings `json:"secureSettings,omitempty"`
}

type WebhookSecureSettings struct {
	HMACSecret               *string `json:"hmacSecret,omitempty"`
	AuthorizationCredentials *string `json:"authorizationCredentials,omitempty"`
}
