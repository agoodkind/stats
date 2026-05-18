// Package model defines the data shapes shared across the GitHub fetch,
// aggregation, rendering, and diagnostics layers of stats-gh.
package model

import "time"

// RepositorySource distinguishes repositories the viewer owns from those they
// merely contribute to.
type RepositorySource string

const (
	// RepositorySourceOwned marks a repository that the configured actor owns.
	RepositorySourceOwned RepositorySource = "owned"
	// RepositorySourceExternal marks a repository the actor only contributes
	// to and does not own.
	RepositorySourceExternal RepositorySource = "external"
)

// ViewerSummary holds the authenticated user's display identity used to title
// the rendered SVGs.
type ViewerSummary struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

// LanguageStat is one slice of the language-breakdown pie, with both raw and
// recency-weighted byte totals plus the rendered percentage.
type LanguageStat struct {
	Name       string  `json:"name"`
	Color      string  `json:"color"`
	Bytes      int     `json:"bytes"`
	Weighted   float64 `json:"weighted"`
	Percentage float64 `json:"percentage"`
}

// RepositoryLanguage carries a single language's byte count for one
// repository, as reported by the GraphQL languages edge.
type RepositoryLanguage struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	Bytes int    `json:"bytes"`
}

// Repository is the post-fetch projection of a GitHub repository node, with
// only the fields stats-gh uses.
type Repository struct {
	NameWithOwner string               `json:"nameWithOwner"`
	Source        RepositorySource     `json:"source"`
	IsFork        bool                 `json:"isFork"`
	IsArchived    bool                 `json:"isArchived"`
	IsDisabled    bool                 `json:"isDisabled"`
	IsPrivate     bool                 `json:"isPrivate"`
	Stars         int                  `json:"stars"`
	Forks         int                  `json:"forks"`
	PushedAt      time.Time            `json:"pushedAt"`
	UpdatedAt     time.Time            `json:"updatedAt"`
	Languages     []RepositoryLanguage `json:"languages"`
}

// InclusionDecision records why a repository was included in or excluded from
// the aggregated language stats, for diagnostics output.
type InclusionDecision struct {
	RepositoryName string           `json:"repositoryName"`
	Source         RepositorySource `json:"source"`
	Included       bool             `json:"included"`
	Reason         string           `json:"reason"`
	RawBytes       int              `json:"rawBytes"`
	WeightedBytes  float64          `json:"weightedBytes"`
	RecencyWeight  float64          `json:"recencyWeight"`
	PushedAt       time.Time        `json:"pushedAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
}

// RepoActivity is one row of the top-repos chart: a repository plus its raw
// commit count, the same count weighted by per-commit recency, stars, and the
// final composite score.
type RepoActivity struct {
	RepositoryName  string  `json:"repositoryName"`
	Commits         int     `json:"commits"`
	WeightedCommits float64 `json:"weightedCommits"`
	Stars           int     `json:"stars"`
	Score           float64 `json:"score"`
}

// ExternalContributionEstimate represents an approximated share of an external
// repository's language bytes that the viewer is credited with, derived from
// contributor stats.
type ExternalContributionEstimate struct {
	RepositoryName         string         `json:"repositoryName"`
	Method                 string         `json:"method"`
	Confidence             string         `json:"confidence"`
	EstimatedRatio         float64        `json:"estimatedRatio"`
	RawEstimatedBytes      float64        `json:"rawEstimatedBytes"`
	WeightedEstimatedBytes float64        `json:"weightedEstimatedBytes"`
	RecencyWeight          float64        `json:"recencyWeight"`
	Languages              []LanguageStat `json:"languages"`
	EstimateNote           string         `json:"estimateNote"`
}

// OverviewStats backs the overview SVG: totals across the viewer's owned
// repositories.
type OverviewStats struct {
	Name               string `json:"name"`
	Stars              int    `json:"stars"`
	Forks              int    `json:"forks"`
	TotalContributions int    `json:"totalContributions"`
	LinesChanged       int    `json:"linesChanged"`
	Views              int    `json:"views"`
	RepositoryCount    int    `json:"repositoryCount"`
}

// DiagnosticsSummary is the high-level scoreboard at the top of the
// diagnostics report (counts, totals, flags).
type DiagnosticsSummary struct {
	OwnedRepositoryCount    int     `json:"ownedRepositoryCount"`
	ExternalRepositoryCount int     `json:"externalRepositoryCount"`
	IncludedOwnedCount      int     `json:"includedOwnedCount"`
	ExcludedOwnedCount      int     `json:"excludedOwnedCount"`
	IncludeExternal         bool    `json:"includeExternal"`
	EstimatedExternalCount  int     `json:"estimatedExternalCount"`
	UnknownExternalCount    int     `json:"unknownExternalCount"`
	OwnedWeightedBytes      float64 `json:"ownedWeightedBytes"`
	ExternalWeightedBytes   float64 `json:"externalWeightedBytes"`
}

// DiagnosticsReport is the full breakdown the diagnose subcommand emits: per
// repository decisions, language stats both weighted and raw, and external
// estimates.
type DiagnosticsReport struct {
	Scope                     string                         `json:"scope"`
	Summary                   DiagnosticsSummary             `json:"summary"`
	WeightedOwnedLanguage     []LanguageStat                 `json:"weightedOwnedLanguage"`
	RawOwnedLanguage          []LanguageStat                 `json:"rawOwnedLanguage"`
	ExternalEstimatedLanguage []LanguageStat                 `json:"externalEstimatedLanguage"`
	EffectiveLanguage         []LanguageStat                 `json:"effectiveLanguage"`
	ExternalRepositories      []Repository                   `json:"externalRepositories"`
	ExternalEstimates         []ExternalContributionEstimate `json:"externalEstimates"`
	Decisions                 []InclusionDecision            `json:"decisions"`
}

// StatsSummary is the top-level result handed from the collector to the
// renderer, bundling everything needed to produce the SVGs and the
// diagnostics output.
type StatsSummary struct {
	Overview     OverviewStats     `json:"overview"`
	Languages    []LanguageStat    `json:"languages"`
	TopRepos     []RepoActivity    `json:"topRepos"`
	Diagnostics  DiagnosticsReport `json:"diagnostics"`
	Repositories []Repository      `json:"repositories"`
}
