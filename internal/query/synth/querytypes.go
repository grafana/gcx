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
	"io"
	"net/http"

	"github.com/Masterminds/semver/v3"
	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

// smDatasourcePluginID is the plugin id of the SM datasource, matching
// DatasourceType in backend_datasource_client.go, but spelled out again here:
// this package deliberately has no dependency between its files beyond the
// shared HTTP conventions.
const smDatasourcePluginID = "synthetic-monitoring-datasource"

// smAppPluginID is the SM app's plugin id, used only to look up the installed
// version when the catalog itself 404s.
const smAppPluginID = "grafana-synthetic-monitoring-app"

// minDiscoveryAppVersion is the first SM app release whose datasource ships a
// query.types.json (app PR #1843, landed 2026-09-11, releasing in v1.62.0).
const minDiscoveryAppVersion = "1.62.0"

// ExpectedQueryTypesAPIVersion is the apiVersion this package was built
// against. CatalogResult.APIVersion is still populated when the served file
// uses a different one -- the file is additive, so this package parses it
// regardless. Exported so a caller (e.g. the `queries` command) can compare
// and warn on a mismatch rather than failing the fetch.
const ExpectedQueryTypesAPIVersion = "datasource.grafana.app/v0alpha1"

func queryTypesSchemaPath() string {
	return fmt.Sprintf("/public/plugins/%s/schema/v0alpha1/query.types.json", smDatasourcePluginID)
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
	// APIVersion is the apiVersion the server reported. Compare against
	// expectedQueryTypesAPIVersion if you need to decide whether to warn --
	// this package parses the file regardless, since it is additive.
	APIVersion string
	QueryTypes []QueryType
}

// queryTypeDefinitionList mirrors the QueryTypeDefinitionList Kubernetes-style
// envelope Grafana's schemabuilder writes. Only the fields this package reads
// are declared.
type queryTypeDefinitionList struct {
	Kind       string                `json:"kind"`
	APIVersion string                `json:"apiVersion"`
	Items      []queryTypeDefinition `json:"items"`
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

// unavailableError explains a query.types.json 404 by re-querying the SM
// app's settings endpoint. Only an actual 404 there means "not installed" --
// any other failure (403, 500, decode error) is returned as-is instead of
// being misreported as a missing app, and the reported version is compared
// against minDiscoveryAppVersion so the message doesn't blame a version that
// already satisfies it.
func (c *CatalogClient) unavailableError(ctx context.Context) error {
	body, status, err := c.get(ctx, smAppSettingsPath())
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
// (maxResponseBytes, proxy_client.go) so a misbehaving server cannot exhaust
// memory.
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read response: %w", err)
	}
	if int64(len(body)) > maxResponseBytes {
		return nil, 0, fmt.Errorf("named-query catalog response exceeds %d MB limit", int64(maxResponseBytes)>>20)
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
		APIVersion: list.APIVersion,
		QueryTypes: queryTypes,
	}, nil
}
