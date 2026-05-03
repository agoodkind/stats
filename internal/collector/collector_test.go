package collector

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	internalconfig "github.com/agoodkind/stats/internal/config"
	internalmodel "github.com/agoodkind/stats/internal/model"
)

const fixtureFloatTolerance = 0.000001

type collectorFixture struct {
	Now                  time.Time                                    `json:"now"`
	Config               collectorFixtureConfig                       `json:"config"`
	Viewer               internalmodel.ViewerSummary                  `json:"viewer"`
	OwnedRepositories    []internalmodel.Repository                   `json:"ownedRepositories"`
	ExternalRepositories []internalmodel.Repository                   `json:"externalRepositories"`
	ContributionCount    int                                          `json:"contributionCount"`
	Views                int                                          `json:"views"`
	Activities           []internalmodel.RepoActivity                 `json:"activities"`
	Additions            int                                          `json:"additions"`
	Deletions            int                                          `json:"deletions"`
	ExternalEstimates    []internalmodel.ExternalContributionEstimate `json:"externalEstimates"`
}

type collectorFixtureConfig struct {
	ExcludedRepos        []string `json:"excludedRepos"`
	ExcludedLangs        []string `json:"excludedLangs"`
	ExcludeForks         bool     `json:"excludeForks"`
	RecencyHalfLifeHours int      `json:"recencyHalfLifeHours"`
	RecencyFloor         float64  `json:"recencyFloor"`
}

type fakeGitHubService struct {
	viewer               internalmodel.ViewerSummary
	ownedRepositories    []internalmodel.Repository
	externalRepositories []internalmodel.Repository
	contributionCount    int
	activities           []internalmodel.RepoActivity
	additions            int
	deletions            int
	views                int
	externalEstimates    []internalmodel.ExternalContributionEstimate
}

func (service fakeGitHubService) FetchViewerRepositories(context.Context) (internalmodel.ViewerSummary, []internalmodel.Repository, []internalmodel.Repository, error) {
	return service.viewer, cloneRepositories(service.ownedRepositories), cloneRepositories(service.externalRepositories), nil
}

func (service fakeGitHubService) FetchTotalContributions(context.Context) (int, error) {
	return service.contributionCount, nil
}

func (service fakeGitHubService) FetchContributorActivity(context.Context, []internalmodel.Repository) ([]internalmodel.RepoActivity, int, int, error) {
	return append([]internalmodel.RepoActivity(nil), service.activities...), service.additions, service.deletions, nil
}

func (service fakeGitHubService) FetchViews(context.Context, []internalmodel.Repository) (int, error) {
	return service.views, nil
}

func (service fakeGitHubService) EstimateExternalContributions(context.Context, []internalmodel.Repository) ([]internalmodel.ExternalContributionEstimate, error) {
	return cloneExternalEstimates(service.externalEstimates), nil
}

func TestCollectFixtureSDKRegression(t *testing.T) {
	fixture := loadCollectorFixture(t)
	cfg := fixture.Config.toConfig()
	service := fakeGitHubService{
		viewer:               fixture.Viewer,
		ownedRepositories:    fixture.OwnedRepositories,
		externalRepositories: fixture.ExternalRepositories,
		contributionCount:    fixture.ContributionCount,
		activities:           fixture.Activities,
		additions:            fixture.Additions,
		deletions:            fixture.Deletions,
		views:                fixture.Views,
		externalEstimates:    fixture.ExternalEstimates,
	}
	collector := New(service)
	collector.now = func() time.Time {
		return fixture.Now
	}

	summary, err := collector.Collect(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	assertOverviewStats(t, summary.Overview)
	assertLanguageOrderingAndWeights(t, summary.Languages)
	assertTopReposOrdering(t, summary.TopRepos)
	assertDiagnosticsCoverage(t, summary.Diagnostics)

	if strings.Contains(languageNames(summary.Languages), "HTML") {
		t.Fatalf("owned languages should exclude HTML after fixture filtering: %s", languageNames(summary.Languages))
	}

	externalDiagnostics := FormatDiagnostics(summary.Diagnostics)
	goldenPath := filepath.Join("testdata", "sdk_diagnostics.golden.json")
	expectedDiagnosticsBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden diagnostics: %v", err)
	}
	if externalDiagnostics != string(expectedDiagnosticsBytes) {
		t.Fatalf("diagnostics output mismatch\nexpected:\n%s\nactual:\n%s", string(expectedDiagnosticsBytes), externalDiagnostics)
	}
}

func TestFormatDiagnosticsDeterministic(t *testing.T) {
	fixture := loadCollectorFixture(t)
	cfg := fixture.Config.toConfig()
	service := fakeGitHubService{
		viewer:               fixture.Viewer,
		ownedRepositories:    fixture.OwnedRepositories,
		externalRepositories: fixture.ExternalRepositories,
		contributionCount:    fixture.ContributionCount,
		activities:           fixture.Activities,
		additions:            fixture.Additions,
		deletions:            fixture.Deletions,
		views:                fixture.Views,
		externalEstimates:    fixture.ExternalEstimates,
	}
	collector := New(service)
	collector.now = func() time.Time {
		return fixture.Now
	}

	summary, err := collector.Collect(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}

	first := FormatDiagnostics(summary.Diagnostics)
	second := FormatDiagnostics(summary.Diagnostics)
	if first != second {
		t.Fatalf("expected deterministic diagnostics output")
	}
}

func loadCollectorFixture(t *testing.T) collectorFixture {
	t.Helper()
	fixturePath := filepath.Join("testdata", "sdk_fixture.json")
	fixtureBytes, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture file: %v", err)
	}

	var fixture collectorFixture
	if err := json.Unmarshal(fixtureBytes, &fixture); err != nil {
		t.Fatalf("decode fixture file: %v", err)
	}
	return fixture
}

func (fixtureConfig collectorFixtureConfig) toConfig() internalconfig.Config {
	excludedRepos := make(map[string]struct{}, len(fixtureConfig.ExcludedRepos))
	for _, repositoryName := range fixtureConfig.ExcludedRepos {
		excludedRepos[repositoryName] = struct{}{}
	}

	excludedLangs := make(map[string]struct{}, len(fixtureConfig.ExcludedLangs))
	for _, languageName := range fixtureConfig.ExcludedLangs {
		excludedLangs[strings.ToLower(languageName)] = struct{}{}
	}

	return internalconfig.Config{
		ExcludedRepos:   excludedRepos,
		ExcludedLangs:   excludedLangs,
		ExcludeForks:    fixtureConfig.ExcludeForks,
		RecencyHalfLife: time.Duration(fixtureConfig.RecencyHalfLifeHours) * time.Hour,
		RecencyFloor:    fixtureConfig.RecencyFloor,
	}
}

func assertOverviewStats(t *testing.T, overview internalmodel.OverviewStats) {
	t.Helper()

	if overview.Name != "SDK User" {
		t.Fatalf("expected display name SDK User, got %q", overview.Name)
	}
	if overview.Stars != 34 {
		t.Fatalf("expected 34 stars, got %d", overview.Stars)
	}
	if overview.Forks != 4 {
		t.Fatalf("expected 4 forks, got %d", overview.Forks)
	}
	if overview.TotalContributions != 321 {
		t.Fatalf("expected 321 total contributions, got %d", overview.TotalContributions)
	}
	if overview.LinesChanged != 333 {
		t.Fatalf("expected 333 lines changed, got %d", overview.LinesChanged)
	}
	if overview.Views != 789 {
		t.Fatalf("expected 789 views, got %d", overview.Views)
	}
	if overview.RepositoryCount != 8 {
		t.Fatalf("expected 8 owned repositories, got %d", overview.RepositoryCount)
	}
}

func assertLanguageOrderingAndWeights(t *testing.T, languages []internalmodel.LanguageStat) {
	t.Helper()

	expectedNames := []string{"Go", "Rust", "TypeScript"}
	actualNames := make([]string, 0, len(languages))
	for _, language := range languages {
		actualNames = append(actualNames, language.Name)
	}
	if !reflect.DeepEqual(actualNames, expectedNames) {
		t.Fatalf("unexpected language order: got %v want %v", actualNames, expectedNames)
	}

	if languages[0].Color != "#00ADD8" {
		t.Fatalf("expected Go fallback or explicit color to be preserved, got %q", languages[0].Color)
	}
	assertClose(t, languages[0].Weighted, 1139.121852, "Go weighted bytes")
	assertClose(t, languages[1].Weighted, 400, "Rust weighted bytes")
	assertClose(t, languages[2].Weighted, 141.019084, "TypeScript weighted bytes")

	assertClose(t, languages[0].Percentage, 67.799184, "Go weighted percentage")
	assertClose(t, languages[1].Percentage, 23.807527, "Rust weighted percentage")
	assertClose(t, languages[2].Percentage, 8.393289, "TypeScript weighted percentage")
}

func assertTopReposOrdering(t *testing.T, repos []internalmodel.RepoActivity) {
	t.Helper()

	expected := []string{
		"me/recent-go",
		"me/shared-sdk",
		"me/older-rust",
		"me/forked-go",
		"me/archived-old",
		"me/excluded-repo",
	}
	actual := make([]string, 0, len(repos))
	for _, repository := range repos {
		actual = append(actual, repository.RepositoryName)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("unexpected top repo order: got %v want %v", actual, expected)
	}
}

func assertDiagnosticsCoverage(t *testing.T, diagnostics internalmodel.DiagnosticsReport) {
	t.Helper()

	if diagnostics.Scope != diagnosticsScope {
		t.Fatalf("expected diagnostics scope %q, got %q", diagnosticsScope, diagnostics.Scope)
	}
	if diagnostics.Summary.OwnedRepositoryCount != 8 {
		t.Fatalf("expected 8 owned repositories in diagnostics, got %d", diagnostics.Summary.OwnedRepositoryCount)
	}
	if diagnostics.Summary.ExternalRepositoryCount != 2 {
		t.Fatalf("expected 2 external repositories in diagnostics, got %d", diagnostics.Summary.ExternalRepositoryCount)
	}
	if diagnostics.Summary.IncludedOwnedCount != 3 {
		t.Fatalf("expected 3 included owned repositories, got %d", diagnostics.Summary.IncludedOwnedCount)
	}
	if diagnostics.Summary.ExcludedOwnedCount != 5 {
		t.Fatalf("expected 5 excluded owned repositories, got %d", diagnostics.Summary.ExcludedOwnedCount)
	}

	if diagnostics.Summary.IncludeExternal {
		t.Fatalf("expected includeExternal to be false in diagnostics summary")
	}
	if diagnostics.Summary.EstimatedExternalCount != 2 {
		t.Fatalf("expected 2 estimated external repositories, got %d", diagnostics.Summary.EstimatedExternalCount)
	}
	if diagnostics.Summary.UnknownExternalCount != 0 {
		t.Fatalf("expected 0 unknown external repositories, got %d", diagnostics.Summary.UnknownExternalCount)
	}
	assertClose(t, diagnostics.Summary.OwnedWeightedBytes, 1680.140936, "owned weighted bytes summary")
	assertClose(t, diagnostics.Summary.ExternalWeightedBytes, 76288.482967, "external weighted bytes summary")

	expectedReasons := map[string]string{
		"me/recent-go":     "recency_weighted",
		"me/older-rust":    "recency_weighted",
		"me/shared-sdk":    "recency_weighted",
		"me/forked-go":     "fork",
		"me/excluded-repo": "excluded_repo",
		"me/archived-old":  "archived",
		"me/no-langs":      "missing_language_data",
		"me/disabled-zero": "disabled",
	}
	for _, decision := range diagnostics.Decisions {
		expectedReason, found := expectedReasons[decision.RepositoryName]
		if !found {
			t.Fatalf("unexpected diagnostics decision for %q", decision.RepositoryName)
		}
		if decision.Reason != expectedReason {
			t.Fatalf("unexpected diagnostics reason for %q: got %q want %q", decision.RepositoryName, decision.Reason, expectedReason)
		}
	}

	if len(diagnostics.ExternalEstimates) != 2 {
		t.Fatalf("expected 2 external estimates, got %d", len(diagnostics.ExternalEstimates))
	}

	hugeExternal := diagnostics.ExternalEstimates[0]
	if hugeExternal.RepositoryName != "org/huge-external-docs" {
		t.Fatalf("unexpected first external estimate %q", hugeExternal.RepositoryName)
	}
	assertClose(t, hugeExternal.WeightedEstimatedBytes, 75581.360995, "huge external weighted bytes")
	assertClose(t, hugeExternal.RecencyWeight, 0.839700, "huge external recency weight")
	assertClose(t, hugeExternal.Languages[0].Percentage, 99.98889, "huge external HTML percentage")
	assertClose(t, hugeExternal.Languages[1].Percentage, 0.01111, "huge external Go percentage")

	secondExternal := diagnostics.ExternalEstimates[1]
	if secondExternal.RepositoryName != "org/external-sdk" {
		t.Fatalf("unexpected second external estimate %q", secondExternal.RepositoryName)
	}
	assertClose(t, secondExternal.WeightedEstimatedBytes, 707.121972, "second external weighted bytes")
	assertClose(t, secondExternal.RecencyWeight, 0.471415, "second external recency weight")
	if hugeExternal.EstimateNote != "" || secondExternal.EstimateNote != "" {
		t.Fatalf("expected fixture-based external estimates to preserve empty notes after weighting")
	}
	if len(diagnostics.ExternalEstimatedLanguage) != 2 {
		t.Fatalf("expected 2 external estimated languages, got %d", len(diagnostics.ExternalEstimatedLanguage))
	}
	assertClose(t, diagnostics.ExternalEstimatedLanguage[0].Weighted, 574.094574, "external Go weighted bytes")
	assertClose(t, diagnostics.ExternalEstimatedLanguage[1].Weighted, 141.424394, "external TypeScript weighted bytes")

	if reflect.DeepEqual(diagnostics.WeightedOwnedLanguage, diagnostics.RawOwnedLanguage) {
		t.Fatalf("expected weighted and raw owned language diagnostics to differ")
	}
}

func cloneRepositories(repositories []internalmodel.Repository) []internalmodel.Repository {
	cloned := make([]internalmodel.Repository, 0, len(repositories))
	for _, repository := range repositories {
		repositoryClone := repository
		repositoryClone.Languages = append([]internalmodel.RepositoryLanguage(nil), repository.Languages...)
		cloned = append(cloned, repositoryClone)
	}
	return cloned
}

func cloneExternalEstimates(estimates []internalmodel.ExternalContributionEstimate) []internalmodel.ExternalContributionEstimate {
	cloned := make([]internalmodel.ExternalContributionEstimate, 0, len(estimates))
	for _, estimate := range estimates {
		estimateClone := estimate
		estimateClone.Languages = append([]internalmodel.LanguageStat(nil), estimate.Languages...)
		cloned = append(cloned, estimateClone)
	}
	return cloned
}

func languageNames(languages []internalmodel.LanguageStat) string {
	names := make([]string, 0, len(languages))
	for _, language := range languages {
		names = append(names, language.Name)
	}
	return strings.Join(names, ",")
}

func assertClose(t *testing.T, actual float64, expected float64, label string) {
	t.Helper()
	if math.Abs(actual-expected) > fixtureFloatTolerance {
		t.Fatalf("unexpected %s: got %.6f want %.6f", label, actual, expected)
	}
}
