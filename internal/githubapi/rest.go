package githubapi

import (
	"context"
	"log/slog"

	internalmodel "github.com/agoodkind/stats/internal/model"
	github "github.com/google/go-github/v81/github"
)

// FetchViews sums the trailing-14-day traffic view counts across every owned
// repository. Repos the token lacks push access to return 403 and are skipped
// with a warning log.
func (client *Client) FetchViews(ctx context.Context, repositories []internalmodel.Repository) (int, error) {
	totalViews := 0
	options := &github.TrafficBreakdownOptions{Per: "day"}

	for _, repository := range repositories {
		owner, repo, err := splitRepositoryName(repository.NameWithOwner)
		if err != nil {
			slog.WarnContext(ctx, "skip views: repository name", "repository", repository.NameWithOwner, "error", err)
			continue
		}

		views, _, err := client.rest.Repositories.ListTrafficViews(ctx, owner, repo, options)
		if err != nil {
			if isAcceptedError(err) {
				continue
			}
			slog.WarnContext(ctx, "skip views: fetch failed (does the token have push access to this repo?)", "repository", repository.NameWithOwner, "error", err)
			continue
		}
		if views == nil {
			continue
		}
		totalViews += views.GetCount()
	}

	return totalViews, nil
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
