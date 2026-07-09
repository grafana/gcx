package remote

import (
	"sync"
	"sync/atomic"

	"github.com/grafana/gcx/internal/resources"
)

// OperationSummary tracks the results of a batch resource operation in a thread-safe manner.
// It uses atomic counters for success/failure counts and a mutex-protected slice for failure details.
type OperationSummary struct {
	successCount atomic.Int64
	failedCount  atomic.Int64
	skippedCount atomic.Int64
	truncated    atomic.Bool
	mu           sync.Mutex
	failures     []OperationFailure
}

// OperationFailure describes a single resource operation failure.
type OperationFailure struct {
	// Resource is the resource that failed. May be nil for non-resource failures
	// (e.g., failures fetching all resources of a given type).
	Resource *resources.Resource

	// Error is the error that caused the failure.
	Error error
}

// RecordSuccess records a successful operation.
func (s *OperationSummary) RecordSuccess() {
	s.successCount.Add(1)
}

// RecordSkipped records something the API could not do through no fault of the user: a
// resource type it cannot list (puller), or a dry-run against a resource that ignores
// server-side dryRun (pusher/deleter). Skips are not failures and do not affect the exit code.
func (s *OperationSummary) RecordSkipped() {
	s.skippedCount.Add(1)
}

// RecordFailure records a failed operation. res may be nil when the failure is not
// associated with a specific resource (e.g., a filter-level pull failure).
func (s *OperationSummary) RecordFailure(res *resources.Resource, err error) {
	s.failedCount.Add(1)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.failures = append(s.failures, OperationFailure{
		Resource: res,
		Error:    err,
	})
}

// SuccessCount returns the number of successfully processed resources.
func (s *OperationSummary) SuccessCount() int {
	return int(s.successCount.Load())
}

// FailedCount returns the number of failed resource operations.
func (s *OperationSummary) FailedCount() int {
	return int(s.failedCount.Load())
}

// SkippedCount returns the number of skipped resources (see RecordSkipped).
func (s *OperationSummary) SkippedCount() int {
	return int(s.skippedCount.Load())
}

// RecordTruncated records that a list response was truncated (more items available).
func (s *OperationSummary) RecordTruncated() {
	s.truncated.Store(true)
}

// IsTruncated reports whether any list response was truncated.
func (s *OperationSummary) IsTruncated() bool {
	return s.truncated.Load()
}

// Failures returns all recorded operation failures.
func (s *OperationSummary) Failures() []OperationFailure {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.failures
}
