package experiments //nolint:testpackage // Tests drive the unexported waitForFinished helper.

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedGetter returns one step per Get call and keeps returning the last
// step once the script runs out.
type scriptedGetter struct {
	steps []scriptedStep
	calls int
}

type scriptedStep struct {
	status string
	err    error
}

func (g *scriptedGetter) Get(_ context.Context, _ string) (*Experiment, error) {
	step := g.steps[min(g.calls, len(g.steps)-1)]
	g.calls++
	if step.err != nil {
		return nil, step.err
	}
	return &Experiment{ExperimentID: "r-1", Status: step.status}, nil
}

func TestWaitForFinished(t *testing.T) {
	pollErr := errors.New("connection reset")
	tests := []struct {
		name    string
		steps   []scriptedStep
		timeout time.Duration
		// wantErr substrings; nil means the wait must succeed.
		wantErr []string
		// wantStderr substrings the progress stream must carry.
		wantStderr []string
		// wantMinCalls guards against a loop that returns before it polled.
		wantMinCalls int
	}{
		{
			name:         "an already finished run returns without waiting",
			steps:        []scriptedStep{{status: "completed"}},
			timeout:      time.Second,
			wantMinCalls: 1,
		},
		{
			name: "a run that finishes while polling returns",
			steps: []scriptedStep{
				{status: "pending"},
				{status: "running"},
				{status: "completed"},
			},
			timeout:      time.Second,
			wantStderr:   []string{`status "pending"`, `status "running"`},
			wantMinCalls: 3,
		},
		{
			// failed and canceled end the wait too: grading them is the check's
			// job, and it fails them with exit 4 rather than spinning here.
			name: "a failed run ends the wait",
			steps: []scriptedStep{
				{status: "running"},
				{status: "failed"},
			},
			timeout:      time.Second,
			wantMinCalls: 2,
		},
		{
			name:         "a run that never finishes times out with its last status",
			steps:        []scriptedStep{{status: "running"}},
			timeout:      30 * time.Millisecond,
			wantErr:      []string{"Timed out", "r-1", `"running"`, "30ms"},
			wantMinCalls: 1,
		},
		{
			// The first poll failing fast is what keeps a wrong run ID or a broken
			// token from spinning until the timeout.
			name:         "a first poll error returns immediately",
			steps:        []scriptedStep{{err: pollErr}},
			timeout:      time.Second,
			wantErr:      []string{"connection reset"},
			wantMinCalls: 1,
		},
		{
			name: "a transient poll error does not end the wait",
			steps: []scriptedStep{
				{status: "running"},
				{err: pollErr},
				{status: "completed"},
			},
			timeout:      time.Second,
			wantMinCalls: 3,
		},
		{
			name: "a timeout names the last poll error",
			steps: []scriptedStep{
				{status: "running"},
				{err: pollErr},
			},
			timeout:      40 * time.Millisecond,
			wantErr:      []string{"Timed out", "last poll error", "connection reset"},
			wantMinCalls: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			getter := &scriptedGetter{steps: tc.steps}
			var stderr bytes.Buffer

			err := waitForFinished(t.Context(), getter, "r-1", tc.timeout, time.Millisecond, &stderr)

			if tc.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				for _, s := range tc.wantErr {
					assert.Contains(t, err.Error(), s)
				}
			}
			assert.GreaterOrEqual(t, getter.calls, tc.wantMinCalls, "polls")
			for _, s := range tc.wantStderr {
				assert.Contains(t, stderr.String(), s)
			}
		})
	}
}

// TestWaitForFinished_TimeoutIsAStateError pins the exit code contract: a
// timeout is "not finished yet", the same class as running without --wait, so
// it must exit 1 and not borrow the quality verdict's 4.
func TestWaitForFinished_TimeoutIsAStateError(t *testing.T) {
	getter := &scriptedGetter{steps: []scriptedStep{{status: "running"}}}

	err := waitForFinished(t.Context(), getter, "r-1", 20*time.Millisecond, time.Millisecond, &bytes.Buffer{})

	var detailed *gcxerrors.DetailedError
	require.ErrorAs(t, err, &detailed)
	assert.Nil(t, detailed.ExitCode, "a nil exit code means 1")
	require.NotEmpty(t, detailed.Suggestions)
	assert.Contains(t, detailed.Suggestions[1], "gcx agento11y experiments get r-1")
	var emitted *gcxerrors.EmittedError
	assert.NotErrorAs(t, err, &emitted, "no document was written, so nothing was emitted")
}

func TestWaitForFinished_ContextCancelEndsTheWait(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	getter := &scriptedGetter{steps: []scriptedStep{{status: "running"}}}

	err := waitForFinished(ctx, getter, "r-1", time.Second, time.Millisecond, &bytes.Buffer{})

	require.ErrorIs(t, err, context.Canceled)
}
