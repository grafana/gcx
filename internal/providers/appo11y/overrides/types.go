package overrides

// MetricsGeneratorConfig represents the App Observability metrics generator configuration.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type MetricsGeneratorConfig struct {
	etag             string
	CostAttribution  map[string]any    `json:"cost_attribution,omitempty"`
	MetricsGenerator *MetricsGenerator `json:"metrics_generator,omitempty"`
}

// ETag returns the ETag value captured from the HTTP response header.
func (o MetricsGeneratorConfig) ETag() string { return o.etag }

// SetETag stores the ETag value from the HTTP response header.
func (o *MetricsGeneratorConfig) SetETag(etag string) { o.etag = etag }

// GetResourceName returns the fixed singleton name.
func (o MetricsGeneratorConfig) GetResourceName() string { return "default" }

// SetResourceName is a no-op because this is a singleton resource.
func (o *MetricsGeneratorConfig) SetResourceName(_ string) {}

// MetricsGenerator represents the metrics generator settings.
type MetricsGenerator struct {
	DisableCollection  bool       `json:"disable_collection"`
	CollectionInterval string     `json:"collection_interval,omitempty"`
	Processor          *Processor `json:"processor,omitempty"`
}

// Processor represents the processor configuration.
type Processor struct {
	ServiceGraphs *ServiceGraphs `json:"service_graphs,omitempty"`
	SpanMetrics   *SpanMetrics   `json:"span_metrics,omitempty"`
}

// ServiceGraphs represents service graphs processor configuration.
type ServiceGraphs struct {
	EnableClientServerPrefix bool      `json:"enable_client_server_prefix,omitempty"`
	Dimensions               []string  `json:"dimensions,omitempty"`
	PeerAttributes           []string  `json:"peer_attributes,omitempty"`
	HistogramBuckets         []float64 `json:"histogram_buckets,omitempty"`
}

// SpanMetrics represents span metrics processor configuration.
type SpanMetrics struct {
	EnableTargetInfo bool      `json:"enable_target_info,omitempty"`
	Dimensions       []string  `json:"dimensions,omitempty"`
	HistogramBuckets []float64 `json:"histogram_buckets,omitempty"`
}
