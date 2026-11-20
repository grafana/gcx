// Package changereview is the client for Grafana Assistant change review:
// the checks it runs on GitHub pull requests and how those checks perform.
package changereview

import "time"

// Check is one change review check as the Assistant API serves it.
type Check struct {
	ID                  string                   `json:"id"`
	BuiltIn             bool                     `json:"builtIn"`
	Name                string                   `json:"name"`
	Description         string                   `json:"description,omitempty"`
	Instructions        string                   `json:"instructions"`
	Enabled             bool                     `json:"enabled"`
	Blocking            bool                     `json:"blocking"`
	Paths               []string                 `json:"paths"`
	RepositoryOverrides map[string]CheckOverride `json:"repositoryOverrides,omitempty"`
	AppliesTo           []string                 `json:"appliesTo,omitempty"`
}

// CheckOverride changes a check on one repository; unset fields keep the
// check's own setting.
type CheckOverride struct {
	Enabled  *bool `json:"enabled,omitempty"`
	Blocking *bool `json:"blocking,omitempty"`
}

// Checks is the tenant's full check list with the revision that guards saves.
type Checks struct {
	Revision string  `json:"revision"`
	Checks   []Check `json:"checks"`
}

// Window is the time range a statistics response covers.
type Window struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// StatsQuery narrows a statistics read. Zero times use the server defaults:
// the 30 days before now.
type StatsQuery struct {
	From       time.Time
	To         time.Time
	Repository string
}

// Totals counts the reviews in a window.
type Totals struct {
	Reviews        int `json:"reviews"`
	PullRequests   int `json:"pullRequests"`
	Incomplete     int `json:"incomplete"`
	Confirmed      int `json:"confirmed"`
	Fixed          int `json:"fixed"`
	StillPresent   int `json:"stillPresent"`
	AwaitingReview int `json:"awaitingReview"`
	Unknown        int `json:"unknown"`
	Holds          int `json:"holds"`
}

// CheckStats counts the issues one check raised.
type CheckStats struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Custom         bool   `json:"custom"`
	Blocking       bool   `json:"blocking"`
	Confirmed      int    `json:"confirmed"`
	Unconfirmed    int    `json:"unconfirmed"`
	RuledOut       int    `json:"ruledOut"`
	Fixed          int    `json:"fixed"`
	StillPresent   int    `json:"stillPresent"`
	AwaitingReview int    `json:"awaitingReview"`
	Unknown        int    `json:"unknown"`
	Holds          int    `json:"holds"`
}

// RepositoryStats counts the reviews of one repository.
type RepositoryStats struct {
	Repository string `json:"repository"`
	Reviews    int    `json:"reviews"`
	Confirmed  int    `json:"confirmed"`
	Fixed      int    `json:"fixed"`
	Incomplete int    `json:"incomplete"`
}

// IncompleteReason counts incomplete reviews by why they were incomplete.
type IncompleteReason struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// Stats summarizes published reviews in a window.
type Stats struct {
	Window            Window             `json:"window"`
	Truncated         bool               `json:"truncated"`
	Totals            Totals             `json:"totals"`
	Checks            []CheckStats       `json:"checks"`
	Repositories      []RepositoryStats  `json:"repositories"`
	IncompleteReasons []IncompleteReason `json:"incompleteReasons"`
}

// PullRequest is one pull request behind a check's statistics.
type PullRequest struct {
	Repository        string    `json:"repository"`
	PullNumber        int       `json:"pullNumber"`
	Reviews           int       `json:"reviews"`
	LatestReviewID    string    `json:"latestReviewId"`
	LatestPublishedAt time.Time `json:"latestPublishedAt"`
	Confirmed         int       `json:"confirmed"`
	Unconfirmed       int       `json:"unconfirmed"`
	RuledOut          int       `json:"ruledOut"`
	Fixed             int       `json:"fixed"`
	StillPresent      int       `json:"stillPresent"`
	AwaitingReview    int       `json:"awaitingReview"`
	Unknown           int       `json:"unknown"`
	Holds             int       `json:"holds"`
}

// PullRequests lists the pull requests behind a check's statistics.
type PullRequests struct {
	Window       Window        `json:"window"`
	Truncated    bool          `json:"truncated"`
	PullRequests []PullRequest `json:"pullRequests"`
}
