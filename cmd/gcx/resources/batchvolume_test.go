package resources //nolint:testpackage // exercises the unexported emit/capture helpers directly

import (
	"bytes"
	"errors"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/telemetry/capture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func jsonOptions() cmdio.Options {
	return cmdio.Options{OutputFormat: "json"}
}

func TestCaptureBatchVolumeRecordsFinalizedCounts(t *testing.T) {
	capture.Reset()
	t.Cleanup(capture.Reset)

	captureBatchVolume(cmdio.MutationSummary{Succeeded: 12, Failed: 3, Skipped: 4}, true)

	got := capture.CurrentBatch()
	require.NotNil(t, got)
	assert.Equal(t, capture.Batch{Succeeded: 12, Failed: 3, Skipped: 4, DryRun: true}, *got)
}

// The whole volume contract rests on this ordering: a successful emit records
// volume, and nothing else does.
func TestEmitBatchResultCapturesAfterSuccessfulEmit(t *testing.T) {
	capture.Reset()
	t.Cleanup(capture.Reset)

	result := cmdio.NewBatchMutation("pushed")
	result.Summary = cmdio.MutationSummary{Succeeded: 47, Failed: 2, Skipped: 1}

	var stdout bytes.Buffer
	require.NoError(t, emitBatchResult(&stdout, jsonOptions(), result))

	assert.NotEmpty(t, stdout.String(), "the result document must reach the user")
	got := capture.CurrentBatch()
	require.NotNil(t, got)
	assert.Equal(t, capture.Batch{Succeeded: 47, Failed: 2, Skipped: 1}, *got)
}

// If the document could not be written, the user saw no summary, so telemetry
// must report no volume.
func TestEmitBatchResultCapturesNothingWhenEmitFails(t *testing.T) {
	capture.Reset()
	t.Cleanup(capture.Reset)

	result := cmdio.NewBatchMutation("pushed")
	result.Summary = cmdio.MutationSummary{Succeeded: 47}

	err := emitBatchResult(failingWriter{}, jsonOptions(), result)

	require.Error(t, err)
	assert.Nil(t, capture.CurrentBatch(),
		"a document the user never received must not report volume")
}

// dry_run comes from the emitted document, so a rehearsal can never be recorded
// as applied work.
func TestEmitBatchResultCarriesDryRunFromDocument(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		capture.Reset()

		result := cmdio.NewBatchMutation("pushed")
		result.Summary = cmdio.MutationSummary{Succeeded: 5}
		result.DryRun = dryRun

		var stdout bytes.Buffer
		require.NoError(t, emitBatchResult(&stdout, jsonOptions(), result))

		got := capture.CurrentBatch()
		require.NotNil(t, got)
		assert.Equal(t, dryRun, got.DryRun)
	}
	capture.Reset()
}

// A batch that matched nothing reports zeroes, which is distinct from reporting
// nothing at all.
func TestEmitBatchResultReportsEmptyBatch(t *testing.T) {
	capture.Reset()
	t.Cleanup(capture.Reset)

	var stdout bytes.Buffer
	require.NoError(t, emitBatchResult(&stdout, jsonOptions(), cmdio.NewBatchMutation("pushed")))

	got := capture.CurrentBatch()
	require.NotNil(t, got, "an empty batch is still a batch")
	assert.Equal(t, capture.Batch{}, *got)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("stdout closed") }
