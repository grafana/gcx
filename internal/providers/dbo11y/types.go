package dbo11y

// The API's own field names and nesting are confined to the api wire types
// in this file; everything the commands print or encode uses the output
// types below, so a change in the API's shape does not reach the output
// contract.

// apiInstance is the wire shape of GET /instances and GET
// /instances/{server_id}. It exists only to decode the response; everything
// downstream works with the Instance output type.
type apiInstance struct {
	ServerID      string            `json:"server_id"`
	Instance      string            `json:"instance"`
	Engine        string            `json:"engine"`
	EngineVersion string            `json:"engine_version"`
	CloudProvider cloudProvider     `json:"cloud_provider"`
	Labels        map[string]string `json:"labels"`
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
// Name is the collector's `service_name` where it sets one, and the monitored
// endpoint otherwise. Host is always the endpoint. ServerID is the product's
// own identifier, which `instances get` also accepts.
type Instance struct {
	Name string `json:"name" yaml:"name"`

	// Namespace and Environment are the two labels this command has always
	// reported as fields of their own. They are `service_namespace` and
	// `deployment_environment` in Labels, lifted out so the shape a caller
	// parsed before still holds.
	Namespace   string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Environment string `json:"environment,omitempty" yaml:"environment,omitempty"`

	Engine             string `json:"engine,omitempty" yaml:"engine,omitempty"`
	EngineVersion      string `json:"engine_version,omitempty" yaml:"engine_version,omitempty"`
	Host               string `json:"host,omitempty" yaml:"host,omitempty"`
	InstanceIdentifier string `json:"instance_identifier,omitempty" yaml:"instance_identifier,omitempty"`
	ProviderName       string `json:"provider_name,omitempty" yaml:"provider_name,omitempty"`
	ProviderAccount    string `json:"provider_account,omitempty" yaml:"provider_account,omitempty"`
	ProviderRegion     string `json:"provider_region,omitempty" yaml:"provider_region,omitempty"`
	ServerID           string `json:"server_id,omitempty" yaml:"server_id,omitempty"`

	// Labels carries the collector's own labels for the instance — `cluster`,
	// `deployment_environment`, `service_name` and whatever else an operator
	// set. Keyed by raw label name, which is also what `--filter` matches on,
	// so what you see here is what you can filter by.
	//
	// Absent for an instance with none, and for any stack whose plugin predates
	// the field.
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`

	Metrics *Metrics `json:"metrics,omitempty" yaml:"metrics,omitempty"`
}

// toInstance converts the API's wire shape into the output contract.
//
// Name prefers the collector's `service_name` label and falls back to the
// endpoint, which is the identity this command has always reported: a stack
// that sets a service name sees it here, and one that does not sees the
// endpoint. Host carries the endpoint either way, and `instances get` resolves
// an argument against both, so neither lookup depends on which one Name took.
func (a apiInstance) toInstance() Instance {
	name := a.Labels["service_name"]
	if name == "" {
		name = a.Instance
	}
	return Instance{
		Name:               name,
		Engine:             a.Engine,
		EngineVersion:      a.EngineVersion,
		Host:               a.Instance,
		InstanceIdentifier: a.CloudProvider.dbInstanceID(),
		ProviderName:       a.CloudProvider.name(),
		ProviderAccount:    a.CloudProvider.account(),
		ProviderRegion:     a.CloudProvider.region(),
		ServerID:           a.ServerID,
		Namespace:          a.Labels["service_namespace"],
		Environment:        a.Labels["deployment_environment"],
		Labels:             a.Labels,
		Metrics:            a.Metrics,
	}
}

// apiSessions is the wire shape of GET /instances/{server_id}/sessions.
type apiSessions struct {
	Engine                    string             `json:"engine"`
	Total                     *float64           `json:"total"`
	MaxConnections            *float64           `json:"max_connections"`
	ByState                   map[string]float64 `json:"by_state"`
	LongestTransactionSeconds *float64           `json:"longest_transaction_seconds"`
}

// Sessions is the instance's session state as `instances get` reports it: how
// many connections it holds, how that compares with the engine's own limit, and
// how long the oldest transaction has been open.
//
// Engine coverage is uneven and shows in which fields arrive. Postgres and
// MySQL both report a count and a limit; only Postgres breaks the count down by
// state or reports a transaction age. SQL Server reports none of it, and so does
// any stack whose plugin predates the route — in both cases `sessions` is
// absent rather than zeroed.
type Sessions struct {
	Total          *float64 `json:"total,omitempty" yaml:"total,omitempty"`
	MaxConnections *float64 `json:"max_connections,omitempty" yaml:"max_connections,omitempty"`

	// ByState names the engine's own session states verbatim, and carries only
	// the ones currently holding a session.
	ByState map[string]float64 `json:"by_state,omitempty" yaml:"by_state,omitempty"`

	// LongestTransactionSeconds is Postgres-only. A reported 0 means nothing is
	// open, which is the ordinary case.
	LongestTransactionSeconds *float64 `json:"longest_transaction_seconds,omitempty" yaml:"longest_transaction_seconds,omitempty"`
}

// toSessions converts the wire shape, dropping the engine: `instances get`
// already reports it on the instance itself.
//
// A response carrying nothing but identity — an engine that reports no sessions
// — becomes nil, so the field is omitted rather than serialized as an empty
// object.
func (a apiSessions) toSessions() *Sessions {
	if a.Total == nil && a.MaxConnections == nil && len(a.ByState) == 0 && a.LongestTransactionSeconds == nil {
		return nil
	}
	return &Sessions{
		Total:                     a.Total,
		MaxConnections:            a.MaxConnections,
		ByState:                   a.ByState,
		LongestTransactionSeconds: a.LongestTransactionSeconds,
	}
}

// apiWaitEvents is the wire shape of GET /instances/{server_id}/wait-events.
type apiWaitEvents struct {
	WaitEvents []apiWaitEvent `json:"wait_events"`
}

type apiWaitEvent struct {
	Type        string  `json:"type"`
	WaitSeconds float64 `json:"wait_seconds"`
}

// WaitEvent is one category of the instance-wide wait-time breakdown, ordered
// by the API with the dominant cause first.
//
// The categories are the collector's own — `IO Wait`, `Lock Wait`,
// `Network Wait`, `Engine Wait`, `Replication Wait`, `Other Wait` — not raw
// engine event names, and categories with no recorded wait are omitted.
type WaitEvent struct {
	Type        string  `json:"type" yaml:"type"`
	WaitSeconds float64 `json:"wait_seconds" yaml:"wait_seconds"`
}

// toWaitEvents converts the wire rows, preserving the API's ordering. The two
// row types are field-identical, so this is a conversion rather than a copy —
// they stay separate because the wire shape is the API's to change. Returns
// nil for an empty breakdown so the field is omitted rather than printed as an
// empty list — which an instance that simply waited for nothing shares with one
// whose collector predates the categorised stream.
func (a apiWaitEvents) toWaitEvents() []WaitEvent {
	if len(a.WaitEvents) == 0 {
		return nil
	}
	out := make([]WaitEvent, 0, len(a.WaitEvents))
	for _, e := range a.WaitEvents {
		out = append(out, WaitEvent(e))
	}
	return out
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
	Instance   Instance    `json:"instance" yaml:"instance"`
	Window     string      `json:"window" yaml:"window"`
	Health     Health      `json:"health" yaml:"health"`
	Sessions   *Sessions   `json:"sessions,omitempty" yaml:"sessions,omitempty"`
	WaitEvents []WaitEvent `json:"wait_events,omitempty" yaml:"wait_events,omitempty"`
	TopQueries []TopQuery  `json:"top_queries,omitempty" yaml:"top_queries,omitempty"`
}

// Health is whether the instance is reporting to Database Observability.
//
// It is the instance's presence in the inventory rather than a separate
// measurement, because presence already is one: the collector unregisters an
// instance after three consecutive failed database pings, so one it cannot
// reach stops being returned after about three minutes. Up is therefore true
// for every instance the API knows about and false for one it does not.
//
// What that cannot distinguish is an instance that has gone away from one that
// never existed — both are simply absent — so a false Up is reported alongside
// the not-found hint rather than on its own.
type Health struct {
	Up bool `json:"up" yaml:"up"`
}
