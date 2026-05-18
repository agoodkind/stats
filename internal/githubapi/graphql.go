package githubapi

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	internalmodel "github.com/agoodkind/stats/internal/model"
)

//go:embed queries/*.graphql
var graphQLQueries embed.FS

type graphQLError struct {
	Message string `json:"message"`
}

type graphQLVariables interface {
	graphQLVariablesMarker()
}

type viewerRepositoriesVariables struct {
	Login          string  `json:"login"`
	OwnedCursor    *string `json:"ownedCursor,omitempty"`
	ExternalCursor *string `json:"externalCursor,omitempty"`
}

func (viewerRepositoriesVariables) graphQLVariablesMarker() {}

type loginVariables struct {
	Login string `json:"login"`
}

func (loginVariables) graphQLVariablesMarker() {}

type repositoryCommitActivityVariables struct {
	Owner   string  `json:"owner"`
	Name    string  `json:"name"`
	ActorID string  `json:"actorID"`
	Cursor  *string `json:"cursor,omitempty"`
}

func (repositoryCommitActivityVariables) graphQLVariablesMarker() {}

func marshalVariables(variables graphQLVariables) (json.RawMessage, error) {
	encoded, err := json.Marshal(variables)
	if err != nil {
		slog.Error("marshal graphql variables", "error", err)
		return nil, fmt.Errorf("marshal graphql variables: %w", err)
	}
	return encoded, nil
}

type viewerPageResponse struct {
	User actorRepositoriesPayload `json:"user"`
}

type actorRepositoriesPayload struct {
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
	IsPrivate     bool               `json:"isPrivate"`
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
	User struct {
		ContributionsCollection struct {
			ContributionYears []int `json:"contributionYears"`
		} `json:"contributionsCollection"`
	} `json:"user"`
}

type contributionsByYearResponse struct {
	User map[string]yearContributions `json:"user"`
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
	Additions     int    `json:"additions"`
	Deletions     int    `json:"deletions"`
	CommittedDate string `json:"committedDate"`
}

// FetchViewerRepositories returns the authenticated viewer plus two paginated
// repository slices: those owned by the viewer, and those they have
// contributed to but do not own.
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
		variables, err := marshalVariables(viewerRepositoriesVariables{
			Login:          client.actor,
			OwnedCursor:    optionalCursor(ownedCursor),
			ExternalCursor: optionalCursor(externalCursor),
		})
		if err != nil {
			return viewer, nil, nil, err
		}
		envelope, err := client.doGraphQL(ctx, queryTemplate, variables)
		if err != nil {
			slog.ErrorContext(ctx, "fetch viewer repositories", "error", err)
			return viewer, nil, nil, fmt.Errorf("fetch viewer repositories: %w", err)
		}
		if len(envelope.Errors) > 0 {
			return viewer, nil, nil, fmt.Errorf("graphql error: %s", envelope.Errors[0].Message)
		}
		var data viewerPageResponse
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			slog.ErrorContext(ctx, "decode viewer repositories", "error", err)
			return viewer, nil, nil, fmt.Errorf("decode viewer repositories: %w", err)
		}

		viewer = internalmodel.ViewerSummary{Login: data.User.Login, Name: data.User.Name}
		ownedRepositories = append(ownedRepositories, mapRepositories(data.User.Repositories.Nodes, internalmodel.RepositorySourceOwned)...)
		externalRepositories = append(externalRepositories, mapRepositories(data.User.RepositoriesContributedTo.Nodes, internalmodel.RepositorySourceExternal)...)

		ownedHasNext := data.User.Repositories.PageInfo.HasNextPage
		externalHasNext := data.User.RepositoriesContributedTo.PageInfo.HasNextPage
		if !ownedHasNext && !externalHasNext {
			break
		}
		if ownedHasNext {
			ownedCursor = data.User.Repositories.PageInfo.EndCursor
		}
		if externalHasNext {
			externalCursor = data.User.RepositoriesContributedTo.PageInfo.EndCursor
		}
	}

	return viewer, ownedRepositories, externalRepositories, nil
}

// FetchTotalContributions returns the viewer's lifetime contribution count by
// summing per-year contribution calendars across every year GitHub reports
// activity for.
func (client *Client) FetchTotalContributions(ctx context.Context) (int, error) {
	yearsQuery, err := loadGraphQLQuery("queries/contribution_years.graphql")
	if err != nil {
		return 0, err
	}
	yearsVariables, err := marshalVariables(loginVariables{Login: client.actor})
	if err != nil {
		return 0, err
	}
	yearsEnvelope, err := client.doGraphQL(ctx, yearsQuery, yearsVariables)
	if err != nil {
		slog.ErrorContext(ctx, "fetch contribution years", "error", err)
		return 0, fmt.Errorf("fetch contribution years: %w", err)
	}
	if len(yearsEnvelope.Errors) > 0 {
		return 0, fmt.Errorf("graphql error: %s", yearsEnvelope.Errors[0].Message)
	}
	var yearsData contributionYearsResponse
	if err := json.Unmarshal(yearsEnvelope.Data, &yearsData); err != nil {
		slog.ErrorContext(ctx, "decode contribution years", "error", err)
		return 0, fmt.Errorf("decode contribution years: %w", err)
	}

	years := yearsData.User.ContributionsCollection.ContributionYears
	if len(years) == 0 {
		return 0, nil
	}

	query, err := buildContributionsByYearQuery(years)
	if err != nil {
		return 0, err
	}
	contributionsVariables, err := marshalVariables(loginVariables{Login: client.actor})
	if err != nil {
		return 0, err
	}
	contributionsEnvelope, err := client.doGraphQL(ctx, query, contributionsVariables)
	if err != nil {
		slog.ErrorContext(ctx, "fetch contributions by year", "error", err)
		return 0, fmt.Errorf("fetch contributions by year: %w", err)
	}
	if len(contributionsEnvelope.Errors) > 0 {
		return 0, fmt.Errorf("graphql error: %s", contributionsEnvelope.Errors[0].Message)
	}
	var contributionsData contributionsByYearResponse
	if err := json.Unmarshal(contributionsEnvelope.Data, &contributionsData); err != nil {
		slog.ErrorContext(ctx, "decode contributions by year", "error", err)
		return 0, fmt.Errorf("decode contributions by year: %w", err)
	}

	totalContributions := 0
	for _, contribution := range contributionsData.User {
		totalContributions += contribution.ContributionCalendar.TotalContributions
	}
	return totalContributions, nil
}

// FetchContributorActivity walks each repository's default-branch commit
// history filtered by the configured actor, returning per-repo commit counts
// (raw and per-commit recency-weighted) plus the global additions/deletions
// totals. Each commit's contribution to WeightedCommits is
// max(floor, 0.5^(age/halfLife)).
func (client *Client) FetchContributorActivity(ctx context.Context, repositories []internalmodel.Repository, now time.Time, halfLife time.Duration, floor float64) ([]internalmodel.RepoActivity, int, int, error) {
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

		commits, weightedCommits, additions, deletions, err := client.fetchRepositoryCommitActivity(ctx, activityQuery, actorID, owner, repo, now, halfLife, floor)
		if err != nil {
			slog.ErrorContext(ctx, "skip repository commit activity", "repository", repository.NameWithOwner, "error", err)
			continue
		}
		if commits <= 0 {
			continue
		}

		activities = append(activities, internalmodel.RepoActivity{
			RepositoryName:  repository.NameWithOwner,
			Commits:         commits,
			WeightedCommits: weightedCommits,
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
	variables, err := marshalVariables(loginVariables{Login: client.actor})
	if err != nil {
		return "", err
	}
	envelope, err := client.doGraphQL(ctx, query, variables)
	if err != nil {
		slog.ErrorContext(ctx, "fetch actor id", "actor", client.actor, "error", err)
		return "", fmt.Errorf("fetch actor id: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return "", fmt.Errorf("graphql error: %s", envelope.Errors[0].Message)
	}
	var data actorIDResponse
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		slog.ErrorContext(ctx, "decode actor id", "error", err)
		return "", fmt.Errorf("decode actor id: %w", err)
	}
	if data.User.ID == "" {
		return "", fmt.Errorf("github actor %q not found", client.actor)
	}
	return data.User.ID, nil
}

func (client *Client) fetchRepositoryCommitActivity(ctx context.Context, query string, actorID string, owner string, repo string, now time.Time, halfLife time.Duration, floor float64) (int, float64, int, int, error) {
	commits := 0
	weightedCommits := 0.0
	additions := 0
	deletions := 0
	cursor := ""
	for {
		variables, err := marshalVariables(repositoryCommitActivityVariables{
			Owner:   owner,
			Name:    repo,
			ActorID: actorID,
			Cursor:  optionalCursor(cursor),
		})
		if err != nil {
			return 0, 0, 0, 0, err
		}
		envelope, err := client.doGraphQL(ctx, query, variables)
		if err != nil {
			slog.ErrorContext(ctx, "fetch repository commit activity", "repository", owner+"/"+repo, "error", err)
			return 0, 0, 0, 0, fmt.Errorf("fetch repository commit activity for %s/%s: %w", owner, repo, err)
		}
		if len(envelope.Errors) > 0 {
			return 0, 0, 0, 0, fmt.Errorf("graphql error: %s", envelope.Errors[0].Message)
		}
		var data repositoryCommitActivityResponse
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			slog.ErrorContext(ctx, "decode repository commit activity", "repository", owner+"/"+repo, "error", err)
			return 0, 0, 0, 0, fmt.Errorf("decode repository commit activity for %s/%s: %w", owner, repo, err)
		}

		history := data.Repository.DefaultBranchRef.Target.History
		for _, commit := range history.Nodes {
			additions += commit.Additions
			deletions += commit.Deletions
			weightedCommits += commitRecencyWeight(commit.CommittedDate, now, halfLife, floor)
		}
		commits += len(history.Nodes)
		if !history.PageInfo.HasNextPage {
			break
		}
		cursor = history.PageInfo.EndCursor
	}
	return commits, weightedCommits, additions, deletions, nil
}

func commitRecencyWeight(committedDate string, now time.Time, halfLife time.Duration, floor float64) float64 {
	parsedTime := parseGitHubTime(committedDate)
	if parsedTime.IsZero() {
		return floor
	}
	age := now.Sub(parsedTime)
	if age <= 0 {
		return 1
	}
	if halfLife <= 0 {
		return 1
	}
	halfLives := float64(age) / float64(halfLife)
	weight := math.Pow(0.5, halfLives)
	if weight < floor {
		return floor
	}
	return weight
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
			IsPrivate:     node.IsPrivate,
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

func optionalCursor(cursor string) *string {
	trimmedCursor := strings.TrimSpace(cursor)
	if trimmedCursor == "" {
		return nil
	}
	return &trimmedCursor
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
