package rmw

import (
	"errors"
	"fmt"
)

// ConflictError is returned by Update when the backend state changed between the
// initial GET and the pre-write re-check, indicating a concurrent modification.
// The Command and Namespace fields are filled in by the calling command layer.
type ConflictError struct {
	// Namespace names the conflicting namespace for app-level mutations.
	// Empty for cluster-level mutations.
	Namespace string
	// Diff is a human-readable description of the detected change
	// (e.g. "tracing: true → false", "added apps[]: frontend").
	Diff string
	// Command is the command name as it appears in error output
	// (e.g. "apps enable", "clusters enable").
	Command string
}

// Error implements the error interface. Format:
//   - namespace-level: `<Command>: cannot write — namespace "<Namespace>" was modified concurrently (<Diff>). Re-fetch and retry.`
//   - cluster-level:   `<Command>: cannot write — cluster configuration was modified concurrently (<Diff>). Re-fetch and retry.`
func (e ConflictError) Error() string {
	if e.Namespace != "" {
		return fmt.Sprintf("%s: cannot write — namespace %q was modified concurrently (%s). Re-fetch and retry.",
			e.Command, e.Namespace, e.Diff)
	}
	return fmt.Sprintf("%s: cannot write — cluster configuration was modified concurrently (%s). Re-fetch and retry.",
		e.Command, e.Diff)
}

// IsConflictError returns true when err is or wraps a ConflictError.
func IsConflictError(err error) bool {
	var ce ConflictError
	return errors.As(err, &ce)
}
