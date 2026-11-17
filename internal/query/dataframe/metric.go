package dataframe

import "strconv"

// MetricValue preserves missing and non-numeric samples instead of reporting
// a real zero. Strings also keep non-finite values safe for JSON encoding.
func MetricValue(value any) string {
	switch v := value.(type) {
	case nil:
		return "NaN"
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return "NaN"
	}
}
