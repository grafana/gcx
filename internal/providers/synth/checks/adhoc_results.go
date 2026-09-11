package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/grafana/gcx/internal/query/loki"
)

// AdHocProbeStatus is the outcome of a single probe's execution of an ad-hoc check.
type AdHocProbeStatus string

const (
	AdHocSuccess AdHocProbeStatus = "success"
	AdHocFailure AdHocProbeStatus = "failure"
	AdHocTimeout AdHocProbeStatus = "timeout"
)

// adHocPollInterval matches the polling cadence the Synthetic Monitoring app
// uses for the same Loki query (useAdHocLogs.ts refetchInterval).
const adHocPollInterval = 3 * time.Second

// AdHocProbeResult is the outcome for one probe's execution of an ad-hoc check.
type AdHocProbeResult struct {
	ProbeID   int64            `json:"probeId"`
	ProbeName string           `json:"probeName"`
	Status    AdHocProbeStatus `json:"status"`
	LogCount  int              `json:"logCount"`
}

// adHocLogLine mirrors the JSON body a probe pushes into Loki (tagged
// type="adhoc") for each ad-hoc execution — one line per probe. This is the
// same shape the Synthetic Monitoring app decodes client-side
// (types.adhoc-check.ts AdHocResultLine): Probe is the probe's NAME, not its
// numeric ID, and probe_success arrives embedded in Timeseries rather than
// through a separate metrics query.
type adHocLogLine struct {
	ID         string            `json:"id"`
	Probe      string            `json:"probe"`
	Logs       []json.RawMessage `json:"logs"`
	Timeseries []adHocTimeseries `json:"timeseries"`
}

type adHocTimeseries struct {
	Name   string             `json:"name"`
	Metric []adHocGaugeMetric `json:"metric"`
}

type adHocGaugeMetric struct {
	Gauge struct {
		Value float64 `json:"value"`
	} `json:"gauge"`
}

// probeSuccess reports the probe_success gauge value embedded in the line's
// timeseries, if present.
func (l adHocLogLine) probeSuccess() (float64, bool) {
	for _, ts := range l.Timeseries {
		if ts.Name != "probe_success" || len(ts.Metric) == 0 {
			continue
		}
		return ts.Metric[0].Gauge.Value, true
	}
	return 0, false
}

// BuildAdHocLogsQuery builds the LogQL expression that selects the ad-hoc
// result lines for a single ad-hoc check execution — the same query the
// Synthetic Monitoring app issues against Loki for the same purpose.
func BuildAdHocLogsQuery(id string) string {
	return fmt.Sprintf(`{type="adhoc"} |~ %q | json`, id)
}

// PollAdHocResults polls Loki for ad-hoc check results until every probe in
// probeNames has reported or timeout elapses. Probes that never report a
// matching log line come back with status AdHocTimeout.
func PollAdHocResults(ctx context.Context, lokiClient *loki.Client, dsUID, id string, probeNames map[int64]string, timeout time.Duration) ([]AdHocProbeResult, error) {
	pendingByName := make(map[string]int64, len(probeNames))
	for probeID, name := range probeNames {
		pendingByName[name] = probeID
	}

	reported := make(map[string]AdHocProbeResult, len(probeNames))
	expr := BuildAdHocLogsQuery(id)

	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	poll := func() error {
		resp, err := lokiClient.Query(pollCtx, dsUID, loki.QueryRequest{Query: expr})
		if err != nil {
			return err
		}
		for _, stream := range resp.Data.Result {
			for _, entry := range stream.Values {
				var line adHocLogLine
				if err := json.Unmarshal([]byte(entry.Line), &line); err != nil {
					continue // not a parseable ad-hoc result line
				}
				probeID, isPending := pendingByName[line.Probe]
				if !isPending {
					continue
				}

				status := AdHocSuccess
				if value, ok := line.probeSuccess(); ok && value != 1 {
					status = AdHocFailure
				}

				reported[line.Probe] = AdHocProbeResult{
					ProbeID:   probeID,
					ProbeName: line.Probe,
					Status:    status,
					LogCount:  len(line.Logs),
				}
				delete(pendingByName, line.Probe)
			}
		}
		return nil
	}

	if err := poll(); err != nil {
		return nil, fmt.Errorf("querying ad-hoc results: %w", err)
	}

	ticker := time.NewTicker(adHocPollInterval)
	defer ticker.Stop()

	for len(pendingByName) > 0 {
		select {
		case <-pollCtx.Done():
			for name, probeID := range pendingByName {
				reported[name] = AdHocProbeResult{ProbeID: probeID, ProbeName: name, Status: AdHocTimeout}
			}
			return orderedAdHocResults(reported, probeNames), nil
		case <-ticker.C:
			if err := poll(); err != nil {
				return nil, fmt.Errorf("querying ad-hoc results: %w", err)
			}
		}
	}

	return orderedAdHocResults(reported, probeNames), nil
}

// orderedAdHocResults returns results in a stable order (ascending probe ID)
// so command output is deterministic across runs.
func orderedAdHocResults(reported map[string]AdHocProbeResult, probeNames map[int64]string) []AdHocProbeResult {
	ids := make([]int64, 0, len(probeNames))
	for id := range probeNames {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	results := make([]AdHocProbeResult, 0, len(ids))
	for _, id := range ids {
		if r, ok := reported[probeNames[id]]; ok {
			results = append(results, r)
		}
	}
	return results
}
