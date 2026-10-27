package githubapi_test

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agoodkind/stats/internal/config"
	"github.com/agoodkind/stats/internal/githubapi"
	"github.com/agoodkind/stats/internal/model"
)

type graphQLRequest struct {
	Query     string          `json:"query"`
	Variables json.RawMessage `json:"variables"`
}

func TestFetchViewerRepositoriesUsesAuthenticatedHTTPAndPagination(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/graphql" {
			t.Errorf("GraphQL request = %s %s", request.Method, request.URL.Path)
			http.Error(writer, "unexpected route", http.StatusNotFound)
			return
		}
		if request.Header.Get("Authorization") != "Bearer local-test-placeholder" ||
			request.Header.Get("Accept") != "application/vnd.github+json" ||
			request.Header.Get("X-Github-Api-Version") != "2026-03-10" ||
			request.Header.Get("Content-Type") != "application/json" {
			t.Error("GraphQL request omitted the production authentication or API headers")
		}
		var payload graphQLRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode GraphQL request: %v", err)
			http.Error(writer, "invalid request", http.StatusBadRequest)
			return
		}
		var variables struct {
			Login          string  `json:"login"`
			OwnedCursor    *string `json:"ownedCursor"`
			ExternalCursor *string `json:"externalCursor"`
		}
		if err := json.Unmarshal(payload.Variables, &variables); err != nil {
			t.Errorf("decode GraphQL variables: %v", err)
			http.Error(writer, "invalid variables", http.StatusBadRequest)
			return
		}
		if variables.Login != "sample" || !strings.Contains(payload.Query, "repositoriesContributedTo") {
			t.Errorf("viewer query did not select the configured actor and repository connections")
		}
		writer.Header().Set("Content-Type", "application/json")
		if requests.Add(1) == 1 {
			if variables.OwnedCursor != nil || variables.ExternalCursor != nil {
				t.Error("first viewer page supplied a cursor")
			}
			_, _ = io.WriteString(writer, `{"data":{"user":{"login":"sample","name":"Sample User","repositories":{"nodes":[{"nameWithOwner":"sample/first","languages":{"edges":[{"size":100,"node":{"name":"Go","color":"#00ADD8"}}]}}],"pageInfo":{"hasNextPage":true,"endCursor":"owned-1"}},"repositoriesContributedTo":{"nodes":[{"nameWithOwner":"group/first"}],"pageInfo":{"hasNextPage":true,"endCursor":"external-1"}}}}}`)
			return
		}
		if variables.OwnedCursor == nil || *variables.OwnedCursor != "owned-1" ||
			variables.ExternalCursor == nil || *variables.ExternalCursor != "external-1" {
			t.Error("second viewer page omitted a returned cursor")
		}
		_, _ = io.WriteString(writer, `{"data":{"user":{"login":"sample","name":"Sample User","repositories":{"nodes":[{"nameWithOwner":"sample/second","stargazers":{"totalCount":7}}],"pageInfo":{"hasNextPage":false}},"repositoriesContributedTo":{"nodes":[{"nameWithOwner":"group/second"}],"pageInfo":{"hasNextPage":false}}}}}`)
	}))
	defer server.Close()
	client, err := githubapi.NewClient(config.Config{GitHubActor: "sample", GitHubToken: "local-test-placeholder"}, githubapi.Options{
		HTTPClient: server.Client(), GraphQLEndpoint: server.URL + "/graphql",
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	viewer, owned, external, err := client.FetchViewerRepositories(context.Background())
	if err != nil {
		t.Fatalf("fetch viewer repositories: %v", err)
	}
	if requests.Load() != 2 || viewer.Login != "sample" || viewer.Name != "Sample User" ||
		len(owned) != 2 || len(external) != 2 {
		t.Fatalf("viewer pagination = %d requests, viewer=%+v owned=%+v external=%+v", requests.Load(), viewer, owned, external)
	}
	if owned[0].Source != model.RepositorySourceOwned || owned[0].Languages[0].Bytes != 100 ||
		owned[1].Stars != 7 || external[0].Source != model.RepositorySourceExternal {
		t.Fatalf("mapped repository data = owned:%+v external:%+v", owned, external)
	}
}

func TestFetchViewerRepositoriesDistinguishesScopeAndRateLimits(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		remaining  string
		retryAfter string
		want       string
	}{
		{name: "scope refusal", status: http.StatusForbidden, remaining: "4998", want: "graphql returned 403: permission denied"},
		{name: "primary limit", status: http.StatusForbidden, remaining: "0", want: "rate limit response 403"},
		{name: "secondary limit", status: http.StatusForbidden, retryAfter: "60", want: "rate limit response 403"},
		{name: "too many requests", status: http.StatusTooManyRequests, retryAfter: "30", want: "rate limit response 429"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				if test.remaining != "" {
					writer.Header().Set("X-Ratelimit-Remaining", test.remaining)
				}
				if test.retryAfter != "" {
					writer.Header().Set("Retry-After", test.retryAfter)
				}
				http.Error(writer, "permission denied", test.status)
			}))
			defer server.Close()
			client, err := githubapi.NewClient(config.Config{GitHubActor: "sample", GitHubToken: "local-test-placeholder"}, githubapi.Options{HTTPClient: server.Client(), GraphQLEndpoint: server.URL})
			if err != nil {
				t.Fatalf("create client: %v", err)
			}
			viewer, owned, external, err := client.FetchViewerRepositories(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("viewer error = %v, want %q", err, test.want)
			}
			if viewer.Login != "" || owned != nil || external != nil {
				t.Fatalf("refused query returned repository data: %+v %+v %+v", viewer, owned, external)
			}
		})
	}
}

func TestFetchContributorActivityPaginates(t *testing.T) {
	var pages atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload graphQLRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode GraphQL request: %v", err)
			http.Error(writer, "invalid request", http.StatusBadRequest)
			return
		}
		if strings.Contains(payload.Query, "user(login:") && !strings.Contains(payload.Query, "repository(") {
			_, _ = io.WriteString(writer, `{"data":{"user":{"id":"actor-id"}}}`)
			return
		}
		var variables struct {
			Owner   string  `json:"owner"`
			Name    string  `json:"name"`
			ActorID string  `json:"actorID"`
			Cursor  *string `json:"cursor"`
		}
		if err := json.Unmarshal(payload.Variables, &variables); err != nil {
			t.Errorf("decode activity variables: %v", err)
			http.Error(writer, "invalid variables", http.StatusBadRequest)
			return
		}
		if variables.Owner != "sample" || variables.Name != "stats" || variables.ActorID != "actor-id" {
			t.Errorf("activity selected unexpected repository or actor: %+v", variables)
		}
		if pages.Add(1) == 1 {
			if variables.Cursor != nil {
				t.Error("first activity page supplied a cursor")
			}
			_, _ = io.WriteString(writer, `{"data":{"repository":{"defaultBranchRef":{"target":{"history":{"nodes":[{"additions":4,"deletions":6,"committedDate":"2026-01-01T00:00:00Z"}],"pageInfo":{"hasNextPage":true,"endCursor":"cursor-1"}}}}}}}`)
			return
		}
		if variables.Cursor == nil || *variables.Cursor != "cursor-1" {
			t.Error("second activity page omitted its cursor")
		}
		_, _ = io.WriteString(writer, `{"data":{"repository":{"defaultBranchRef":{"target":{"history":{"nodes":[{"additions":8,"deletions":2,"committedDate":"2025-01-01T00:00:00Z"}],"pageInfo":{"hasNextPage":false}}}}}}}`)
	}))
	defer server.Close()
	client, err := githubapi.NewClient(config.Config{GitHubActor: "sample", GitHubToken: "local-test-placeholder"}, githubapi.Options{HTTPClient: server.Client(), GraphQLEndpoint: server.URL})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	activities, additions, deletions, err := client.FetchContributorActivity(context.Background(), []model.Repository{{NameWithOwner: "sample/stats"}},
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 365*24*time.Hour, 0.05)
	if err != nil {
		t.Fatalf("fetch contributor activity: %v", err)
	}
	if pages.Load() != 2 || len(activities) != 1 || activities[0].Commits != 2 ||
		math.Abs(activities[0].WeightedCommits-1.5) > 0.001 || additions != 12 || deletions != 8 {
		t.Fatalf("activity = %+v additions=%d deletions=%d pages=%d", activities, additions, deletions, pages.Load())
	}
}

func TestFetchViewsUsesConfiguredRESTEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/sample/stats/traffic/views" || request.URL.Query().Get("per") != "day" {
			t.Errorf("traffic route = %s", request.URL.String())
		}
		_, _ = io.WriteString(writer, `{"count":7,"uniques":1,"views":[{"timestamp":"2026-05-23T00:00:00Z","count":7,"uniques":1}]}`)
	}))
	defer server.Close()
	client, err := githubapi.NewClient(config.Config{GitHubActor: "sample", GitHubToken: "local-test-placeholder"}, githubapi.Options{HTTPClient: server.Client(), RESTEndpoint: server.URL})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	views, err := client.FetchViews(context.Background(), []model.Repository{{NameWithOwner: "sample/stats"}})
	if err != nil {
		t.Fatalf("fetch views: %v", err)
	}
	if views["sample/stats"]["2026-05-23"] != 7 {
		t.Fatalf("traffic views = %+v, want seven daily views", views)
	}
}
