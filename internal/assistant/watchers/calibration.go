package watchers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// RequestInitialCalibration requests calibration without activating monitoring.
// The returned state is an observation, not a readiness guarantee.
func (c *Client) RequestInitialCalibration(ctx context.Context, id string) (*Calibration, error) {
	path, err := watcherPath(id)
	if err != nil {
		return nil, err
	}
	request := struct {
		StartWhenReady bool `json:"startWhenReady"`
	}{StartWhenReady: false}
	const operation = "request Watcher initial calibration"
	var data json.RawMessage
	if err := c.write(ctx, http.MethodPost, path+"/initial-calibration", operation, request, &data); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && errors.Is(err, ErrNotFound) {
			// A missing optional route and a deleted Watcher both return 404.
			// Confirm the saved identity before treating calibration as unavailable.
			if item, readErr := c.Get(ctx, id); readErr == nil && item.ID == id {
				apiErr.kind = ErrCapabilityUnavailable
			}
		}
		return nil, err
	}
	var response struct {
		Calibration

		StartWhenReady *bool `json:"startWhenReady"`
	}
	const invalidResponse = operation + ": invalid response; calibration request acceptance is uncertain"
	if json.Unmarshal(data, &response) != nil || response.StartWhenReady == nil || *response.StartWhenReady {
		return nil, errors.New(invalidResponse)
	}
	switch response.Status {
	case "queued", "running", "completed", "failed", "cancelled", "needs_input":
		return &response.Calibration, nil
	default:
		return nil, errors.New(invalidResponse)
	}
}
