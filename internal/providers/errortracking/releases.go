package errortracking

type release struct {
	Release    string `json:"release"`
	FirstSeen  string `json:"firstSeen"`
	LastSeen   string `json:"lastSeen"`
	EventCount int64  `json:"eventCount"`
}

const facetCap = 100
