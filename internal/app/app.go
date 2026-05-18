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
		return internaloutput.WriteStdout(internalversion.String() + "\n")
	case commandEmpty:
		return fmt.Errorf("empty command")
	default:
		return fmt.Errorf("unknown command %q", string(command))
	}
}

func runGenerate(ctx context.Context, cfg internalconfig.Config) error {
	collector := internalcollector.New(internalgithubapi.NewClient(cfg))
	summary, err := collector.Collect(ctx, cfg)
	if err != nil {
		return err
	}
	if err := internalrender.WriteSVGs(summary); err != nil {
		return err
	}
	return internaloutput.WriteStdout("generated overview.svg, languages.svg, and top_repos.svg\n")
}

func runDiagnose(ctx context.Context, cfg internalconfig.Config) error {
	collector := internalcollector.New(internalgithubapi.NewClient(cfg))
	summary, err := collector.Collect(ctx, cfg)
	if err != nil {
		return err
	}
	return internaloutput.WriteStdout(internalcollector.FormatDiagnostics(summary.Diagnostics))
}
