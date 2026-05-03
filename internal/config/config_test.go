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
		"[filters]\n" +
		"excluded_repos = [\"owner/one\", \"owner/two\"]\n" +
		"excluded_langs = [\"Java\", \"CSharp\"]\n" +
		"exclude_forked_repos = true\n\n" +
		"[recency]\n" +
		"half_life = \"3y\"\n" +
		"floor = 0.10\n\n" +
		"[logging]\n" +
		"level = \"DEBUG\"\n"

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
	if !cfg.ExcludeForks {
		t.Fatalf("expected exclude forks to be true")
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
