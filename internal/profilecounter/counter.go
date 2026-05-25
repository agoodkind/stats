// Package profilecounter reads the profile-view badge that the profile README
// renders and turns its SVG value back into a numeric counter.
package profilecounter

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultEndpoint       = "https://komarev.com/ghpvc/"
	defaultRequestTimeout = 10 * time.Second
	maxBadgeBytes         = 64 * 1024
)

var badgeValuePattern = regexp.MustCompile(`<text\b[^>]*>([0-9][0-9,]*)</text>`)

// Client fetches the Komarev profile-view badge that used to be rendered
// directly by the profile README.
type Client struct {
	httpClient *http.Client
	endpoint   string
}

// New returns a Client configured for the historical Komarev badge endpoint.
func New() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: defaultRequestTimeout},
		endpoint:   defaultEndpoint,
	}
}

// FetchProfileViews fetches and parses the current profile-view count for the
// given GitHub actor.
func (client *Client) FetchProfileViews(ctx context.Context, actor string) (int, error) {
	badgeURL, err := client.badgeURL(actor)
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, badgeURL, nil)
	if err != nil {
		slog.ErrorContext(ctx, "create profile counter request", "url", badgeURL, "error", err)
		return 0, fmt.Errorf("create profile counter request: %w", err)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		slog.ErrorContext(ctx, "fetch profile counter", "url", badgeURL, "error", err)
		return 0, fmt.Errorf("fetch profile counter: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return 0, fmt.Errorf("fetch profile counter: status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBadgeBytes))
	if err != nil {
		slog.ErrorContext(ctx, "read profile counter", "url", badgeURL, "error", err)
		return 0, fmt.Errorf("read profile counter: %w", err)
	}
	count, err := parseBadgeCount(string(body))
	if err != nil {
		slog.ErrorContext(ctx, "parse profile counter", "url", badgeURL, "error", err)
		return 0, fmt.Errorf("parse profile counter: %w", err)
	}
	return count, nil
}

func (client *Client) badgeURL(actor string) (string, error) {
	trimmedActor := strings.TrimSpace(actor)
	if trimmedActor == "" {
		return "", fmt.Errorf("profile counter actor is empty")
	}
	parsedURL, err := url.Parse(client.endpoint)
	if err != nil {
		slog.Error("parse profile counter endpoint", "endpoint", client.endpoint, "error", err)
		return "", fmt.Errorf("parse profile counter endpoint: %w", err)
	}
	query := parsedURL.Query()
	query.Set("username", trimmedActor)
	parsedURL.RawQuery = query.Encode()
	return parsedURL.String(), nil
}

func parseBadgeCount(svgText string) (int, error) {
	matches := badgeValuePattern.FindAllStringSubmatch(svgText, -1)
	if len(matches) == 0 {
		return 0, fmt.Errorf("badge value not found")
	}
	lastValue := 0
	for _, match := range matches {
		valueText := strings.ReplaceAll(match[1], ",", "")
		value, err := strconv.Atoi(valueText)
		if err != nil {
			slog.Error("decode profile counter badge value", "value", match[1], "error", err)
			return 0, fmt.Errorf("decode badge value %q: %w", match[1], err)
		}
		lastValue = value
	}
	return lastValue, nil
}
