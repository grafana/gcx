package telemetry

// Bucket labels for the batch volume fields. These are the complete vocabulary:
// a bucket field never carries any other value, and no raw numeric count field
// is sent alongside them.
//
// Be precise about what this does and does not hide. BucketZero and BucketOne
// are singleton categories: a batch of 0 or of 1 is recoverable exactly from the
// label. That is deliberate — "matched nothing" and "one resource" are the two
// answers worth distinguishing on their own, and neither describes an inventory
// — but it means "volumes are never exact" is false as a blanket claim, and must
// not be written anywhere. Everything above 1 is a range.
//
// The reason for ranges at all is correlation: events carry a persistent
// per-install device ID and the receiver adds the network organisation name from
// a whois lookup, so an exact count of a large batch would attribute a precise
// managed-resource inventory to a named organisation. A range keeps the
// questions we actually ask answerable — batch-size distribution, single-item
// versus real batch work, how failure rate varies with size — without carrying
// that inference. The singletons carry no inventory to infer.
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

// Bucket maps a count to its bucket label. Every range is closed at both ends
// and none overlap, so each count falls in exactly one bucket: "2-5" contains
// both 2 and 5, and "101-1000" contains 1000.
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
