// Package datasources bridges Grafana datasources into the unified resources
// pipeline so they can be managed with `gcx resources get/pull/push/delete`.
//
// Datasources are not exposed through Grafana's Kubernetes-compatible /apis
// surface today, so this provider backs a resource descriptor
// (datasource.grafana.app/v0alpha1, DataSource) with the legacy /api/datasources
// REST API via a custom ResourceAdapter (see adapter.go). The manifest shape is
// the shared DataSourceManifest — identical to what the dedicated
// `gcx datasources` commands emit: per-plugin apiVersion, a top-level `secure`
// block, and a `spec` keyed by `title`.
package datasources

import (
	"context"
	"encoding/json"
	"fmt"

	internalconfig "github.com/grafana/gcx/internal/config"
	dsclient "github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func init() { //nolint:gochecknoinits // Natural-key + GVK-normalizer registration for the resources pipeline.
	canonical := StaticDescriptor().GroupVersionKind()

	// Cross-stack push identity: the display name lives at spec.title (canonical
	// app-platform DataSourceSpec field).
	adapter.RegisterNaturalKey(canonical, adapter.SpecFieldKey("title"))

	// Collapse Grafana's per-plugin datasource groups
	// ({pluginID}.datasource.grafana.app/v0alpha1) onto the single canonical
	// descriptor so manifests carrying a per-plugin apiVersion route to the
	// type-agnostic legacy REST adapter. The datasource type is read from
	// spec.type, so dropping the per-plugin group loses no information.
	resources.RegisterGVKNormalizer(func(gvk schema.GroupVersionKind) (schema.GroupVersionKind, bool) {
		if gvk.Kind != canonical.Kind || gvk.Version != canonical.Version {
			return schema.GroupVersionKind{}, false
		}
		if _, ok := dsclient.IsDatasourceGroup(gvk.Group); !ok {
			return schema.GroupVersionKind{}, false
		}
		return canonical, true
	})
}

// StaticDescriptor returns the canonical resource descriptor for datasources.
// It is the single GVK the resources discovery registry routes on; the rendered
// manifests carry a per-plugin apiVersion (collapsed back by the normalizer).
func StaticDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{
			Group:   "datasource.grafana.app",
			Version: "v0alpha1",
		},
		Kind:     "DataSource",
		Singular: "datasource",
		Plural:   "datasources",
	}
}

// apiVersionPattern matches both the canonical group and any per-plugin
// datasource group, e.g. prometheus.datasource.grafana.app/v0alpha1.
const apiVersionPattern = `^([a-z0-9][a-z0-9-]*\.)?datasource\.grafana\.app/v0alpha1$`

// DatasourceSchema returns the JSON Schema envelope for the converged datasource
// manifest: a spec reflected from DataSourceSpec plus a top-level `secure` block,
// with apiVersion accepting per-plugin and canonical groups.
func DatasourceSchema() json.RawMessage {
	// Reflect the spec shape (title-keyed, no secrets) via the shared helper,
	// then adapt the envelope: relax apiVersion and add the `secure` sibling.
	base := adapter.SchemaFromType[dsclient.DataSourceSpec](StaticDescriptor())

	var env map[string]any
	if err := json.Unmarshal(base, &env); err != nil {
		panic(fmt.Sprintf("providers/datasources: unmarshal base schema: %v", err))
	}
	props, ok := env["properties"].(map[string]any)
	if !ok {
		panic("providers/datasources: base schema has no properties")
	}

	props["apiVersion"] = map[string]any{
		"type":        "string",
		"pattern":     apiVersionPattern,
		"description": "{pluginID}.datasource.grafana.app/v0alpha1 (the canonical group is also accepted)",
	}
	props["secure"] = map[string]any{
		"type":        "object",
		"description": "Write-only secrets keyed by name; reads return only the configured names.",
		"additionalProperties": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"create":      map[string]any{"type": "string", "description": "Inline secret value (write-only)."},
				"description": map[string]any{"type": "string", "description": "Optional description stored with the secret."},
				"fromEnv":     map[string]any{"type": "string", "description": "Read the value from this environment variable (resolved by gcx)."},
				"fromFile":    map[string]any{"type": "string", "description": "Read the value from this file (resolved by gcx)."},
				"name":        map[string]any{"type": "string", "description": "Stored secret reference (read-back)."},
				"remove":      map[string]any{"type": "boolean", "description": "Delete the stored secret."},
			},
		},
	}

	b, err := json.Marshal(env)
	if err != nil {
		panic(fmt.Sprintf("providers/datasources: marshal schema: %v", err))
	}
	return b
}

// DatasourceExample returns an example datasource manifest as JSON, in the
// converged shape (per-plugin apiVersion, top-level secure, spec.title).
func DatasourceExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": "prometheus.datasource.grafana.app/v0alpha1",
		"kind":       "DataSource",
		"metadata": map[string]any{
			// metadata.name is the datasource UID — stable and user-chosen.
			"name": "my-prometheus",
		},
		// secure is a top-level sibling of spec (canonical InlineSecureValue
		// shape); values are write-only and never returned on reads.
		"secure": map[string]any{
			"basicAuthPassword": map[string]any{"create": "REDACTED"},
		},
		"spec": map[string]any{
			"type":      "prometheus",
			"title":     "My Prometheus",
			"access":    "proxy",
			"url":       "https://prometheus.example.com",
			"basicAuth": true,
			"jsonData": map[string]any{
				"httpMethod": "POST",
			},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("providers/datasources: failed to marshal example: %v", err))
	}
	return b
}

// newAdapter builds the custom datasource ResourceAdapter from a REST config.
// It uses the dual-mode transport so resource-pipeline writes prefer the
// app-platform API and fall back to legacy REST, mirroring the `gcx datasources`
// commands.
func newAdapter(cfg internalconfig.NamespacedRESTConfig) (*datasourceAdapter, error) {
	client, err := dsclient.NewTransport(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create datasource client: %w", err)
	}
	return &datasourceAdapter{
		client:    client,
		namespace: cfg.Namespace,
		desc:      StaticDescriptor(),
		schema:    DatasourceSchema(),
		example:   DatasourceExample(),
	}, nil
}

// NewLazyFactory returns an adapter.Factory that loads its config lazily from
// the default config file when invoked.
func NewLazyFactory() adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		var loader providers.ConfigLoader
		cfg, err := loader.LoadGrafanaConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load REST config for datasources: %w", err)
		}
		return newAdapter(cfg)
	}
}
