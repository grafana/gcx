package query

const (
	// DefaultLokiLimit is the default result cap for Loki queries when --limit
	// is not explicitly provided. A smaller value avoids overwhelming output;
	// use --limit 0 for no cap or --limit N for a custom value.
	DefaultLokiLimit = 50
)
