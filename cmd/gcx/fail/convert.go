package fail

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/grafana"
	"github.com/grafana/gcx/internal/linter"
	"github.com/grafana/gcx/internal/resources"
	k8sapi "k8s.io/apimachinery/pkg/api/errors"
)

func ErrorToDetailedError(err error) *DetailedError {
	var converted bool
	detailedErr := &DetailedError{}
	if errors.As(err, detailedErr) {
		return detailedErr
	}

	// Try to convert the error for common error categories
	errorConverters := []func(err error) (*DetailedError, bool){
		convertUsageErrors,
		convertContextCanceled,    // Context cancellation (must be first — cancellation can wrap other errors)
		convertRequiredFlagErrors, // Cobra required-flag errors — must appear before generic checks
		convertConfigErrors,       // Config-related
		convertFSErrors,           // FS-related
		convertResourcesErrors,    // Resources-related
		convertNetworkErrors,      // Network-related errors
		convertAPIErrors,          // API-related errors
		convertVersionErrors,      // Version incompatibility errors
		convertLinterErrors,       // Linter-related errors
	}

	for _, converter := range errorConverters {
		detailedErr, converted = converter(err)
		if converted {
			return detailedErr
		}
	}

	return &DetailedError{
		Summary: "Unexpected error",
		Parent:  err,
	}
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
