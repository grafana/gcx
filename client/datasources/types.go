package datasources

// Datasource is the datasource domain type exchanged with the legacy
// /api/datasources REST API. The same struct is used for reads (list/get) and
// writes (create/update), so write-only and optional fields carry omitempty.
//
// secureJsonData is write-only: the API never returns it on reads, so it is
// absent from read results; secureJsonFields reports which secrets are set.
//
//nolint:recvcheck // Mixed receivers are intentional for the ResourceIdentity contract.
type Datasource struct {
	UID              string            `json:"uid,omitempty"`
	Name             string            `json:"name"`
	Type             string            `json:"type"`
	URL              string            `json:"url,omitempty"`
	Access           string            `json:"access,omitempty"`
	Database         string            `json:"database,omitempty"`
	User             string            `json:"user,omitempty"`
	BasicAuth        bool              `json:"basicAuth,omitempty"`
	BasicAuthUser    string            `json:"basicAuthUser,omitempty"`
	WithCredentials  bool              `json:"withCredentials,omitempty"`
	IsDefault        bool              `json:"isDefault,omitempty"`
	ReadOnly         bool              `json:"readOnly,omitempty"`
	JSONData         map[string]any    `json:"jsonData,omitempty"`
	SecureJSONData   map[string]string `json:"secureJsonData,omitempty"`
	SecureJSONFields map[string]bool   `json:"secureJsonFields,omitempty"`
}

// GetResourceName returns the datasource UID — its stable resource identity.
func (d Datasource) GetResourceName() string { return d.UID }

// SetResourceName restores the UID after a round-trip.
func (d *Datasource) SetResourceName(name string) { d.UID = name }

// HealthResult is the outcome of a datasource health check.
type HealthResult struct {
	UID     string `json:"uid"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// PluginType is an installed datasource plugin type, as returned by the Grafana
// plugins listing. ID is the value used as spec.type in a manifest and as
// --type for `datasources schemas get`.
type PluginType struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

// mutationResponse is the envelope returned by create/update, e.g.
// {"datasource": {...}, "id": 1, "message": "Datasource added"}.
type mutationResponse struct {
	Datasource *Datasource `json:"datasource"`
}
