package collector

import (
	"context"
	"encoding/json"
	"fmt"
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

func (service fakeGitHubService) FetchContributorActivity(_ context.Context, _ []internalmodel.Repository, _ time.Time, _ time.Duration, _ float64) ([]internalmodel.RepoActivity, int, int, error) {
	return append([]internalmodel.RepoActivity(nil), service.activities...), service.additions, service.deletions, nil
}

func (service fakeGitHubService) FetchViews(context.Context, []internalmodel.Repository) (map[string]map[string]int, error) {
	if service.views <= 0 {
		return map[string]map[string]int{}, nil
	}
	return map[string]map[string]int{
		"me/recent-go": {"2026-01-01": service.views},
	}, nil
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
	collector.viewsHistoryPath = filepath.Join(t.TempDir(), "views_history.json")

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
	assertDiagnosticsJSONEqual(t, expectedDiagnosticsBytes, []byte(externalDiagnostics))
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
	collector.viewsHistoryPath = filepath.Join(t.TempDir(), "views_history.json")

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
	if overview.RepositoryCount != 10 {
		t.Fatalf("expected 10 repositories (8 owned + 2 external), got %d", overview.RepositoryCount)
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
	assertClose(t, languages[0].Weighted, 1139.0, "Go weighted bytes")
	assertClose(t, languages[1].Weighted, 400.0, "Rust weighted bytes")
	assertClose(t, languages[2].Weighted, 141.0, "TypeScript weighted bytes")

	assertClose(t, languages[0].Percentage, 67.797619, "Go weighted percentage")
	assertClose(t, languages[1].Percentage, 23.809524, "Rust weighted percentage")
	assertClose(t, languages[2].Percentage, 8.392857, "TypeScript weighted percentage")
}

func assertTopReposOrdering(t *testing.T, repos []internalmodel.RepoActivity) {
	t.Helper()

	expected := []string{
		"me/recent-go",
		"me/older-rust",
		"me/shared-sdk",
	}
	actual := make([]string, 0, len(repos))
	for _, repository := range repos {
		actual = append(actual, repository.RepositoryName)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("unexpected top repo order: got %v want %v", actual, expected)
	}

	for _, repository := range repos {
		if repository.Score <= 0 {
			t.Fatalf("expected positive top-repo score for %q, got %.6f", repository.RepositoryName, repository.Score)
		}
	}
	for index := 1; index < len(repos); index += 1 {
		if repos[index-1].Score < repos[index].Score {
			t.Fatalf("expected top repos sorted by score desc, got %.6f before %.6f", repos[index-1].Score, repos[index].Score)
		}
	}
	if repos[0].RepositoryName != "me/recent-go" || repos[0].Commits != 50 || repos[0].Stars != 10 {
		t.Fatalf("unexpected top repo commits/stars: %+v", repos[0])
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
	assertClose(t, diagnostics.Summary.OwnedWeightedBytes, 1680.0, "owned weighted bytes summary")
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

func assertDiagnosticsJSONEqual(t *testing.T, expectedBytes []byte, actualBytes []byte) {
	t.Helper()

	var expectedReport internalmodel.DiagnosticsReport
	if err := json.Unmarshal(expectedBytes, &expectedReport); err != nil {
		t.Fatalf("decode expected diagnostics JSON: %v", err)
	}

	var actualReport internalmodel.DiagnosticsReport
	if err := json.Unmarshal(actualBytes, &actualReport); err != nil {
		t.Fatalf("decode actual diagnostics JSON: %v", err)
	}

	if diff := compareDiagnosticsReport(expectedReport, actualReport); diff != "" {
		t.Fatalf("diagnostics output mismatch: %s\nexpected:\n%s\nactual:\n%s", diff, string(expectedBytes), string(actualBytes))
	}
}

func compareDiagnosticsReport(expectedReport internalmodel.DiagnosticsReport, actualReport internalmodel.DiagnosticsReport) string {
	if expectedReport.Scope != actualReport.Scope {
		return fmt.Sprintf("scope got %q want %q", actualReport.Scope, expectedReport.Scope)
	}
	if diff := compareDiagnosticsSummary(expectedReport.Summary, actualReport.Summary); diff != "" {
		return diff
	}
	if diff := compareLanguageStats("weightedOwnedLanguage", expectedReport.WeightedOwnedLanguage, actualReport.WeightedOwnedLanguage); diff != "" {
		return diff
	}
	if diff := compareLanguageStats("rawOwnedLanguage", expectedReport.RawOwnedLanguage, actualReport.RawOwnedLanguage); diff != "" {
		return diff
	}
	if diff := compareLanguageStats("externalEstimatedLanguage", expectedReport.ExternalEstimatedLanguage, actualReport.ExternalEstimatedLanguage); diff != "" {
		return diff
	}
	if diff := compareLanguageStats("effectiveLanguage", expectedReport.EffectiveLanguage, actualReport.EffectiveLanguage); diff != "" {
		return diff
	}
	if !reflect.DeepEqual(expectedReport.ExternalRepositories, actualReport.ExternalRepositories) {
		return "externalRepositories differ"
	}
	if diff := compareExternalEstimates(expectedReport.ExternalEstimates, actualReport.ExternalEstimates); diff != "" {
		return diff
	}
	if diff := compareInclusionDecisions(expectedReport.Decisions, actualReport.Decisions); diff != "" {
		return diff
	}
	return ""
}

func compareDiagnosticsSummary(expectedSummary internalmodel.DiagnosticsSummary, actualSummary internalmodel.DiagnosticsSummary) string {
	if expectedSummary.OwnedRepositoryCount != actualSummary.OwnedRepositoryCount {
		return fmt.Sprintf("ownedRepositoryCount got %d want %d", actualSummary.OwnedRepositoryCount, expectedSummary.OwnedRepositoryCount)
	}
	if expectedSummary.ExternalRepositoryCount != actualSummary.ExternalRepositoryCount {
		return fmt.Sprintf("externalRepositoryCount got %d want %d", actualSummary.ExternalRepositoryCount, expectedSummary.ExternalRepositoryCount)
	}
	if expectedSummary.IncludedOwnedCount != actualSummary.IncludedOwnedCount {
		return fmt.Sprintf("includedOwnedCount got %d want %d", actualSummary.IncludedOwnedCount, expectedSummary.IncludedOwnedCount)
	}
	if expectedSummary.ExcludedOwnedCount != actualSummary.ExcludedOwnedCount {
		return fmt.Sprintf("excludedOwnedCount got %d want %d", actualSummary.ExcludedOwnedCount, expectedSummary.ExcludedOwnedCount)
	}
	if expectedSummary.IncludeExternal != actualSummary.IncludeExternal {
		return fmt.Sprintf("includeExternal got %t want %t", actualSummary.IncludeExternal, expectedSummary.IncludeExternal)
	}
	if expectedSummary.EstimatedExternalCount != actualSummary.EstimatedExternalCount {
		return fmt.Sprintf("estimatedExternalCount got %d want %d", actualSummary.EstimatedExternalCount, expectedSummary.EstimatedExternalCount)
	}
	if expectedSummary.UnknownExternalCount != actualSummary.UnknownExternalCount {
		return fmt.Sprintf("unknownExternalCount got %d want %d", actualSummary.UnknownExternalCount, expectedSummary.UnknownExternalCount)
	}
	if !floatsClose(expectedSummary.OwnedWeightedBytes, actualSummary.OwnedWeightedBytes) {
		return fmt.Sprintf("ownedWeightedBytes got %.12f want %.12f", actualSummary.OwnedWeightedBytes, expectedSummary.OwnedWeightedBytes)
	}
	if !floatsClose(expectedSummary.ExternalWeightedBytes, actualSummary.ExternalWeightedBytes) {
		return fmt.Sprintf("externalWeightedBytes got %.12f want %.12f", actualSummary.ExternalWeightedBytes, expectedSummary.ExternalWeightedBytes)
	}
	return ""
}

func compareLanguageStats(label string, expectedStats []internalmodel.LanguageStat, actualStats []internalmodel.LanguageStat) string {
	if len(expectedStats) != len(actualStats) {
		return fmt.Sprintf("%s length got %d want %d", label, len(actualStats), len(expectedStats))
	}
	for index, expectedStat := range expectedStats {
		actualStat := actualStats[index]
		if expectedStat.Name != actualStat.Name {
			return fmt.Sprintf("%s[%d].name got %q want %q", label, index, actualStat.Name, expectedStat.Name)
		}
		if expectedStat.Color != actualStat.Color {
			return fmt.Sprintf("%s[%d].color got %q want %q", label, index, actualStat.Color, expectedStat.Color)
		}
		if expectedStat.Bytes != actualStat.Bytes {
			return fmt.Sprintf("%s[%d].bytes got %d want %d", label, index, actualStat.Bytes, expectedStat.Bytes)
		}
		if !floatsClose(expectedStat.Weighted, actualStat.Weighted) {
			return fmt.Sprintf("%s[%d].weighted got %.12f want %.12f", label, index, actualStat.Weighted, expectedStat.Weighted)
		}
		if !floatsClose(expectedStat.Percentage, actualStat.Percentage) {
			return fmt.Sprintf("%s[%d].percentage got %.12f want %.12f", label, index, actualStat.Percentage, expectedStat.Percentage)
		}
	}
	return ""
}

func compareExternalEstimates(expectedEstimates []internalmodel.ExternalContributionEstimate, actualEstimates []internalmodel.ExternalContributionEstimate) string {
	if len(expectedEstimates) != len(actualEstimates) {
		return fmt.Sprintf("externalEstimates length got %d want %d", len(actualEstimates), len(expectedEstimates))
	}
	for index, expectedEstimate := range expectedEstimates {
		actualEstimate := actualEstimates[index]
		if expectedEstimate.RepositoryName != actualEstimate.RepositoryName {
			return fmt.Sprintf("externalEstimates[%d].repositoryName got %q want %q", index, actualEstimate.RepositoryName, expectedEstimate.RepositoryName)
		}
		if expectedEstimate.Method != actualEstimate.Method {
			return fmt.Sprintf("externalEstimates[%d].method got %q want %q", index, actualEstimate.Method, expectedEstimate.Method)
		}
		if expectedEstimate.Confidence != actualEstimate.Confidence {
			return fmt.Sprintf("externalEstimates[%d].confidence got %q want %q", index, actualEstimate.Confidence, expectedEstimate.Confidence)
		}
		if !floatsClose(expectedEstimate.EstimatedRatio, actualEstimate.EstimatedRatio) {
			return fmt.Sprintf("externalEstimates[%d].estimatedRatio got %.12f want %.12f", index, actualEstimate.EstimatedRatio, expectedEstimate.EstimatedRatio)
		}
		if !floatsClose(expectedEstimate.RawEstimatedBytes, actualEstimate.RawEstimatedBytes) {
			return fmt.Sprintf("externalEstimates[%d].rawEstimatedBytes got %.12f want %.12f", index, actualEstimate.RawEstimatedBytes, expectedEstimate.RawEstimatedBytes)
		}
		if !floatsClose(expectedEstimate.WeightedEstimatedBytes, actualEstimate.WeightedEstimatedBytes) {
			return fmt.Sprintf("externalEstimates[%d].weightedEstimatedBytes got %.12f want %.12f", index, actualEstimate.WeightedEstimatedBytes, expectedEstimate.WeightedEstimatedBytes)
		}
		if !floatsClose(expectedEstimate.RecencyWeight, actualEstimate.RecencyWeight) {
			return fmt.Sprintf("externalEstimates[%d].recencyWeight got %.12f want %.12f", index, actualEstimate.RecencyWeight, expectedEstimate.RecencyWeight)
		}
		if expectedEstimate.EstimateNote != actualEstimate.EstimateNote {
			return fmt.Sprintf("externalEstimates[%d].estimateNote got %q want %q", index, actualEstimate.EstimateNote, expectedEstimate.EstimateNote)
		}
		if diff := compareLanguageStats(fmt.Sprintf("externalEstimates[%d].languages", index), expectedEstimate.Languages, actualEstimate.Languages); diff != "" {
			return diff
		}
	}
	return ""
}

func compareInclusionDecisions(expectedDecisions []internalmodel.InclusionDecision, actualDecisions []internalmodel.InclusionDecision) string {
	if len(expectedDecisions) != len(actualDecisions) {
		return fmt.Sprintf("decisions length got %d want %d", len(actualDecisions), len(expectedDecisions))
	}
	for index, expectedDecision := range expectedDecisions {
		actualDecision := actualDecisions[index]
		if expectedDecision.RepositoryName != actualDecision.RepositoryName {
			return fmt.Sprintf("decisions[%d].repositoryName got %q want %q", index, actualDecision.RepositoryName, expectedDecision.RepositoryName)
		}
		if expectedDecision.Source != actualDecision.Source {
			return fmt.Sprintf("decisions[%d].source got %q want %q", index, actualDecision.Source, expectedDecision.Source)
		}
		if expectedDecision.Included != actualDecision.Included {
			return fmt.Sprintf("decisions[%d].included got %t want %t", index, actualDecision.Included, expectedDecision.Included)
		}
		if expectedDecision.Reason != actualDecision.Reason {
			return fmt.Sprintf("decisions[%d].reason got %q want %q", index, actualDecision.Reason, expectedDecision.Reason)
		}
		if expectedDecision.RawBytes != actualDecision.RawBytes {
			return fmt.Sprintf("decisions[%d].rawBytes got %d want %d", index, actualDecision.RawBytes, expectedDecision.RawBytes)
		}
		if !floatsClose(expectedDecision.WeightedBytes, actualDecision.WeightedBytes) {
			return fmt.Sprintf("decisions[%d].weightedBytes got %.12f want %.12f", index, actualDecision.WeightedBytes, expectedDecision.WeightedBytes)
		}
		if !floatsClose(expectedDecision.RecencyWeight, actualDecision.RecencyWeight) {
			return fmt.Sprintf("decisions[%d].recencyWeight got %.12f want %.12f", index, actualDecision.RecencyWeight, expectedDecision.RecencyWeight)
		}
		if !expectedDecision.PushedAt.Equal(actualDecision.PushedAt) {
			return fmt.Sprintf("decisions[%d].pushedAt got %s want %s", index, actualDecision.PushedAt, expectedDecision.PushedAt)
		}
		if !expectedDecision.UpdatedAt.Equal(actualDecision.UpdatedAt) {
			return fmt.Sprintf("decisions[%d].updatedAt got %s want %s", index, actualDecision.UpdatedAt, expectedDecision.UpdatedAt)
		}
	}
	return ""
}

func floatsClose(expected float64, actual float64) bool {
	return math.Abs(actual-expected) <= fixtureFloatTolerance
}

func assertClose(t *testing.T, actual float64, expected float64, label string) {
	t.Helper()
	if math.Abs(actual-expected) > fixtureFloatTolerance {
		t.Fatalf("unexpected %s: got %.6f want %.6f", label, actual, expected)
	}
}
