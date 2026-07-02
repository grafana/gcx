package datasources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"k8s.io/client-go/rest"
)

const (
	maxResponseBytes = 10 << 20 // 10 MB

	datasourcesPath       = "/api/datasources"
	datasourceByUIDPath   = "/api/datasources/uid/"
	datasourceByNamePath  = "/api/datasources/name/"
	datasourcePluginsPath = "/api/plugins?type=datasource"
)

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

// Client talks to the legacy /api/datasources REST API via the
// NamespacedRESTConfig transport, so OAuth proxy mode and token refresh are
// respected. It implements Transport.
type Client struct {
	host       string
	httpClient *http.Client
}

// NewClient creates a client backed by the given REST config's transport.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &Client{host: cfg.Host, httpClient: httpClient}, nil
}

// List returns all datasources visible to the authenticated user.
func (c *Client) List(ctx context.Context) ([]*Datasource, error) {
	body, err := c.do(ctx, http.MethodGet, datasourcesPath, nil, "", "list datasources")
	if err != nil {
		return nil, err
	}
	var out []*Datasource
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("failed to parse datasources response: %w", err)
	}
	return out, nil
}

// ListPluginTypes returns the datasource plugin types installed on the Grafana
// instance — the valid values for a manifest's spec.type and for
// `datasources schemas get --type`. Results are sorted by plugin id.
func (c *Client) ListPluginTypes(ctx context.Context) ([]PluginType, error) {
	body, err := c.do(ctx, http.MethodGet, datasourcePluginsPath, nil, "", "list datasource plugin types")
	if err != nil {
		return nil, err
	}
	var out []PluginType
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("failed to parse plugins response: %w", err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// GetByUID returns the datasource with the given UID.
func (c *Client) GetByUID(ctx context.Context, uid string) (*Datasource, error) {
	return c.getOne(ctx, datasourceByUIDPath+url.PathEscape(uid), uid)
}

// GetByName returns the datasource with the given display name.
func (c *Client) GetByName(ctx context.Context, name string) (*Datasource, error) {
	return c.getOne(ctx, datasourceByNamePath+url.PathEscape(name), name)
}

// Create creates a new datasource and returns it as echoed back by the API.
func (c *Client) Create(ctx context.Context, ds *Datasource) (*Datasource, error) {
	return c.write(ctx, http.MethodPost, datasourcesPath, ds.UID, "create datasource", ds)
}

// Update replaces the datasource identified by uid (last-write-wins).
func (c *Client) Update(ctx context.Context, uid string, ds *Datasource) (*Datasource, error) {
	return c.write(ctx, http.MethodPut, datasourceByUIDPath+url.PathEscape(uid), uid, "update datasource", ds)
}

// Delete removes the datasource identified by uid.
func (c *Client) Delete(ctx context.Context, uid string) error {
	_, err := c.do(ctx, http.MethodDelete, datasourceByUIDPath+url.PathEscape(uid), nil, uid, "delete datasource")
	return err
}

// Health checks the datasource identified by uid.
func (c *Client) Health(ctx context.Context, uid string) (*HealthResult, error) {
	body, err := c.do(ctx, http.MethodGet, datasourceByUIDPath+url.PathEscape(uid)+"/health", nil, uid, "check datasource health")
	if err != nil {
		return nil, err
	}
	var payload struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse health response: %w", err)
	}
	return &HealthResult{UID: uid, Status: payload.Status, Message: payload.Message}, nil
}

func (c *Client) getOne(ctx context.Context, path, identifier string) (*Datasource, error) {
	body, err := c.do(ctx, http.MethodGet, path, nil, identifier, "get datasource")
	if err != nil {
		return nil, err
	}
	var ds Datasource
	if err := json.Unmarshal(body, &ds); err != nil {
		return nil, fmt.Errorf("failed to parse datasource response: %w", err)
	}
	return &ds, nil
}

// mutationResponse is the envelope returned by create/update, e.g.
// {"datasource": {...}, "id": 1, "message": "Datasource added"}.
type mutationResponse struct {
	Datasource *Datasource `json:"datasource"`
}

func (c *Client) write(ctx context.Context, method, path, identifier, op string, ds *Datasource) (*Datasource, error) {
	body, err := c.do(ctx, method, path, ds, identifier, op)
	if err != nil {
		return nil, err
	}
	var wrapped mutationResponse
	if err := json.Unmarshal(body, &wrapped); err != nil || wrapped.Datasource == nil {
		// Some responses echo the datasource directly rather than wrapped.
		var direct Datasource
		if jerr := json.Unmarshal(body, &direct); jerr != nil {
			return nil, fmt.Errorf("failed to parse %s response: %w", op, jerr)
		}
		return &direct, nil
	}
	return wrapped.Datasource, nil
}

// do issues an HTTP request to the datasource REST API. When payload is non-nil
// it is JSON-encoded as the request body. It returns a typed APIError on any
// non-2xx response.
func (c *Client) do(ctx context.Context, method, path string, payload any, identifier, op string) ([]byte, error) {
	var reader io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to encode request: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.host+path, reader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to %s: %w", op, err)
	}
	defer resp.Body.Close()

	body, err := httputils.ReadResponseBody(resp.Body, maxResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, NewAPIError(op, identifier, resp.StatusCode, body)
	}
	return body, nil
}
