package config

import (
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/credentials"
)

var ErrContextNotFound = errors.New("context not found")

type ValidationError struct {
	// File holds the path to the configuration file containing the error.
	File    string
	Message string
	// Path refers to the location of the error in the configuration file.
	// It is expressed as a YAMLPath, as described in https://pkg.go.dev/github.com/goccy/go-yaml#PathString
	Path            string
	AnnotatedSource string
	Suggestions     []string
}

func (e ValidationError) Error() string {
	return e.Message
}

type UnmarshalError struct {
	File string
	Err  error
}

func (e UnmarshalError) Error() string {
	return e.Err.Error()
}

// UnsupportedVersionError reports a config format version this build cannot
// safely interpret. Version checks run before secret resolution, migration, or
// any other write-side effect.
type UnsupportedVersionError struct {
	File    string
	Version int64
}

// CredentialRejectedError reports that configuration expressed credential
// intent, but gcx refused to resolve or use that value because its source or
// destination could not be trusted. Credential-consuming commands surface this
// before constructing a request; inspection and repair commands remain usable.
type CredentialRejectedError struct {
	Source string
	Owner  string
	Field  credentials.Field
	Reason string
}

func (e CredentialRejectedError) Error() string {
	message := fmt.Sprintf("configured credential %q field %q was rejected before network use", e.Owner, e.Field)
	if e.Reason != "" {
		message += ": " + e.Reason
	}
	if e.Source != "" {
		message += fmt.Sprintf("; review the file and re-authenticate with --config %q", e.Source)
	} else {
		message += "; re-authenticate with an explicitly selected --config file"
	}
	return message
}

func (e UnsupportedVersionError) Error() string {
	return fmt.Sprintf("unsupported config version %d in %s; this gcx build supports version %d",
		e.Version, e.File, ConfigVersion)
}

func ContextNotFound(name string) error {
	return fmt.Errorf("invalid context \"%s\": %w", name, ErrContextNotFound)
}
