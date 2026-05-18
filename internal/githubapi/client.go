// Package githubapi wraps the GitHub REST and GraphQL APIs that stats-gh
// uses, including a rate-limit-aware HTTP transport and typed query helpers.
package githubapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	internalconfig "github.com/agoodkind/stats/internal/config"
	github "github.com/google/go-github/v81/github"
	"golang.org/x/oauth2"
)

const (
	defaultGraphQLEndpoint = "https://api.github.com/graphql"
	defaultRequestTimeout  = 30 * time.Second
	defaultRESTAPIVersion  = "2026-03-10"
)

// Client is the stats-gh GitHub API wrapper, holding the REST client and the
// authenticated viewer login it should query on behalf of.
type Client struct {
	httpClient *http.Client
	rest       *github.Client
	actor      string
}

type graphQLRequest struct {
	Query     string          `json:"query"`
	Variables json.RawMessage `json:"variables,omitempty"`
}

type graphQLEnvelope struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors"`
}

type rateLimitedTransport struct {
	base http.RoundTripper
}

// NewClient returns a Client configured from the given Config, using OAuth2
// bearer authentication on every outbound HTTP request.
func NewClient(cfg internalconfig.Config) *Client {
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: cfg.GitHubToken})
	httpClient := &http.Client{
		Timeout: defaultRequestTimeout,
		Transport: &rateLimitedTransport{
			base: &oauth2.Transport{
				Source: tokenSource,
				Base:   http.DefaultTransport,
			},
		},
	}

	return &Client{
		httpClient: httpClient,
		rest:       github.NewClient(httpClient),
		actor:      cfg.GitHubActor,
	}
}

func (client *Client) doGraphQL(ctx context.Context, query string, variables json.RawMessage) (graphQLEnvelope, error) {
	payload, err := json.Marshal(graphQLRequest{Query: query, Variables: variables})
	if err != nil {
		slog.ErrorContext(ctx, "marshal graphql request", "error", err)
		return graphQLEnvelope{}, fmt.Errorf("marshal graphql request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, defaultGraphQLEndpoint, bytes.NewReader(payload))
	if err != nil {
		slog.ErrorContext(ctx, "create graphql request", "error", err)
		return graphQLEnvelope{}, fmt.Errorf("create graphql request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := client.httpClient.Do(request)
	if err != nil {
		slog.ErrorContext(ctx, "perform graphql request", "error", err)
		return graphQLEnvelope{}, fmt.Errorf("perform graphql request: %w", err)
	}

	responseBody, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil {
		slog.ErrorContext(ctx, "read graphql response", "error", readErr)
		return graphQLEnvelope{}, fmt.Errorf("read graphql response: %w", readErr)
	}
	if closeErr != nil {
		slog.ErrorContext(ctx, "close graphql response body", "error", closeErr)
		return graphQLEnvelope{}, fmt.Errorf("close graphql response body: %w", closeErr)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		responseText := strings.TrimSpace(string(responseBody))
		return graphQLEnvelope{}, fmt.Errorf("graphql returned %d: %s", response.StatusCode, responseText)
	}
	var envelope graphQLEnvelope
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		slog.ErrorContext(ctx, "decode graphql response", "error", err)
		return graphQLEnvelope{}, fmt.Errorf("decode graphql response: %w", err)
	}
	return envelope, nil
}

func (transport *rateLimitedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-Github-Api-Version", defaultRESTAPIVersion)

	response, err := transport.base.RoundTrip(request)
	if err != nil {
		slog.ErrorContext(request.Context(), "github transport round trip", "error", err)
		return nil, fmt.Errorf("github transport round trip: %w", err)
	}
	if !isRateLimited(response) {
		return response, nil
	}

	retryAfter := strings.TrimSpace(response.Header.Get("Retry-After"))
	resetAt := strings.TrimSpace(response.Header.Get("X-Ratelimit-Reset"))
	return nil, fmt.Errorf("github rate limit response %d, retry-after=%q, reset=%q", response.StatusCode, retryAfter, resetAt)
}

// isRateLimited returns true only when the response actually indicates a
// primary or secondary rate-limit, not for every 403. A 403 with a non-zero
// X-Ratelimit-Remaining header is a permissions / scope failure and should
// be surfaced to the caller as a normal error rather than masquerading as a
// rate-limit.
func isRateLimited(response *http.Response) bool {
	if response.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if response.StatusCode != http.StatusForbidden {
		return false
	}
	if response.Header.Get("Retry-After") != "" {
		return true
	}
	remaining := strings.TrimSpace(response.Header.Get("X-Ratelimit-Remaining"))
	return remaining == "0"
}

func splitRepositoryName(nameWithOwner string) (string, string, error) {
	owner, repo, found := strings.Cut(nameWithOwner, "/")
	if !found {
		return "", "", fmt.Errorf("split repository name %q", nameWithOwner)
	}
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	if owner == "" || repo == "" {
		return "", "", fmt.Errorf("split repository name %q", nameWithOwner)
	}
	return owner, repo, nil
}

func isAcceptedError(err error) bool {
	var acceptedError *github.AcceptedError
	return errors.As(err, &acceptedError)
}
