package kg

import (
	"net/url"
	"strconv"
	"time"
)

// TimeRangeParams returns query params with start/end set for the given
// window, defaulting to the last hour when either bound is unset.
func TimeRangeParams(startMs, endMs int64) url.Values {
	startMs, endMs = defaultTimeWindow(startMs, endMs)
	q := url.Values{}
	q.Set("start", strconv.FormatInt(startMs, 10))
	q.Set("end", strconv.FormatInt(endMs, 10))
	return q
}

// defaultTimeWindow returns (startMs, endMs) unchanged unless either bound is
// unset, in which case it defaults to the last hour ending now.
func defaultTimeWindow(startMs, endMs int64) (int64, int64) {
	if startMs == 0 || endMs == 0 {
		endMs = time.Now().UnixMilli()
		startMs = endMs - 3600000
	}
	return startMs, endMs
}
