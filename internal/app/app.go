// Package app is the stats-gh control plane: it parses the requested
// subcommand and dispatches to the collector + renderer or to the diagnostics
// formatter.
package app

import (
	"context"
	"fmt"
	"strings"

	internalcollector "github.com/agoodkind/stats/internal/collector"
	internalconfig "github.com/agoodkind/stats/internal/config"
	internalgithubapi "github.com/agoodkind/stats/internal/githubapi"
	internallogging "github.com/agoodkind/stats/internal/logging"
	internaloutput "github.com/agoodkind/stats/internal/output"
	internalrender "github.com/agoodkind/stats/internal/render"
	internalversion "github.com/agoodkind/stats/internal/version"
)

type commandName string

const (
	commandGenerate commandName = "generate"
	commandDiagnose commandName = "diagnose"
	commandVersion  commandName = "version"
	commandEmpty    commandName = ""
)

// Run dispatches to the named subcommand using the supplied configuration.
// It defaults to "generate" when args is empty.
func Run(ctx context.Context, cfg internalconfig.Config, args []string) error {
	logger := internallogging.LoggerFromContext(ctx)
	command := commandGenerate
	if len(args) > 0 {
		command = commandName(strings.TrimSpace(args[0]))
	}

	logger.InfoContext(ctx, "starting stats-gh", "command", string(command), "actor", cfg.GitHubActor)

	switch command {
	case commandGenerate:
		return runGenerate(ctx, cfg)
	case commandDiagnose:
		return runDiagnose(ctx, cfg)
	case commandVersion:
		if err := internaloutput.WriteStdout(internalversion.String() + "\n"); err != nil {
			logger.ErrorContext(ctx, "write version", "error", err)
			return fmt.Errorf("write version: %w", err)
		}
		return nil
	case commandEmpty:
		return fmt.Errorf("empty command")
	default:
		return fmt.Errorf("unknown command %q", string(command))
	}
}

func runGenerate(ctx context.Context, cfg internalconfig.Config) error {
	logger := internallogging.LoggerFromContext(ctx)
	collector := internalcollector.New(internalgithubapi.NewClient(cfg))
	summary, err := collector.Collect(ctx, cfg)
	if err != nil {
		logger.ErrorContext(ctx, "collect stats", "error", err)
		return fmt.Errorf("collect stats: %w", err)
	}
	if err := internalrender.WriteSVGs(summary, internalrender.Options{LanguagesCompression: string(cfg.LanguagesCompression)}); err != nil {
		logger.ErrorContext(ctx, "write svgs", "error", err)
		return fmt.Errorf("write svgs: %w", err)
	}
	if err := internaloutput.WriteStdout("generated overview.svg, languages.svg, and top_repos.svg\n"); err != nil {
		logger.ErrorContext(ctx, "write generate summary", "error", err)
		return fmt.Errorf("write generate summary: %w", err)
	}
	return nil
}

func runDiagnose(ctx context.Context, cfg internalconfig.Config) error {
	logger := internallogging.LoggerFromContext(ctx)
	collector := internalcollector.New(internalgithubapi.NewClient(cfg))
	summary, err := collector.Collect(ctx, cfg)
	if err != nil {
		logger.ErrorContext(ctx, "collect stats for diagnose", "error", err)
		return fmt.Errorf("collect stats for diagnose: %w", err)
	}
	if err := internaloutput.WriteStdout(internalcollector.FormatDiagnostics(summary.Diagnostics)); err != nil {
		logger.ErrorContext(ctx, "write diagnostics", "error", err)
		return fmt.Errorf("write diagnostics: %w", err)
	}
	return nil
}
