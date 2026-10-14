package dbo11y

// The API's own field names and nesting are confined to the api wire types
// in this file; everything the commands print or encode uses the output
// types below, so a change in the API's shape does not reach the output
// contract.

// apiInstance is the wire shape of GET /instances and GET
// /instances/{server_id}. It exists only to decode the response; everything
// downstream works with the Instance output type.
type apiInstance struct {
	ServerID      string        `json:"server_id"`
	Instance      string        `json:"instance"`
	Engine        string        `json:"engine"`
	EngineVersion string        `json:"engine_version"`
	CloudProvider cloudProvider `json:"cloud_provider"`
	// Metrics is populated on the detail route only; the list route never
	// includes it.
	Metrics *Metrics `json:"metrics,omitempty"`
}

// cloudProvider is the wire shape of the nested provider object: exactly one
// of AWS, Azure, GCP is populated for a cloud-hosted instance, all nil for a
// self-hosted or unrecognised one. Instance flattens it into four
// provider_* fields.
type cloudProvider struct {
	AWS   *awsProvider   `json:"aws,omitempty"`
	Azure *azureProvider `json:"azure,omitempty"`
	GCP   *gcpProvider   `json:"gcp,omitempty"`
}

type awsProvider struct {
	AccountID    string `json:"account_id"`
	DBInstanceID string `json:"db_instance_id"`
	Region       string `json:"region"`
}

type azureProvider struct {
	DBInstanceID   string `json:"db_instance_id"`
	ResourceGroup  string `json:"resource_group"`
	SubscriptionID string `json:"subscription_id"`
}

type gcpProvider struct {
	DBInstanceID string `json:"db_instance_id"`
	ProjectID    string `json:"project_id"`
	Region       string `json:"region"`
}

// name returns which of AWS/Azure/GCP is populated, or "" if none.
func (c cloudProvider) name() string {
	switch {
	case c.AWS != nil:
		return "aws"
	case c.Azure != nil:
		return "azure"
	case c.GCP != nil:
		return "gcp"
	default:
		return ""
	}
}

// region returns the populated provider's region, or its resource group for
// Azure, which has no region field.
func (c cloudProvider) region() string {
	switch {
	case c.AWS != nil:
		return c.AWS.Region
	case c.Azure != nil:
		return c.Azure.ResourceGroup
	case c.GCP != nil:
		return c.GCP.Region
	default:
		return ""
	}
}

// dbInstanceID returns the populated provider's own instance identifier, or "".
func (c cloudProvider) dbInstanceID() string {
	switch {
	case c.AWS != nil:
		return c.AWS.DBInstanceID
	case c.Azure != nil:
		return c.Azure.DBInstanceID
	case c.GCP != nil:
		return c.GCP.DBInstanceID
	default:
		return ""
	}
}

// account returns the populated provider's account, subscription or project
// identifier, or "".
func (c cloudProvider) account() string {
	switch {
	case c.AWS != nil:
		return c.AWS.AccountID
	case c.Azure != nil:
		return c.Azure.SubscriptionID
	case c.GCP != nil:
		return c.GCP.ProjectID
	default:
		return ""
	}
}

// Metrics is the RED + CPU rollup for one instance, over a fixed 5-minute
// window the backend owns.
//
// Every field is omitted rather than zeroed when its underlying series is
// absent, so these are pointers: an omitted field means "not reported", never
// "zero". QPS/AvgLatencyMs/ErrorRate are populated for mysql and postgres
// only; CPUPercent only when the instance has an AWS db_instance_id to key
// CloudWatch on. `metrics` itself is absent when neither applies.
type Metrics struct {
	QPS          *float64 `json:"qps,omitempty" yaml:"qps,omitempty"`
	AvgLatencyMs *float64 `json:"avg_latency_ms,omitempty" yaml:"avg_latency_ms,omitempty"`
	ErrorRate    *float64 `json:"error_rate,omitempty" yaml:"error_rate,omitempty"`
	CPUPercent   *float64 `json:"cpu_percent,omitempty" yaml:"cpu_percent,omitempty"`
}

// Instance is a single row of the database inventory.
//
// Name and Host both carry the `instance` label, the only human-readable
// identifier the API returns: depending on how the collector is configured
// that is either a host:port endpoint or a logical name. ServerID is what
// `instances get` takes.
type Instance struct {
	Name               string   `json:"name" yaml:"name"`
	Engine             string   `json:"engine,omitempty" yaml:"engine,omitempty"`
	EngineVersion      string   `json:"engine_version,omitempty" yaml:"engine_version,omitempty"`
	Host               string   `json:"host,omitempty" yaml:"host,omitempty"`
	InstanceIdentifier string   `json:"instance_identifier,omitempty" yaml:"instance_identifier,omitempty"`
	ProviderName       string   `json:"provider_name,omitempty" yaml:"provider_name,omitempty"`
	ProviderAccount    string   `json:"provider_account,omitempty" yaml:"provider_account,omitempty"`
	ProviderRegion     string   `json:"provider_region,omitempty" yaml:"provider_region,omitempty"`
	ServerID           string   `json:"server_id,omitempty" yaml:"server_id,omitempty"`
	Metrics            *Metrics `json:"metrics,omitempty" yaml:"metrics,omitempty"`
}

// toInstance converts the API's wire shape into the output contract.
func (a apiInstance) toInstance() Instance {
	return Instance{
		Name:               a.Instance,
		Engine:             a.Engine,
		EngineVersion:      a.EngineVersion,
		Host:               a.Instance,
		InstanceIdentifier: a.CloudProvider.dbInstanceID(),
		ProviderName:       a.CloudProvider.name(),
		ProviderAccount:    a.CloudProvider.account(),
		ProviderRegion:     a.CloudProvider.region(),
		ServerID:           a.ServerID,
		Metrics:            a.Metrics,
	}
}

// InstancesResponse is the top-level shape returned by `instances list`. The
// envelope is what `-o json` and `-o yaml` emit, so consumers index into
// `.items`.
type InstancesResponse struct {
	Items []Instance `json:"items" yaml:"items"`
}

// apiQuery is the wire shape of one row of GET
// /instances/{server_id}/queries.
type apiQuery struct {
	// Exactly one of Digest (MySQL) or QueryID (Postgres) identifies the
	// statement.
	Digest       string   `json:"digest,omitempty"`
	QueryID      string   `json:"query_id,omitempty"`
	Database     string   `json:"database,omitempty"`
	QueryText    string   `json:"query_text,omitempty"`
	Calls        *float64 `json:"calls,omitempty"`
	QPS          *float64 `json:"qps,omitempty"`
	AvgLatencyMs *float64 `json:"avg_latency_ms,omitempty"`
	DBLoad       float64  `json:"db_load"`
	ErrorRate    *float64 `json:"error_rate,omitempty"`
	RowsExamined *float64 `json:"rows_examined,omitempty"`
}

// identifier returns whichever of Digest or QueryID the API populated —
// exactly one is, depending on engine (digest for MySQL, query_id for
// Postgres). Both are reported under the single query_id output key.
func (a apiQuery) identifier() string {
	if a.Digest != "" {
		return a.Digest
	}
	return a.QueryID
}

// TopQuery is one row of the top-queries view.
type TopQuery struct {
	QueryID            string   `json:"query_id" yaml:"query_id"`
	Datname            string   `json:"datname" yaml:"datname"`
	CallsPerSecond     float64  `json:"calls_per_second" yaml:"calls_per_second"`
	TimePerSecond      float64  `json:"time_per_second" yaml:"time_per_second"`
	MeanLatencySeconds float64  `json:"mean_latency_seconds" yaml:"mean_latency_seconds"`
	HasMeanLatency     bool     `json:"has_mean_latency" yaml:"has_mean_latency"`
	RowsPerCall        float64  `json:"rows_per_call" yaml:"rows_per_call"`
	HasRowsPerCall     bool     `json:"has_rows_per_call" yaml:"has_rows_per_call"`
	QueryText          string   `json:"query_text,omitempty" yaml:"query_text,omitempty"`
	RowsExamined       *float64 `json:"rows_examined,omitempty" yaml:"rows_examined,omitempty"`
	ErrorRate          *float64 `json:"error_rate,omitempty" yaml:"error_rate,omitempty"`
}

// toTopQuery converts the API's wire shape into the output contract,
// deriving mean latency in seconds from the API's milliseconds and rows per
// call from its rows examined over the window.
func (a apiQuery) toTopQuery() TopQuery {
	q := TopQuery{
		QueryID:       a.identifier(),
		Datname:       a.Database,
		TimePerSecond: a.DBLoad,
		QueryText:     a.QueryText,
		RowsExamined:  a.RowsExamined,
		ErrorRate:     a.ErrorRate,
	}
	if a.QPS != nil {
		q.CallsPerSecond = *a.QPS
	}
	if a.AvgLatencyMs != nil {
		q.MeanLatencySeconds = *a.AvgLatencyMs / 1000
		q.HasMeanLatency = true
	}
	if a.Calls != nil && *a.Calls > 0 && a.RowsExamined != nil {
		q.RowsPerCall = *a.RowsExamined / *a.Calls
		q.HasRowsPerCall = true
	}
	return q
}

// InstanceDetail is the `instances get` response.
type InstanceDetail struct {
	Instance   Instance   `json:"instance" yaml:"instance"`
	Window     string     `json:"window" yaml:"window"`
	TopQueries []TopQuery `json:"top_queries,omitempty" yaml:"top_queries,omitempty"`
}
