package remote

import (
	"maps"
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
	skipReasons  map[string]int
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

// RecordSkipped records a non-failure outcome and its human-readable reason.
func (s *OperationSummary) RecordSkipped(reason string) {
	s.skippedCount.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.skipReasons == nil {
		s.skipReasons = make(map[string]int)
	}
	s.skipReasons[reason]++
}

// SkippedReasons returns an independent snapshot of skip counts by reason.
func (s *OperationSummary) SkippedReasons() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.skipReasons)
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
