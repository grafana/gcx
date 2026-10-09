package watchers

import (
	"fmt"
	"time"
)

const (
	DefaultIntervalSeconds = 600
	DefaultSensitivity     = "balanced"
)

func ValidateIntervalSeconds(seconds int64) error {
	if seconds < 300 || seconds > 10800 {
		return fmt.Errorf("interval must be between %s and %s", 5*time.Minute, 3*time.Hour)
	}
	return nil
}

type Watcher struct {
	ID                     string            `json:"id"`
	Name                   string            `json:"name"`
	Description            string            `json:"description"`
	Prompt                 string            `json:"prompt"`
	Status                 string            `json:"status"`
	ArchivedAt             *time.Time        `json:"archivedAt"`
	TriggerIntervalSeconds int               `json:"triggerIntervalSeconds"`
	Sensitivity            string            `json:"sensitivity"`
	DisableDecisionSkip    bool              `json:"disableDecisionSkip"`
	DatasourceUIDs         []string          `json:"datasourceUids"`
	Labels                 map[string]string `json:"labels"`
	AutoStop               *AutoStop         `json:"autoStop"`
	Actions                Actions           `json:"actions"`
	Queries                []map[string]any  `json:"queries"`
	CalibrationContext     string            `json:"calibrationContext"`
	LastRunAt              *time.Time        `json:"lastRunAt"`
	LastRunAssessment      string            `json:"lastRunAssessment"`
	NextRunAt              *time.Time        `json:"nextRunAt"`
	TokenConsumption       *TokenConsumption `json:"tokenConsumption"`
	CalibratedAt           *time.Time        `json:"calibratedAt"`
	CreatedBy              string            `json:"createdBy"`
	CreatedAt              time.Time         `json:"createdAt"`
	UpdatedAt              time.Time         `json:"updatedAt"`
	DefinitionVersion      int64             `json:"definitionVersion"`
}

type AutoStop struct {
	PauseAt *time.Time `json:"pauseAt"`
	Archive bool       `json:"archive"`
}

type Actions struct {
	Slack         *ChatAction          `json:"slack"`
	MSTeams       *ChatAction          `json:"msteams"`
	Webhook       *WebhookAction       `json:"webhook"`
	Alerting      *AlertingAction      `json:"alerting"`
	Investigation *InvestigationAction `json:"investigation"`
}

type ChatAction struct {
	Enabled     bool        `json:"enabled"`
	Target      *ChatTarget `json:"target"`
	MinSeverity string      `json:"minSeverity"`
}

type ChatTarget struct {
	ChannelID string `json:"channelId"`
}

type WebhookAction struct {
	Enabled      bool          `json:"enabled"`
	MinSeverity  string        `json:"minSeverity"`
	SecureFields *SecureFields `json:"secureFields"`
}

type SecureFields struct {
	URL                      bool `json:"url"`
	HMACSecret               bool `json:"hmacSecret"`
	AuthorizationCredentials bool `json:"authorizationCredentials"`
}

type AlertingAction struct {
	Enabled bool `json:"enabled"`
}

type InvestigationAction struct {
	Enabled   bool     `json:"enabled"`
	TeamNames []string `json:"teamNames"`
}

type TokenConsumption struct {
	AveragePerRun    int64 `json:"averagePerRun"`
	EstimatedPerHour int64 `json:"estimatedPerHour"`
	SampleSize       int   `json:"sampleSize"`
}

type Enrollment struct {
	Enabled bool `json:"enabled"`
}

type Calibration struct {
	Status    string               `json:"status"`
	Message   string               `json:"message"`
	StartedAt *time.Time           `json:"startedAt"`
	Activity  *CalibrationActivity `json:"activity"`
}

type CalibrationActivity struct {
	ChatID string `json:"chatId"`
	TaskID string `json:"taskId"`
}
