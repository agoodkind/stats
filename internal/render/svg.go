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
	// languagesChromeHeight covers the h2, the progress bar, and the
	// foreignObject vertical chrome above and below the language pills.
	languagesChromeHeight = 90
	// languagesItemRowHeight is the per-row pitch for li elements with the
	// shared svg line-height of 21px plus a couple of pixels of slack.
	languagesItemRowHeight = 22
	// languagesContainerWidth is the inner width of the foreignObject the
	// language list flows inside.
	languagesContainerWidth = 318
	// languagesItemBasePx covers the octicon, its margin, the lang span's
	// trailing margin, and the li margin-right so the rest of the width is
	// just the label + percent text.
	languagesItemBasePx = 48
	// languagesCharPx is a pessimistic 12px sans-serif char width. We
	// overestimate so the packing simulation always produces at least as many
	// rows as the browser will, never fewer.
	languagesCharPx       = 7
	languagesMinSVGHeight = 140
	topReposFrameHeight   = 42
	topReposTitleHeight   = 34
	topReposCardHeight    = 74
	topReposGridGap       = 8
	topReposMinSVGHeight  = 150
)

var hexColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type svgTemplateData interface {
	svgTemplateMarker()
}

type overviewTemplateData struct {
	Name                    string
	Stars                   string
	Forks                   string
	TotalContributions      string
	LinesChanged            string
	Views                   string
	OwnedRepositories       string
	ContributedRepositories string
}

func (overviewTemplateData) svgTemplateMarker() {}

type languageTemplateData struct {
	Name                string
	Items               []languageTemplateItem
	SVGHeight           int
	ForeignObjectHeight int
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
	RepositoryName   string
	Description      string
	LangColor        string
	StarsDisplay     string
	UpdatedAgo       string
	AnimationDelayMs int
}

type topReposTemplateData struct {
	Name                string
	Repos               []topRepoView
	SVGHeight           int
	ForeignObjectHeight int
}

func (topReposTemplateData) svgTemplateMarker() {}

const animationDelayStepMs = 150

// Options controls render-time behavior that is not part of the data model
// (e.g. percentage-compression shape for the languages chart).
type Options struct {
	// LanguagesCompression selects how language byte totals are mapped to
	// displayed percentages: "linear", "sqrt", or "log". Empty defaults to
	// "sqrt".
	LanguagesCompression string
}

// WriteSVGs renders the three stats-gh SVGs (overview, languages, top_repos)
// from the supplied summary into the generated/ directory.
func WriteSVGs(summary internalmodel.StatsSummary, opts Options) error {
	if err := os.MkdirAll(generatedDirectory, 0o755); err != nil {
		slog.Error("create generated directory", "directory", generatedDirectory, "error", err)
		return fmt.Errorf("create generated directory: %w", err)
	}
	if err := writeTemplate(filepath.Join(generatedDirectory, "overview.svg"), overviewTemplatePath, buildOverviewTemplateData(summary.Overview)); err != nil {
		return err
	}
	if err := writeTemplate(filepath.Join(generatedDirectory, "languages.svg"), languagesTemplatePath, buildLanguageTemplateData(summary.Overview.Name, summary.Languages, opts.LanguagesCompression)); err != nil {
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
		Name:                    strings.TrimSpace(overview.Name),
		Stars:                   formatInteger(overview.Stars),
		Forks:                   formatInteger(overview.Forks),
		TotalContributions:      formatInteger(overview.TotalContributions),
		LinesChanged:            formatInteger(overview.LinesChanged),
		Views:                   formatInteger(overview.Views),
		OwnedRepositories:       formatInteger(overview.OwnedRepositories),
		ContributedRepositories: formatInteger(overview.ContributedRepositories),
	}
}

func buildLanguageTemplateData(name string, languages []internalmodel.LanguageStat, compression string) languageTemplateData {
	// Display percentages are compressed before normalization so a single
	// dominant language does not crowd out smaller-but-still-substantive
	// ones. The model's untransformed .Percentage stays unchanged so
	// diagnostics still report the raw distribution.
	compress := compressionFor(compression)
	totalCompressed := 0.0
	for _, language := range languages {
		totalCompressed += compress(language.Weighted)
	}
	items := make([]languageTemplateItem, 0, len(languages))
	for index, language := range languages {
		percentage := 0.0
		if totalCompressed > 0 {
			percentage = clampPercentage(100 * compress(language.Weighted) / totalCompressed)
		}
		items = append(items, languageTemplateItem{
			Name:              strings.TrimSpace(language.Name),
			Color:             sanitizeColor(language.Color),
			PercentageValue:   percentage,
			PercentageDisplay: fmt.Sprintf("%.2f", percentage),
			AnimationDelayMs:  index * animationDelayStepMs,
		})
	}
	rows := packLanguageRows(items)
	svgHeight := max(languagesChromeHeight+rows*languagesItemRowHeight, languagesMinSVGHeight)
	displayName := strings.TrimSpace(name)
	if displayName == "" {
		displayName = topRepositoryNameDefault
	}
	return languageTemplateData{
		Name:                displayName,
		Items:               items,
		SVGHeight:           svgHeight,
		ForeignObjectHeight: svgHeight - 34,
	}
}

// packLanguageRows simulates the browser's first-fit inline-flex wrap so the
// SVG can be sized to the actual row count rather than a fixed estimate. Each
// item's width is approximated from the visible character count; widths are
// pessimistic on purpose so the simulation never undercounts rows.
func packLanguageRows(items []languageTemplateItem) int {
	if len(items) == 0 {
		return 0
	}
	rows := 1
	rowWidth := 0
	for _, item := range items {
		itemWidth := languagesItemBasePx + languagesCharPx*(len(item.Name)+len(item.PercentageDisplay)+1)
		if rowWidth > 0 && rowWidth+itemWidth > languagesContainerWidth {
			rows++
			rowWidth = itemWidth
			continue
		}
		rowWidth += itemWidth
	}
	return rows
}

func buildTopReposTemplateData(name string, repos []internalmodel.RepoActivity) topReposTemplateData {
	rows := make([]topRepoView, 0, len(repos))
	for index, repo := range repos {
		rows = append(rows, topRepoView{
			RepositoryName:   stripOwnerPrefix(strings.TrimSpace(repo.RepositoryName)),
			Description:      truncateString(strings.TrimSpace(repo.Description), 38),
			LangColor:        sanitizeColor(repo.LangColor),
			StarsDisplay:     formatInteger(repo.Stars),
			UpdatedAgo:       repo.UpdatedAgo,
			AnimationDelayMs: index * animationDelayStepMs,
		})
	}

	displayName := strings.TrimSpace(name)
	if displayName == "" {
		displayName = topRepositoryNameDefault
	}
	svgHeight := topReposSVGHeight(len(rows))
	return topReposTemplateData{
		Name:                displayName,
		Repos:               rows,
		SVGHeight:           svgHeight,
		ForeignObjectHeight: svgHeight - topReposFrameHeight,
	}
}

func topReposSVGHeight(repoCount int) int {
	if repoCount <= 0 {
		return topReposMinSVGHeight
	}
	rowCount := (repoCount + 1) / 2
	contentHeight := topReposTitleHeight + rowCount*topReposCardHeight
	if rowCount > 1 {
		contentHeight += (rowCount - 1) * topReposGridGap
	}
	return max(topReposFrameHeight+contentHeight, topReposMinSVGHeight)
}

// compressionMode mirrors the LanguagesCompression enum from config but is
// kept local so the render package does not depend on the config package.
type compressionMode string

const (
	compressionLinear compressionMode = "linear"
	compressionSqrt   compressionMode = "sqrt"
	compressionLog    compressionMode = "log"
)

func compressionFor(mode string) func(float64) float64 {
	parsed := compressionMode(strings.ToLower(strings.TrimSpace(mode)))
	switch parsed {
	case compressionLinear:
		return func(value float64) float64 { return value }
	case compressionLog:
		return func(value float64) float64 {
			if value <= 0 {
				return 0
			}
			return math.Log10(1 + value)
		}
	case compressionSqrt:
		fallthrough
	default:
		return func(value float64) float64 {
			if value <= 0 {
				return 0
			}
			return math.Sqrt(value)
		}
	}
}

func truncateString(value string, maxLen int) string {
	if len(value) <= maxLen {
		return value
	}
	if maxLen <= 1 {
		return value[:maxLen]
	}
	return value[:maxLen-1] + "…"
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
