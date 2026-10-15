package watchers

import (
	"errors"

	"github.com/grafana/gcx/internal/assistant/watcher"
	clientwatchers "github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

type calibrationRequestResult struct {
	Outcome  string `json:"outcome"`
	State    string `json:"state,omitempty"`
	Guidance string `json:"guidance"`
}

type createResult struct {
	mutationResult

	Resource    map[string]any           `json:"resource,omitempty"`
	Calibration calibrationRequestResult `json:"calibration"`
}

func emitCreate(cmd *cobra.Command, opts *cmdio.Options, client *clientwatchers.Client, crud *adapter.TypedCRUD[watcher.Watcher], saved watcher.Watcher, partial *watcher.PartialCompletionError) error {
	result := createResult{mutationResult: newMutationResult("created", saved), Calibration: calibrationRequestResult{Outcome: "not-requested", Guidance: "Resolve the reported failure, then calibrate in Grafana. Do not rerun create."}}
	var failure error
	if partial != nil {
		failure = partial
		result.Error = partial.Error() + "; inspect with gcx assistant watchers get " + saved.ServerID() + " and resolve the failed configuration stage with update"
	} else {
		resource, err := crud.ToUnstructured(saved)
		if err != nil {
			failure = err
			result.Error = "Watcher was created but its saved configuration could not be encoded; inspect with gcx assistant watchers get " + saved.ServerID()
		} else {
			result.Resource = resource.Object
			progress, requestErr := client.RequestInitialCalibration(cmd.Context(), saved.ServerID())
			switch {
			case requestErr == nil:
				result.Calibration = calibrationRequestResult{Outcome: "accepted", State: progress.Status, Guidance: "Calibration request accepted; this does not establish readiness. " + calibrationGuidance(progress.Status, "")}
			case errors.Is(requestErr, clientwatchers.ErrCapabilityUnavailable):
				result.Calibration = calibrationRequestResult{Outcome: "unavailable", Guidance: "Watcher created; background calibration is unavailable. Calibrate it in Grafana, review the checks, and start monitoring explicitly when ready."}
			default:
				failure = requestErr
				result.Calibration = calibrationRequestResult{Outcome: "error", Guidance: "Watcher created; calibration acceptance could not be confirmed. Inspect gcx assistant watchers status " + saved.ServerID() + " and continue in Grafana. Do not rerun create."}
				result.Error = requestErr.Error()
			}
		}
	}
	if err := opts.Encode(cmd.OutOrStdout(), result); err != nil {
		return err
	}
	if failure != nil {
		return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, failure)
	}
	return nil
}

func calibrationGuidance(state, lifecycle string) string {
	switch state {
	case "idle":
		return "No background calibration is observed. Calibrate in Grafana before starting monitoring."
	case "queued":
		return "Calibration is queued. Follow status or Grafana until it finishes; do not start monitoring yet."
	case "running":
		return "Calibration is running. Wait for it to finish or cancel it in Grafana; do not start monitoring yet."
	case "completed":
		if lifecycle == "running" {
			return "Calibration completed. Review the calibrated checks in Grafana; monitoring is already active."
		}
		return "Calibration completed. Review the enabled checks and lifecycle in Grafana before explicitly starting monitoring; completion alone does not establish readiness."
	case "failed":
		return "Calibration failed. Continue in Grafana to resolve the failure; do not start monitoring."
	case "cancelled":
		return "Calibration was cancelled. Review the Watcher and continue calibration in Grafana before starting monitoring."
	case "needs_input":
		return "Calibration needs more input. Continue in Grafana; do not start monitoring."
	default:
		return "Calibration state is unknown. Inspect it in Grafana before starting monitoring."
	}
}
