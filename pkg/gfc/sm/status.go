package sm

// BuildCheckStatusResults merges check definitions with metric data.
// successMap, probeCountMap, and latencyMap are keyed by "job/instance".
// latencyMap values are in seconds (converted to ms in the result).
// probeNames maps probe ID to display name.
func BuildCheckStatusResults(checks []Check, successMap, probeCountMap, latencyMap map[string]float64, probeNames map[int64]string) []CheckStatusResult {
	results := make([]CheckStatusResult, 0, len(checks))

	for _, c := range checks {
		key := c.Job + "/" + c.Target

		r := CheckStatusResult{
			ID:          c.ID,
			Job:         c.Job,
			Target:      c.Target,
			Type:        c.Settings.CheckType(),
			ProbesTotal: len(c.Probes),
		}

		if val, ok := successMap[key]; ok {
			s := val
			r.Success = &s
		}

		if cnt, ok := probeCountMap[key]; ok {
			r.ProbesUp = int(cnt)
		}

		if sec, ok := latencyMap[key]; ok {
			ms := sec * 1000
			r.LatencyMs = &ms
		}

		for _, pid := range c.Probes {
			if name, ok := probeNames[pid]; ok {
				r.ProbeNames = append(r.ProbeNames, name)
			}
		}

		r.Status = ComputeCheckStatus(r.Success, c.AlertSensitivity)
		results = append(results, r)
	}

	return results
}

// BuildProbeNameMap builds a probe ID → display name map.
// Offline probes get a "(offline)" suffix.
func BuildProbeNameMap(probes []Probe) map[int64]string {
	m := make(map[int64]string, len(probes))
	for _, p := range probes {
		name := p.Name
		if !p.Online {
			name += " (offline)"
		}
		m[p.ID] = name
	}
	return m
}

// ComputeCheckStatus determines the display status for a check based on the
// success rate and the check's alertSensitivity setting. Thresholds match the
// Grafana SM alerting defaults: high=95%, medium=90%, low=75%.
func ComputeCheckStatus(success *float64, sensitivity string) string {
	if success == nil {
		return "NODATA"
	}
	threshold := 0.90
	switch sensitivity {
	case "high":
		threshold = 0.95
	case "low":
		threshold = 0.75
	}
	if *success >= threshold {
		return "OK"
	}
	return "FAILING"
}
