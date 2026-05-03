package githubapi

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"strings"
	"time"

	internalmodel "github.com/agoodkind/stats/internal/model"
)

//go:embed queries/*.graphql
var graphQLQueries embed.FS

type graphQLResponse[T any] struct {
	Data   T              `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type graphQLError struct {
	Message string `json:"message"`
}

type viewerPageResponse struct {
	Viewer viewerRepositoriesPayload `json:"viewer"`
}

type viewerRepositoriesPayload struct {
	Login                     string               `json:"login"`
	Name                      string               `json:"name"`
	Repositories              repositoryConnection `json:"repositories"`
	RepositoriesContributedTo repositoryConnection `json:"repositoriesContributedTo"`
}

type repositoryConnection struct {
	Nodes    []repositoryNode `json:"nodes"`
	PageInfo pageInfo         `json:"pageInfo"`
}

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type repositoryNode struct {
	NameWithOwner string             `json:"nameWithOwner"`
	IsFork        bool               `json:"isFork"`
	IsArchived    bool               `json:"isArchived"`
	IsDisabled    bool               `json:"isDisabled"`
	PushedAt      string             `json:"pushedAt"`
	UpdatedAt     string             `json:"updatedAt"`
	Stargazers    totalCountNode     `json:"stargazers"`
	ForkCount     int                `json:"forkCount"`
	Languages     languageConnection `json:"languages"`
}

type totalCountNode struct {
	TotalCount int `json:"totalCount"`
}

type languageConnection struct {
	Edges []languageEdge `json:"edges"`
}

type languageEdge struct {
	Size int          `json:"size"`
	Node languageNode `json:"node"`
}

type languageNode struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type contributionYearsResponse struct {
	Viewer struct {
		ContributionsCollection struct {
			ContributionYears []int `json:"contributionYears"`
		} `json:"contributionsCollection"`
	} `json:"viewer"`
}

type contributionsByYearResponse struct {
	Viewer map[string]yearContributions `json:"viewer"`
}

type yearContributions struct {
	ContributionCalendar struct {
		TotalContributions int `json:"totalContributions"`
	} `json:"contributionCalendar"`
}

type actorIDResponse struct {
	User struct {
		ID string `json:"id"`
	} `json:"user"`
}

type repositoryCommitActivityResponse struct {
	Repository struct {
		DefaultBranchRef struct {
			Target struct {
				History commitHistoryConnection `json:"history"`
			} `json:"target"`
		} `json:"defaultBranchRef"`
	} `json:"repository"`
}

type commitHistoryConnection struct {
	Nodes    []commitActivityNode `json:"nodes"`
	PageInfo pageInfo             `json:"pageInfo"`
}

type commitActivityNode struct {
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

func (client *Client) FetchViewerRepositories(ctx context.Context) (internalmodel.ViewerSummary, []internalmodel.Repository, []internalmodel.Repository, error) {
	queryTemplate, err := loadGraphQLQuery("queries/viewer_repositories.graphql")
	if err != nil {
		return internalmodel.ViewerSummary{}, nil, nil, err
	}

	ownedRepositories := make([]internalmodel.Repository, 0)
	externalRepositories := make([]internalmodel.Repository, 0)
	viewer := internalmodel.ViewerSummary{}
	ownedCursor := ""
	externalCursor := ""

	for {
		var response graphQLResponse[viewerPageResponse]
		variables := map[string]any{
			"ownedCursor":    optionalCursor(ownedCursor),
			"externalCursor": optionalCursor(externalCursor),
		}
		if err := client.doGraphQL(ctx, queryTemplate, variables, &response); err != nil {
			slog.ErrorContext(ctx, "fetch viewer repositories", "error", err)
			return viewer, nil, nil, fmt.Errorf("fetch viewer repositories: %w", err)
		}
		if len(response.Errors) > 0 {
			return viewer, nil, nil, fmt.Errorf("graphql error: %s", response.Errors[0].Message)
		}

		viewer = internalmodel.ViewerSummary{Login: response.Data.Viewer.Login, Name: response.Data.Viewer.Name}
		ownedRepositories = append(ownedRepositories, mapRepositories(response.Data.Viewer.Repositories.Nodes, internalmodel.RepositorySourceOwned)...)
		externalRepositories = append(externalRepositories, mapRepositories(response.Data.Viewer.RepositoriesContributedTo.Nodes, internalmodel.RepositorySourceExternal)...)

		ownedHasNext := response.Data.Viewer.Repositories.PageInfo.HasNextPage
		externalHasNext := response.Data.Viewer.RepositoriesContributedTo.PageInfo.HasNextPage
		if !ownedHasNext && !externalHasNext {
			break
		}
		if ownedHasNext {
			ownedCursor = response.Data.Viewer.Repositories.PageInfo.EndCursor
		}
		if externalHasNext {
			externalCursor = response.Data.Viewer.RepositoriesContributedTo.PageInfo.EndCursor
		}
	}

	return viewer, ownedRepositories, externalRepositories, nil
}

func (client *Client) FetchTotalContributions(ctx context.Context) (int, error) {
	yearsQuery, err := loadGraphQLQuery("queries/contribution_years.graphql")
	if err != nil {
		return 0, err
	}
	var yearsResponse graphQLResponse[contributionYearsResponse]
	if err := client.doGraphQL(ctx, yearsQuery, nil, &yearsResponse); err != nil {
		slog.ErrorContext(ctx, "fetch contribution years", "error", err)
		return 0, fmt.Errorf("fetch contribution years: %w", err)
	}
	if len(yearsResponse.Errors) > 0 {
		return 0, fmt.Errorf("graphql error: %s", yearsResponse.Errors[0].Message)
	}

	years := yearsResponse.Data.Viewer.ContributionsCollection.ContributionYears
	if len(years) == 0 {
		return 0, nil
	}

	query, err := buildContributionsByYearQuery(years)
	if err != nil {
		return 0, err
	}
	var contributionsResponse graphQLResponse[contributionsByYearResponse]
	if err := client.doGraphQL(ctx, query, nil, &contributionsResponse); err != nil {
		slog.ErrorContext(ctx, "fetch contributions by year", "error", err)
		return 0, fmt.Errorf("fetch contributions by year: %w", err)
	}
	if len(contributionsResponse.Errors) > 0 {
		return 0, fmt.Errorf("graphql error: %s", contributionsResponse.Errors[0].Message)
	}

	totalContributions := 0
	for _, contribution := range contributionsResponse.Data.Viewer {
		totalContributions += contribution.ContributionCalendar.TotalContributions
	}
	return totalContributions, nil
}

func (client *Client) FetchContributorActivity(ctx context.Context, repositories []internalmodel.Repository) ([]internalmodel.RepoActivity, int, int, error) {
	actorID, err := client.fetchActorID(ctx)
	if err != nil {
		return nil, 0, 0, err
	}
	activityQuery, err := loadGraphQLQuery("queries/repository_commit_activity.graphql")
	if err != nil {
		return nil, 0, 0, err
	}

	activities := make([]internalmodel.RepoActivity, 0, len(repositories))
	totalAdditions := 0
	totalDeletions := 0
	for _, repository := range repositories {
		owner, repo, err := splitRepositoryName(repository.NameWithOwner)
		if err != nil {
			continue
		}

		activity, additions, deletions, err := client.fetchRepositoryCommitActivity(ctx, activityQuery, actorID, owner, repo)
		if err != nil {
			slog.ErrorContext(ctx, "skip repository commit activity", "repository", repository.NameWithOwner, "error", err)
			continue
		}
		if activity <= 0 {
			continue
		}

		activities = append(activities, internalmodel.RepoActivity{
			RepositoryName: repository.NameWithOwner,
			Activity:       activity,
		})
		totalAdditions += additions
		totalDeletions += deletions
	}

	return activities, totalAdditions, totalDeletions, nil
}

func (client *Client) fetchActorID(ctx context.Context) (string, error) {
	query, err := loadGraphQLQuery("queries/actor_id.graphql")
	if err != nil {
		return "", err
	}
	var response graphQLResponse[actorIDResponse]
	variables := map[string]any{"login": client.actor}
	if err := client.doGraphQL(ctx, query, variables, &response); err != nil {
		slog.ErrorContext(ctx, "fetch actor id", "actor", client.actor, "error", err)
		return "", fmt.Errorf("fetch actor id: %w", err)
	}
	if len(response.Errors) > 0 {
		return "", fmt.Errorf("graphql error: %s", response.Errors[0].Message)
	}
	if response.Data.User.ID == "" {
		return "", fmt.Errorf("github actor %q not found", client.actor)
	}
	return response.Data.User.ID, nil
}

func (client *Client) fetchRepositoryCommitActivity(ctx context.Context, query string, actorID string, owner string, repo string) (int, int, int, error) {
	activity := 0
	additions := 0
	deletions := 0
	cursor := ""
	for {
		var response graphQLResponse[repositoryCommitActivityResponse]
		variables := map[string]any{
			"owner":   owner,
			"name":    repo,
			"actorID": actorID,
			"cursor":  optionalCursor(cursor),
		}
		if err := client.doGraphQL(ctx, query, variables, &response); err != nil {
			slog.ErrorContext(ctx, "fetch repository commit activity", "repository", owner+"/"+repo, "error", err)
			return 0, 0, 0, fmt.Errorf("fetch repository commit activity for %s/%s: %w", owner, repo, err)
		}
		if len(response.Errors) > 0 {
			return 0, 0, 0, fmt.Errorf("graphql error: %s", response.Errors[0].Message)
		}

		history := response.Data.Repository.DefaultBranchRef.Target.History
		for _, commit := range history.Nodes {
			additions += commit.Additions
			deletions += commit.Deletions
		}
		activity = additions + deletions
		if !history.PageInfo.HasNextPage {
			break
		}
		cursor = history.PageInfo.EndCursor
	}
	return activity, additions, deletions, nil
}

func mapRepositories(nodes []repositoryNode, source internalmodel.RepositorySource) []internalmodel.Repository {
	repositories := make([]internalmodel.Repository, 0, len(nodes))
	for _, node := range nodes {
		repositories = append(repositories, internalmodel.Repository{
			NameWithOwner: node.NameWithOwner,
			Source:        source,
			IsFork:        node.IsFork,
			IsArchived:    node.IsArchived,
			IsDisabled:    node.IsDisabled,
			Stars:         node.Stargazers.TotalCount,
			Forks:         node.ForkCount,
			PushedAt:      parseGitHubTime(node.PushedAt),
			UpdatedAt:     parseGitHubTime(node.UpdatedAt),
			Languages:     mapLanguages(node.Languages.Edges),
		})
	}
	return repositories
}

func mapLanguages(edges []languageEdge) []internalmodel.RepositoryLanguage {
	languages := make([]internalmodel.RepositoryLanguage, 0, len(edges))
	for _, edge := range edges {
		languages = append(languages, internalmodel.RepositoryLanguage{
			Name:  edge.Node.Name,
			Color: edge.Node.Color,
			Bytes: edge.Size,
		})
	}
	return languages
}

func parseGitHubTime(value string) time.Time {
	parsedTime, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsedTime
}

func optionalCursor(cursor string) any {
	trimmedCursor := strings.TrimSpace(cursor)
	if trimmedCursor == "" {
		return nil
	}
	return trimmedCursor
}

func buildContributionsByYearQuery(years []int) (string, error) {
	queryTemplate, err := loadGraphQLQuery("queries/contributions_by_year.graphql")
	if err != nil {
		return "", err
	}

	parts := make([]string, 0, len(years))
	for _, year := range years {
		nextYear := year + 1
		parts = append(parts, fmt.Sprintf(`    year%d: contributionsCollection(from: "%d-01-01T00:00:00Z", to: "%d-01-01T00:00:00Z") {
      contributionCalendar {
        totalContributions
      }
    }`, year, year, nextYear))
	}

	return fmt.Sprintf(queryTemplate, strings.Join(parts, "\n")), nil
}

func loadGraphQLQuery(path string) (string, error) {
	queryBytes, err := graphQLQueries.ReadFile(path)
	if err != nil {
		slog.Error("read graphql query", "path", path, "error", err)
		return "", fmt.Errorf("read graphql query %q: %w", path, err)
	}
	return string(queryBytes), nil
}
