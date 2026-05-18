// Package render turns a collected StatsSummary into the overview, languages,
// and top_repos SVG files written under generated/.
package render

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	internalmodel "github.com/agoodkind/stats/internal/model"
)

//go:embed templates/*.svg.tmpl
var embeddedTemplates embed.FS

const (
	generatedDirectory       = "generated"
	overviewTemplatePath     = "templates/overview.svg.tmpl"
	languagesTemplatePath    = "templates/languages.svg.tmpl"
	topReposTemplatePath     = "templates/top_repos.svg.tmpl"
	fallbackColor            = "#586069"
	topRepositoryNameDefault = "Top GitHub Repos"
	topRepoMinBarPercent     = 25.0
)

var hexColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type svgTemplateData interface {
	svgTemplateMarker()
}

type overviewTemplateData struct {
	Name               string
	Stars              string
	Forks              string
	TotalContributions string
	LinesChanged       string
	Views              string
	RepositoryCount    string
}

func (overviewTemplateData) svgTemplateMarker() {}

type languageTemplateData struct {
	Items []languageTemplateItem
}

func (languageTemplateData) svgTemplateMarker() {}

type languageTemplateItem struct {
	Name              string
	Color             string
	PercentageValue   float64
	PercentageDisplay string
	AnimationDelayMs  int
}

type topRepoView struct {
	RepositoryName      string
	Display             string
	Color               string
	WidthDisplayPercent string
	AnimationDelayMs    int
}

type topReposTemplateData struct {
	Name  string
	Repos []topRepoView
}

func (topReposTemplateData) svgTemplateMarker() {}

const animationDelayStepMs = 150

// WriteSVGs renders the three stats-gh SVGs (overview, languages, top_repos)
// from the supplied summary into the generated/ directory.
func WriteSVGs(summary internalmodel.StatsSummary) error {
	if err := os.MkdirAll(generatedDirectory, 0o755); err != nil {
		slog.Error("create generated directory", "directory", generatedDirectory, "error", err)
		return fmt.Errorf("create generated directory: %w", err)
	}
	if err := writeTemplate(filepath.Join(generatedDirectory, "overview.svg"), overviewTemplatePath, buildOverviewTemplateData(summary.Overview)); err != nil {
		return err
	}
	if err := writeTemplate(filepath.Join(generatedDirectory, "languages.svg"), languagesTemplatePath, buildLanguageTemplateData(summary.Languages)); err != nil {
		return err
	}
	return writeTemplate(filepath.Join(generatedDirectory, "top_repos.svg"), topReposTemplatePath, buildTopReposTemplateData(summary.Overview.Name, summary.TopRepos))
}

func writeTemplate(outputPath string, templatePath string, data svgTemplateData) error {
	parsedTemplate, err := template.New(filepath.Base(templatePath)).ParseFS(embeddedTemplates, templatePath)
	if err != nil {
		slog.Error("parse svg template", "template", templatePath, "error", err)
		return fmt.Errorf("parse template %q: %w", templatePath, err)
	}

	var buffer bytes.Buffer
	if err := parsedTemplate.Execute(&buffer, data); err != nil {
		slog.Error("execute svg template", "template", templatePath, "error", err)
		return fmt.Errorf("execute template %q: %w", templatePath, err)
	}

	output := strings.ReplaceAll(buffer.String(), "GH_DARK_MODE_ONLY", "gh-dark-mode-only")
	if err := os.WriteFile(outputPath, []byte(output), 0o600); err != nil {
		slog.Error("write svg", "path", outputPath, "error", err)
		return fmt.Errorf("write svg %q: %w", outputPath, err)
	}
	return nil
}

func buildOverviewTemplateData(overview internalmodel.OverviewStats) overviewTemplateData {
	return overviewTemplateData{
		Name:               strings.TrimSpace(overview.Name),
		Stars:              formatInteger(overview.Stars),
		Forks:              formatInteger(overview.Forks),
		TotalContributions: formatInteger(overview.TotalContributions),
		LinesChanged:       formatInteger(overview.LinesChanged),
		Views:              formatInteger(overview.Views),
		RepositoryCount:    formatInteger(overview.RepositoryCount),
	}
}

func buildLanguageTemplateData(languages []internalmodel.LanguageStat) languageTemplateData {
	items := make([]languageTemplateItem, 0, len(languages))
	for index, language := range languages {
		percentage := clampPercentage(language.Percentage)
		items = append(items, languageTemplateItem{
			Name:              strings.TrimSpace(language.Name),
			Color:             sanitizeColor(language.Color),
			PercentageValue:   percentage,
			PercentageDisplay: fmt.Sprintf("%.2f", percentage),
			AnimationDelayMs:  index * animationDelayStepMs,
		})
	}
	return languageTemplateData{Items: items}
}

func buildTopReposTemplateData(name string, repos []internalmodel.RepoActivity) topReposTemplateData {
	maxScore := 0.0
	minScore := math.MaxFloat64
	for _, repo := range repos {
		if repo.Score > maxScore {
			maxScore = repo.Score
		}
		if repo.Score < minScore {
			minScore = repo.Score
		}
	}
	scoreRange := maxScore - minScore
	colors := []string{"#3572A5", "#555555", "#3178c6", "#DA3434", "#89e051", "#00ADD8"}
	rows := make([]topRepoView, 0, len(repos))
	for index, repo := range repos {
		width := 100.0
		if scoreRange > 0 {
			width = topRepoMinBarPercent + (repo.Score-minScore)/scoreRange*(100-topRepoMinBarPercent)
		}
		rows = append(rows, topRepoView{
			RepositoryName:      stripOwnerPrefix(strings.TrimSpace(repo.RepositoryName)),
			Display:             fmt.Sprintf("%s · ★%s", formatInteger(repo.Commits), formatInteger(repo.Stars)),
			Color:               sanitizeColor(colors[index%len(colors)]),
			WidthDisplayPercent: fmt.Sprintf("%.2f", clampPercentage(width)),
			AnimationDelayMs:    index * animationDelayStepMs,
		})
	}

	displayName := strings.TrimSpace(name)
	if displayName == "" {
		displayName = topRepositoryNameDefault
	}
	return topReposTemplateData{Name: displayName, Repos: rows}
}

func stripOwnerPrefix(repositoryName string) string {
	if _, after, found := strings.Cut(repositoryName, "/"); found {
		return after
	}
	return repositoryName
}

func sanitizeColor(color string) string {
	trimmedColor := strings.TrimSpace(color)
	if hexColorPattern.MatchString(trimmedColor) {
		return trimmedColor
	}
	return fallbackColor
}

func formatInteger(value int) string {
	valueText := strconv.Itoa(value)
	if value < 0 {
		return "-" + formatPositiveInteger(valueText[1:])
	}
	return formatPositiveInteger(valueText)
}

func formatPositiveInteger(valueText string) string {
	if len(valueText) <= 3 {
		return valueText
	}

	prefixLength := len(valueText) % 3
	if prefixLength == 0 {
		prefixLength = 3
	}
	parts := []string{valueText[:prefixLength]}
	for index := prefixLength; index < len(valueText); index += 3 {
		parts = append(parts, valueText[index:index+3])
	}
	return strings.Join(parts, ",")
}

func clampPercentage(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
