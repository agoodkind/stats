package githubapi

import (
	"context"
	"log/slog"

	internalmodel "github.com/agoodkind/stats/internal/model"
	github "github.com/google/go-github/v81/github"
)

// FetchViews returns a per-repo map of ISO date strings to view counts for
// the trailing-14-day window across every owned repository. Callers merge
// the result into a persisted history file so view totals can accumulate
// past the 14-day API window. Repos the token lacks push access to return
// 403 and are skipped with a warning log.
func (client *Client) FetchViews(ctx context.Context, repositories []internalmodel.Repository) (map[string]map[string]int, error) {
	daily := make(map[string]map[string]int, len(repositories))
	succeeded := 0
	failed := 0
	pending := 0
	freshCount := 0
	options := &github.TrafficBreakdownOptions{Per: "day"}

	for _, repository := range repositories {
		owner, repo, err := splitRepositoryName(repository.NameWithOwner)
		if err != nil {
			slog.WarnContext(ctx, "skip views: repository name", "repository", repository.NameWithOwner, "error", err)
			failed += 1
			continue
		}

		views, _, err := client.rest.Repositories.ListTrafficViews(ctx, owner, repo, options)
		if err != nil {
			if isAcceptedError(err) {
				pending += 1
				continue
			}
			slog.WarnContext(ctx, "skip views: fetch failed (does the token have push access to this repo?)", "repository", repository.NameWithOwner, "error", err)
			failed += 1
			continue
		}
		if views == nil {
			continue
		}
		succeeded += 1
		days := make(map[string]int, len(views.Views))
		for _, day := range views.Views {
			if day == nil || day.Timestamp == nil {
				continue
			}
			date := day.Timestamp.Format("2006-01-02")
			days[date] = day.GetCount()
			freshCount += day.GetCount()
		}
		if len(days) > 0 {
			daily[repository.NameWithOwner] = days
		}
	}

	slog.InfoContext(ctx, "views summary", "fresh_14d_total", freshCount, "succeeded", succeeded, "failed", failed, "pending", pending, "repos", len(repositories))
	return daily, nil
}

// EstimateExternalContributions approximates the viewer's contribution share
// for each external repository by ratio of their additions+deletions to the
// repo's total contributor activity, then scales the repo's language bytes by
// that ratio.
func (client *Client) EstimateExternalContributions(ctx context.Context, repositories []internalmodel.Repository) ([]internalmodel.ExternalContributionEstimate, error) {
	estimates := make([]internalmodel.ExternalContributionEstimate, 0, len(repositories))

	for _, repository := range repositories {
		owner, repo, err := splitRepositoryName(repository.NameWithOwner)
		if err != nil {
			estimates = append(estimates, unknownEstimate(repository.NameWithOwner))
			continue
		}

		stats, _, err := client.rest.Repositories.ListContributorsStats(ctx, owner, repo)
		if err != nil {
			estimates = append(estimates, unknownEstimate(repository.NameWithOwner))
			continue
		}

		actorActivity := 0
		totalActivity := 0
		for _, contributor := range stats {
			if contributor == nil {
				continue
			}

			contributorActivity := 0
			for _, week := range contributor.Weeks {
				if week == nil {
					continue
				}
				contributorActivity += week.GetAdditions() + week.GetDeletions()
			}
			totalActivity += contributorActivity
			if contributor.Author != nil && contributor.Author.GetLogin() == client.actor {
				actorActivity = contributorActivity
			}
		}

		if totalActivity <= 0 || actorActivity <= 0 {
			estimates = append(estimates, unknownEstimate(repository.NameWithOwner))
			continue
		}

		ratio := float64(actorActivity) / float64(totalActivity)
		languages := make([]internalmodel.LanguageStat, 0, len(repository.Languages))
		rawEstimatedBytes := 0.0
		for _, language := range repository.Languages {
			estimatedBytes := float64(language.Bytes) * ratio
			rawEstimatedBytes += estimatedBytes
			languages = append(languages, internalmodel.LanguageStat{
				Name:     language.Name,
				Color:    language.Color,
				Bytes:    int(estimatedBytes),
				Weighted: estimatedBytes,
			})
		}

		estimates = append(estimates, internalmodel.ExternalContributionEstimate{
			RepositoryName:    repository.NameWithOwner,
			Method:            "contributors_stats_ratio",
			Confidence:        "approximate",
			EstimatedRatio:    ratio,
			RawEstimatedBytes: rawEstimatedBytes,
			Languages:         languages,
		})
	}

	return estimates, nil
}

func unknownEstimate(repositoryName string) internalmodel.ExternalContributionEstimate {
	return internalmodel.ExternalContributionEstimate{
		RepositoryName: repositoryName,
		Method:         "contributors_stats_ratio",
		Confidence:     "unknown",
	}
}
