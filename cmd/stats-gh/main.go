package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/agoodkind/stats/internal/app"
	internalconfig "github.com/agoodkind/stats/internal/config"
	internallogging "github.com/agoodkind/stats/internal/logging"
	internaloutput "github.com/agoodkind/stats/internal/output"
	internalversion "github.com/agoodkind/stats/internal/version"
)

func main() {
	ctx := context.Background()
	configPath, commandArgs, err := parseArgs(os.Args[1:])
	if err != nil {
		_ = internaloutput.WriteStderr(fmt.Sprintf("argument error: %v\n", err))
		os.Exit(1)
	}

	cfg, err := internalconfig.LoadFromPath(configPath)
	if err != nil {
		_ = internaloutput.WriteStderr(fmt.Sprintf("config error: %v\n", err))
		os.Exit(1)
	}

	logger, closer := internallogging.New(cfg.LogLevel, internalversion.BuildVersion)
	defer func() {
		_ = closer.Close()
	}()

	ctx = internallogging.WithLogger(ctx, logger.With("config_path", cfg.Path, "github_actor", cfg.GitHubActor))
	if err := app.Run(ctx, cfg, commandArgs); err != nil {
		logger.ErrorContext(ctx, "command failed", "err", err)
		os.Exit(1)
	}
}

func parseArgs(args []string) (string, []string, error) {
	flagSet := flag.NewFlagSet("stats-gh", flag.ContinueOnError)
	flagSet.SetOutput(os.Stderr)

	configPath := flagSet.String("config", internalconfig.DefaultPath(), "path to config TOML file")
	if err := flagSet.Parse(args); err != nil {
		return "", nil, err
	}
	return *configPath, flagSet.Args(), nil
}
