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
			if request.Header.Get("X-GitHub-Api-Version") != "2026-03-10" {
				t.Fatalf("unexpected X-GitHub-Api-Version header %q", request.Header.Get("X-GitHub-Api-Version"))
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
				if payload.Variables["cursor"] != "abc123" {
					t.Fatalf("unexpected GraphQL variables %#v", payload.Variables)
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"data":{"viewer":{"login":"me"}}}`)),
				}, nil
			}),
		},
	}

	var response struct {
		Data struct {
			Viewer struct {
				Login string `json:"login"`
			} `json:"viewer"`
		} `json:"data"`
	}

	err := client.doGraphQL(
		context.Background(),
		"query Test($cursor: String) { viewer { login } }",
		map[string]any{"cursor": "abc123"},
		&response,
	)
	if err != nil {
		t.Fatalf("doGraphQL returned error: %v", err)
	}
	if response.Data.Viewer.Login != "me" {
		t.Fatalf("unexpected viewer login %q", response.Data.Viewer.Login)
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
				if payload.Variables["owner"] != "agoodkind" {
					t.Fatalf("unexpected owner variable %#v", payload.Variables["owner"])
				}
				if payload.Variables["name"] != "stats-gh" {
					t.Fatalf("unexpected name variable %#v", payload.Variables["name"])
				}
				if payload.Variables["actorID"] != "actor-id" {
					t.Fatalf("unexpected actorID variable %#v", payload.Variables["actorID"])
				}

				if requests == 1 {
					if payload.Variables["cursor"] != nil {
						t.Fatalf("unexpected first cursor %#v", payload.Variables["cursor"])
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"data":{"repository":{"defaultBranchRef":{"target":{"history":{"nodes":[{"additions":4,"deletions":6}],"pageInfo":{"hasNextPage":true,"endCursor":"cursor-1"}}}}}}}`)),
					}, nil
				}

				if payload.Variables["cursor"] != "cursor-1" {
					t.Fatalf("unexpected second cursor %#v", payload.Variables["cursor"])
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
