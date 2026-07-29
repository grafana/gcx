package telemetry

// Bucket labels for the batch volume fields. These are the complete vocabulary:
// a bucket field never carries any other value.
//
// Volumes are reported as buckets rather than exact counts on purpose. An exact
// resource count is operationally sensitive once it is correlated: events carry
// a persistent per-install device ID, and the receiver enriches them with the
// network organisation name from a whois lookup, so exact volumes would let a
// precise managed-resource inventory be attributed to a named organisation.
// Buckets keep every question we actually ask answerable — the batch-size
// distribution, single-item versus real batch work, how failure rate varies with
// size — without carrying that inference.
//
// If exact counts are ever approved, they must arrive as new numeric fields
// alongside these. Changing a bucket field from string to integer would change
// the type of a live column in the usage-stats schema.
const (
	BucketZero         = "0"
	BucketOne          = "1"
	BucketTwoToFive    = "2-5"
	BucketSixToTwenty  = "6-20"
	BucketToHundred    = "21-100"
	BucketToThousand   = "101-1000"
	BucketOverThousand = "1001+"
)

// Buckets returns every bucket label, for tests and for validating the
// vocabulary against the receiver.
func Buckets() []string {
	return []string{
		BucketZero, BucketOne, BucketTwoToFive, BucketSixToTwenty,
		BucketToHundred, BucketToThousand, BucketOverThousand,
	}
}

// Bucket maps a count to its bucket label. The ranges are half-open and do not
// overlap: each count falls in exactly one bucket.
//
// Zero is a real, distinct answer — a batch that matched nothing — so it gets
// its own label rather than being folded into the smallest range. Counts below
// zero cannot occur for a finalized summary, but report as "0" rather than
// panicking: telemetry must never affect the command's outcome.
func Bucket(n int) string {
	switch {
	case n <= 0:
		return BucketZero
	case n == 1:
		return BucketOne
	case n <= 5:
		return BucketTwoToFive
	case n <= 20:
		return BucketSixToTwenty
	case n <= 100:
		return BucketToHundred
	case n <= 1000:
		return BucketToThousand
	default:
		return BucketOverThousand
	}
}
