package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// gcSettings parses GOGC and GOMEMLIMIT as the Go runtime does, for a
// pre-initialized module (see preinit_wasip1.go), where the runtime read
// them before the command's environment existed. An empty value leaves that
// setting as it is, and returns nil for it. As in the runtime, an invalid
// GOGC means the default, 100, and an invalid GOMEMLIMIT is an error.
func gcSettings(gogc, gomemlimit string) (*int, *int64, error) {
	var percent *int
	switch gogc {
	case "":
	case "off":
		percent = new(-1)
	default:
		n := 100
		if p, err := strconv.ParseInt(gogc, 10, 32); err == nil {
			n = int(p)
		}
		percent = &n
	}

	var limit *int64
	if gomemlimit != "" {
		n, err := parseMemoryLimit(gomemlimit)
		if err != nil {
			return nil, nil, err
		}
		limit = &n
	}
	return percent, limit, nil
}

// parseMemoryLimit parses a GOMEMLIMIT value: "off", or a number of bytes
// with an optional B, KiB, MiB, GiB or TiB suffix.
func parseMemoryLimit(s string) (int64, error) {
	if s == "off" {
		return math.MaxInt64, nil
	}
	num, unit := s, int64(1)
	for _, suffix := range []struct {
		name string
		size int64
	}{{"KiB", 1 << 10}, {"MiB", 1 << 20}, {"GiB", 1 << 30}, {"TiB", 1 << 40}, {"B", 1}} {
		if before, ok := strings.CutSuffix(s, suffix.name); ok {
			num, unit = before, suffix.size
			break
		}
	}
	n, err := strconv.ParseInt(num, 10, 64)
	if err != nil || n < 0 || n > math.MaxInt64/unit {
		return 0, fmt.Errorf("invalid GOMEMLIMIT %q", s)
	}
	return n * unit, nil
}
