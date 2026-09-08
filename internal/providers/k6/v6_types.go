package k6

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// AuthValidation is the response from the k6 Cloud token validation endpoint.
type AuthValidation struct {
	StackID          int `json:"stack_id"`
	DefaultProjectID int `json:"default_project_id"`
}

// LabelKey defines an organization-wide project label key.
type LabelKey struct {
	ID          int     `json:"id"`
	Key         string  `json:"key"`
	Description *string `json:"description"`
}

type LabelKeyCreateRequest struct {
	Value []LabelKeyCreateItem `json:"value"`
}

type LabelKeyCreateItem struct {
	Key         string  `json:"key"`
	Description *string `json:"description,omitempty"`
}

// LabelKeyPatch preserves an explicit null value for fields that can be cleared.
type LabelKeyPatch map[string]*string

type ProjectLabel struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type ProjectLabelPutRequest struct {
	Value []ProjectLabelPutItem `json:"value"`
}

type ProjectLabelPutItem struct {
	KeyID *int    `json:"key_id,omitempty"`
	Key   *string `json:"key,omitempty"`
	Value string  `json:"value"`
}

// CloudSchedule keeps the v6 operation name while sharing the complete
// published schedule model used by the existing schedule commands.
type CloudSchedule = Schedule

type CloudScheduleRecurrence = RecurrenceRule

type CloudScheduleCron = ScheduleCron

// FlexibleNumber accepts the number and numeric-string forms returned by k6.
type FlexibleNumber float64

func (n *FlexibleNumber) UnmarshalJSON(data []byte) error {
	raw := string(bytes.TrimSpace(data))
	if len(raw) > 1 && raw[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		raw = text
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fmt.Errorf("invalid numeric value %q: %w", raw, err)
	}
	*n = FlexibleNumber(value)
	return nil
}

type TestRunCostBreakdown struct {
	ProtocolVUH            FlexibleNumber            `json:"protocol_vuh"`
	BrowserVUH             FlexibleNumber            `json:"browser_vuh"`
	BaseTotalVUH           FlexibleNumber            `json:"base_total_vuh"`
	ReductionRate          *FlexibleNumber           `json:"reduction_rate"`
	ReductionRateBreakdown map[string]FlexibleNumber `json:"reduction_rate_breakdown"`
}

type TestRunCost struct {
	TotalVUH  FlexibleNumber       `json:"total_vuh"`
	Breakdown TestRunCostBreakdown `json:"breakdown"`
}

type TestRunStatusDetail struct {
	Type    string         `json:"type"`
	Entered string         `json:"entered"`
	Extra   map[string]any `json:"extra"`
}

type TestRunResultDetails struct {
	Type    string  `json:"type"`
	Message *string `json:"message"`
	Code    *int    `json:"code"`
}

type TestRunDistributionZone struct {
	LoadZone string  `json:"load_zone"`
	Percent  float64 `json:"percent"`
}

// TestRun contains the full v6 test-run response.
type TestRun struct {
	ID                    int                       `json:"id"`
	TestID                int                       `json:"test_id"`
	LoadTestID            int                       `json:"load_test_id,omitempty"`
	ProjectID             int                       `json:"project_id"`
	StartedBy             *string                   `json:"started_by"`
	Created               string                    `json:"created"`
	Ended                 *string                   `json:"ended"`
	Note                  string                    `json:"note"`
	RetentionExpiry       *string                   `json:"retention_expiry"`
	Cost                  *TestRunCost              `json:"cost"`
	Status                string                    `json:"status"`
	StatusDetails         TestRunStatusDetail       `json:"status_details"`
	StatusHistory         []TestRunStatusDetail     `json:"status_history"`
	Distribution          []TestRunDistributionZone `json:"distribution"`
	Result                *string                   `json:"result"`
	ResultStatus          int                       `json:"result_status,omitempty"`
	ResultDetails         *TestRunResultDetails     `json:"result_details"`
	Options               map[string]any            `json:"options"`
	K6Dependencies        map[string]string         `json:"k6_dependencies"`
	K6Versions            map[string]string         `json:"k6_versions"`
	MaxVUs                *int                      `json:"max_vus"`
	MaxBrowserVUs         *int                      `json:"max_browser_vus"`
	EstimatedDuration     *int                      `json:"estimated_duration"`
	ExecutionDuration     int                       `json:"execution_duration"`
	IsStarred             bool                      `json:"is_starred"`
	Labels                map[string]string         `json:"labels"`
	CostAttributionLabels map[string]string         `json:"cost_attribution_labels"`
	DetailsPageURL        string                    `json:"test_run_details_page_url,omitempty"`
}

type TestRunList struct {
	Value    []TestRun `json:"value"`
	Count    int       `json:"@count,omitempty"`
	NextLink string    `json:"@nextLink,omitempty"`
}

type TestRunListParams struct {
	Top           int
	Skip          int
	CreatedAfter  string
	CreatedBefore string
}

type TestRunDistribution struct {
	Distribution map[string]TestRunDistributionEntry `json:"distribution"`
}

type TestRunDistributionEntry struct {
	Percentage FlexibleNumber `json:"percentage"`
	Nodes      []TestRunNode  `json:"nodes"`
}

type TestRunNode struct {
	Size     string `json:"size"`
	PublicIP string `json:"public_ip"`
}

type ProjectLimits struct {
	ProjectID              int  `json:"project_id"`
	VUHMaxPerMonth         *int `json:"vuh_max_per_month"`
	VUMaxPerTest           *int `json:"vu_max_per_test"`
	VUBrowserMaxPerTest    *int `json:"vu_browser_max_per_test"`
	DurationMaxPerTestSecs *int `json:"duration_max_per_test"`
}

type ProjectLimitsList struct {
	Value    []ProjectLimits `json:"value"`
	Count    int             `json:"@count,omitempty"`
	NextLink string          `json:"@nextLink,omitempty"`
}

// ProjectLimitsPatch preserves omitted fields and explicit null values.
type ProjectLimitsPatch map[string]*int

type ValidateOptionsRequest struct {
	ProjectID        *int              `json:"project_id,omitempty"`
	Options          map[string]any    `json:"options"`
	K6Dependencies   map[string]string `json:"k6_dependencies,omitempty"`
	K6Version        *int              `json:"k6_version,omitempty"`
	IsLocalExecution bool              `json:"is_local_execution"`
}

type ValidateOptionsResult struct {
	VUHUsage  FlexibleNumber       `json:"vuh_usage"`
	Breakdown TestRunCostBreakdown `json:"breakdown"`
}

type ScriptDownload struct {
	ContentType string `json:"content_type"`
	Data        []byte `json:"-"`
}
