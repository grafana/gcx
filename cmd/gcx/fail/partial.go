package fail

import "fmt"

// PartialFailureError indicates that a batch operation completed but some
// resources failed. Commands should return this instead of a plain fmt.Errorf
// so that the error converter can set exit code 4.
type PartialFailureError struct {
	Total  int
	Failed int
	Op     string // "push", "pull", "delete", "validate"
}

func (e *PartialFailureError) Error() string {
	return fmt.Sprintf("%d resource(s) failed to %s", e.Failed, e.Op)
}

// NewPartialFailureError creates a PartialFailureError for a batch operation
// where some resources failed.
func NewPartialFailureError(op string, total, failed int) *PartialFailureError {
	return &PartialFailureError{
		Total:  total,
		Failed: failed,
		Op:     op,
	}
}
