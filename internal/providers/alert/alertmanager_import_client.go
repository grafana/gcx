package alert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"
)

// alertmanagerImportPath is Grafana's Mimir-compatible Alertmanager convert
// API. It stages an imported Alertmanager configuration next to the Grafana
// configuration, and it promotes it into the Grafana configuration.
const alertmanagerImportPath = "/api/convert/api/v1/alerts"

// The datasource proxy path exposes the raw Mimir/Cortex Alertmanager user
// configuration. Error responses are read with a bounded size and displayed
// with the same 1 KiB limit as Grafana's external Alertmanager sync error.
const (
	alertmanagerDatasourceProxyPath        = "/api/datasources/proxy/uid/"
	alertmanagerDatasourceConfigPath       = "/api/v1/alerts"
	alertmanagerDatasourceMaxErrorBodySize = 1 << 20
	alertmanagerDatasourceErrorDisplaySize = 1 << 10
)

// Request headers of the Alertmanager convert API.
const (
	configIdentifierHeader   = "X-Grafana-Alerting-Config-Identifier"
	dryRunHeader             = "X-Grafana-Alerting-Dry-Run"
	promoteHeader            = "X-Grafana-Alerting-Promote"
	configForceReplaceHeader = "X-Grafana-Alerting-Config-Force-Replace"
)

// defaultAlertmanagerImportIdentifier is the identifier Grafana uses when a
// request does not set one.
const defaultAlertmanagerImportIdentifier = "imported"

// alertmanagerImportToggle is the Grafana feature toggle that enables the
// Alertmanager convert API. Without it, every endpoint returns 501.
const alertmanagerImportToggle = "alertingImportAlertmanagerAPI"

// Server messages that identify specific 409 responses.
const (
	externalSyncConfiguredMessage = "external alertmanager sync is configured"
	multipleImportsMessage        = "multiple extra configurations are not supported"
	// otherImportMarker precedes the identifier of the staged import in the
	// multiple-imports message.
	otherImportMarker = "with identifier: "
)

// ErrAlertmanagerImportNotFound is returned when no imported Alertmanager
// configuration has the requested identifier.
var ErrAlertmanagerImportNotFound = errors.New("imported alertmanager configuration not found")

// errAlertmanagerImportAPINotFound is returned on a 404 from an endpoint that
// never reports a missing import: the Grafana instance has no convert API.
var errAlertmanagerImportAPINotFound = errors.New("alertmanager import API not found")

// AlertmanagerUserConfig is a Mimir/Cortex Alertmanager user configuration:
// the Alertmanager configuration YAML as a string and its template files.
type AlertmanagerUserConfig struct {
	AlertmanagerConfig string            `json:"alertmanager_config" yaml:"alertmanager_config"`
	TemplateFiles      map[string]string `json:"template_files" yaml:"template_files"`
}

// AlertmanagerImportOptions configures an import request.
type AlertmanagerImportOptions struct {
	// Identifier names the import and the routing tree that it adds.
	Identifier string
	// DryRun validates the configuration without saving it.
	DryRun bool
	// Promote merges the configuration into the Grafana configuration.
	Promote bool
	// ForceReplace replaces an import that has another identifier.
	ForceReplace bool
}

// AlertmanagerMergeStats lists the resources that an import added. Renamed
// resources are listed with their new names.
type AlertmanagerMergeStats struct {
	AddedRoute           string   `json:"added_route" yaml:"added_route"`
	AddedReceivers       []string `json:"added_receivers" yaml:"added_receivers"`
	AddedTimeIntervals   []string `json:"added_time_intervals" yaml:"added_time_intervals"`
	AddedTemplates       []string `json:"added_templates" yaml:"added_templates"`
	AddedInhibitionRules []string `json:"added_inhibition_rules" yaml:"added_inhibition_rules"`
}

// AlertmanagerRenames maps the original name of each renamed resource to the
// name that Grafana gave it to avoid a conflict.
type AlertmanagerRenames struct {
	Receivers     map[string]string `json:"receivers" yaml:"receivers"`
	TimeIntervals map[string]string `json:"time_intervals" yaml:"time_intervals"`
}

// AlertmanagerMergeResult is the response of an import or a promotion.
type AlertmanagerMergeResult struct {
	RenameResources *AlertmanagerRenames    `json:"rename_resources,omitempty" yaml:"rename_resources,omitempty"`
	Stats           *AlertmanagerMergeStats `json:"stats,omitempty" yaml:"stats,omitempty"`
}

// AlertmanagerConfigFromDatasource returns the raw configuration stored by the
// Mimir or Cortex Alertmanager behind a Grafana datasource and its datasource
// name. It does not parse or re-serialize the Alertmanager configuration.
func (c *Client) AlertmanagerConfigFromDatasource(ctx context.Context, uid string) (*AlertmanagerUserConfig, string, error) {
	datasource, err := datasources.NewClientWithHTTP(c.host, c.httpClient).GetByUID(ctx, uid)
	if err != nil {
		if apiErr, ok := errors.AsType[*datasources.APIError](err); ok && apiErr.StatusCode == http.StatusNotFound {
			return nil, "", fmt.Errorf("datasource %s not found", uid)
		}
		return nil, "", err
	}
	if datasource.Type != "alertmanager" {
		return nil, "", fmt.Errorf("datasource must be of type alertmanager, got %s", datasource.Type)
	}
	implementation, _ := datasource.JSONData["implementation"].(string)
	implementation = strings.ToLower(implementation)
	if implementation == "" {
		implementation = "mimir"
	}
	if implementation != "mimir" && implementation != "cortex" {
		return nil, "", fmt.Errorf("datasource implementation %s is not supported; only Mimir and Cortex Alertmanagers expose their configuration", implementation)
	}

	path := alertmanagerDatasourceProxyPath + url.PathEscape(uid) + alertmanagerDatasourceConfigPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+path, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create datasource proxy request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to execute datasource proxy request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", alertmanagerDatasourceProxyError(resp, uid)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read Alertmanager datasource configuration: %w", err)
	}
	var userConfig AlertmanagerUserConfig
	if err := yaml.Unmarshal(body, &userConfig); err != nil {
		return nil, "", fmt.Errorf("failed to decode Alertmanager datasource configuration: %w", err)
	}
	if userConfig.AlertmanagerConfig == "" {
		return nil, "", fmt.Errorf("the Alertmanager behind datasource %s has no stored configuration", uid)
	}
	return &userConfig, datasource.Name, nil
}

func alertmanagerDatasourceProxyError(resp *http.Response, uid string) error {
	if resp.StatusCode < http.StatusBadRequest {
		return fmt.Errorf("datasource proxy returned %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, alertmanagerDatasourceMaxErrorBodySize))
	if err != nil {
		return fmt.Errorf("datasource proxy returned %s and its response body could not be read: %w", resp.Status, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		var proxyError struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &proxyError) == nil && proxyError.Message != "" {
			message := truncateAlertmanagerDatasourceError([]byte(proxyError.Message))
			return fmt.Errorf("datasource proxy returned 404: %s", message)
		}
		return fmt.Errorf("the Alertmanager behind datasource %s has no stored configuration", uid)
	}
	return fmt.Errorf("datasource proxy: %w", providers.FormatError(resp.StatusCode, truncateAlertmanagerDatasourceError(body)))
}

func truncateAlertmanagerDatasourceError(body []byte) []byte {
	if len(body) <= alertmanagerDatasourceErrorDisplaySize {
		return body
	}
	body = body[:alertmanagerDatasourceErrorDisplaySize]
	for !utf8.Valid(body) {
		body = body[:len(body)-1]
	}
	return body
}

// ImportAlertmanagerConfig stages, validates (DryRun), or promotes (Promote)
// an Alertmanager configuration.
func (c *Client) ImportAlertmanagerConfig(ctx context.Context, cfg AlertmanagerUserConfig, opts AlertmanagerImportOptions) (*AlertmanagerMergeResult, error) {
	headers := http.Header{configIdentifierHeader: {opts.Identifier}}
	// Grafana reads an absent boolean header as false, so send only the
	// options that are set.
	if opts.DryRun {
		headers.Set(dryRunHeader, "true")
	}
	if opts.Promote {
		headers.Set(promoteHeader, "true")
	}
	if opts.ForceReplace {
		headers.Set(configForceReplaceHeader, "true")
	}

	var out AlertmanagerMergeResult
	err := doBody(ctx, c.httpClient, http.MethodPost, c.host+alertmanagerImportPath, alertmanagerImportPath,
		jsonBodyCodec(), errAlertmanagerImportAPINotFound, cfg, &out, headers)
	if err != nil {
		return nil, alertmanagerImportError(err)
	}
	return &out, nil
}

// GetImportedAlertmanagerConfig returns the staged Alertmanager configuration
// with the given identifier. Grafana masks its secrets.
func (c *Client) GetImportedAlertmanagerConfig(ctx context.Context, identifier string) (*AlertmanagerUserConfig, error) {
	headers := http.Header{configIdentifierHeader: {identifier}}

	var out AlertmanagerUserConfig
	err := doBody(ctx, c.httpClient, http.MethodGet, c.host+alertmanagerImportPath, alertmanagerImportPath,
		jsonBodyCodec(), ErrAlertmanagerImportNotFound, nil, &out, headers)
	if err != nil {
		return nil, alertmanagerImportNotFoundError(alertmanagerImportError(err), identifier)
	}
	return &out, nil
}

// DeleteImportedAlertmanagerConfig deletes the staged Alertmanager
// configuration with the given identifier. The Grafana configuration does not
// change. Grafana deletes idempotently: a missing identifier is not an error.
func (c *Client) DeleteImportedAlertmanagerConfig(ctx context.Context, identifier string) error {
	headers := http.Header{configIdentifierHeader: {identifier}}
	err := doBody(ctx, c.httpClient, http.MethodDelete, c.host+alertmanagerImportPath, alertmanagerImportPath,
		jsonBodyCodec(), errAlertmanagerImportAPINotFound, nil, nil, headers)
	return alertmanagerImportError(err)
}

// PromoteImportedAlertmanagerConfig merges the staged Alertmanager
// configuration with the given identifier into the Grafana configuration.
// The merge cannot be undone.
func (c *Client) PromoteImportedAlertmanagerConfig(ctx context.Context, identifier string) (*AlertmanagerMergeResult, error) {
	path := alertmanagerImportPath + "/" + url.PathEscape(identifier) + "/promote"

	var out AlertmanagerMergeResult
	err := doBody(ctx, c.httpClient, http.MethodPost, c.host+path, path,
		jsonBodyCodec(), ErrAlertmanagerImportNotFound, nil, &out, nil)
	if err != nil {
		return nil, alertmanagerImportNotFoundError(alertmanagerImportError(err), identifier)
	}
	return &out, nil
}

// alertmanagerImportError adds the cause and the remedy to the convert API
// failures whose cause is known. Other errors are returned unchanged.
func alertmanagerImportError(err error) error {
	if errors.Is(err, errAlertmanagerImportAPINotFound) {
		return &gcxerrors.DetailedError{
			Summary: gcxerrors.SummaryEndpointNotAvailable,
			Details: "The Alertmanager import API is not available on this Grafana instance (HTTP 404). " +
				"The Grafana version may predate the API.",
			Parent: err,
			Suggestions: []string{
				"Upgrade Grafana to a version that has the Alertmanager import API, " +
					"and enable the " + alertmanagerImportToggle + " feature toggle",
			},
		}
	}

	statusErr, ok := errors.AsType[*gcxerrors.HTTPStatusError](err)
	if !ok {
		return err
	}

	message := strings.ToLower(statusErr.ServerMessage)
	switch {
	case statusErr.Status == http.StatusNotImplemented:
		return &gcxerrors.DetailedError{
			Summary: gcxerrors.SummaryEndpointNotAvailable,
			Details: fmt.Sprintf("Alertmanager import API is not enabled on this Grafana instance (feature toggle %s)",
				alertmanagerImportToggle),
			Parent: err,
			Suggestions: []string{
				"Enable the " + alertmanagerImportToggle + " feature toggle on the Grafana instance",
				"In Grafana Cloud, contact Support to enable the feature toggle",
			},
		}
	case statusErr.Status == http.StatusConflict && strings.Contains(message, externalSyncConfiguredMessage):
		return &gcxerrors.DetailedError{
			Summary: gcxerrors.SummaryResourceConflict,
			Details: "External Alertmanager sync is configured for this organization. " +
				"Disable the sync before you import an Alertmanager configuration manually.",
			Parent: err,
			Suggestions: []string{
				"Disable the sync in Grafana: Alerting > Settings > Import > Disable sync",
				"If the operator manages the sync, ask your Grafana administrator to disable it (in Grafana Cloud, contact Support)",
			},
		}
	case statusErr.Status == http.StatusConflict && strings.Contains(message, multipleImportsMessage):
		return &gcxerrors.DetailedError{
			Summary:     gcxerrors.SummaryResourceConflict,
			Details:     "Grafana keeps one imported Alertmanager configuration per organization: " + statusErr.ServerMessage,
			Parent:      err,
			Suggestions: multipleImportsSuggestions(statusErr.ServerMessage),
		}
	}
	return err
}

// multipleImportsSuggestions names the commands that resolve a conflict with
// the staged import. The server message ends with the identifier of that
// import; if gcx cannot find it, the user reads it from the message.
func multipleImportsSuggestions(serverMessage string) []string {
	const replace = "Replace the staged import: add --force-replace"
	_, other, found := strings.Cut(serverMessage, otherImportMarker)
	other = strings.TrimSpace(other)
	if !found || len(validation.IsDNS1123Subdomain(other)) != 0 {
		return []string{
			"Find the identifier of the staged import in the server message, then delete it: " +
				"gcx alert alertmanager-imports delete <identifier>",
			replace,
		}
	}
	return []string{
		"Delete the staged import: gcx alert alertmanager-imports delete " + other,
		"Promote the staged import: gcx alert alertmanager-imports promote " + other,
		replace,
	}
}

// alertmanagerImportNotFoundError names the identifier when no imported
// configuration has it. A Grafana instance without the convert API also
// answers 404, and gcx cannot tell the two causes apart.
func alertmanagerImportNotFoundError(err error, identifier string) error {
	if !errors.Is(err, ErrAlertmanagerImportNotFound) {
		return err
	}
	return &gcxerrors.DetailedError{
		Summary: gcxerrors.SummaryResourceNotFound,
		Details: fmt.Sprintf("No imported Alertmanager configuration has the identifier %q.", identifier),
		Parent:  err,
		Suggestions: []string{
			"Import a configuration: gcx alert alertmanager-imports upsert " + identifier + " -f <file>",
			"If the identifier is correct, the Grafana version may predate the Alertmanager import API",
		},
	}
}
