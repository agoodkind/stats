package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

const defaultConfigPath = "config.toml"
const defaultRecencyHalfLife = 3 * 365 * 24 * time.Hour
const defaultRecencyFloor = 0.05
const hoursPerYear = 365 * 24

var githubTokenEnvironmentKeys = []string{"GITHUB_TOKEN", "GH_TOKEN"}
var githubActorEnvironmentKeys = []string{"GITHUB_ACTOR", "GH_ACTOR"}

type Config struct {
	Path            string
	GitHubToken     string
	GitHubActor     string
	ExcludedRepos   map[string]struct{}
	ExcludedLangs   map[string]struct{}
	ExcludeForks    bool
	IncludeExternal bool
	RecencyHalfLife time.Duration
	RecencyFloor    float64
	LogLevel        string
}

type fileConfig struct {
	GitHub  githubConfig  `toml:"github"`
	Filters filtersConfig `toml:"filters"`
	Recency recencyConfig `toml:"recency"`
	Logging loggingConfig `toml:"logging"`
}

type githubConfig struct {
	Token string `toml:"token"`
	Actor string `toml:"actor"`
}

type filtersConfig struct {
	ExcludedRepos   []string `toml:"excluded_repos"`
	ExcludedLangs   []string `toml:"excluded_langs"`
	ExcludeForks    bool     `toml:"exclude_forked_repos"`
	IncludeExternal bool     `toml:"include_external"`
}

type recencyConfig struct {
	HalfLife string   `toml:"half_life"`
	Floor    *float64 `toml:"floor"`
}

type loggingConfig struct {
	Level string `toml:"level"`
}

func DefaultPath() string {
	return defaultConfigPath
}

func Load() (Config, error) {
	return LoadFromPath(defaultConfigPath)
}

func LoadFromPath(path string) (Config, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		cleanPath = defaultConfigPath
	}

	configBytes, err := os.ReadFile(cleanPath)
	if err != nil {
		slog.Error("read config file", "path", cleanPath, "error", err)
		return Config{}, fmt.Errorf("read config file %q: %w", cleanPath, err)
	}

	var rawConfig fileConfig
	if err := toml.Unmarshal(configBytes, &rawConfig); err != nil {
		slog.Error("decode config file", "path", cleanPath, "error", err)
		return Config{}, fmt.Errorf("decode config file %q: %w", cleanPath, err)
	}

	cfg := Config{
		Path:            cleanPath,
		GitHubToken:     firstNonEmpty(rawConfig.GitHub.Token, firstEnvironmentValue(githubTokenEnvironmentKeys)),
		GitHubActor:     firstNonEmpty(rawConfig.GitHub.Actor, firstEnvironmentValue(githubActorEnvironmentKeys)),
		ExcludedRepos:   stringSet(rawConfig.Filters.ExcludedRepos, false),
		ExcludedLangs:   stringSet(rawConfig.Filters.ExcludedLangs, true),
		ExcludeForks:    rawConfig.Filters.ExcludeForks,
		IncludeExternal: rawConfig.Filters.IncludeExternal,
		RecencyHalfLife: defaultRecencyHalfLife,
		RecencyFloor:    defaultRecencyFloor,
		LogLevel:        firstNonEmpty(rawConfig.Logging.Level, "INFO"),
	}

	if cfg.GitHubToken == "" {
		return Config{}, fmt.Errorf("config file %q is missing github.token", cleanPath)
	}
	if cfg.GitHubActor == "" {
		return Config{}, fmt.Errorf("config file %q is missing github.actor", cleanPath)
	}

	if rawConfig.Recency.HalfLife != "" {
		parsedHalfLife, err := parseHalfLife(rawConfig.Recency.HalfLife)
		if err != nil {
			return Config{}, err
		}
		cfg.RecencyHalfLife = parsedHalfLife
	}
	if rawConfig.Recency.Floor != nil {
		parsedFloor, err := validateFloor(*rawConfig.Recency.Floor)
		if err != nil {
			return Config{}, err
		}
		cfg.RecencyFloor = parsedFloor
	}

	cfg.Path = resolvedPath(cleanPath)
	return cfg, nil
}

func resolvedPath(path string) string {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return absolutePath
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmedValue := strings.TrimSpace(value)
		if trimmedValue != "" {
			return trimmedValue
		}
	}
	return ""
}

func firstEnvironmentValue(keys []string) string {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return value
		}
	}
	return ""
}

func stringSet(values []string, lower bool) map[string]struct{} {
	result := make(map[string]struct{})
	for _, value := range values {
		trimmedValue := strings.TrimSpace(value)
		if trimmedValue == "" {
			continue
		}
		if lower {
			trimmedValue = strings.ToLower(trimmedValue)
		}
		result[trimmedValue] = struct{}{}
	}
	return result
}

func parseHalfLife(value string) (time.Duration, error) {
	trimmedValue := strings.TrimSpace(strings.ToLower(value))
	if strings.HasSuffix(trimmedValue, "y") {
		yearCountValue := strings.TrimSuffix(trimmedValue, "y")
		yearCount, err := strconv.ParseFloat(yearCountValue, 64)
		if err != nil {
			return 0, fmt.Errorf("parse recency.half_life years %q: %w", value, err)
		}
		return time.Duration(yearCount * float64(hoursPerYear) * float64(time.Hour)), nil
	}

	duration, err := time.ParseDuration(trimmedValue)
	if err != nil {
		return 0, fmt.Errorf("parse recency.half_life %q: %w", value, err)
	}
	return duration, nil
}

func validateFloor(value float64) (float64, error) {
	if value < 0 || value > 1 {
		return 0, fmt.Errorf("recency.floor must be between 0 and 1")
	}
	return value, nil
}
