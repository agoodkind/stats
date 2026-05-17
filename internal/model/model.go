package model

import "time"

type RepositorySource string

const (
	RepositorySourceOwned    RepositorySource = "owned"
	RepositorySourceExternal RepositorySource = "external"
)

type ViewerSummary struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

type LanguageStat struct {
	Name       string  `json:"name"`
	Color      string  `json:"color"`
	Bytes      int     `json:"bytes"`
	Weighted   float64 `json:"weighted"`
	Percentage float64 `json:"percentage"`
}

type RepositoryLanguage struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	Bytes int    `json:"bytes"`
}

type Repository struct {
	NameWithOwner string               `json:"nameWithOwner"`
	Source        RepositorySource     `json:"source"`
	IsFork        bool                 `json:"isFork"`
	IsArchived    bool                 `json:"isArchived"`
	IsDisabled    bool                 `json:"isDisabled"`
	Stars         int                  `json:"stars"`
	Forks         int                  `json:"forks"`
	PushedAt      time.Time            `json:"pushedAt"`
	UpdatedAt     time.Time            `json:"updatedAt"`
	Languages     []RepositoryLanguage `json:"languages"`
}

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

type RepoActivity struct {
	RepositoryName string  `json:"repositoryName"`
	Commits        int     `json:"commits"`
	Stars          int     `json:"stars"`
	Score          float64 `json:"score"`
}

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

type OverviewStats struct {
	Name               string `json:"name"`
	Stars              int    `json:"stars"`
	Forks              int    `json:"forks"`
	TotalContributions int    `json:"totalContributions"`
	LinesChanged       int    `json:"linesChanged"`
	Views              int    `json:"views"`
	RepositoryCount    int    `json:"repositoryCount"`
}

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

type StatsSummary struct {
	Overview     OverviewStats     `json:"overview"`
	Languages    []LanguageStat    `json:"languages"`
	TopRepos     []RepoActivity    `json:"topRepos"`
	Diagnostics  DiagnosticsReport `json:"diagnostics"`
	Repositories []Repository      `json:"repositories"`
}
