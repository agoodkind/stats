package readmecache

import (
	"strings"
	"testing"
)

func TestUpdateAddsCacheKeyAndPreservesFragments(t *testing.T) {
	input := strings.Join([]string{
		`<picture><source srcset="https://raw.githubusercontent.com/agoodkind/stats/master/generated/overview.svg#gh-dark-mode-only" /></picture>`,
		`<img src="https://raw.githubusercontent.com/agoodkind/stats/master/generated/languages.svg" />`,
		`<img src="https://raw.githubusercontent.com/agoodkind/stats/master/generated/top_repos.svg?cache=old#gh-dark-mode-only" />`,
	}, "\n")

	updatedContent, replacementCount, err := Update(input, "run 123")
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
	_, _, err := Update("https://raw.githubusercontent.com/agoodkind/stats/master/generated/overview.svg", " ")
	if err == nil {
		t.Fatal("Update returned nil error")
	}
}

func TestUpdateRejectsContentWithoutStatsImages(t *testing.T) {
	_, _, err := Update("## Hi there", "run-123")
	if err == nil {
		t.Fatal("Update returned nil error")
	}
}
