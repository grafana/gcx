package services //nolint:testpackage // Tests cover unexported fleet merge logic.

import (
	"math"
	"testing"
)

// TestMergeFleetOperations_TimeShareAgainstFleetTotal locks in the one
// place fleet merge logic must diverge from mergeOperations: TimeSharePercent
// is normalized against the caller-supplied fleetTotal, not a per-row or
// per-group total.
func TestMergeFleetOperations_TimeShareAgainstFleetTotal(t *testing.T) {
	keyA := opAggKey{name: "GET /a", groupKey: string("\x00svcA")}
	keyB := opAggKey{name: "GET /b", groupKey: string("\x00svcB")}

	rates := map[opAggKey]groupBucket{
		keyA: {value: 10, labels: map[string]string{"job": "svcA"}},
		keyB: {value: 5, labels: map[string]string{"job": "svcB"}},
	}
	avgs := map[opAggKey]groupBucket{
		keyA: {value: 0.1, labels: map[string]string{"job": "svcA"}}, // busy = 1.0/s
		keyB: {value: 0.2, labels: map[string]string{"job": "svcB"}}, // busy = 1.0/s
	}

	// fleetTotal = 10 (an order of magnitude above the two rows' own
	// busy-time sum) — proves normalization uses the supplied total, not
	// sum(avg*rate) over the rows present (which would be 2.0, giving 50%
	// each instead of 10%).
	got := mergeFleetOperations(rates, nil, avgs, nil, nil, nil, 10, nil)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	for _, row := range got {
		if !row.HasAvgLatency || !row.HasTraffic {
			t.Fatalf("row %q missing traffic/latency: %+v", row.Name, row)
		}
		if math.Abs(row.TimeSharePercent-10) > 0.001 {
			t.Errorf("row %q time-share = %v, want 10 (busy 1.0 / fleetTotal 10 * 100)", row.Name, row.TimeSharePercent)
		}
	}
}

// TestMergeFleetOperations_ZeroFleetTotal confirms a zero fleetTotal
// (no traffic anywhere) doesn't divide by zero.
func TestMergeFleetOperations_ZeroFleetTotal(t *testing.T) {
	key := opAggKey{name: "GET /a"}
	rates := map[opAggKey]groupBucket{key: {value: 1, labels: map[string]string{"job": "svcA"}}}
	avgs := map[opAggKey]groupBucket{key: {value: 0.5, labels: map[string]string{"job": "svcA"}}}

	got := mergeFleetOperations(rates, nil, avgs, nil, nil, nil, 0, nil)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].TimeSharePercent != 0 {
		t.Errorf("TimeSharePercent = %v, want 0 when fleetTotal is 0", got[0].TimeSharePercent)
	}
}

// TestMergeFleetOperations_SortOrder locks in the fleet sort: busy
// (avg*rate) desc, then service asc, then operation asc.
func TestMergeFleetOperations_SortOrder(t *testing.T) {
	mk := func(job, op string, rate, avg float64) (opAggKey, groupBucket, groupBucket) {
		k := opAggKey{name: op, groupKey: job}
		return k, groupBucket{value: rate, labels: map[string]string{"job": job}}, groupBucket{value: avg, labels: map[string]string{"job": job}}
	}

	k1, r1, a1 := mk("svcB", "GET /low", 1, 0.001) // busy 0.001
	k2, r2, a2 := mk("svcA", "GET /high", 10, 1)   // busy 10
	k3, r3, a3 := mk("svcA", "GET /mid-b", 1, 1)   // busy 1
	k4, r4, a4 := mk("svcA", "GET /mid-a", 1, 1)   // busy 1, tie with mid-b, service asc then op asc

	rates := map[opAggKey]groupBucket{k1: r1, k2: r2, k3: r3, k4: r4}
	avgs := map[opAggKey]groupBucket{k1: a1, k2: a2, k3: a3, k4: a4}

	got := mergeFleetOperations(rates, nil, avgs, nil, nil, nil, 100, nil)
	wantOrder := []string{"GET /high", "GET /mid-a", "GET /mid-b", "GET /low"}
	if len(got) != len(wantOrder) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(wantOrder))
	}
	for i, name := range wantOrder {
		if got[i].Name != name {
			t.Errorf("got[%d].Name = %q, want %q (full order: %v)", i, got[i].Name, name, namesOf(got))
		}
	}
}

func namesOf(ops []FleetOperation) []string {
	out := make([]string, len(ops))
	for i, o := range ops {
		out[i] = o.Name
	}
	return out
}

// TestMergeFleetOperations_ServiceNamespaceFromJob confirms Service/Namespace
// are parsed from the job label carried in the buckets.
func TestMergeFleetOperations_ServiceNamespaceFromJob(t *testing.T) {
	key := opAggKey{name: "GET /a"}
	rates := map[opAggKey]groupBucket{key: {value: 1, labels: map[string]string{"job": "payments/checkout"}}}

	got := mergeFleetOperations(rates, nil, nil, nil, nil, nil, 1, nil)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Service != "checkout" || got[0].Namespace != "payments" {
		t.Errorf("Service/Namespace = %q/%q, want checkout/payments", got[0].Service, got[0].Namespace)
	}
}

// TestMergeFleetOperations_HasErrorsInferredFromTraffic mirrors
// mergeOperations' rule: a row with traffic but no error series is 0
// errors (measured), not "unknown".
func TestMergeFleetOperations_HasErrorsInferredFromTraffic(t *testing.T) {
	key := opAggKey{name: "GET /a"}
	rates := map[opAggKey]groupBucket{key: {value: 5, labels: map[string]string{"job": "svcA"}}}

	got := mergeFleetOperations(rates, nil, nil, nil, nil, nil, 1, nil)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if !got[0].HasErrors || got[0].ErrorPercent != 0 {
		t.Errorf("HasErrors/ErrorPercent = %v/%v, want true/0", got[0].HasErrors, got[0].ErrorPercent)
	}
}
