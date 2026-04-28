package fail

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/grafana"
	"github.com/grafana/gcx/internal/linter"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/grafana/gcx/internal/resources"
	k8sapi "k8s.io/apimachinery/pkg/api/errors"
)

const reauthSuggestion = "Re-authenticate if needed: gcx login"

func ErrorToDetailedError(err error) *DetailedError {
	var converted bool
	detailedErr := &DetailedError{}
	if errors.As(err, detailedErr) {
		return detailedErr
	}

	// Try to convert the error for common error categories
	errorConverters := []func(err error) (*DetailedError, bool){
		convertUsageErrors,
		convertCobraUnknownCommandErrors,
		convertContextCanceled,    // Context cancellation (must be first — cancellation can wrap other errors)
		convertRequiredFlagErrors, // Cobra required-flag errors — must appear before generic checks
		convertConfigErrors,       // Config-related
		convertAuthErrors,         // Auth-related (expired tokens)
		convertQueryErrors,        // Datasource query errors
		convertDatasourceErrors,   // Grafana datasource REST API errors
		convertServiceAPIErrors,   // Other structured HTTP API errors
		convertFSErrors,           // FS-related
		convertResourcesErrors,    // Resources-related
		convertNetworkErrors,      // Network-related errors
		convertAPIErrors,          // API-related errors
		convertVersionErrors,      // Version incompatibility errors
		convertLinterErrors,       // Linter-related errors
		convertSMConfigErrors,     // Synthetic Monitoring config errors
		convertCloudConfigErrors,  // Cloud config / fleet / setup errors
	}

	for _, converter := range errorConverters {
		detailedErr, converted = converter(err)
		if converted {
			return detailedErr
		}
	}

	return fallbackDetailedError(err)
}

func convertUsageErrors(err error) (*DetailedError, bool) {
	usageErr := &UsageError{}
	if !errors.As(err, &usageErr) {
		return nil, false
	}

	details := usageErr.Error()
	if usageErr.Expected != "" {
		details = fmt.Sprintf("%s\n\nExpected:\n  %s", details, usageErr.Expected)
	}

	return &DetailedError{
		Summary:     "Invalid command usage",
		Details:     details,
		Suggestions: usageErr.Suggestions,
	}, true
}

func convertCobraUnknownCommandErrors(err error) (*DetailedError, bool) {
	msg := strings.TrimSpace(err.Error())
	if !strings.HasPrefix(msg, "unknown command ") {
		return nil, false
	}

	detailed := &DetailedError{
		Summary: "Invalid command usage",
		Details: msg,
	}

	const marker = ` for "`
	idx := strings.LastIndex(msg, marker)
	if idx == -1 || !strings.HasSuffix(msg, `"`) {
		return detailed, true
	}

	commandPath := strings.TrimSpace(msg[idx+len(marker) : len(msg)-1])
	if commandPath == "" {
		return detailed, true
	}

	detailed.Suggestions = []string{
		fmt.Sprintf("Run '%s --help' for full usage and examples", commandPath),
	}
	return detailed, true
}

func convertConfigErrors(err error) (*DetailedError, bool) {
	validationErr := config.ValidationError{}
	if errors.As(err, &validationErr) {
		message := fmt.Sprintf("Invalid configuration found in '%s':\n%s", validationErr.File, validationErr.Message)
		if validationErr.AnnotatedSource != "" {
			message += "\n\n" + validationErr.AnnotatedSource
		}

		return &DetailedError{
			Summary: "Invalid configuration",
			Details: message,
			Suggestions: append([]string{
				"Review your configuration: gcx config view",
			}, validationErr.Suggestions...),
		}, true
	}

	unmarshalErr := config.UnmarshalError{}
	if errors.As(err, &unmarshalErr) {
		return &DetailedError{
			Summary: "Could not parse configuration",
			Details: fmt.Sprintf("Invalid configuration found in '%s'.", unmarshalErr.File),
			Parent:  unmarshalErr.Err,
			Suggestions: []string{
				"Fix the file with: gcx config edit",
				"Check for syntax errors such as incorrect indentation or unknown fields",
			},
		}, true
	}

	if errors.Is(err, config.ErrContextNotFound) {
		return &DetailedError{
			Summary: "Invalid configuration",
			Parent:  err,
			Suggestions: []string{
				"Check for typos in the context name",
				"Review your configuration: gcx config view",
			},
		}, true
	}

	return nil, false
}

func convertAuthErrors(err error) (*DetailedError, bool) {
	if errors.Is(err, auth.ErrRefreshTokenExpired) {
		return &DetailedError{
			Parent:  err,
			Summary: "Session expired",
			Suggestions: []string{
				"Run `gcx login` to re-authenticate",
			},
		}, true
	}
	return nil, false
}

func convertNetworkErrors(err error) (*DetailedError, bool) {
	urlErr := &url.Error{}
	if errors.As(err, &urlErr) {
		return &DetailedError{
			Parent:  err,
			Summary: "Network error",
			Suggestions: []string{
				"Make sure that the API is reachable",
				"Make sure that the configured target server is correct",
			},
		}, true
	}

	return nil, false
}

func convertAPIErrors(err error) (*DetailedError, bool) {
	statusErr := &k8sapi.StatusError{}
	if !errors.As(err, &statusErr) {
		return nil, false
	}

	reason := k8sapi.ReasonForError(statusErr)
	code := statusErr.Status().Code

	switch {
	case k8sapi.IsUnauthorized(statusErr),
		k8sapi.IsForbidden(statusErr):
		return &DetailedError{
			Parent:  err,
			Summary: fmt.Sprintf("%s - code %d", reason, code),
			Suggestions: []string{
				"Make sure that the configured credentials are correct",
				"Make sure that the configured credentials have enough permissions",
			},
			ExitCode: new(ExitAuthFailure),
		}, true
	case k8sapi.IsNotFound(statusErr):
		return &DetailedError{
			Parent:  err,
			Summary: fmt.Sprintf("Resource not found - code %d", code),
			Suggestions: []string{
				"Make sure that your are passing in valid resource selectors",
			},
		}, true
	}

	return &DetailedError{
		Parent:  err,
		Summary: fmt.Sprintf("API error: %s - code %d", reason, code),
	}, true
}

func convertQueryErrors(err error) (*DetailedError, bool) {
	apiErr := &queryerror.APIError{}
	if !errors.As(err, &apiErr) {
		return nil, false
	}

	detailedErr := &DetailedError{
		Summary:     queryErrorSummary(apiErr),
		Details:     joinErrorDetails(wrappedTypedErrorContext(err, apiErr), queryErrorDetails(apiErr)),
		Suggestions: queryErrorSuggestions(apiErr),
	}
	if sameRenderedMessage(detailedErr.Details, detailedErr.Summary) {
		detailedErr.Details = ""
	}

	if apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden {
		detailedErr.ExitCode = new(ExitAuthFailure)
	}

	return detailedErr, true
}

func queryErrorSummary(apiErr *queryerror.APIError) string {
	datasource := queryErrorDatasourceName(apiErr.Datasource)

	switch apiErr.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "Authentication failed querying " + datasource
	case http.StatusBadRequest:
		if language := queryErrorLanguage(apiErr); apiErr.IsParseError() && language != "" {
			return fmt.Sprintf("Invalid %s query", language)
		}
		if apiErr.Operation != "" {
			return fmt.Sprintf("Invalid %s %s", datasource, apiErr.Operation)
		}
		return fmt.Sprintf("Invalid %s request", datasource)
	case http.StatusNotFound:
		return queryErrorNotFoundSummary(apiErr)
	default:
		if apiErr.Operation != "" {
			return fmt.Sprintf("%s %s failed (HTTP %d)", datasource, apiErr.Operation, apiErr.StatusCode)
		}
		return fmt.Sprintf("%s request failed (HTTP %d)", datasource, apiErr.StatusCode)
	}
}

func queryErrorNotFoundSummary(apiErr *queryerror.APIError) string {
	if apiErr.Datasource == "tempo" && apiErr.Operation == "get trace" {
		return "Trace not found"
	}

	return queryErrorDatasourceName(apiErr.Datasource) + " resource not found"
}

func queryErrorDetails(apiErr *queryerror.APIError) string {
	details := apiErr.Message
	if details == "" {
		details = fmt.Sprintf("%s returned HTTP %d", queryErrorDatasourceName(apiErr.Datasource), apiErr.StatusCode)
	}

	if apiErr.ErrorSource != "" && apiErr.ErrorSource != "downstream" {
		details = fmt.Sprintf("%s\n\nSource: %s", details, apiErr.ErrorSource)
	}

	return details
}

func queryErrorSuggestions(apiErr *queryerror.APIError) []string {
	if apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden {
		return []string{
			"Review your Grafana credentials: gcx config view",
			reauthSuggestion,
		}
	}

	suggestions := []string{}
	if apiErr.IsParseError() && strings.Contains(strings.ToLower(apiErr.Message), "expecting string") {
		if example := queryErrorStringLiteralExample(apiErr); example != "" {
			suggestions = append(suggestions, example)
		}
	}

	if help := queryErrorHelpCommand(apiErr); help != "" {
		suggestions = append(suggestions, fmt.Sprintf("Run '%s' for usage and examples", help))
	}

	return suggestions
}

func queryErrorDatasourceName(datasource string) string {
	switch datasource {
	case "loki":
		return "Loki"
	case "prometheus":
		return "Prometheus"
	case "pyroscope":
		return "Pyroscope"
	case "tempo":
		return "Tempo"
	default:
		if datasource == "" {
			return "Datasource"
		}
		return strings.ToUpper(datasource[:1]) + datasource[1:]
	}
}

func queryErrorLanguage(apiErr *queryerror.APIError) string {
	switch apiErr.Datasource {
	case "loki":
		if apiErr.Operation == "query" || apiErr.Operation == "metric query" || apiErr.Operation == "series query" {
			return "LogQL"
		}
	case "prometheus":
		if apiErr.Operation == "query" {
			return "PromQL"
		}
	case "pyroscope":
		if apiErr.Operation == "query" || apiErr.Operation == "series query" {
			return "Pyroscope selector"
		}
	case "tempo":
		if apiErr.Operation == "search query" || apiErr.Operation == "metrics query" {
			return "TraceQL"
		}
	}

	return ""
}

func queryErrorStringLiteralExample(apiErr *queryerror.APIError) string {
	switch apiErr.Datasource {
	case "loki":
		return `Try a quoted selector value, e.g. gcx logs query '{namespace="prod"}'`
	case "prometheus":
		return `Try a quoted selector value, e.g. gcx metrics query 'up{job="grafana"}'`
	case "pyroscope":
		return `Try a quoted selector value, e.g. gcx profiles query '{service_name="frontend"}' --profile-type <PROFILE_TYPE>`
	case "tempo":
		return `Try a quoted string literal, e.g. gcx traces query '{ resource.service.name = "checkout" }'`
	default:
		return ""
	}
}

func queryErrorHelpCommand(apiErr *queryerror.APIError) string {
	switch apiErr.Datasource {
	case "loki":
		switch apiErr.Operation {
		case "query":
			return "gcx logs query --help"
		case "metric query":
			return "gcx logs metrics --help"
		case "labels query", "label values query":
			return "gcx logs labels --help"
		case "series query":
			return "gcx logs series --help"
		}
	case "prometheus":
		switch apiErr.Operation {
		case "query":
			return "gcx metrics query --help"
		case "labels query", "label values query":
			return "gcx metrics labels --help"
		case "metadata query":
			return "gcx metrics metadata --help"
		}
	case "pyroscope":
		switch apiErr.Operation {
		case "query":
			return "gcx profiles query --help"
		case "profile types query":
			return "gcx profiles profile-types --help"
		case "label names query", "label values query":
			return "gcx profiles labels --help"
		case "series query":
			return "gcx profiles metrics --help"
		}
	case "tempo":
		switch apiErr.Operation {
		case "search query":
			return "gcx traces query --help"
		case "get trace":
			return "gcx traces get --help"
		case "tags query", "tag values query":
			return "gcx traces labels --help"
		case "metrics query":
			return "gcx traces metrics --help"
		}
	}

	return ""
}

type serviceAPIError interface {
	error
	HTTPStatusCode() int
	APIServiceName() string
	APIUserMessage() string
}

func convertDatasourceErrors(err error) (*DetailedError, bool) {
	apiErr := &datasources.APIError{}
	if !errors.As(err, &apiErr) {
		return nil, false
	}

	detailedErr := &DetailedError{
		Summary:     datasourceErrorSummary(apiErr),
		Details:     joinErrorDetails(wrappedTypedErrorContext(err, apiErr), strings.TrimSpace(apiErr.APIUserMessage())),
		Suggestions: datasourceErrorSuggestions(apiErr),
	}
	if sameRenderedMessage(detailedErr.Details, detailedErr.Summary) {
		detailedErr.Details = ""
	}
	if apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden {
		detailedErr.ExitCode = new(ExitAuthFailure)
	}

	return detailedErr, true
}

func datasourceErrorSummary(apiErr *datasources.APIError) string {
	switch apiErr.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "Authentication failed querying datasources"
	case http.StatusNotFound:
		if apiErr.Identifier != "" {
			return fmt.Sprintf("Datasource %q not found", apiErr.Identifier)
		}
		return "Datasource not found"
	default:
		if apiErr.Operation != "" {
			return fmt.Sprintf("Could not %s (HTTP %d)", apiErr.Operation, apiErr.StatusCode)
		}
		return fmt.Sprintf("Datasource API request failed (HTTP %d)", apiErr.StatusCode)
	}
}

func datasourceErrorSuggestions(apiErr *datasources.APIError) []string {
	switch apiErr.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return []string{
			"Review your Grafana credentials: gcx config view",
			reauthSuggestion,
		}
	case http.StatusNotFound:
		return []string{
			"List available datasources: gcx datasources list",
		}
	default:
		return nil
	}
}

func convertServiceAPIErrors(err error) (*DetailedError, bool) {
	var apiErr serviceAPIError
	if !errors.As(err, &apiErr) {
		return nil, false
	}

	// Adaptive Logs scope errors — handled here (not in convertCloudConfigErrors with
	// traces/metrics) because the logs client returns a typed APIError that this converter
	// catches before convertCloudConfigErrors runs.
	if apiErr.APIServiceName() == "Adaptive Logs" &&
		strings.Contains(apiErr.APIUserMessage(), "invalid scope") &&
		(apiErr.HTTPStatusCode() == http.StatusUnauthorized || apiErr.HTTPStatusCode() == http.StatusForbidden) {
		return &DetailedError{
			Parent:  err,
			Summary: "Adaptive Logs: permission denied",
			Suggestions: []string{
				"Ensure your Grafana Cloud access policy includes the adaptive-logs:admin scope",
			},
			ExitCode: new(ExitAuthFailure),
		}, true
	}

	detailedErr := &DetailedError{
		Summary:     serviceAPIErrorSummary(apiErr),
		Details:     joinErrorDetails(wrappedTypedErrorContext(err, apiErr), strings.TrimSpace(apiErr.APIUserMessage())),
		Suggestions: serviceAPIErrorSuggestions(apiErr),
	}
	if sameRenderedMessage(detailedErr.Details, detailedErr.Summary) {
		detailedErr.Details = ""
	}
	if code := apiErr.HTTPStatusCode(); code == http.StatusUnauthorized || code == http.StatusForbidden {
		detailedErr.ExitCode = new(ExitAuthFailure)
	}

	return detailedErr, true
}

func serviceAPIErrorSummary(apiErr serviceAPIError) string {
	service := strings.TrimSpace(apiErr.APIServiceName())
	if service == "" {
		service = "API"
	}

	switch apiErr.HTTPStatusCode() {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "Authentication failed querying " + service
	case http.StatusNotFound:
		return service + " API resource not found"
	default:
		return fmt.Sprintf("%s API request failed (HTTP %d)", service, apiErr.HTTPStatusCode())
	}
}

func serviceAPIErrorSuggestions(apiErr serviceAPIError) []string {
	switch apiErr.HTTPStatusCode() {
	case http.StatusUnauthorized, http.StatusForbidden:
		return []string{
			"Review your Grafana credentials: gcx config view",
			reauthSuggestion,
		}
	default:
		return nil
	}
}

func wrappedTypedErrorContext(err error, inner error) string {
	if err == nil || inner == nil {
		return ""
	}

	message := strings.TrimSpace(err.Error())
	innerMessage := strings.TrimSpace(inner.Error())
	if message == "" || innerMessage == "" || message == innerMessage {
		return ""
	}

	prefix, after, found := strings.Cut(message, innerMessage)
	if !found {
		return ""
	}

	prefix = trimWrapperPrefix(prefix)
	suffix := trimWrapperSuffix(after)

	parts := []string{}
	if prefix != "" && !isGenericAPIWrapperPrefix(prefix) {
		parts = append(parts, prefix)
	}
	if suffix != "" {
		parts = append(parts, suffix)
	}

	return joinErrorDetails(parts...)
}

func trimWrapperPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	prefix = strings.TrimRight(prefix, ":;,- ")
	return strings.TrimSpace(prefix)
}

func trimWrapperSuffix(suffix string) string {
	suffix = strings.TrimSpace(suffix)
	suffix = strings.TrimLeft(suffix, ":;,- ")
	return strings.TrimSpace(suffix)
}

func isGenericAPIWrapperPrefix(prefix string) bool {
	prefix = strings.ToLower(strings.TrimSpace(prefix))

	switch prefix {
	case "",
		"query failed",
		"search failed",
		"get trace failed",
		"metrics query failed",
		"labels query failed",
		"label values query failed",
		"metadata query failed",
		"failed to get labels",
		"failed to get label values",
		"failed to get metadata",
		"failed to get profile types",
		"failed to get series",
		"failed to get datasource":
		// Exact-match only: UID-containing variants such as
		// `failed to get datasource "my-uid"` identify which datasource
		// failed and must be preserved as wrapper context.
		return true
	default:
		return false
	}
}

func joinErrorDetails(parts ...string) string {
	joined := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if len(joined) > 0 && sameRenderedMessage(joined[len(joined)-1], part) {
			continue
		}
		joined = append(joined, part)
	}

	return strings.Join(joined, "\n\n")
}

func convertResourcesErrors(err error) (*DetailedError, bool) {
	invalidCommandErr := &resources.InvalidSelectorError{}
	if err != nil && errors.As(err, invalidCommandErr) {
		return &DetailedError{
			Parent:  err,
			Summary: "Could not parse resource(s) selector",
			Details: fmt.Sprintf("Failed to parse command '%s'", invalidCommandErr.Command),
			Suggestions: []string{
				"Make sure that your are passing in valid resource selectors",
			},
		}, true
	}

	return nil, false
}

func convertFSErrors(err error) (*DetailedError, bool) {
	pathErr := &fs.PathError{}

	if errors.Is(err, os.ErrNotExist) && errors.As(err, &pathErr) {
		return &DetailedError{
			Summary: "File not found",
			Details: fmt.Sprintf("could not read '%s'", pathErr.Path),
			Parent:  err,
			Suggestions: []string{
				"Check for typos in the command's arguments",
			},
		}, true
	}

	if errors.Is(err, os.ErrInvalid) && errors.As(err, &pathErr) {
		return &DetailedError{
			Summary: "Invalid path",
			Details: fmt.Sprintf("path '%s' is not valid", pathErr.Path),
			Parent:  err,
			Suggestions: []string{
				"Make sure that you are passing in a valid path",
				"If you are pulling resources make sure that the path is a directory",
			},
		}, true
	}

	if errors.Is(err, os.ErrPermission) && errors.As(err, &pathErr) {
		return &DetailedError{
			Summary: "Permission denied",
			Parent:  err,
			Suggestions: []string{
				"Review the permissions on the file",
			},
		}, true
	}

	return nil, false
}

func convertLinterErrors(err error) (*DetailedError, bool) {
	if errors.Is(err, linter.ErrTestsFailed) {
		return nil, true
	}

	return nil, false
}

func convertVersionErrors(err error) (*DetailedError, bool) {
	vErr := &grafana.VersionIncompatibleError{}
	if errors.As(err, &vErr) {
		return &DetailedError{
			Parent:  err,
			Summary: fmt.Sprintf("Grafana version %s is not supported", vErr.Version),
			Details: "gcx requires Grafana 12.0.0 or later",
			Suggestions: []string{
				"Upgrade your Grafana instance to version 12.0.0 or later",
			},
			ExitCode: new(ExitVersionIncompatible),
		}, true
	}

	return nil, false
}

func convertRequiredFlagErrors(err error) (*DetailedError, bool) {
	// Cobra returns a plain error (not a typed error) for missing required flags.
	// The message is always of the form: `required flag(s) "foo", "bar" not set`
	msg := err.Error()
	if strings.HasPrefix(msg, "required flag(s)") && strings.HasSuffix(msg, "not set") {
		return &DetailedError{
			Summary: "Missing required flags",
			Parent:  err,
			Suggestions: []string{
				"Run the command with --help to see available flags and usage examples",
			},
		}, true
	}
	return nil, false
}

func convertSMConfigErrors(err error) (*DetailedError, bool) {
	msg := err.Error()

	if strings.Contains(msg, "SM URL not configured") {
		return &DetailedError{
			Summary: "SM URL not configured",
			Details: msg,
			Parent:  err,
			Suggestions: []string{
				"Set manually: gcx config set providers.synth.sm-url https://synthetic-monitoring-api-<region>.grafana.net",
				"Or use env var: export GRAFANA_PROVIDER_SYNTH_SM_URL=<URL>",
				"Auto-discovery requires grafana.server in the current context",
				"Check config: gcx config view",
			},
		}, true
	}

	if strings.Contains(msg, "SM token not configured") && strings.Contains(msg, "register/install") &&
		(strings.Contains(msg, "status 401") || strings.Contains(msg, "status 403") ||
			(strings.Contains(msg, "status 400") && strings.Contains(msg, "permission"))) {
		return &DetailedError{
			Parent:  err,
			Summary: "SM token auto-discovery: permission denied",
			Details: msg,
			Suggestions: []string{
				"Ensure your cloud.token access policy includes these scopes: stacks:read, metrics:write, logs:write, traces:write",
				"Or set the SM token directly: gcx config set providers.synth.sm-token <TOKEN>",
				"Or use env var: export GRAFANA_PROVIDER_SYNTH_SM_TOKEN=<TOKEN>",
			},
			ExitCode: new(ExitAuthFailure),
		}, true
	}

	if strings.Contains(msg, "SM token not configured") {
		return &DetailedError{
			Summary: "SM token not configured",
			Details: msg,
			Parent:  err,
			Suggestions: []string{
				"Set it: gcx config set providers.synth.sm-token <TOKEN>",
				"Or use env var: export GRAFANA_PROVIDER_SYNTH_SM_TOKEN=<TOKEN>",
				"Auto-discovery requires cloud.token and cloud.stack in the current context",
				"Check config: gcx config view",
			},
		}, true
	}

	return nil, false
}

func convertCloudConfigErrors(err error) (*DetailedError, bool) {
	msg := err.Error()

	// Cloud token missing.
	if strings.Contains(msg, "cloud token is required") {
		return &DetailedError{
			Summary: "Cloud credentials not configured",
			Details: msg,
			Parent:  err,
			Suggestions: []string{
				"Set cloud.token in your config: gcx config set cloud.token <TOKEN>",
				"Or set GRAFANA_CLOUD_TOKEN environment variable",
			},
		}, true
	}

	// Cloud stack not configured.
	if strings.Contains(msg, "cloud stack is not configured") {
		return &DetailedError{
			Summary: "Cloud stack not configured",
			Details: msg,
			Parent:  err,
			Suggestions: []string{
				"Set cloud.stack in your config: gcx config set cloud.stack <STACK_SLUG>",
				"Or set GRAFANA_CLOUD_STACK environment variable",
			},
		}, true
	}

	// Fleet API scope error on read operations.
	if strings.Contains(msg, "fleet:") && strings.Contains(msg, "invalid scope") &&
		(strings.Contains(msg, "list ") || strings.Contains(msg, "get ")) {
		return &DetailedError{
			Parent:  err,
			Summary: "Fleet Management: permission denied",
			Suggestions: []string{
				"Ensure your cloud.token access policy includes the fleet-management:read scope",
			},
			ExitCode: new(ExitAuthFailure),
		}, true
	}

	// Fleet API scope error on write operations.
	if strings.Contains(msg, "fleet:") && strings.Contains(msg, "invalid scope") &&
		(strings.Contains(msg, "create ") || strings.Contains(msg, "update ") || strings.Contains(msg, "delete ")) {
		return &DetailedError{
			Parent:  err,
			Summary: "Fleet Management: permission denied",
			Suggestions: []string{
				"Ensure your cloud.token access policy includes the fleet-management:write scope",
			},
			ExitCode: new(ExitAuthFailure),
		}, true
	}

	// Adaptive Traces scope errors.
	if strings.Contains(msg, "adaptive-traces:") && strings.Contains(msg, "invalid scope") {
		return &DetailedError{
			Parent:  err,
			Summary: "Adaptive Traces: permission denied",
			Suggestions: []string{
				"Ensure your Grafana Cloud access policy includes the adaptive-traces:admin scope",
			},
			ExitCode: new(ExitAuthFailure),
		}, true
	}

	// Adaptive Metrics scope errors.
	if strings.Contains(msg, "adaptive-metrics:") && strings.Contains(msg, "invalid scope") {
		scope := adaptiveMetricsScopeFromError(msg)
		suggestion := fmt.Sprintf("Ensure your Grafana Cloud access policy includes the %s scope", scope)
		if scope == "" {
			suggestion = "Adaptive Metrics commands require an adaptive-metrics-* scope on your Grafana Cloud access policy (the specific scope depends on the subcommand)"
		}
		return &DetailedError{
			Parent:      err,
			Summary:     "Adaptive Metrics: permission denied",
			Suggestions: []string{suggestion},
			ExitCode:    new(ExitAuthFailure),
		}, true
	}

	// Fleet management not available.
	if strings.Contains(msg, "fleet management endpoint is not available") ||
		strings.Contains(msg, "fleet management instance ID is not available") {
		return &DetailedError{
			Summary: "Fleet Management not available",
			Details: msg,
			Parent:  err,
			Suggestions: []string{
				"Fleet Management may not be enabled for this stack",
				"Contact Grafana Cloud support to enable Fleet Management",
			},
		}, true
	}

	// Stack info lookup forbidden — access policy missing stacks:read scope.
	if strings.Contains(msg, "failed to get stack info for") && strings.Contains(msg, "status 403") {
		suggestions := []string{
			"Ensure your Grafana Cloud access policy includes the stacks:read scope",
		}
		if suggestion := adaptiveScopeSuggestionFromSignalPrefix(msg); suggestion != "" {
			suggestions = append(suggestions, suggestion)
		}
		return &DetailedError{
			Parent:      err,
			Summary:     "Cloud stack lookup: permission denied",
			Suggestions: suggestions,
			ExitCode:    new(ExitAuthFailure),
		}, true
	}

	// Setup/instrumentation prefixed errors — surface them directly instead of "Unexpected error".
	if strings.HasPrefix(msg, "setup/instrumentation:") || strings.Contains(msg, "setup/instrumentation:") {
		// Extract the message after the prefix for the summary.
		summary := "Setup instrumentation error"
		return &DetailedError{
			Summary: summary,
			Details: msg,
			Parent:  err,
		}, true
	}

	return nil, false
}

func adaptiveScopeSuggestionFromSignalPrefix(msg string) string {
	switch {
	case strings.Contains(msg, "adaptive-logs:"):
		return "Ensure your Grafana Cloud access policy includes the adaptive-logs:admin scope"
	case strings.Contains(msg, "adaptive-metrics:"):
		return "Adaptive Metrics commands also require an adaptive-metrics-* scope on your Grafana Cloud access policy (the specific scope depends on the subcommand)"
	case strings.Contains(msg, "adaptive-traces:"):
		return "Ensure your Grafana Cloud access policy includes the adaptive-traces:admin scope"
	default:
		return ""
	}
}

func adaptiveMetricsScopeFromError(msg string) string {
	type resource struct {
		keyword   string
		base      string
		reads     []string
		writes    []string
		deleteKey string
	}
	// Operation matches are checked in priority order: delete > write > read.
	resources := []resource{
		{"rule", "adaptive-metrics-rules",
			[]string{"list rules", "get rule", "list recommended rules"},
			[]string{"create rule", "update rule", "sync rules", "validate rules"},
			"delete rule"},
		{"recommendation", "adaptive-metrics-recommendations",
			[]string{"list recommendations"}, nil, ""},
		{"segment", "adaptive-metrics-segments",
			[]string{"list segments"},
			[]string{"create segment", "update segment"},
			"delete segment"},
		{"exemption", "adaptive-metrics-exemptions",
			[]string{"list exemptions", "list segmented exemptions", "get exemption"},
			[]string{"create exemption", "update exemption"},
			"delete exemption"},
	}
	for _, r := range resources {
		if !strings.Contains(msg, r.keyword) {
			continue
		}
		if r.deleteKey != "" && strings.Contains(msg, r.deleteKey) {
			return r.base + ":delete"
		}
		for _, v := range r.writes {
			if strings.Contains(msg, v) {
				return r.base + ":write"
			}
		}
		for _, v := range r.reads {
			if strings.Contains(msg, v) {
				return r.base + ":read"
			}
		}
	}
	return ""
}

func fallbackDetailedError(err error) *DetailedError {
	summary, details, parent := summarizeFallbackError(err)
	return &DetailedError{
		Summary: summary,
		Details: details,
		Parent:  parent,
	}
}

func summarizeFallbackError(err error) (string, string, error) {
	if err == nil {
		return "Unexpected error", "", nil
	}

	if wrappedSummary, wrappedParent, ok := fallbackWrappedSummary(err); ok {
		return humanizeSummary(wrappedSummary), "", wrappedParent
	}

	summary, details := splitErrorMessage(err.Error())
	return humanizeSummary(summary), details, nil
}

func fallbackWrappedSummary(err error) (string, error, bool) {
	parent := errors.Unwrap(err)
	if parent == nil {
		return "", nil, false
	}

	message := strings.TrimSpace(err.Error())
	parentMsg := strings.TrimSpace(parent.Error())
	if parentMsg != "" && strings.HasSuffix(message, ": "+parentMsg) {
		message = strings.TrimSpace(strings.TrimSuffix(message, ": "+parentMsg))
	}

	if message == "" {
		message = strings.TrimSpace(err.Error())
	}

	return message, parent, true
}

func splitErrorMessage(message string) (string, string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "Unexpected error", ""
	}

	if i := strings.Index(message, ": "); i > 0 {
		prefix := strings.TrimSpace(message[:i])
		// Only treat sentence-like prefixes as summaries. Single-token
		// provider tags (e.g. "k6:", "fleet:") make poor summaries —
		// fall back to "Unexpected error" and surface the raw message
		// as details. A typed converter should handle the provider's
		// error type for a richer summary.
		if strings.Contains(prefix, " ") {
			return prefix, strings.TrimSpace(message[i+2:])
		}
		return "Unexpected error", message
	}
	if i := strings.Index(message, "\n"); i > 0 {
		return strings.TrimSpace(message[:i]), strings.TrimSpace(message[i+1:])
	}

	return message, ""
}

func humanizeSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "Unexpected error"
	}

	r, size := utf8.DecodeRuneInString(summary)
	if r == utf8.RuneError && size == 0 {
		return "Unexpected error"
	}
	if unicode.IsLower(r) {
		return string(unicode.ToUpper(r)) + summary[size:]
	}

	return summary
}

func convertContextCanceled(err error) (*DetailedError, bool) {
	if errors.Is(err, context.Canceled) {
		return &DetailedError{
			Summary:  "Operation cancelled",
			Parent:   err,
			ExitCode: new(ExitCancelled),
		}, true
	}

	return nil, false
}
