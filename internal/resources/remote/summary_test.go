package remote_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/require"
)

func TestOperationSummary_ThreadSafety(t *testing.T) {
	const goroutines = 50

	summary := &remote.OperationSummary{}

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for range goroutines {
		go func() {
			defer wg.Done()
			summary.RecordSuccess()
		}()
		go func() {
			defer wg.Done()
			summary.RecordFailure(nil, errors.New("test error"))
		}()
	}

	wg.Wait()

	require.Equal(t, goroutines, summary.SuccessCount())
	require.Equal(t, goroutines, summary.FailedCount())
	require.Len(t, summary.Failures(), goroutines)
}

func TestOperationSummary_Failures(t *testing.T) {
	tests := []struct {
		name             string
		successes        int
		failures         []error
		wantSuccessCount int
		wantFailedCount  int
		wantFailureLen   int
	}{
		{
			name:             "all successes",
			successes:        5,
			failures:         nil,
			wantSuccessCount: 5,
			wantFailedCount:  0,
			wantFailureLen:   0,
		},
		{
			name:             "all failures",
			successes:        0,
			failures:         []error{errors.New("err1"), errors.New("err2")},
			wantSuccessCount: 0,
			wantFailedCount:  2,
			wantFailureLen:   2,
		},
		{
			name:             "mixed",
			successes:        3,
			failures:         []error{errors.New("err1")},
			wantSuccessCount: 3,
			wantFailedCount:  1,
			wantFailureLen:   1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			summary := &remote.OperationSummary{}

			for range tc.successes {
				summary.RecordSuccess()
			}
			for _, err := range tc.failures {
				summary.RecordFailure(nil, err)
			}

			require.Equal(t, tc.wantSuccessCount, summary.SuccessCount())
			require.Equal(t, tc.wantFailedCount, summary.FailedCount())
			require.Len(t, summary.Failures(), tc.wantFailureLen)
		})
	}
}

func TestOperationSummary_FailureContents(t *testing.T) {
	summary := &remote.OperationSummary{}
	err := errors.New("something went wrong")

	summary.RecordFailure(nil, err)

	failures := summary.Failures()
	require.Len(t, failures, 1)
	require.Nil(t, failures[0].Resource)
	require.Equal(t, err, failures[0].Error)
}
