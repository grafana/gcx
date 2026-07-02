package datasources

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
)

const (
	// datasourceAPIGroupSuffix is appended to a plugin ID to form the
	// per-plugin Kubernetes API group for that datasource type. It is used to
	// derive the manifest apiVersion; the legacy REST transport ignores it.
	datasourceAPIGroupSuffix = ".datasource.grafana.app"
	datasourceAPIVersion     = "v0alpha1"
	datasourceKind           = "DataSource"
)

// DataSourceManifest is the Kubernetes-style envelope for a datasource
// instance. apiVersion encodes the plugin via the per-plugin group
// ({pluginID}.datasource.grafana.app/v0alpha1).
//
// The secure block is a top-level sibling of spec. On write, secret values are
// supplied via {create|fromEnv|fromFile}; on read, only the stored secret name
// is returned via {name: "..."}.
type DataSourceManifest struct {
	APIVersion string                 `json:"apiVersion" yaml:"apiVersion"`
	Kind       string                 `json:"kind" yaml:"kind"`
	Metadata   DataSourceMetadata     `json:"metadata" yaml:"metadata"`
	Secure     map[string]SecureValue `json:"secure,omitempty" yaml:"secure,omitempty"`
	Spec       DataSourceSpec         `json:"spec" yaml:"spec"`
}

// DataSourceMetadata carries the object metadata relevant to datasource CRUD.
// Name is the datasource UID.
type DataSourceMetadata struct {
	Name            string `json:"name,omitempty" yaml:"name,omitempty"`
	Namespace       string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	ResourceVersion string `json:"resourceVersion,omitempty" yaml:"resourceVersion,omitempty"`
}

// DataSourceSpec mirrors the visible configuration of a datasource instance.
// Secrets live in the top-level Secure block instead.
type DataSourceSpec struct {
	// Type is the plugin ID (e.g. "grafana-github-datasource"). It is optional
	// in a manifest: when omitted it is derived from the apiVersion group.
	Type string `json:"type,omitempty" yaml:"type,omitempty"`

	Title           string `json:"title,omitempty" yaml:"title,omitempty"`
	Access          string `json:"access,omitempty" yaml:"access,omitempty"`
	URL             string `json:"url,omitempty" yaml:"url,omitempty"`
	User            string `json:"user,omitempty" yaml:"user,omitempty"`
	Database        string `json:"database,omitempty" yaml:"database,omitempty"`
	BasicAuth       bool   `json:"basicAuth,omitempty" yaml:"basicAuth,omitempty"`
	BasicAuthUser   string `json:"basicAuthUser,omitempty" yaml:"basicAuthUser,omitempty"`
	WithCredentials bool   `json:"withCredentials,omitempty" yaml:"withCredentials,omitempty"`
	IsDefault       bool   `json:"isDefault,omitempty" yaml:"isDefault,omitempty"`
	ReadOnly        bool   `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`

	JSONData map[string]any `json:"jsonData,omitempty" yaml:"jsonData,omitempty"`
}

// SecureValue is one entry in the top-level secure block. It mirrors the
// app-platform InlineSecureValue ({create, name, remove, description}) and adds
// gcx-side indirection (fromEnv / fromFile) that is resolved into create before
// the object is sent.
//
// On write, set exactly one of Create / FromEnv / FromFile to supply the secret
// value. On read, only Name (the stored secret reference) is populated. Set
// Remove to delete a previously-stored secret. Description is optional metadata
// carried with a created secret (ignored by the legacy REST transport).
type SecureValue struct {
	Create      string `json:"create,omitempty" yaml:"create,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	FromEnv     string `json:"fromEnv,omitempty" yaml:"fromEnv,omitempty"`
	FromFile    string `json:"fromFile,omitempty" yaml:"fromFile,omitempty"`
	Name        string `json:"name,omitempty" yaml:"name,omitempty"`
	Remove      bool   `json:"remove,omitempty" yaml:"remove,omitempty"`
}

// GroupForPluginID returns the per-plugin API group for a datasource plugin.
func GroupForPluginID(pluginID string) string {
	return pluginID + datasourceAPIGroupSuffix
}

// IsDatasourceGroup reports whether an API group is a per-plugin datasource
// group of the form "{pluginID}.datasource.grafana.app", returning the plugin
// ID. The canonical base group "datasource.grafana.app" is not a per-plugin
// group and yields ("", false).
func IsDatasourceGroup(group string) (string, bool) {
	if !strings.HasSuffix(group, datasourceAPIGroupSuffix) {
		return "", false
	}
	return strings.TrimSuffix(group, datasourceAPIGroupSuffix), true
}

// pluginIDFromAPIVersion extracts the plugin ID from an apiVersion of the form
// "{pluginID}.datasource.grafana.app/v0alpha1". It returns "" when the
// apiVersion is not a datasource group.
func pluginIDFromAPIVersion(apiVersion string) string {
	group, _, _ := strings.Cut(apiVersion, "/")
	if !strings.HasSuffix(group, datasourceAPIGroupSuffix) {
		return ""
	}
	return strings.TrimSuffix(group, datasourceAPIGroupSuffix)
}

// PluginType returns the plugin id for the manifest, preferring spec.type and
// falling back to the apiVersion group.
func (m *DataSourceManifest) PluginType() string {
	if m.Spec.Type != "" {
		return m.Spec.Type
	}
	return pluginIDFromAPIVersion(m.APIVersion)
}

// ReadManifestFile reads a DataSourceManifest from a file path, or from stdin
// when path is "-". It accepts both YAML and JSON (YAML is a JSON superset).
// The plugin type must be resolvable from spec.type or the apiVersion group.
func ReadManifestFile(path string, stdin io.Reader) (*DataSourceManifest, error) {
	data, err := readBytes(path, stdin)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", manifestSource(path), err)
	}

	var manifest DataSourceManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", manifestSource(path), err)
	}

	derived := pluginIDFromAPIVersion(manifest.APIVersion)
	switch {
	case manifest.Spec.Type == "" && derived == "":
		return nil, fmt.Errorf("spec.type or a datasource apiVersion is required in %s", manifestSource(path))
	case manifest.Spec.Type != "" && derived != "" && manifest.Spec.Type != derived:
		return nil, fmt.Errorf("spec.type %q conflicts with apiVersion group %q in %s",
			manifest.Spec.Type, derived, manifestSource(path))
	}

	return &manifest, nil
}

func readBytes(path string, stdin io.Reader) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(stdin)
	}
	return os.ReadFile(path)
}

func manifestSource(path string) string {
	if path == "-" {
		return "stdin"
	}
	return path
}

// ToDatasource maps a manifest into the datasource wire type. Secrets must
// already be resolved (see ResolveSecrets); only SecureValue.Create is carried
// into secureJsonData.
func (m *DataSourceManifest) ToDatasource() *Datasource {
	name := m.Spec.Title
	if name == "" {
		name = m.Metadata.Name
	}
	access := m.Spec.Access
	if access == "" {
		access = "proxy"
	}

	ds := &Datasource{
		UID:             m.Metadata.Name,
		Name:            name,
		Type:            m.PluginType(),
		Access:          access,
		URL:             m.Spec.URL,
		User:            m.Spec.User,
		Database:        m.Spec.Database,
		BasicAuth:       m.Spec.BasicAuth,
		BasicAuthUser:   m.Spec.BasicAuthUser,
		WithCredentials: m.Spec.WithCredentials,
		IsDefault:       m.Spec.IsDefault,
		ReadOnly:        m.Spec.ReadOnly,
		JSONData:        m.Spec.JSONData,
	}

	secure := make(map[string]string)
	for k, sv := range m.Secure {
		if sv.Create != "" {
			secure[k] = sv.Create
		}
	}
	if len(secure) > 0 {
		ds.SecureJSONData = secure
	}
	return ds
}

// manifestFromDatasource maps a wire datasource into a manifest. Secret values
// are never present on read; configured secrets surface as {name} placeholders
// derived from secureJsonFields.
func manifestFromDatasource(ds *Datasource) *DataSourceManifest {
	m := &DataSourceManifest{
		APIVersion: GroupForPluginID(ds.Type) + "/" + datasourceAPIVersion,
		Kind:       datasourceKind,
		Metadata:   DataSourceMetadata{Name: ds.UID},
		Spec: DataSourceSpec{
			Type:            ds.Type,
			Title:           ds.Name,
			Access:          ds.Access,
			URL:             ds.URL,
			User:            ds.User,
			Database:        ds.Database,
			BasicAuth:       ds.BasicAuth,
			BasicAuthUser:   ds.BasicAuthUser,
			WithCredentials: ds.WithCredentials,
			IsDefault:       ds.IsDefault,
			ReadOnly:        ds.ReadOnly,
			JSONData:        ds.JSONData,
		},
	}
	if len(ds.SecureJSONFields) > 0 {
		m.Secure = make(map[string]SecureValue, len(ds.SecureJSONFields))
		for k, set := range ds.SecureJSONFields {
			if set {
				m.Secure[k] = SecureValue{Name: k}
			}
		}
	}
	return m
}

// ManifestFromDatasource is the exported mapping from a wire datasource to a
// manifest, used by commands to render apply-ready output.
func ManifestFromDatasource(ds *Datasource) *DataSourceManifest {
	return manifestFromDatasource(ds)
}

// Sanitize strips server-managed fields so the manifest is apply-ready
// (suitable for `get -o yaml | update -f -`).
func (m *DataSourceManifest) Sanitize() {
	m.Metadata.Namespace = ""
	m.Metadata.ResourceVersion = ""
}

func specToMap(spec DataSourceSpec) (map[string]any, error) {
	b, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("failed to encode spec: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("failed to decode spec: %w", err)
	}
	// type only routes to a plugin group; it is not a config field.
	delete(out, "type")
	return out, nil
}
