package ml

import "time"

// Job defines a forecast job.
//
//nolint:recvcheck // TypedCRUD needs a value getter and a pointer setter.
type Job struct {
	ID string `json:"id,omitempty"`
	// Name is a human readable name for the job.
	Name string `json:"name"`
	// Metric is the metric name used to query the job. Must match Prometheus
	// naming requirements:
	// https://prometheus.io/docs/concepts/data_model/#metric-names-and-labels.
	Metric      string `json:"metric"`
	Description string `json:"description"`

	// GrafanaURL is the full URL to the Grafana instance. For example,
	// https://myinstance.grafana.net/.
	GrafanaURL string `json:"grafanaUrl"`
	// DatasourceID is the numeric ID of the datasource to query when training
	// data.
	DatasourceID uint `json:"datasourceId"`
	// DatasourceUID is the string UID of the datasource to query when training
	// data.
	DatasourceUID  string         `json:"datasourceUid"`
	DatasourceType string         `json:"datasourceType"`
	CustomLabels   map[string]any `json:"customLabels"`
	QueryParams    map[string]any `json:"queryParams"`
	// Interval is the data resolution in seconds.
	Interval uint `json:"interval"`
	// TrainingWindow is the lookback window to train on in seconds.
	TrainingWindow uint `json:"trainingWindow"`
	// TrainingFrequency is how often to re-train a model in seconds.
	TrainingFrequency uint `json:"trainingFrequency"`

	// Algorithm is the algorithm to use for machine learning.
	// https://grafana.com/docs/grafana-cloud/machine-learning/models/ contains
	// information on all supported algorithms.
	Algorithm string `json:"algorithm"`
	// HyperParams are the hyperparameters that can be specified. See
	// https://grafana.com/docs/grafana-cloud/machine-learning/models/ for the
	// various hyperparameters that can be changed.
	HyperParams map[string]any `json:"hyperParams"`

	// Holidays is a slice of IDs or names of Holidays to be linked to this job.
	// Requests may specify either IDs or names. Responses will always contain IDs.
	Holidays []string `json:"holidays"`

	// ManagedBy is used to identify who controls system forecasts. It is
	// required when creating system forecasts and must not be set otherwise.
	ManagedBy string `json:"managedBy,omitempty"`
}

//nolint:recvcheck // TypedCRUD needs a value getter and a pointer setter.
type Holiday struct {
	ID string `json:"id,omitempty"`
	// Name is a human readable name for the holiday.
	Name string `json:"name"`
	// Description is a human readable description for the holiday.
	Description string `json:"description"`

	// ICalURL is the URL to an iCal file containing all occurrences
	// of the holiday.
	ICalURL *string `json:"iCalUrl,omitempty"`
	// ICalTimeZone is the timezone to use for 'All Day' events on the iCal in ICalURL,
	// if present. This is required because All Day events don't come with time zone information by default,
	// and there is no well-adhered-to standard for timezones of entire iCal files.
	ICalTimeZone *string `json:"iCalTimeZone,omitempty"`

	// CustomPeriods are holiday periods that are specified explicitly.
	CustomPeriods CustomPeriods `json:"customPeriods,omitempty"`

	// Jobs is a slice of IDs of Jobs that are using this holiday.
	// Requests may specify either IDs or names. Responses will always contain IDs.
	Jobs []string `json:"jobs"`
}

// CustomPeriods is a slice of CustomPeriods representing all occurrences
// of a holiday.
type CustomPeriods []CustomPeriod

// CustomPeriod is a single period to be included in a holiday.
type CustomPeriod struct {
	// Name is the name of this period.
	Name string `json:"name"`
	// StartTime is the (inclusive) start of this period.
	StartTime time.Time `json:"startTime"`
	// EndTime is the (exclusive) end of this period.
	EndTime time.Time `json:"endTime"`
}

type OutlierAlgorithmConfig struct {
	Epsilon float64 `json:"epsilon"`
}

type OutlierAlgorithm struct {
	Name        string                  `json:"name"`
	Sensitivity float64                 `json:"sensitivity"` // used by MAD
	Config      *OutlierAlgorithmConfig `json:"config"`      // used by DBSCAN
}

// OutlierDetector defines an outlier detector instance.
//
//nolint:recvcheck // TypedCRUD needs a value getter and a pointer setter.
type OutlierDetector struct {
	ID string `json:"id,omitempty"`
	// Name is a human readable name for the outlier detector.
	Name string `json:"name"`
	// Metric is the metric name used to query the outlier detector. Must match Prometheus
	// naming requirements:
	// https://prometheus.io/docs/concepts/data_model/#metric-names-and-labels.
	Metric      string `json:"metric"`
	Description string `json:"description"`

	// GrafanaURL is the full URL to the Grafana instance. For example,
	// https://myinstance.grafana.net/.
	GrafanaURL string `json:"grafanaUrl"`
	// DatasourceID is the numeric ID of the datasource to query when training
	// data.
	DatasourceID uint `json:"datasourceId"`
	// DatasourceUID is the string UID of the datasource to query when training
	// data.
	DatasourceUID  string         `json:"datasourceUid"`
	DatasourceType string         `json:"datasourceType"`
	QueryParams    map[string]any `json:"queryParams"`
	// Interval is the data resolution in seconds.
	Interval uint `json:"interval"`

	// Algorithm specifies the algorithm to use and its configuration. See
	// https://grafana.com/docs/grafana-cloud/machine-learning/outlier-detection/ for the
	// options.
	Algorithm OutlierAlgorithm `json:"algorithm"`
}

// GetResourceName returns the server ID.
func (r Job) GetResourceName() string { return r.ID }

// SetResourceName sets the server ID.
func (r *Job) SetResourceName(name string) { r.ID = name }

// GetResourceName returns the server ID.
func (r Holiday) GetResourceName() string { return r.ID }

// SetResourceName sets the server ID.
func (r *Holiday) SetResourceName(name string) { r.ID = name }

// GetResourceName returns the server ID.
func (r OutlierDetector) GetResourceName() string { return r.ID }

// SetResourceName sets the server ID.
func (r *OutlierDetector) SetResourceName(name string) { r.ID = name }

// Alert is an alert rule attached to a forecast job or an outlier detector.
//
//nolint:recvcheck // Resource identity needs a value getter and a pointer setter.
type Alert struct {
	ID                   string            `json:"id,omitempty"`
	Title                string            `json:"title"`
	AnomalyCondition     string            `json:"anomalyCondition,omitempty"`
	For                  string            `json:"for"`
	Threshold            string            `json:"threshold,omitempty"`
	Window               string            `json:"window"`
	Labels               map[string]string `json:"labels"`
	Annotations          map[string]string `json:"annotations"`
	NoDataCondition      string            `json:"noDataCondition,omitempty"`
	CustomQuery          string            `json:"customQuery,omitempty"`
	NotificationSettings map[string]any    `json:"notificationSettings,omitempty"`
}

// GetResourceName returns the server ID.
func (r Alert) GetResourceName() string { return r.ID }

// SetResourceName sets the server ID.
func (r *Alert) SetResourceName(name string) { r.ID = name }

type displayResource interface {
	GetResourceName() string
	displayName() string
}

func (r Job) displayName() string             { return r.Name }
func (r Holiday) displayName() string         { return r.Name }
func (r OutlierDetector) displayName() string { return r.Name }
