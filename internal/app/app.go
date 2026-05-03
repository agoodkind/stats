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

func Run(ctx context.Context, cfg internalconfig.Config, args []string) error {
	logger := internallogging.LoggerFromContext(ctx)
	command := "generate"
	if len(args) > 0 {
		command = strings.TrimSpace(args[0])
	}

	logger.InfoContext(ctx, "starting stats-gh", "command", command, "actor", cfg.GitHubActor)

	switch command {
	case "generate":
		return runGenerate(ctx, cfg)
	case "diagnose":
		return runDiagnose(ctx, cfg)
	case "version":
		return internaloutput.WriteStdout(internalversion.String() + "\n")
	case "":
		return fmt.Errorf("empty command")
	default:
		return fmt.Errorf("unknown command %q", command)
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
