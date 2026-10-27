package readmecache_test

import (
	"strings"
	"testing"

	"github.com/agoodkind/stats/internal/readmecache"
)

func TestUpdateAddsCacheKeyAndPreservesFragments(t *testing.T) {
	input := strings.Join([]string{
		`<picture><source srcset="https://raw.githubusercontent.com/agoodkind/stats/master/generated/overview.svg#gh-dark-mode-only" /></picture>`,
		`<img src="https://raw.githubusercontent.com/agoodkind/stats/master/generated/languages.svg" />`,
		`<img src="https://raw.githubusercontent.com/agoodkind/stats/master/generated/top_repos.svg?cache=old#gh-dark-mode-only" />`,
	}, "\n")

	updatedContent, replacementCount, err := readmecache.Update(input, "run 123")
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if replacementCount != 3 {
		t.Fatalf("replacementCount = %d, want 3", replacementCount)
	}

	expectedFragments := []string{
		"overview.svg?cache=run+123#gh-dark-mode-only",
		"languages.svg?cache=run+123",
		"top_repos.svg?cache=run+123#gh-dark-mode-only",
	}
	for _, expectedFragment := range expectedFragments {
		if !strings.Contains(updatedContent, expectedFragment) {
			t.Fatalf("updated content missing %q:\n%s", expectedFragment, updatedContent)
		}
	}
	if strings.Contains(updatedContent, "?cache=old") {
		t.Fatalf("updated content kept old cache key:\n%s", updatedContent)
	}
}

func TestUpdateRejectsMissingCacheKey(t *testing.T) {
	updatedContent, replacementCount, err := readmecache.Update("https://raw.githubusercontent.com/agoodkind/stats/master/generated/overview.svg", " ")
	if err == nil || err.Error() != "cache key is required" {
		t.Fatalf("Update error = %v, want cache key is required", err)
	}
	if updatedContent != "" || replacementCount != 0 {
		t.Fatalf("rejected update returned content %q and count %d, want empty content and zero", updatedContent, replacementCount)
	}
}

func TestUpdateRejectsContentWithoutStatsImages(t *testing.T) {
	updatedContent, replacementCount, err := readmecache.Update("## Hi there", "run-123")
	if err == nil || err.Error() != "no stats image URLs found" {
		t.Fatalf("Update error = %v, want missing stats image diagnostic", err)
	}
	if updatedContent != "" || replacementCount != 0 {
		t.Fatalf("rejected update returned content %q and count %d, want empty content and zero", updatedContent, replacementCount)
	}
}
