package synth

// Named-query discovery is a third, distinct way to talk to the Synthetic
// Monitoring datasource, alongside the check/probe proxy (proxy_client.go) and
// query execution (backend_datasource_client.go). It answers "which named
// queries does this tenant's instance serve, and with what parameters" so a
// caller of BackendDatasourceClient.Query does not have to already know.
//
// The catalog is Grafana's generic plugin schema-publishing mechanism, not an
// SM-specific API: a datasource plugin that ships a schemabuilder-generated
// query.types.json exposes it as a static, unauthenticated asset at
// /public/plugins/<plugin-id>/schema/v0alpha1/query.types.json. No datasource
// UID, no SM token, and no datasources:query permission are involved.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/Masterminds/semver/v3"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"k8s.io/client-go/rest"
)

// smAppPluginID is the SM app's plugin id, used only to look up the installed
// version when the catalog itself 404s.
const smAppPluginID = "grafana-synthetic-monitoring-app"

// minDiscoveryAppVersion is the first SM app release whose datasource ships a
// query.types.json (app PR #1843, landed 2026-09-11, releasing in v1.62.0).
const minDiscoveryAppVersion = "1.62.0"

func queryTypesSchemaPath() string {
	return fmt.Sprintf("/public/plugins/%s/schema/v0alpha1/query.types.json", DatasourceType)
}

func smAppSettingsPath() string {
	return fmt.Sprintf("/api/plugins/%s/settings", smAppPluginID)
}

// QueryType describes one named query the SM datasource's backend knows how to
// run, as published in its query.types.json.
type QueryType struct {
	// Name is the queryType value NamedQuery.Name must match to run this query.
	Name string
	// Description is the entry's own description, written by whoever added it
	// to the registry -- see namedqueries.go / schema_test.go in
	// synthetic-monitoring-app.
	Description string
	// Schema is the entry's full draft-04 JSON Schema for its parameters, kept
	// raw so `queries get` can show it in full without this package needing to
	// understand every constraint shape.
	Schema json.RawMessage
	// Required lists the parameter names the schema marks required. Extracted
	// for `queries list`'s summary column; nil for a query with no required
	// parameters (e.g. a TenantWideQuery entry).
	Required []string
}

// CatalogResult is the outcome of a successful catalog fetch.
type CatalogResult struct {
	QueryTypes []QueryType
}

// queryTypeDefinitionListKind is the only Kind parseCatalog accepts. Without
// checking it, any 200 response that happens to decode into this shape (e.g.
// an empty JSON object) is silently read as a catalog with zero query types,
// indistinguishable from a tenant that genuinely has none.
const queryTypeDefinitionListKind = "QueryTypeDefinitionList"

// queryTypeDefinitionList mirrors the QueryTypeDefinitionList Kubernetes-style
// envelope Grafana's schemabuilder writes. Only the fields this package reads
// are declared.
type queryTypeDefinitionList struct {
	Kind  string                `json:"kind"`
	Items []queryTypeDefinition `json:"items"`
}

type queryTypeDefinition struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Description string          `json:"description"`
		Schema      json.RawMessage `json:"schema"`
	} `json:"spec"`
}

// CatalogClient fetches the SM datasource's published named-query catalog.
type CatalogClient struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// NewCatalogClient creates a catalog client using the caller's Grafana
// credential from the REST config. The catalog endpoint itself needs no
// credential -- it is an unauthenticated static asset -- but cfg.Host already
// carries the right base URL for both direct and OAuth-proxy contexts, so
// reusing it here avoids a second way to resolve the target server.
func NewCatalogClient(cfg config.NamespacedRESTConfig) (*CatalogClient, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	return &CatalogClient{restConfig: cfg, httpClient: httpClient}, nil
}

// Catalog fetches and parses the SM datasource's named-query catalog.
//
// A 404 means the deployed SM app predates the catalog file, not that the
// tenant has no named queries -- so this looks up the installed app version
// (or its absence) to turn that into an actionable message rather than a bare
// HTTP status.
func (c *CatalogClient) Catalog(ctx context.Context) (*CatalogResult, error) {
	body, status, err := c.get(ctx, queryTypesSchemaPath())
	if err != nil {
		return nil, err
	}

	switch status {
	case http.StatusOK:
		return parseCatalog(body)
	case http.StatusNotFound:
		return nil, c.unavailableError(ctx)
	default:
		return nil, fmt.Errorf("named-query catalog: unexpected HTTP %d", status)
	}
}

// FetchSMAppSettings performs the shared, unauthenticated GET against the SM
// app's plugin settings endpoint, returning the raw body and status so each
// caller can decode whatever subtree it needs (this package reads
// info.version; others read jsonData.* subtrees).
//
// This is meant to be the one place in the codebase that builds this
// request. discoverSMURL (internal/providers/synth/provider.go) and
// smPluginDatasourceName (internal/providers/synth/checks/status.go) predate
// it and each hand-roll their own version of this fetch, disagreeing on
// details (trailing-slash trimming, OAuth-proxy host selection) -- they are
// candidates to adopt this as a follow-up, not duplicated further.
func FetchSMAppSettings(ctx context.Context, cfg config.NamespacedRESTConfig) ([]byte, int, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.Host+smAppSettingsPath(), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := httputils.ReadResponseBody(resp.Body, maxResponseBytes)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read response: %w", err)
	}

	return body, resp.StatusCode, nil
}

// unavailableError explains a query.types.json 404 by re-querying the SM
// app's settings endpoint. Only an actual 404 there means "not installed" --
// any other failure (403, 500, decode error) is returned as-is instead of
// being misreported as a missing app, and the reported version is compared
// against minDiscoveryAppVersion so the message doesn't blame a version that
// already satisfies it.
func (c *CatalogClient) unavailableError(ctx context.Context) error {
	body, status, err := FetchSMAppSettings(ctx, c.restConfig)
	switch {
	case err != nil:
		return fmt.Errorf("named-query catalog: %w", err)
	case status == http.StatusNotFound:
		return errors.New("no Synthetic Monitoring app installed in this context; named-query discovery requires one")
	case status != http.StatusOK:
		return fmt.Errorf("named-query catalog: SM app settings returned HTTP %d", status)
	}

	var settings struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := json.Unmarshal(body, &settings); err != nil {
		return fmt.Errorf("named-query catalog: failed to decode SM app settings: %w", err)
	}
	if settings.Info.Version == "" {
		return errors.New("named-query catalog: SM app settings did not report an installed version")
	}

	installed, err := semver.NewVersion(settings.Info.Version)
	if err != nil {
		return fmt.Errorf("SM app reports invalid version %q: %w", settings.Info.Version, err)
	}

	if installed.LessThan(semver.MustParse(minDiscoveryAppVersion)) {
		return fmt.Errorf(
			"named-query discovery requires Synthetic Monitoring app v%s or later; this context has v%s installed",
			minDiscoveryAppVersion, settings.Info.Version,
		)
	}

	return fmt.Errorf(
		"named-query catalog not found even though Synthetic Monitoring app v%s is installed; the datasource may not have published query.types.json",
		settings.Info.Version,
	)
}

// get performs an unauthenticated-endpoint GET against the Grafana host,
// enforcing the same response-size cap as the SM proxy transport
// (maxResponseBytes, proxy_client.go) via the shared httputils reader.
func (c *CatalogClient) get(ctx context.Context, path string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+path, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := httputils.ReadResponseBody(resp.Body, maxResponseBytes)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read response: %w", err)
	}

	return body, resp.StatusCode, nil
}

// parseCatalog decodes the QueryTypeDefinitionList envelope and reduces each
// item to a QueryType, extracting Required from its schema for the summary
// view.
func parseCatalog(body []byte) (*CatalogResult, error) {
	var list queryTypeDefinitionList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("failed to parse named-query catalog: %w", err)
	}
	if list.Kind != queryTypeDefinitionListKind {
		return nil, fmt.Errorf(
			"named-query catalog: expected kind %q, got %q -- this does not look like a query.types.json catalog",
			queryTypeDefinitionListKind, list.Kind,
		)
	}

	queryTypes := make([]QueryType, 0, len(list.Items))
	for _, item := range list.Items {
		var schemaMeta struct {
			Required []string `json:"required"`
		}
		// Best-effort: a schema this package cannot parse for "required" still
		// has a name, description, and raw schema worth surfacing.
		_ = json.Unmarshal(item.Spec.Schema, &schemaMeta)

		queryTypes = append(queryTypes, QueryType{
			Name:        item.Metadata.Name,
			Description: item.Spec.Description,
			Schema:      item.Spec.Schema,
			Required:    schemaMeta.Required,
		})
	}

	return &CatalogResult{
		QueryTypes: queryTypes,
	}, nil
}
