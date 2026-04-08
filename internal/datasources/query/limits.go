package query

const (
	// DefaultLokiLimit is the legacy/default result cap used for machine-oriented
	// formats unless the user explicitly overrides --limit.
	DefaultLokiLimit = 1000

	// HumanLokiLimit is the smaller default used for human-oriented table views
	// when --limit is omitted.
	HumanLokiLimit = 50
)

// EffectiveLokiLimit returns the limit to send to Loki for the current output
// format. Explicit --limit always wins. When omitted, human-oriented formats
// use a smaller default to avoid overwhelming tables.
func EffectiveLokiLimit(limit int, outputFormat string, limitFlagChanged bool) int {
	if limitFlagChanged {
		return limit
	}

	switch outputFormat {
	case "table", "wide":
		return HumanLokiLimit
	default:
		return DefaultLokiLimit
	}
}
