package alert

import (
	"encoding/json"
	"fmt"
)

// JSONModel wraps json.RawMessage to support YAML string round-tripping.
// When loading from YAML, the model is a JSON string. When serializing to
// JSON it emits raw bytes. Used for contact-point settings.
type JSONModel json.RawMessage

// MarshalJSON outputs the raw JSON bytes.
func (m JSONModel) MarshalJSON() ([]byte, error) {
	if len(m) == 0 {
		return []byte("null"), nil
	}
	return m, nil
}

// UnmarshalJSON stores the raw bytes.
func (m *JSONModel) UnmarshalJSON(data []byte) error {
	*m = append((*m)[:0], data...)
	return nil
}

// ContactPoint represents a Grafana alerting contact point (notifier).
type ContactPoint struct {
	UID                   string    `json:"uid,omitempty"`
	Name                  string    `json:"name"`
	Type                  string    `json:"type"`
	Settings              JSONModel `json:"settings"`
	DisableResolveMessage bool      `json:"disableResolveMessage,omitempty"`
	Provenance            string    `json:"provenance,omitempty"`
}

// GetResourceName returns the contact point UID.
func (cp ContactPoint) GetResourceName() string { return cp.UID }

// MuteTiming represents a named set of time intervals during which
// notifications are suppressed.
type MuteTiming struct {
	Name          string         `json:"name"`
	TimeIntervals []TimeInterval `json:"time_intervals"`
}

// GetResourceName returns the mute timing name.
func (m MuteTiming) GetResourceName() string { return m.Name }

// TimeInterval is a single entry inside a MuteTiming.
type TimeInterval struct {
	Times       []TimeRangeHHMM `json:"times,omitempty"`
	Weekdays    []string        `json:"weekdays,omitempty"`
	DaysOfMonth []string        `json:"days_of_month,omitempty"`
	Months      []string        `json:"months,omitempty"`
	Years       []string        `json:"years,omitempty"`
	Location    string          `json:"location,omitempty"`
}

// TimeRangeHHMM represents a start/end clock time in HH:MM.
type TimeRangeHHMM struct {
	Start string `json:"start_time"`
	End   string `json:"end_time"`
}

// Matcher selects alerts for a notification route.
// Wire format is a 3-element array: ["label", "operator", "value"].
type Matcher struct {
	Label string
	Match string // "=", "!=", "=~", "!~"
	Value string
}

// MarshalJSON emits the API-expected 3-element array form.
func (m Matcher) MarshalJSON() ([]byte, error) {
	return json.Marshal([]string{m.Label, m.Match, m.Value})
}

// UnmarshalJSON decodes the 3-element array form.
func (m *Matcher) UnmarshalJSON(data []byte) error {
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	if len(arr) != 3 {
		return fmt.Errorf("expected 3-element array for matcher, got %d", len(arr))
	}
	m.Label = arr[0]
	m.Match = arr[1]
	m.Value = arr[2]
	return nil
}

// NotificationPolicy is the root of the notification policy tree.
type NotificationPolicy struct {
	Receiver          string              `json:"receiver"`
	GroupBy           []string            `json:"group_by,omitempty"`
	GroupWait         string              `json:"group_wait,omitempty"`
	GroupInterval     string              `json:"group_interval,omitempty"`
	RepeatInterval    string              `json:"repeat_interval,omitempty"`
	Routes            []NotificationRoute `json:"routes,omitempty"`
	MuteTimeIntervals []string            `json:"mute_time_intervals,omitempty"`
	Provenance        string              `json:"provenance,omitempty"`
}

// NotificationRoute is a nested route in the notification policy tree.
type NotificationRoute struct {
	Receiver          string              `json:"receiver,omitempty"`
	GroupBy           []string            `json:"group_by,omitempty"`
	GroupWait         string              `json:"group_wait,omitempty"`
	GroupInterval     string              `json:"group_interval,omitempty"`
	RepeatInterval    string              `json:"repeat_interval,omitempty"`
	Matchers          []Matcher           `json:"object_matchers,omitempty"`
	Continue          bool                `json:"continue,omitempty"`
	Routes            []NotificationRoute `json:"routes,omitempty"`
	MuteTimeIntervals []string            `json:"mute_time_intervals,omitempty"`
}

// NotificationTemplate is a named Go template used to render alert messages.
type NotificationTemplate struct {
	Name       string `json:"name"`
	Template   string `json:"template"`
	Provenance string `json:"provenance,omitempty"`
}

// GetResourceName returns the template name.
func (t NotificationTemplate) GetResourceName() string { return t.Name }
