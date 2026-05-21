// Package config loads stats-gh runtime configuration from a TOML file and
// resolves GitHub credentials from environment-variable fallbacks.
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

const (
	defaultConfigPath      = "config.toml"
	defaultRecencyHalfLife = 3 * 365 * 24 * time.Hour
	defaultRecencyFloor    = 0.05
	hoursPerYear           = 365 * 24
	defaultTopReposLimit   = 6
	defaultStarCoefficient = 2.0
)

var (
	githubTokenEnvironmentKeys = []string{"GH_TOKEN", "GITHUB_TOKEN"}
	githubActorEnvironmentKeys = []string{"GH_ACTOR", "GITHUB_ACTOR"}
)

// ContributedInclude controls whether private external repos count toward the
// Overview "open-source repos I contribute to" tally.
type ContributedInclude string

const (
	// ContributedAll includes private (SSO-gated) external repos.
	ContributedAll ContributedInclude = "all"
	// ContributedPublicOnly drops private external repos.
	ContributedPublicOnly ContributedInclude = "public-only"
)

// LanguagesCompression controls the curve applied to weighted byte totals
// before language percentages are rendered.
type LanguagesCompression string

const (
	// LanguagesLinear keeps raw byte ratios (single dominant language stays
	// dominant).
	LanguagesLinear LanguagesCompression = "linear"
	// LanguagesSqrt compresses with sqrt(bytes) so the long tail gets a
	// visible slice.
	LanguagesSqrt LanguagesCompression = "sqrt"
	// LanguagesLog compresses with log10(1+bytes) - most aggressive
	// flattening.
	LanguagesLog LanguagesCompression = "log"
)

// Config holds the fully-resolved runtime configuration that the rest of the
// application reads.
type Config struct {
	Path        string
	GitHubToken string
	GitHubActor string
	LogLevel    string

	RecencyHalfLife time.Duration
	RecencyFloor    float64

	// Owned-repo inclusion rules.
	ExcludeArchived  bool
	ExcludeDisabled  bool
	ExcludeForks     bool
	RequireLanguages bool
	ExcludedRepos    map[string]struct{}
	ExcludedLangs    map[string]struct{}

	// External (contributed) repos behavior.
	ContributedInclude        ContributedInclude
	ContributedIncludeInLOC   bool
	ContributedIncludeInLangs bool

	// Top-repos card grid.
	TopReposLimit           int
	TopReposStarCoefficient float64

	// Languages chart.
	LanguagesCompression LanguagesCompression

	// Lifetime views counter seed.
	ViewsSeed int
}

type fileConfig struct {
	GitHub      githubConfig      `toml:"github"`
	Logging     loggingConfig     `toml:"logging"`
	Recency     recencyConfig     `toml:"recency"`
	Owned       ownedConfig       `toml:"owned"`
	Contributed contributedConfig `toml:"contributed"`
	TopRepos    topReposConfig    `toml:"top_repos"`
	Languages   languagesConfig   `toml:"languages"`
	Views       viewsConfig       `toml:"views"`
}

type githubConfig struct {
	Token string `toml:"token"`
	Actor string `toml:"actor"`
}

type loggingConfig struct {
	Level string `toml:"level"`
}

type recencyConfig struct {
	HalfLife string   `toml:"half_life"`
	Floor    *float64 `toml:"floor"`
}

type ownedConfig struct {
	ExcludeArchived  *bool    `toml:"exclude_archived"`
	ExcludeDisabled  *bool    `toml:"exclude_disabled"`
	ExcludeForks     *bool    `toml:"exclude_forks"`
	RequireLanguages *bool    `toml:"require_languages"`
	ExcludedRepos    []string `toml:"excluded_repos"`
	ExcludedLangs    []string `toml:"excluded_langs"`
}

type contributedConfig struct {
	Include            string `toml:"include"`
	IncludeInLOC       *bool  `toml:"include_in_loc"`
	IncludeInLanguages *bool  `toml:"include_in_languages"`
}

type topReposConfig struct {
	Limit           *int     `toml:"limit"`
	StarCoefficient *float64 `toml:"star_coefficient"`
}

type languagesConfig struct {
	Compression string `toml:"compression"`
}

type viewsConfig struct {
	Seed *int `toml:"seed"`
}

// DefaultPath returns the default config file path used when no -config flag is supplied.
func DefaultPath() string {
	return defaultConfigPath
}

// LoadFromPath reads, parses, and validates the TOML configuration at the given path,
// merging in environment-variable overrides for the GitHub token and actor.
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
		Path:                      cleanPath,
		GitHubToken:               firstNonEmpty(rawConfig.GitHub.Token, firstEnvironmentValue(githubTokenEnvironmentKeys)),
		GitHubActor:               firstNonEmpty(rawConfig.GitHub.Actor, firstEnvironmentValue(githubActorEnvironmentKeys)),
		LogLevel:                  firstNonEmpty(rawConfig.Logging.Level, "INFO"),
		RecencyHalfLife:           defaultRecencyHalfLife,
		RecencyFloor:              defaultRecencyFloor,
		ExcludeArchived:           boolDefaultTrue(rawConfig.Owned.ExcludeArchived),
		ExcludeDisabled:           boolDefaultTrue(rawConfig.Owned.ExcludeDisabled),
		ExcludeForks:              boolDefaultTrue(rawConfig.Owned.ExcludeForks),
		RequireLanguages:          boolDefaultTrue(rawConfig.Owned.RequireLanguages),
		ExcludedRepos:             stringSet(rawConfig.Owned.ExcludedRepos, false),
		ExcludedLangs:             stringSet(rawConfig.Owned.ExcludedLangs, true),
		ContributedInclude:        ContributedAll,
		ContributedIncludeInLOC:   boolDefaultTrue(rawConfig.Contributed.IncludeInLOC),
		ContributedIncludeInLangs: boolDefaultTrue(rawConfig.Contributed.IncludeInLanguages),
		TopReposLimit:             intOrDefault(rawConfig.TopRepos.Limit, defaultTopReposLimit),
		TopReposStarCoefficient:   floatOrDefault(rawConfig.TopRepos.StarCoefficient, defaultStarCoefficient),
		LanguagesCompression:      LanguagesSqrt,
		ViewsSeed:                 intOrDefault(rawConfig.Views.Seed, 0),
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

	if trimmed := strings.TrimSpace(rawConfig.Contributed.Include); trimmed != "" {
		parsed, err := parseContributedInclude(trimmed)
		if err != nil {
			return Config{}, err
		}
		cfg.ContributedInclude = parsed
	}

	if trimmed := strings.TrimSpace(rawConfig.Languages.Compression); trimmed != "" {
		parsed, err := parseLanguagesCompression(trimmed)
		if err != nil {
			return Config{}, err
		}
		cfg.LanguagesCompression = parsed
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

// boolDefaultTrue returns *value if set, otherwise true. Every owned-/
// contributed-exclusion knob in this config defaults to true; an explicit
// helper makes the call-site assignments mechanical rather than passing a
// constant `true` argument each time.
func boolDefaultTrue(value *bool) bool {
	if value == nil {
		return true
	}
	return *value
}

func intOrDefault(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func floatOrDefault(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}

func parseContributedInclude(value string) (ContributedInclude, error) {
	switch ContributedInclude(strings.ToLower(value)) {
	case ContributedAll:
		return ContributedAll, nil
	case ContributedPublicOnly:
		return ContributedPublicOnly, nil
	default:
		err := fmt.Errorf("contributed.include %q must be %q or %q", value, ContributedAll, ContributedPublicOnly)
		slog.Error("invalid contributed.include", "value", value, "error", err)
		return "", err
	}
}

func parseLanguagesCompression(value string) (LanguagesCompression, error) {
	switch LanguagesCompression(strings.ToLower(value)) {
	case LanguagesLinear:
		return LanguagesLinear, nil
	case LanguagesSqrt:
		return LanguagesSqrt, nil
	case LanguagesLog:
		return LanguagesLog, nil
	default:
		err := fmt.Errorf("languages.compression %q must be %q, %q, or %q", value, LanguagesLinear, LanguagesSqrt, LanguagesLog)
		slog.Error("invalid languages.compression", "value", value, "error", err)
		return "", err
	}
}

func parseHalfLife(value string) (time.Duration, error) {
	trimmedValue := strings.TrimSpace(strings.ToLower(value))
	if yearCountValue, ok := strings.CutSuffix(trimmedValue, "y"); ok {
		yearCount, err := strconv.ParseFloat(yearCountValue, 64)
		if err != nil {
			slog.Error("parse recency.half_life years", "value", value, "error", err)
			return 0, fmt.Errorf("parse recency.half_life years %q: %w", value, err)
		}
		return time.Duration(yearCount * float64(hoursPerYear) * float64(time.Hour)), nil
	}

	duration, err := time.ParseDuration(trimmedValue)
	if err != nil {
		slog.Error("parse recency.half_life", "value", value, "error", err)
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
