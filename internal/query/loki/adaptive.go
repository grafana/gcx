package loki

import (
	"strconv"
	"time"
)

const (
	// SampledMetadataKey is the structured metadata Adaptive Logs attaches to
	// lines that survived partial sampling; its value is the drop-rate
	// percentage (e.g. "90.00"), or "true" from older gateways.
	SampledMetadataKey = "__adaptive_logs_sampled__"

	// TimeMachineMetadataKey marks every line recovered from the Adaptive Logs
	// time machine; its value identifies the time machine query.
	TimeMachineMetadataKey = "__time_machine__"

	// MaxDroppedLogsLimit is the most lines the time machine returns per query.
	MaxDroppedLogsLimit = 10000

	adaptiveLogsSampledNotice = "Some logs may have been dropped by Adaptive Logs sampling"

	// instantWindow matches the window buildQueryBody sends when no range is set.
	instantWindow = time.Minute
)

// SampledSummary describes Adaptive Logs sampling evidence in a query response.
type SampledSummary struct {
	// Lines is the number of returned lines carrying SampledMetadataKey.
	Lines int
	// MaxDropPercent is the highest drop rate among those lines; 0 when no
	// marker carried a numeric rate.
	MaxDropPercent float64
	// Notice reports Loki's sampling warning, which also covers matching lines
	// beyond the returned page.
	Notice bool
}

// Any reports whether the response shows any sign of sampled-away lines.
func (s SampledSummary) Any() bool {
	return s.Lines > 0 || s.Notice
}

// SampledLines summarizes Adaptive Logs sampling evidence in resp. A response
// without evidence can still be missing lines: rules that drop every line of
// a cohort leave no survivor to carry the marker.
func SampledLines(resp *QueryResponse) SampledSummary {
	var s SampledSummary
	if resp == nil {
		return s
	}
	for _, stream := range resp.Data.Result {
		for _, entry := range stream.Values {
			value, ok := entry.StructuredMetadata[SampledMetadataKey]
			if !ok {
				continue
			}
			s.Lines++
			if pct, err := strconv.ParseFloat(value, 64); err == nil && pct > s.MaxDropPercent {
				s.MaxDropPercent = pct
			}
		}
	}
	for _, notice := range resp.Data.Notices {
		if notice.Text == adaptiveLogsSampledNotice {
			s.Notice = true
			break
		}
	}
	return s
}

// EffectiveRange returns the window a log query covers: the requested range,
// or the trailing minute that an instant query evaluates.
func (r QueryRequest) EffectiveRange(now time.Time) (time.Time, time.Time) {
	if r.IsRange() {
		return r.Start, r.End
	}
	return now.Add(-instantWindow), now
}
