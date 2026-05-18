package githubapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(request *http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestRateLimitedTransportSetsGitHubHeaders(t *testing.T) {
	transport := &rateLimitedTransport{
		base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Header.Get("Accept") != "application/vnd.github+json" {
				t.Fatalf("unexpected Accept header %q", request.Header.Get("Accept"))
			}
			if request.Header.Get("X-Github-Api-Version") != "2026-03-10" {
				t.Fatalf("unexpected X-Github-Api-Version header %q", request.Header.Get("X-Github-Api-Version"))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			}, nil
		}),
	}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext returned error: %v", err)
	}

	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip returned error: %v", err)
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatalf("close response body: %v", closeErr)
	}
}

func TestDoGraphQLSendsVariables(t *testing.T) {
	client := &Client{
		httpClient: &http.Client{
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.Method != http.MethodPost {
					t.Fatalf("unexpected method %q", request.Method)
				}
				if request.URL.String() != defaultGraphQLEndpoint {
					t.Fatalf("unexpected URL %q", request.URL.String())
				}
				if request.Header.Get("Content-Type") != "application/json" {
					t.Fatalf("unexpected Content-Type header %q", request.Header.Get("Content-Type"))
				}

				var payload graphQLRequest
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Fatalf("decode GraphQL request payload: %v", err)
				}
				if payload.Query != "query Test($cursor: String) { viewer { login } }" {
					t.Fatalf("unexpected GraphQL query %q", payload.Query)
				}
				var sentVariables struct {
					Cursor string `json:"cursor"`
				}
				if err := json.Unmarshal(payload.Variables, &sentVariables); err != nil {
					t.Fatalf("decode variables: %v", err)
				}
				if sentVariables.Cursor != "abc123" {
					t.Fatalf("unexpected cursor variable %q", sentVariables.Cursor)
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"data":{"viewer":{"login":"me"}}}`)),
				}, nil
			}),
		},
	}

	variables, err := json.Marshal(map[string]string{"cursor": "abc123"})
	if err != nil {
		t.Fatalf("marshal test variables: %v", err)
	}
	envelope, err := client.doGraphQL(
		context.Background(),
		"query Test($cursor: String) { viewer { login } }",
		variables,
	)
	if err != nil {
		t.Fatalf("doGraphQL returned error: %v", err)
	}
	var response struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
	}
	if err := json.Unmarshal(envelope.Data, &response); err != nil {
		t.Fatalf("decode envelope data: %v", err)
	}
	if response.Viewer.Login != "me" {
		t.Fatalf("unexpected viewer login %q", response.Viewer.Login)
	}
}

func TestFetchRepositoryCommitActivityPaginates(t *testing.T) {
	query := "query RepositoryCommitActivity"
	requests := 0
	client := &Client{
		httpClient: &http.Client{
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requests += 1
				var payload graphQLRequest
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Fatalf("decode GraphQL request payload: %v", err)
				}
				if payload.Query != query {
					t.Fatalf("unexpected GraphQL query %q", payload.Query)
				}
				var sentVariables repositoryCommitActivityVariables
				if err := json.Unmarshal(payload.Variables, &sentVariables); err != nil {
					t.Fatalf("decode variables: %v", err)
				}
				if sentVariables.Owner != "agoodkind" {
					t.Fatalf("unexpected owner variable %q", sentVariables.Owner)
				}
				if sentVariables.Name != "stats-gh" {
					t.Fatalf("unexpected name variable %q", sentVariables.Name)
				}
				if sentVariables.ActorID != "actor-id" {
					t.Fatalf("unexpected actorID variable %q", sentVariables.ActorID)
				}

				if requests == 1 {
					if sentVariables.Cursor != nil {
						t.Fatalf("unexpected first cursor %#v", sentVariables.Cursor)
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"data":{"repository":{"defaultBranchRef":{"target":{"history":{"nodes":[{"additions":4,"deletions":6}],"pageInfo":{"hasNextPage":true,"endCursor":"cursor-1"}}}}}}}`)),
					}, nil
				}

				if sentVariables.Cursor == nil || *sentVariables.Cursor != "cursor-1" {
					t.Fatalf("unexpected second cursor %#v", sentVariables.Cursor)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"data":{"repository":{"defaultBranchRef":{"target":{"history":{"nodes":[{"additions":8,"deletions":2}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}}}`)),
				}, nil
			}),
		},
	}

	commits, additions, deletions, err := client.fetchRepositoryCommitActivity(context.Background(), query, "actor-id", "agoodkind", "stats-gh")
	if err != nil {
		t.Fatalf("fetchRepositoryCommitActivity returned error: %v", err)
	}
	if requests != 2 {
		t.Fatalf("unexpected request count %d", requests)
	}
	if commits != 2 {
		t.Fatalf("unexpected commit count %d", commits)
	}
	if additions != 12 {
		t.Fatalf("unexpected additions %d", additions)
	}
	if deletions != 8 {
		t.Fatalf("unexpected deletions %d", deletions)
	}
}
