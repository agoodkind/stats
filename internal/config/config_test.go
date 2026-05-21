package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultPath(t *testing.T) {
	if DefaultPath() != "config.toml" {
		t.Fatalf("expected config.toml, got %q", DefaultPath())
	}
}

func TestStringSetLowercase(t *testing.T) {
	values := stringSet([]string{"Java", " CSharp "}, true)

	if _, ok := values["java"]; !ok {
		t.Fatalf("expected java to be present")
	}
	if _, ok := values["csharp"]; !ok {
		t.Fatalf("expected csharp to be present")
	}
}

func TestParseHalfLifeYears(t *testing.T) {
	duration, err := parseHalfLife("3y")
	if err != nil {
		t.Fatalf("parseHalfLife returned error: %v", err)
	}

	if duration != 3*365*24*time.Hour {
		t.Fatalf("expected three years, got %s", duration)
	}
}

func TestLoadFromPath(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	configContents := "[github]\n" +
		"token = \"token-value\"\n" +
		"actor = \"agoodkind\"\n\n" +
		"[logging]\n" +
		"level = \"DEBUG\"\n\n" +
		"[recency]\n" +
		"half_life = \"3y\"\n" +
		"floor = 0.10\n\n" +
		"[owned]\n" +
		"exclude_archived = false\n" +
		"exclude_disabled = false\n" +
		"exclude_forks = true\n" +
		"require_languages = false\n" +
		"excluded_repos = [\"owner/one\", \"owner/two\"]\n" +
		"excluded_langs = [\"Java\", \"CSharp\"]\n\n" +
		"[contributed]\n" +
		"include = \"public-only\"\n" +
		"include_in_loc = false\n" +
		"include_in_languages = false\n\n" +
		"[top_repos]\n" +
		"limit = 9\n" +
		"star_coefficient = 3.5\n\n" +
		"[languages]\n" +
		"compression = \"log\"\n\n" +
		"[views]\n" +
		"seed = 42\n"

	if err := os.WriteFile(configPath, []byte(configContents), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := LoadFromPath(configPath)
	if err != nil {
		t.Fatalf("LoadFromPath returned error: %v", err)
	}

	if cfg.GitHubToken != "token-value" {
		t.Fatalf("expected token-value, got %q", cfg.GitHubToken)
	}
	if cfg.GitHubActor != "agoodkind" {
		t.Fatalf("expected agoodkind, got %q", cfg.GitHubActor)
	}
	if cfg.ExcludeArchived {
		t.Fatalf("expected exclude_archived false")
	}
	if cfg.ExcludeDisabled {
		t.Fatalf("expected exclude_disabled false")
	}
	if !cfg.ExcludeForks {
		t.Fatalf("expected exclude_forks true")
	}
	if cfg.RequireLanguages {
		t.Fatalf("expected require_languages false")
	}
	if cfg.LogLevel != "DEBUG" {
		t.Fatalf("expected DEBUG, got %q", cfg.LogLevel)
	}
	if cfg.RecencyHalfLife != 3*365*24*time.Hour {
		t.Fatalf("expected three years, got %s", cfg.RecencyHalfLife)
	}
	if cfg.RecencyFloor != 0.10 {
		t.Fatalf("expected floor 0.10, got %f", cfg.RecencyFloor)
	}
	if _, ok := cfg.ExcludedRepos["owner/one"]; !ok {
		t.Fatalf("expected owner/one to be present")
	}
	if _, ok := cfg.ExcludedLangs["java"]; !ok {
		t.Fatalf("expected java to be present")
	}
	if cfg.ContributedInclude != ContributedPublicOnly {
		t.Fatalf("expected ContributedPublicOnly, got %q", cfg.ContributedInclude)
	}
	if cfg.ContributedIncludeInLOC {
		t.Fatalf("expected ContributedIncludeInLOC false")
	}
	if cfg.ContributedIncludeInLangs {
		t.Fatalf("expected ContributedIncludeInLangs false")
	}
	if cfg.TopReposLimit != 9 {
		t.Fatalf("expected TopReposLimit 9, got %d", cfg.TopReposLimit)
	}
	if cfg.TopReposStarCoefficient != 3.5 {
		t.Fatalf("expected TopReposStarCoefficient 3.5, got %f", cfg.TopReposStarCoefficient)
	}
	if cfg.LanguagesCompression != LanguagesLog {
		t.Fatalf("expected LanguagesLog, got %q", cfg.LanguagesCompression)
	}
	if cfg.ViewsSeed != 42 {
		t.Fatalf("expected ViewsSeed 42, got %d", cfg.ViewsSeed)
	}
}

func TestLoadFromPathDefaults(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	configContents := "[github]\n" +
		"token = \"token-value\"\n" +
		"actor = \"agoodkind\"\n"

	if err := os.WriteFile(configPath, []byte(configContents), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := LoadFromPath(configPath)
	if err != nil {
		t.Fatalf("LoadFromPath returned error: %v", err)
	}

	if !cfg.ExcludeArchived || !cfg.ExcludeDisabled || !cfg.ExcludeForks || !cfg.RequireLanguages {
		t.Fatalf("expected all owned-exclusion flags to default true: %+v", cfg)
	}
	if cfg.ContributedInclude != ContributedAll {
		t.Fatalf("expected ContributedAll default, got %q", cfg.ContributedInclude)
	}
	if !cfg.ContributedIncludeInLOC || !cfg.ContributedIncludeInLangs {
		t.Fatalf("expected contributed inclusion defaults true: %+v", cfg)
	}
	if cfg.TopReposLimit != 6 {
		t.Fatalf("expected TopReposLimit 6 default, got %d", cfg.TopReposLimit)
	}
	if cfg.TopReposStarCoefficient != 2.0 {
		t.Fatalf("expected TopReposStarCoefficient 2.0 default, got %f", cfg.TopReposStarCoefficient)
	}
	if cfg.LanguagesCompression != LanguagesSqrt {
		t.Fatalf("expected LanguagesSqrt default, got %q", cfg.LanguagesCompression)
	}
}

func TestLoadFromPathUsesEnvironmentFallbacks(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "environment-token")
	t.Setenv("GITHUB_ACTOR", "environment-actor")

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	configContents := "[github]\n"

	if err := os.WriteFile(configPath, []byte(configContents), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := LoadFromPath(configPath)
	if err != nil {
		t.Fatalf("LoadFromPath returned error: %v", err)
	}

	if cfg.GitHubToken != "environment-token" {
		t.Fatalf("expected environment-token, got %q", cfg.GitHubToken)
	}
	if cfg.GitHubActor != "environment-actor" {
		t.Fatalf("expected environment-actor, got %q", cfg.GitHubActor)
	}
}

func TestLoadFromPathPrefersFileValuesOverEnvironment(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "environment-token")
	t.Setenv("GITHUB_ACTOR", "environment-actor")

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	configContents := "[github]\n" +
		"token = \"file-token\"\n" +
		"actor = \"file-actor\"\n"

	if err := os.WriteFile(configPath, []byte(configContents), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := LoadFromPath(configPath)
	if err != nil {
		t.Fatalf("LoadFromPath returned error: %v", err)
	}

	if cfg.GitHubToken != "file-token" {
		t.Fatalf("expected file-token, got %q", cfg.GitHubToken)
	}
	if cfg.GitHubActor != "file-actor" {
		t.Fatalf("expected file-actor, got %q", cfg.GitHubActor)
	}
}

func TestLoadFromPathRequiresToken(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	configContents := "[github]\nactor = \"agoodkind\"\n"

	if err := os.WriteFile(configPath, []byte(configContents), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	_, err := LoadFromPath(configPath)
	if err == nil {
		t.Fatalf("expected missing token error")
	}
}
