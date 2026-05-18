// Command stats-gh is the CLI entry point that loads configuration, wires up
// the GitHub client, and dispatches to the requested subcommand (generate /
// diagnose / version).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/agoodkind/stats/internal/app"
	internalconfig "github.com/agoodkind/stats/internal/config"
	internallogging "github.com/agoodkind/stats/internal/logging"
	internaloutput "github.com/agoodkind/stats/internal/output"
	internalversion "github.com/agoodkind/stats/internal/version"
)

func main() {
	exitCode := run()
	slog.Info("stats-gh exit", "code", exitCode)
	os.Exit(exitCode)
}

func run() int {
	ctx := context.Background()
	slog.InfoContext(ctx, "stats-gh starting", "version", internalversion.String())
	configPath, commandArgs, err := parseArgs(os.Args[1:])
	if err != nil {
		_ = internaloutput.WriteStderr(fmt.Sprintf("argument error: %v\n", err))
		return 1
	}

	cfg, err := internalconfig.LoadFromPath(configPath)
	if err != nil {
		_ = internaloutput.WriteStderr(fmt.Sprintf("config error: %v\n", err))
		return 1
	}

	logger, closer := internallogging.New(cfg.LogLevel, internalversion.BuildVersion)
	defer func() {
		_ = closer.Close()
	}()

	ctx = internallogging.WithLogger(ctx, logger.With("config_path", cfg.Path, "github_actor", cfg.GitHubActor))
	if err := app.Run(ctx, cfg, commandArgs); err != nil {
		logger.ErrorContext(ctx, "command failed", "err", err)
		return 1
	}
	return 0
}

func parseArgs(args []string) (string, []string, error) {
	flagSet := flag.NewFlagSet("stats-gh", flag.ContinueOnError)
	flagSet.SetOutput(os.Stderr)

	configPath := flagSet.String("config", internalconfig.DefaultPath(), "path to config TOML file")
	if err := flagSet.Parse(args); err != nil {
		slog.Error("parse stats-gh flags", "error", err)
		return "", nil, fmt.Errorf("parse stats-gh flags: %w", err)
	}
	return *configPath, flagSet.Args(), nil
}
