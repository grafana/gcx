package fail

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// UsageError carries command-specific usage details without relying on Cobra's
// default free-form error strings.
type UsageError struct {
	Message     string
	Expected    string
	Suggestions []string
	Cause       error
}

func (e *UsageError) Error() string {
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "invalid command usage"
}

func (e *UsageError) Unwrap() error {
	return e.Cause
}

// NewCommandUsageError builds a UsageError enriched with the command's usage
// line and a standard help suggestion.
func NewCommandUsageError(cmd *cobra.Command, message string, cause error) *UsageError {
	err := &UsageError{
		Message: strings.TrimSpace(message),
		Cause:   cause,
	}

	if err.Message == "" && cause != nil {
		err.Message = cause.Error()
	}
	if err.Message == "" {
		err.Message = "invalid command usage"
	}

	if cmd == nil {
		return err
	}

	if expected := strings.TrimSpace(cmd.UseLine()); expected != "" {
		err.Expected = expected
	}

	if commandPath := strings.TrimSpace(cmd.CommandPath()); commandPath != "" {
		err.Suggestions = []string{fmt.Sprintf("Run '%s --help' for full usage and examples", commandPath)}
	}

	return err
}
