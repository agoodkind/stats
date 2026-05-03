package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	internalconfig "github.com/agoodkind/stats/internal/config"
	internalmodel "github.com/agoodkind/stats/internal/model"
)

const diagnosticsScope = "all-time owned repositories, recency weighted"

type githubService interface {
	FetchViewerRepositories(ctx context.Context) (internalmodel.ViewerSummary, []internalmodel.Repository, []internalmodel.Repository, error)
	FetchTotalContributions(ctx context.Context) (int, error)
	FetchContributorActivity(ctx context.Context, repositories []internalmodel.Repository) ([]internalmodel.RepoActivity, int, int, error)
	FetchViews(ctx context.Context, repositories []internalmodel.Repository) (int, error)
	EstimateExternalContributions(ctx context.Context, repositories []internalmodel.Repository) ([]internalmodel.ExternalContributionEstimate, error)
}

type Collector struct {
	client githubService
	now    func() time.Time
}

type languageAccumulator struct {
	name     string
	color    string
	bytes    int
	weighted float64
}

func New(client githubService) *Collector {
	return &Collector{client: client, now: time.Now}
}

func (collector *Collector) Collect(ctx context.Context, cfg internalconfig.Config) (internalmodel.StatsSummary, error) {
	viewer, ownedRepositories, externalRepositories, err := collector.client.FetchViewerRepositories(ctx)
	if err != nil {
		return internalmodel.StatsSummary{}, err
	}

	weightedOwned, rawOwned, decisions, includedOwnedCount := collector.collectLanguages(cfg, ownedRepositories)
	contributionCount, err := collector.client.FetchTotalContributions(ctx)
	if err != nil {
		return internalmodel.StatsSummary{}, err
	}
	activities, additions, deletions, err := collector.client.FetchContributorActivity(ctx, ownedRepositories)
	if err != nil {
		return internalmodel.StatsSummary{}, err
	}
	views, err := collector.client.FetchViews(ctx, ownedRepositories)
	if err != nil {
		return internalmodel.StatsSummary{}, err
	}
	externalEstimates, err := collector.client.EstimateExternalContributions(ctx, externalRepositories)
	if err != nil {
		return internalmodel.StatsSummary{}, err
	}
	collector.applyExternalWeights(cfg, externalRepositories, externalEstimates)
	externalEstimatedLanguage := aggregateExternalLanguages(cfg, externalEstimates)
	effectiveLanguage := weightedOwned
	if cfg.IncludeExternal {
		effectiveLanguage = mergeLanguageStats(weightedOwned, externalEstimatedLanguage)
	}

	sort.Slice(activities, func(left int, right int) bool {
		if activities[left].Activity == activities[right].Activity {
			return activities[left].RepositoryName < activities[right].RepositoryName
		}
		return activities[left].Activity > activities[right].Activity
	})
	if len(activities) > 6 {
		activities = activities[:6]
	}

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
			Stars:              sumRepositoryStars(ownedRepositories),
			Forks:              sumRepositoryForks(ownedRepositories),
			TotalContributions: contributionCount,
			LinesChanged:       additions + deletions,
			Views:              views,
			RepositoryCount:    len(ownedRepositories),
		},
		Languages: effectiveLanguage,
		TopRepos:  activities,
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

func (collector *Collector) collectLanguages(cfg internalconfig.Config, repositories []internalmodel.Repository) ([]internalmodel.LanguageStat, []internalmodel.LanguageStat, []internalmodel.InclusionDecision, int) {
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

		recencyWeight := collector.recencyWeight(cfg, repository)
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

func FormatDiagnostics(report internalmodel.DiagnosticsReport) string {
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Sprintf("{\n  \"error\": %q\n}\n", err.Error())
	}
	return string(encoded) + "\n"
}
