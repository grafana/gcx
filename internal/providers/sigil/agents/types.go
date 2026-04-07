package agents

import "time"

// Agent is a list item from GET /query/agents.
type Agent struct {
	AgentName              string        `json:"agent_name"`
	LatestEffectiveVersion string        `json:"latest_effective_version"`
	LatestDeclaredVersion  *string       `json:"latest_declared_version,omitempty"`
	FirstSeenAt            time.Time     `json:"first_seen_at"`
	LatestSeenAt           time.Time     `json:"latest_seen_at"`
	GenerationCount        int64         `json:"generation_count"`
	VersionCount           int           `json:"version_count"`
	ToolCount              int           `json:"tool_count"`
	SystemPromptPrefix     string        `json:"system_prompt_prefix"`
	TokenEstimate          TokenEstimate `json:"token_estimate"`
}

// AgentDetail is the response from GET /query/agents/lookup.
// Uses map[string]any because the shape includes nested objects
// (models, tools, system_prompt) that vary.
type AgentDetail map[string]any

// AgentVersion is a version item from GET /query/agents/versions.
type AgentVersion struct {
	EffectiveVersion      string        `json:"effective_version"`
	DeclaredVersionFirst  string        `json:"declared_version_first,omitempty"`
	DeclaredVersionLatest string        `json:"declared_version_latest,omitempty"`
	FirstSeenAt           time.Time     `json:"first_seen_at"`
	LastSeenAt            time.Time     `json:"last_seen_at"`
	GenerationCount       int64         `json:"generation_count"`
	ToolCount             int           `json:"tool_count"`
	SystemPromptPrefix    string        `json:"system_prompt_prefix"`
	TokenEstimate         TokenEstimate `json:"token_estimate"`
}

// TokenEstimate holds token count estimates for an agent.
type TokenEstimate struct {
	SystemPrompt int `json:"system_prompt"`
	ToolsTotal   int `json:"tools_total"`
	Total        int `json:"total"`
}
