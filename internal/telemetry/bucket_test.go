package telemetry_test

import (
	"testing"

	"github.com/grafana/gcx/internal/telemetry"
	"github.com/stretchr/testify/assert"
)

func TestBucket(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		n    int
		want string
	}{
		{"empty batch is its own answer", 0, "0"},
		{"single item", 1, "1"},
		{"small batch lower edge", 2, "2-5"},
		{"small batch upper edge", 5, "2-5"},
		{"folder-sized lower edge", 6, "6-20"},
		{"folder-sized upper edge", 20, "6-20"},
		{"large lower edge", 21, "21-100"},
		{"large upper edge", 100, "21-100"},
		{"very large lower edge", 101, "101-1000"},
		// 1000 belongs to 101-1000 and nothing else. An earlier draft of this
		// vocabulary used "1000+" for the top bucket, which overlapped here.
		{"very large upper edge", 1000, "101-1000"},
		{"top bucket starts above 1000", 1001, "1001+"},
		{"top bucket is unbounded", 999999, "1001+"},
		// Cannot occur for a finalized summary, but telemetry must never
		// affect the command's outcome, so it degrades instead of panicking.
		{"negative degrades to zero", -1, "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, telemetry.Bucket(tt.n))
		})
	}
}

// Every count must land in exactly one declared bucket: no gaps that would
// produce an undeclared value, no overlaps that would make a count ambiguous.
func TestBucketVocabularyIsTotalAndClosed(t *testing.T) {
	t.Parallel()

	declared := make(map[string]bool, len(telemetry.Buckets()))
	for _, label := range telemetry.Buckets() {
		assert.False(t, declared[label], "duplicate bucket label %q", label)
		declared[label] = true
	}

	seen := make(map[string]bool, len(declared))
	for n := -5; n <= 1500; n++ {
		label := telemetry.Bucket(n)
		assert.True(t, declared[label], "Bucket(%d) returned undeclared label %q", n, label)
		seen[label] = true
	}

	for _, label := range telemetry.Buckets() {
		assert.True(t, seen[label], "bucket %q is declared but unreachable", label)
	}
}

// Bucket boundaries must be monotonic: a larger count never reports a smaller
// bucket. Guards against a future reordering of the switch arms.
func TestBucketIsMonotonic(t *testing.T) {
	t.Parallel()

	order := make(map[string]int, len(telemetry.Buckets()))
	for i, label := range telemetry.Buckets() {
		order[label] = i
	}

	prev := 0
	for n := range 1501 {
		got := order[telemetry.Bucket(n)]
		assert.GreaterOrEqual(t, got, prev, "Bucket(%d) went backwards", n)
		prev = got
	}
}
