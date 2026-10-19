package k6

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// errNoSchedule reports that a load test has no schedule.
var errNoSchedule = errors.New("k6: load test has no schedule")

// updateSchedule replaces a schedule through its load test's schedule route,
// which is the only way k6 Cloud updates a schedule.
func updateSchedule(ctx context.Context, client API, id int, req ScheduleRequest) (*Schedule, error) {
	current, err := client.GetSchedule(ctx, id)
	if err != nil {
		return nil, err
	}
	updated, err := client.CreateSchedule(ctx, current.LoadTestID, req)
	if err != nil {
		return nil, fmt.Errorf("update schedule %d: %w", id, err)
	}
	return updated, nil
}

// deleteLoadTestSchedule deletes a load test's schedule, if it has one.
func deleteLoadTestSchedule(ctx context.Context, client API, loadTestID int) error {
	s, err := client.GetLoadTestSchedule(ctx, loadTestID)
	if errors.Is(err, errNoSchedule) {
		return nil
	}
	if err != nil {
		return err
	}
	return client.DeleteSchedule(ctx, s.ID)
}

// deleteSchedule deletes a schedule by ID. A schedule that is already gone
// counts as deleted.
func deleteSchedule(ctx context.Context, doJSON doJSONFunc, id int) error {
	resp, err := doJSON(ctx, http.MethodDelete, fmt.Sprintf(schedulesPath+"/%d", id), nil)
	if err != nil {
		return fmt.Errorf("k6: delete schedule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("k6: delete schedule %d: status %d: %s", id, resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

// getLoadTestSchedule returns a load test's schedule, or errNoSchedule when
// it has none.
func getLoadTestSchedule(ctx context.Context, doJSON doJSONFunc, loadTestID int) (*Schedule, error) {
	resp, err := doJSON(ctx, http.MethodGet, fmt.Sprintf(loadTestsPath+"/%d/schedule", loadTestID), nil)
	if err != nil {
		return nil, fmt.Errorf("k6: get load test schedule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, errNoSchedule
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("k6: get schedule of load test %d: status %d: %s", loadTestID, resp.StatusCode, readErrorBody(resp))
	}
	s, err := decodeJSON[Schedule](resp)
	if err != nil {
		return nil, err
	}
	return &s, nil
}
