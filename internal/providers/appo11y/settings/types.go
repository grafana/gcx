package settings

// PluginSettings represents the App Observability plugin settings.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type PluginSettings struct {
	JSONData PluginJSONData `json:"jsonData"`
}

// GetResourceName returns the fixed singleton name.
func (s PluginSettings) GetResourceName() string { return "default" }

// SetResourceName is a no-op because this is a singleton resource.
func (s *PluginSettings) SetResourceName(_ string) {}

// PluginJSONData represents the plugin JSON data.
type PluginJSONData struct {
	DefaultLogQueryMode       string `json:"defaultLogQueryMode,omitempty"`
	LogsQueryWithNamespace    string `json:"logsQueryWithNamespace,omitempty"`
	LogsQueryWithoutNamespace string `json:"logsQueryWithoutNamespace,omitempty"`
	MetricsMode               string `json:"metricsMode,omitempty"`
}
