// Package readmecache rewrites profile README stats image URLs so GitHub
// refreshes cached raw SVG assets after generated images change.
package readmecache

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const cacheQueryName = "cache"

var statsImageURLPattern = regexp.MustCompile(
	`https://raw\.githubusercontent\.com/agoodkind/stats/(?:master|main)/generated/(?:overview|languages|top_repos)\.svg(?:\?[^#"'<>[:space:]]*)?(?:#[A-Za-z0-9_-]+)?`,
)

// Update rewrites every stats image URL in content with the supplied cache key.
func Update(content string, cacheKey string) (string, int, error) {
	trimmedCacheKey := strings.TrimSpace(cacheKey)
	if trimmedCacheKey == "" {
		return "", 0, fmt.Errorf("cache key is required")
	}

	escapedCacheKey := url.QueryEscape(trimmedCacheKey)
	replacementCount := 0
	updatedContent := statsImageURLPattern.ReplaceAllStringFunc(content, func(match string) string {
		replacementCount++
		baseURL, fragment := splitFragment(match)
		baseURL = stripQuery(baseURL)
		return fmt.Sprintf("%s?%s=%s%s", baseURL, cacheQueryName, escapedCacheKey, fragment)
	})

	if replacementCount == 0 {
		return "", 0, fmt.Errorf("no stats image URLs found")
	}

	return updatedContent, replacementCount, nil
}

func splitFragment(rawURL string) (string, string) {
	baseURL, fragment, found := strings.Cut(rawURL, "#")
	if !found {
		return rawURL, ""
	}

	return baseURL, "#" + fragment
}

func stripQuery(rawURL string) string {
	baseURL, _, found := strings.Cut(rawURL, "?")
	if !found {
		return rawURL
	}

	return baseURL
}
