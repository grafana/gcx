package kg

// EntityKey identifies a Knowledge Graph entity.
type EntityKey struct {
	Type  string         `json:"type"`
	Name  string         `json:"name"`
	Scope map[string]any `json:"scope,omitempty"`
}

// LLMSummaryRequest is the request body for POST /v1/assertions/llm-summary.
type LLMSummaryRequest struct {
	StartTime                                     int64       `json:"startTime"`
	EndTime                                       int64       `json:"endTime"`
	EntityKeys                                    []EntityKey `json:"entityKeys"`
	SuggestionSrcEntities                         []EntityKey `json:"suggestionSrcEntities"`
	AlertCategories                               []string    `json:"alertCategories,omitempty"`
	HideAssertionsOlderThanNHours                 int         `json:"hideAssertionsOlderThanNHours"`
	HideAssertionsPresentMoreThanPercentageOfTime int         `json:"hideAssertionsPresentMoreThanPercentageOfTime"`
	IncludeSuggestions                            bool        `json:"includeSuggestions"`
	IncludeRcaPatterns                            bool        `json:"includeRcaPatterns"`
}
