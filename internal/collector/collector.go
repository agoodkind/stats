// Package collector aggregates GitHub-sourced repository data into the
// rendered StatsSummary and diagnostics report, applying repo-exclusion and
// recency-weighting rules along the way.
package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	internalconfig "github.com/agoodkind/stats/internal/config"
	internalmodel "github.com/agoodkind/stats/internal/model"
	internalviewshistory "github.com/agoodkind/stats/internal/viewshistory"
)

const diagnosticsScope = "all-time owned repositories, recency weighted"

type githubService interface {
	FetchViewerRepositories(ctx context.Context) (internalmodel.ViewerSummary, []internalmodel.Repository, []internalmodel.Repository, error)
	FetchTotalContributions(ctx context.Context) (int, error)
	FetchContributorActivity(ctx context.Context, repositories []internalmodel.Repository, now time.Time, halfLife time.Duration, floor float64) ([]internalmodel.RepoActivity, int, int, error)
	FetchViews(ctx context.Context, repositories []internalmodel.Repository) (map[string]map[string]int, error)
	EstimateExternalContributions(ctx context.Context, repositories []internalmodel.Repository) ([]internalmodel.ExternalContributionEstimate, error)
}

// ViewsHistoryPath is the location under generated/ where lifetime view
// counts accumulate across CI runs. The bot commits this alongside the
// SVGs so each subsequent run can merge the freshest 14-day window into
// the running total.
const ViewsHistoryPath = "generated/views_history.json"

// Collector orchestrates the GitHub API fetches and aggregation rules that
// produce a StatsSummary.
type Collector struct {
	client           githubService
	now              func() time.Time
	viewsHistoryPath string
}

type languageAccumulator struct {
	name     string
	color    string
	bytes    int
	weighted float64
}

// New returns a Collector wired to the given GitHub service, using [time.Now]
// for recency calculations and the default ViewsHistoryPath for accumulated
// repo views.
func New(client githubService) *Collector {
	return &Collector{client: client, now: time.Now, viewsHistoryPath: ViewsHistoryPath}
}

// Collect runs every GitHub fetch in sequence and assembles a StatsSummary
// plus a detailed diagnostics report.
func (collector *Collector) Collect(ctx context.Context, cfg internalconfig.Config) (internalmodel.StatsSummary, error) {
	viewer, ownedRepositories, externalRepositories, err := collector.client.FetchViewerRepositories(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "fetch viewer repositories", "error", err)
		return internalmodel.StatsSummary{}, fmt.Errorf("fetch viewer repositories: %w", err)
	}
	// publicOwned strips private repos out of every downstream call so their
	// names, traffic counts, and aggregate totals never reach the rendered
	// SVGs or the committed views_history.json.
	publicOwned := filterPublic(ownedRepositories)

	contributionCount, err := collector.client.FetchTotalContributions(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "fetch total contributions", "error", err)
		return internalmodel.StatsSummary{}, fmt.Errorf("fetch total contributions: %w", err)
	}
	activities, additions, deletions, err := collector.client.FetchContributorActivity(ctx, publicOwned, collector.now(), cfg.RecencyHalfLife, cfg.RecencyFloor)
	if err != nil {
		slog.ErrorContext(ctx, "fetch contributor activity", "error", err)
		return internalmodel.StatsSummary{}, fmt.Errorf("fetch contributor activity: %w", err)
	}
	repoCommitWeights := buildCommitWeightMap(activities, cfg.RecencyFloor)
	weightedOwned, rawOwned, decisions, includedOwnedCount := collector.collectLanguages(cfg, ownedRepositories, repoCommitWeights)
	freshViews, err := collector.client.FetchViews(ctx, publicOwned)
	if err != nil {
		slog.ErrorContext(ctx, "fetch views", "error", err)
		return internalmodel.StatsSummary{}, fmt.Errorf("fetch views: %w", err)
	}
	views := 0
	if collector.viewsHistoryPath != "" {
		viewsHistory, err := internalviewshistory.Load(collector.viewsHistoryPath)
		if err != nil {
			slog.ErrorContext(ctx, "load views history", "path", collector.viewsHistoryPath, "error", err)
			return internalmodel.StatsSummary{}, fmt.Errorf("load views history: %w", err)
		}
		viewsHistory = internalviewshistory.Merge(viewsHistory, freshViews)
		if err := internalviewshistory.Save(collector.viewsHistoryPath, viewsHistory); err != nil {
			slog.ErrorContext(ctx, "save views history", "path", collector.viewsHistoryPath, "error", err)
			return internalmodel.StatsSummary{}, fmt.Errorf("save views history: %w", err)
		}
		views = internalviewshistory.Total(viewsHistory)
	}
	externalEstimates, err := collector.client.EstimateExternalContributions(ctx, externalRepositories)
	if err != nil {
		slog.ErrorContext(ctx, "estimate external contributions", "error", err)
		return internalmodel.StatsSummary{}, fmt.Errorf("estimate external contributions: %w", err)
	}
	collector.applyExternalWeights(cfg, externalRepositories, externalEstimates)
	externalEstimatedLanguage := aggregateExternalLanguages(cfg, externalEstimates)
	effectiveLanguage := weightedOwned
	if cfg.IncludeExternal {
		effectiveLanguage = mergeLanguageStats(weightedOwned, externalEstimatedLanguage)
	}

	topRepos := collector.rankTopRepos(cfg, publicOwned, activities)

	sort.Slice(externalEstimates, func(left int, right int) bool {
		if externalEstimates[left].WeightedEstimatedBytes == externalEstimates[right].WeightedEstimatedBytes {
			return externalEstimates[left].RepositoryName < externalEstimates[right].RepositoryName
		}
		return externalEstimates[left].WeightedEstimatedBytes > externalEstimates[right].WeightedEstimatedBytes
	})

	displayName := strings.TrimSpace(viewer.Name)
	if displayName == "" {
		displayName = viewer.Login
	}

	return internalmodel.StatsSummary{
		Overview: internalmodel.OverviewStats{
			Name:               displayName,
			Stars:              sumRepositoryStars(publicOwned),
			Forks:              sumRepositoryForks(publicOwned),
			TotalContributions: contributionCount,
			LinesChanged:       additions + deletions,
			Views:              views,
			RepositoryCount:    len(publicOwned),
		},
		Languages: effectiveLanguage,
		TopRepos:  topRepos,
		Diagnostics: internalmodel.DiagnosticsReport{
			Scope:                     diagnosticsScope,
			Summary:                   buildDiagnosticsSummary(cfg, len(ownedRepositories), len(externalRepositories), includedOwnedCount, weightedOwned, externalEstimates),
			WeightedOwnedLanguage:     weightedOwned,
			RawOwnedLanguage:          rawOwned,
			ExternalEstimatedLanguage: externalEstimatedLanguage,
			EffectiveLanguage:         effectiveLanguage,
			ExternalRepositories:      externalRepositories,
			ExternalEstimates:         externalEstimates,
			Decisions:                 decisions,
		},
		Repositories: ownedRepositories,
	}, nil
}

const topRepoLimit = 6

// filterPublic returns only repositories whose IsPrivate is false. Used to
// strip private repos out of every downstream call so their names, traffic
// counts, and aggregate totals never reach the rendered SVGs or the
// committed views_history.json.
func filterPublic(repositories []internalmodel.Repository) []internalmodel.Repository {
	publicRepos := make([]internalmodel.Repository, 0, len(repositories))
	for _, repository := range repositories {
		if repository.IsPrivate {
			continue
		}
		publicRepos = append(publicRepos, repository)
	}
	return publicRepos
}

// buildCommitWeightMap maps each repository to its average per-commit recency
// weight (weightedCommits / commits). The result is used as the language byte
// multiplier so a repo's language bytes are reduced in proportion to how old
// its commits are.
func buildCommitWeightMap(activities []internalmodel.RepoActivity, floor float64) map[string]float64 {
	weights := make(map[string]float64, len(activities))
	for _, activity := range activities {
		if activity.Commits <= 0 {
			continue
		}
		avg := activity.WeightedCommits / float64(activity.Commits)
		if avg < floor {
			avg = floor
		}
		weights[activity.RepositoryName] = avg
	}
	return weights
}

func (collector *Collector) rankTopRepos(cfg internalconfig.Config, ownedRepositories []internalmodel.Repository, activities []internalmodel.RepoActivity) []internalmodel.RepoActivity {
	repositoryByName := make(map[string]internalmodel.Repository, len(ownedRepositories))
	for _, repository := range ownedRepositories {
		repositoryByName[repository.NameWithOwner] = repository
	}

	ranked := make([]internalmodel.RepoActivity, 0, len(activities))
	for _, activity := range activities {
		repository, found := repositoryByName[activity.RepositoryName]
		if !found {
			continue
		}
		if repositoryExclusionReason(cfg, repository, sumRepositoryBytes(repository)) != "" {
			continue
		}
		activity.Stars = repository.Stars
		activity.Score = math.Log10(1+activity.WeightedCommits) + math.Log10(1+float64(activity.Stars))
		activity.Description = repository.Description
		activity.LangColor = primaryLanguageColor(repository)
		activity.UpdatedAgo = humanizeAge(collector.now().Sub(repository.PushedAt))
		ranked = append(ranked, activity)
	}

	sort.Slice(ranked, func(left int, right int) bool {
		if ranked[left].Score == ranked[right].Score {
			return ranked[left].RepositoryName < ranked[right].RepositoryName
		}
		return ranked[left].Score > ranked[right].Score
	})
	if len(ranked) > topRepoLimit {
		ranked = ranked[:topRepoLimit]
	}
	return ranked
}

func primaryLanguageColor(repository internalmodel.Repository) string {
	if len(repository.Languages) == 0 {
		return ""
	}
	return strings.TrimSpace(repository.Languages[0].Color)
}

// humanizeAge renders a Duration as a short relative-time string the SVG can
// show without needing client-side JS: "today" / "Xd ago" / "Xw ago" /
// "Xmo ago" / "Xy ago".
func humanizeAge(age time.Duration) string {
	if age < 0 {
		return "today"
	}
	days := int(age.Hours() / 24)
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "1d ago"
	case days < 14:
		return fmt.Sprintf("%dd ago", days)
	case days < 60:
		return fmt.Sprintf("%dw ago", days/7)
	case days < 730:
		return fmt.Sprintf("%dmo ago", days/30)
	default:
		return fmt.Sprintf("%dy ago", days/365)
	}
}

func (collector *Collector) collectLanguages(cfg internalconfig.Config, repositories []internalmodel.Repository, repoCommitWeights map[string]float64) ([]internalmodel.LanguageStat, []internalmodel.LanguageStat, []internalmodel.InclusionDecision, int) {
	weighted := make(map[string]*languageAccumulator)
	raw := make(map[string]*languageAccumulator)
	decisions := make([]internalmodel.InclusionDecision, 0, len(repositories))
	includedCount := 0

	for _, repository := range repositories {
		rawBytes := sumRepositoryBytes(repository)
		decision := internalmodel.InclusionDecision{
			RepositoryName: repository.NameWithOwner,
			Source:         repository.Source,
			RawBytes:       rawBytes,
			PushedAt:       repository.PushedAt,
			UpdatedAt:      repository.UpdatedAt,
		}

		reason := repositoryExclusionReason(cfg, repository, rawBytes)
		if reason != "" {
			decision.Reason = reason
			decisions = append(decisions, decision)
			continue
		}

		recencyWeight, ok := repoCommitWeights[repository.NameWithOwner]
		if !ok {
			recencyWeight = collector.recencyWeight(cfg, repository)
		}
		decision.Included = true
		decision.Reason = "recency_weighted"
		decision.RecencyWeight = recencyWeight
		decision.WeightedBytes = float64(rawBytes) * recencyWeight
		decisions = append(decisions, decision)
		includedCount += 1

		for _, language := range repository.Languages {
			if _, excluded := cfg.ExcludedLangs[strings.ToLower(language.Name)]; excluded {
				continue
			}
			weightedAccumulator := getAccumulator(weighted, language.Name, language.Color)
			weightedAccumulator.bytes += language.Bytes
			weightedAccumulator.weighted += float64(language.Bytes) * recencyWeight

			rawAccumulator := getAccumulator(raw, language.Name, language.Color)
			rawAccumulator.bytes += language.Bytes
			rawAccumulator.weighted += float64(language.Bytes)
		}
	}

	return finalizeLanguages(weighted), finalizeLanguages(raw), decisions, includedCount
}

func repositoryExclusionReason(cfg internalconfig.Config, repository internalmodel.Repository, rawBytes int) string {
	if repository.IsPrivate {
		return "private"
	}
	if repository.IsArchived {
		return "archived"
	}
	if repository.IsDisabled {
		return "disabled"
	}
	if cfg.ExcludeForks && repository.IsFork {
		return "fork"
	}
	if _, excluded := cfg.ExcludedRepos[repository.NameWithOwner]; excluded {
		return "excluded_repo"
	}
	if rawBytes == 0 {
		return "missing_language_data"
	}
	return ""
}

func (collector *Collector) applyExternalWeights(cfg internalconfig.Config, repositories []internalmodel.Repository, estimates []internalmodel.ExternalContributionEstimate) {
	repoByName := make(map[string]internalmodel.Repository, len(repositories))
	for _, repository := range repositories {
		repoByName[repository.NameWithOwner] = repository
	}
	for index, estimate := range estimates {
		repository, found := repoByName[estimate.RepositoryName]
		if !found {
			continue
		}
		recencyWeight := collector.recencyWeight(cfg, repository)
		estimates[index].RecencyWeight = recencyWeight
		totalWeighted := 0.0
		for languageIndex, language := range estimates[index].Languages {
			weightedValue := language.Weighted * recencyWeight
			estimates[index].Languages[languageIndex].Weighted = weightedValue
			totalWeighted += weightedValue
		}
		estimates[index].WeightedEstimatedBytes = estimates[index].RawEstimatedBytes * recencyWeight
		if totalWeighted <= 0 {
			continue
		}
		for languageIndex, language := range estimates[index].Languages {
			estimates[index].Languages[languageIndex].Percentage = 100 * language.Weighted / totalWeighted
		}
	}
}

func buildDiagnosticsSummary(cfg internalconfig.Config, ownedCount int, externalCount int, includedOwnedCount int, weightedOwned []internalmodel.LanguageStat, externalEstimates []internalmodel.ExternalContributionEstimate) internalmodel.DiagnosticsSummary {
	estimatedExternalCount := 0
	unknownExternalCount := 0
	externalWeightedBytes := 0.0
	for _, estimate := range externalEstimates {
		externalWeightedBytes += estimate.WeightedEstimatedBytes
		if estimate.Confidence == "unknown" {
			unknownExternalCount += 1
			continue
		}
		estimatedExternalCount += 1
	}

	return internalmodel.DiagnosticsSummary{
		OwnedRepositoryCount:    ownedCount,
		ExternalRepositoryCount: externalCount,
		IncludedOwnedCount:      includedOwnedCount,
		ExcludedOwnedCount:      ownedCount - includedOwnedCount,
		IncludeExternal:         cfg.IncludeExternal,
		EstimatedExternalCount:  estimatedExternalCount,
		UnknownExternalCount:    unknownExternalCount,
		OwnedWeightedBytes:      sumLanguageWeighted(weightedOwned),
		ExternalWeightedBytes:   externalWeightedBytes,
	}
}

func (collector *Collector) recencyWeight(cfg internalconfig.Config, repository internalmodel.Repository) float64 {
	referenceTime := repository.PushedAt
	if referenceTime.IsZero() {
		referenceTime = repository.UpdatedAt
	}
	if referenceTime.IsZero() {
		return cfg.RecencyFloor
	}

	age := collector.now().Sub(referenceTime)
	if age <= 0 {
		return 1
	}
	halfLives := float64(age) / float64(cfg.RecencyHalfLife)
	weight := math.Pow(0.5, halfLives)
	if weight < cfg.RecencyFloor {
		return cfg.RecencyFloor
	}
	return weight
}

func getAccumulator(accumulators map[string]*languageAccumulator, name string, color string) *languageAccumulator {
	accumulator, found := accumulators[name]
	if found {
		return accumulator
	}
	accumulator = &languageAccumulator{name: name, color: color}
	accumulators[name] = accumulator
	return accumulator
}

func finalizeLanguages(accumulators map[string]*languageAccumulator) []internalmodel.LanguageStat {
	languages := make([]internalmodel.LanguageStat, 0, len(accumulators))
	totalWeighted := 0.0
	for _, accumulator := range accumulators {
		totalWeighted += accumulator.weighted
	}
	for _, accumulator := range accumulators {
		percentage := 0.0
		if totalWeighted > 0 {
			percentage = 100 * accumulator.weighted / totalWeighted
		}
		languages = append(languages, internalmodel.LanguageStat{
			Name:       accumulator.name,
			Color:      fallbackColor(accumulator.color),
			Bytes:      accumulator.bytes,
			Weighted:   accumulator.weighted,
			Percentage: percentage,
		})
	}

	sort.Slice(languages, func(left int, right int) bool {
		if languages[left].Weighted == languages[right].Weighted {
			return languages[left].Name < languages[right].Name
		}
		return languages[left].Weighted > languages[right].Weighted
	})
	return languages
}

func aggregateExternalLanguages(cfg internalconfig.Config, estimates []internalmodel.ExternalContributionEstimate) []internalmodel.LanguageStat {
	accumulators := make(map[string]*languageAccumulator)
	for _, estimate := range estimates {
		for _, language := range estimate.Languages {
			if _, excluded := cfg.ExcludedLangs[strings.ToLower(language.Name)]; excluded {
				continue
			}
			accumulator := getAccumulator(accumulators, language.Name, language.Color)
			accumulator.bytes += language.Bytes
			accumulator.weighted += language.Weighted
		}
	}
	return finalizeLanguages(accumulators)
}

func mergeLanguageStats(base []internalmodel.LanguageStat, extras []internalmodel.LanguageStat) []internalmodel.LanguageStat {
	accumulators := make(map[string]*languageAccumulator)
	for _, language := range base {
		accumulator := getAccumulator(accumulators, language.Name, language.Color)
		accumulator.bytes += language.Bytes
		accumulator.weighted += language.Weighted
	}
	for _, language := range extras {
		accumulator := getAccumulator(accumulators, language.Name, language.Color)
		accumulator.bytes += language.Bytes
		accumulator.weighted += language.Weighted
	}
	return finalizeLanguages(accumulators)
}

func sumLanguageWeighted(languages []internalmodel.LanguageStat) float64 {
	total := 0.0
	for _, language := range languages {
		total += language.Weighted
	}
	return total
}

func fallbackColor(color string) string {
	if strings.TrimSpace(color) == "" {
		return "#000000"
	}
	return color
}

func sumRepositoryBytes(repository internalmodel.Repository) int {
	total := 0
	for _, language := range repository.Languages {
		total += language.Bytes
	}
	return total
}

func sumRepositoryStars(repositories []internalmodel.Repository) int {
	total := 0
	for _, repository := range repositories {
		total += repository.Stars
	}
	return total
}

func sumRepositoryForks(repositories []internalmodel.Repository) int {
	total := 0
	for _, repository := range repositories {
		total += repository.Forks
	}
	return total
}

// FormatDiagnostics renders a DiagnosticsReport as pretty-printed JSON
// suitable for the diagnose subcommand's output.
func FormatDiagnostics(report internalmodel.DiagnosticsReport) string {
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Sprintf("{\n  \"error\": %q\n}\n", err.Error())
	}
	return string(encoded) + "\n"
}
