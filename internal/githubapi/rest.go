package githubapi

import (
	"context"

	internalmodel "github.com/agoodkind/stats/internal/model"
	github "github.com/google/go-github/v81/github"
)

func (client *Client) FetchViews(ctx context.Context, repositories []internalmodel.Repository) (int, error) {
	totalViews := 0
	options := &github.TrafficBreakdownOptions{Per: "week"}

	for _, repository := range repositories {
		owner, repo, err := splitRepositoryName(repository.NameWithOwner)
		if err != nil {
			continue
		}

		views, _, err := client.rest.Repositories.ListTrafficViews(ctx, owner, repo, options)
		if err != nil {
			if isAcceptedError(err) {
				continue
			}
			continue
		}
		if views == nil {
			continue
		}
		totalViews += views.GetCount()
	}

	return totalViews, nil
}

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
