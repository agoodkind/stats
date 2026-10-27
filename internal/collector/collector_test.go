package collector_test

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agoodkind/stats/internal/collector"
	"github.com/agoodkind/stats/internal/config"
	"github.com/agoodkind/stats/internal/githubapi"
	"github.com/agoodkind/stats/internal/model"
	"github.com/agoodkind/stats/internal/profilecounter"
	"github.com/agoodkind/stats/internal/viewshistory"
)

func TestCollectUsesProductionClientsAndPersistsHistory(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	historyPath := filepath.Join(root, collector.ViewsHistoryPath)
	if err := os.MkdirAll(filepath.Dir(historyPath), 0o755); err != nil {
		t.Fatalf("create history directory: %v", err)
	}
	if err := os.WriteFile(historyPath, []byte(`{"seed":1152,"repos":{"me/old":{"2026-05-23":3}}}`), 0o600); err != nil {
		t.Fatalf("write legacy history: %v", err)
	}
	server, badgeUnavailable := startCollectionServer(t)
	client, err := githubapi.NewClient(config.Config{GitHubActor: "sdk-user", GitHubToken: "local-test-placeholder"}, githubapi.Options{
		HTTPClient: server.Client(), RESTEndpoint: server.URL, GraphQLEndpoint: server.URL + "/graphql",
	})
	if err != nil {
		t.Fatalf("create GitHub client: %v", err)
	}
	service := collector.New(client, collector.Options{ProfileCounter: profilecounter.New(profilecounter.Options{
		HTTPClient: server.Client(), Endpoint: server.URL + "/badge",
	})})
	cfg := collectionConfig()
	summary, err := service.Collect(context.Background(), cfg)
	if err != nil {
		t.Fatalf("collect HTTP statistics: %v", err)
	}
	if summary.Overview != (model.OverviewStats{
		Name: "SDK User", Stars: 24, Forks: 3, TotalContributions: 321, LinesChanged: 30,
		Views: 1169, OwnedRepositories: 3, ContributedRepositories: 2,
	}) {
		t.Fatalf("overview = %+v", summary.Overview)
	}
	if len(summary.Languages) != 3 || summary.Languages[0].Name != "Go" ||
		summary.Languages[1].Name != "Rust" || summary.Languages[2].Name != "TypeScript" {
		t.Fatalf("owned language ordering = %+v", summary.Languages)
	}
	for index, want := range []float64{1100, 400, 100} {
		if math.Abs(summary.Languages[index].Weighted-want) > 0.01 {
			t.Fatalf("language %s weight = %f, want near %f", summary.Languages[index].Name, summary.Languages[index].Weighted, want)
		}
	}
	if len(summary.TopRepos) != 3 || summary.TopRepos[0].RepositoryName != "me/recent-go" ||
		summary.TopRepos[1].RepositoryName != "me/older-rust" || summary.TopRepos[2].RepositoryName != "me/shared-sdk" {
		t.Fatalf("top repository ordering = %+v", summary.TopRepos)
	}
	report := summary.Diagnostics
	if report.Summary.OwnedRepositoryCount != 9 || report.Summary.IncludedOwnedCount != 3 ||
		report.Summary.ExcludedOwnedCount != 6 || report.Summary.ExternalRepositoryCount != 2 ||
		report.Summary.EstimatedExternalCount != 2 || report.Summary.UnknownExternalCount != 0 {
		t.Fatalf("diagnostics counts = %+v", report.Summary)
	}
	reasons := map[string]string{
		"me/recent-go": "recency_weighted", "me/older-rust": "recency_weighted",
		"me/shared-sdk": "recency_weighted", "me/forked-go": "fork",
		"me/excluded-repo": "excluded_repo", "me/archived-old": "archived",
		"me/no-langs": "missing_language_data", "me/disabled-zero": "disabled", "me/private": "private",
	}
	if len(report.Decisions) != len(reasons) {
		t.Fatalf("inclusion decisions = %+v", report.Decisions)
	}
	for _, decision := range report.Decisions {
		if want, ok := reasons[decision.RepositoryName]; !ok || decision.Reason != want {
			t.Fatalf("inclusion decision = %+v", decision)
		}
	}
	history, err := viewshistory.Load(historyPath)
	if err != nil {
		t.Fatalf("load persisted history: %v", err)
	}
	if history.Counter != 1159 || history.Seed != 0 || viewshistory.Total(history) != 1169 ||
		history.Repos["me/recent-go"]["2026-05-23"] != 7 || len(history.Repos) != 2 {
		t.Fatalf("persisted history = %+v", history)
	}
	encoded := collector.FormatDiagnostics(report)
	var decoded model.DiagnosticsReport
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		t.Fatalf("decode public diagnostics: %v", err)
	}
	if decoded.Scope != "all-time owned repositories, recency weighted" || decoded.Summary != report.Summary ||
		collector.FormatDiagnostics(report) != encoded {
		t.Fatalf("formatted diagnostics = %s", encoded)
	}
	badgeUnavailable.Store(true)
	cfg.ContributedIncludeInLangs = true
	repeated, err := service.Collect(context.Background(), cfg)
	if err != nil {
		t.Fatalf("collect with cached profile counter: %v", err)
	}
	if repeated.Overview.Views != 1169 || !repeated.Diagnostics.Summary.IncludeExternal ||
		len(repeated.Languages) != 3 || repeated.Languages[0].Name != "Go" ||
		math.Abs(repeated.Languages[0].Weighted-2600) > 0.01 {
		t.Fatalf("cached-counter and external-language statistics = %+v", repeated)
	}
}

func TestCollectRejectsUnavailableCounterWithoutCachedHistory(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	historyPath := filepath.Join(root, collector.ViewsHistoryPath)
	if err := os.MkdirAll(filepath.Dir(historyPath), 0o755); err != nil {
		t.Fatalf("create history directory: %v", err)
	}
	server, badgeUnavailable := startCollectionServer(t)
	badgeUnavailable.Store(true)
	client, err := githubapi.NewClient(config.Config{GitHubActor: "sdk-user", GitHubToken: "local-test-placeholder"}, githubapi.Options{
		HTTPClient: server.Client(), RESTEndpoint: server.URL, GraphQLEndpoint: server.URL + "/graphql",
	})
	if err != nil {
		t.Fatalf("create GitHub client: %v", err)
	}
	service := collector.New(client, collector.Options{ProfileCounter: profilecounter.New(profilecounter.Options{
		HTTPClient: server.Client(), Endpoint: server.URL + "/badge",
	})})
	summary, err := service.Collect(context.Background(), collectionConfig())
	if err == nil || !strings.Contains(err.Error(), "fetch profile counter: fetch profile counter: status 503") ||
		summary.Overview != (model.OverviewStats{}) {
		t.Fatalf("unavailable counter returned summary=%+v error=%v", summary, err)
	}
	if _, err := os.Stat(historyPath); !os.IsNotExist(err) {
		t.Fatalf("failed collection persisted history: %v", err)
	}
}

func collectionConfig() config.Config {
	return config.Config{
		GitHubActor: "sdk-user", ExcludedRepos: map[string]struct{}{"me/excluded-repo": {}},
		ExcludedLangs: map[string]struct{}{"html": {}}, ExcludeArchived: true, ExcludeDisabled: true,
		ExcludeForks: true, RequireLanguages: true, ContributedInclude: config.ContributedAll,
		ContributedIncludeInLOC: true, TopReposLimit: 6, TopReposStarCoefficient: 2,
		LanguagesCompression: config.LanguagesSqrt, RecencyHalfLife: 365 * 24 * time.Hour, RecencyFloor: 0.1,
	}
}

func startCollectionServer(t *testing.T) (*httptest.Server, *atomic.Bool) {
	t.Helper()
	var badgeUnavailable atomic.Bool
	recent := time.Now().Add(time.Hour).Format(time.RFC3339)
	older := time.Now().Add(-365 * 24 * time.Hour).Format(time.RFC3339)
	oldest := time.Now().Add(-2 * 365 * 24 * time.Hour).Format(time.RFC3339)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/badge":
			if request.URL.Query().Get("username") != "sdk-user" {
				t.Error("profile request did not select the configured actor")
			}
			if badgeUnavailable.Load() {
				http.Error(writer, "temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			_, _ = io.WriteString(writer, "<svg><text>1,159</text></svg>")
		case request.URL.Path == "/graphql":
			serveCollectionGraphQL(t, writer, request, recent, older, oldest)
		case strings.Contains(request.URL.Path, "/me/private/"):
			t.Error("collector requested a private repository's data")
			http.Error(writer, "private repository", http.StatusForbidden)
		case strings.HasSuffix(request.URL.Path, "/traffic/views"):
			if request.URL.Path == "/repos/me/recent-go/traffic/views" {
				_, _ = io.WriteString(writer, `{"views":[{"timestamp":"2026-05-23T00:00:00Z","count":7}]}`)
				return
			}
			_, _ = io.WriteString(writer, `{"views":[]}`)
		case strings.HasSuffix(request.URL.Path, "/stats/contributors"):
			_, _ = io.WriteString(writer, `[{"author":{"login":"sdk-user"},"weeks":[{"a":3,"d":1}]},{"author":{"login":"other"},"weeks":[{"a":3,"d":1}]}]`)
		default:
			t.Errorf("unexpected collection request path %s", request.URL.Path)
			http.Error(writer, "unknown route", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server, &badgeUnavailable
}

func serveCollectionGraphQL(t *testing.T, writer http.ResponseWriter, request *http.Request, recent string, older string, oldest string) {
	t.Helper()
	var payload struct {
		Query     string `json:"query"`
		Variables struct {
			Owner string `json:"owner"`
			Name  string `json:"name"`
		} `json:"variables"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		t.Errorf("decode collection request: %v", err)
		http.Error(writer, "invalid request", http.StatusBadRequest)
		return
	}
	switch {
	case strings.Contains(payload.Query, "ViewerRepositories"):
		_, _ = io.WriteString(writer, `{"data":{"user":{"login":"sdk-user","name":"SDK User","repositories":{"nodes":[
{"nameWithOwner":"me/recent-go","pushedAt":"`+recent+`","stargazers":{"totalCount":10},"forkCount":1,"languages":{"edges":[{"size":1000,"node":{"name":"Go","color":"#00ADD8"}},{"size":400,"node":{"name":"HTML"}}]}},
{"nameWithOwner":"me/older-rust","pushedAt":"`+older+`","stargazers":{"totalCount":8},"forkCount":2,"languages":{"edges":[{"size":800,"node":{"name":"Rust","color":"#dea584"}}]}},
{"nameWithOwner":"me/shared-sdk","pushedAt":"`+oldest+`","stargazers":{"totalCount":6},"languages":{"edges":[{"size":400,"node":{"name":"Go"}},{"size":400,"node":{"name":"TypeScript"}}]}},
{"nameWithOwner":"me/forked-go","isFork":true,"languages":{"edges":[{"size":999,"node":{"name":"Go"}}]}},
{"nameWithOwner":"me/excluded-repo","languages":{"edges":[{"size":555,"node":{"name":"Go"}}]}},
{"nameWithOwner":"me/archived-old","isArchived":true,"languages":{"edges":[{"size":700,"node":{"name":"Python"}}]}},
{"nameWithOwner":"me/no-langs","languages":{"edges":[]}},
{"nameWithOwner":"me/disabled-zero","isDisabled":true,"languages":{"edges":[{"size":250,"node":{"name":"JavaScript"}}]}},
{"nameWithOwner":"me/private","isPrivate":true,"languages":{"edges":[{"size":9999,"node":{"name":"Go"}}]}}
],"pageInfo":{"hasNextPage":false}},"repositoriesContributedTo":{"nodes":[
{"nameWithOwner":"org/huge-external-docs","pushedAt":"`+recent+`","languages":{"edges":[{"size":9000000,"node":{"name":"HTML"}},{"size":1000,"node":{"name":"Go"}}]}},
{"nameWithOwner":"org/external-sdk","pushedAt":"`+recent+`","languages":{"edges":[{"size":2000,"node":{"name":"Go"}},{"size":500,"node":{"name":"TypeScript"}}]}}
],"pageInfo":{"hasNextPage":false}}}}}`)
	case strings.Contains(payload.Query, "ContributionYears"):
		_, _ = io.WriteString(writer, `{"data":{"user":{"contributionsCollection":{"contributionYears":[2026]}}}}`)
	case strings.Contains(payload.Query, "ActorID"):
		_, _ = io.WriteString(writer, `{"data":{"user":{"id":"sdk-user-id"}}}`)
	case strings.Contains(payload.Query, "RepositoryCommitActivity"):
		if payload.Variables.Owner == "me" && payload.Variables.Name == "private" {
			t.Error("collector requested private repository commits")
		}
		date := recent
		if payload.Variables.Name == "older-rust" {
			date = older
		}
		if payload.Variables.Name == "shared-sdk" {
			date = oldest
		}
		_, _ = io.WriteString(writer, `{"data":{"repository":{"defaultBranchRef":{"target":{"history":{"nodes":[{"additions":1,"deletions":2,"committedDate":"`+date+`"}],"pageInfo":{"hasNextPage":false}}}}}}}`)
	default:
		if !strings.Contains(payload.Query, "year2026:") {
			t.Errorf("unexpected GraphQL operation")
		}
		_, _ = io.WriteString(writer, `{"data":{"user":{"year2026":{"contributionCalendar":{"totalContributions":321}}}}}`)
	}
}
