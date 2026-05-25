package render

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	internalmodel "github.com/agoodkind/stats/internal/model"
)

func TestWriteSVGsEscapesDangerousContent(t *testing.T) {
	temporaryDirectory := t.TempDir()
	currentWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	if err := os.Chdir(temporaryDirectory); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(currentWorkingDirectory); chdirErr != nil {
			t.Fatalf("restore working directory: %v", chdirErr)
		}
	}()

	summary := internalmodel.StatsSummary{
		Overview: internalmodel.OverviewStats{
			Name:                    `A <script>alert("x")</script> & "Owner"`,
			Stars:                   1000,
			Forks:                   2000,
			TotalContributions:      3000000,
			LinesChanged:            4000000,
			Views:                   5000,
			OwnedRepositories:       6000,
			ContributedRepositories: 7000,
		},
		Languages: []internalmodel.LanguageStat{
			{
				Name:       `Go <img src=x onerror=alert(1)>`,
				Color:      `" onload="alert('x')`,
				Bytes:      10,
				Weighted:   10,
				Percentage: 100,
			},
		},
		TopRepos: []internalmodel.RepoActivity{
			{
				RepositoryName: `owner/repo<script>alert("repo")</script>`,
				Description:    `desc <script>alert("d")</script>`,
				LangColor:      "#00ADD8",
				UpdatedAgo:     "today",
				Commits:        42000,
				Stars:          7,
				Score:          1.0,
			},
		},
	}

	if err := WriteSVGs(summary, Options{LanguagesCompression: "sqrt"}); err != nil {
		t.Fatalf("WriteSVGs returned error: %v", err)
	}

	topReposSVGBytes, err := os.ReadFile(filepath.Join(generatedDirectory, "top_repos.svg"))
	if err != nil {
		t.Fatalf("read top_repos.svg: %v", err)
	}
	topReposSVG := string(topReposSVGBytes)
	if strings.Contains(topReposSVG, `<script>alert("repo")</script>`) {
		t.Fatalf("expected top_repos.svg to escape repository names")
	}
	if !strings.Contains(topReposSVG, `repo&lt;script&gt;alert(&#34;repo&#34;)&lt;/script&gt;`) {
		t.Fatalf("expected escaped repository name in top_repos.svg, got %s", topReposSVG)
	}
	if strings.Contains(topReposSVG, `owner/`) {
		t.Fatalf("expected top_repos.svg to strip owner prefix from repository name, got %s", topReposSVG)
	}
	if strings.Contains(topReposSVG, `A <script>alert("x")</script>`) {
		t.Fatalf("expected top_repos.svg to escape owner names")
	}
	if !strings.Contains(topReposSVG, `&#9733; 7`) {
		t.Fatalf("expected star glyph + count in top_repos.svg, got %s", topReposSVG)
	}
	if strings.Contains(topReposSVG, `<script>alert("d")</script>`) {
		t.Fatalf("expected top_repos.svg to escape descriptions")
	}
	if !strings.Contains(topReposSVG, `today`) {
		t.Fatalf("expected updated-ago string in top_repos.svg, got %s", topReposSVG)
	}

	overviewSVGBytes, err := os.ReadFile(filepath.Join(generatedDirectory, "overview.svg"))
	if err != nil {
		t.Fatalf("read overview.svg: %v", err)
	}
	overviewSVG := string(overviewSVGBytes)
	for _, expectedValue := range []string{"1,000", "2,000", "3,000,000", "4,000,000", "5,000", "6,000", "7,000"} {
		if !strings.Contains(overviewSVG, expectedValue) {
			t.Fatalf("expected formatted overview value %q in overview.svg, got %s", expectedValue, overviewSVG)
		}
	}

	languagesSVGBytes, err := os.ReadFile(filepath.Join(generatedDirectory, "languages.svg"))
	if err != nil {
		t.Fatalf("read languages.svg: %v", err)
	}
	languagesSVG := string(languagesSVGBytes)
	if strings.Contains(languagesSVG, `<img src=x onerror=alert(1)>`) {
		t.Fatalf("expected languages.svg to escape language names")
	}
	if !strings.Contains(languagesSVG, `Go &lt;img src=x onerror=alert(1)&gt;`) {
		t.Fatalf("expected escaped language name in languages.svg, got %s", languagesSVG)
	}
	if strings.Contains(languagesSVG, `onload=`) {
		t.Fatalf("expected languages.svg to avoid raw event handler attribute injection")
	}
	if !strings.Contains(languagesSVG, `gh-dark-mode-only`) {
		t.Fatalf("expected dark mode placeholder replacement in languages.svg")
	}
}

func TestTopReposHeightScalesWithRows(t *testing.T) {
	temporaryDirectory := t.TempDir()
	currentWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	if err := os.Chdir(temporaryDirectory); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(currentWorkingDirectory); chdirErr != nil {
			t.Fatalf("restore working directory: %v", chdirErr)
		}
	}()

	repositories := make([]internalmodel.RepoActivity, 0, 6)
	for index := 0; index < 6; index++ {
		repositories = append(repositories, internalmodel.RepoActivity{
			RepositoryName: "agoodkind/repo",
			Description:    "test repository",
			LangColor:      "#00ADD8",
			UpdatedAgo:     "today",
			Stars:          index,
		})
	}

	summary := internalmodel.StatsSummary{
		Overview: internalmodel.OverviewStats{Name: "Alex Goodkind"},
		TopRepos: repositories,
	}
	if err := WriteSVGs(summary, Options{LanguagesCompression: "sqrt"}); err != nil {
		t.Fatalf("WriteSVGs returned error: %v", err)
	}

	topReposSVGBytes, err := os.ReadFile(filepath.Join(generatedDirectory, "top_repos.svg"))
	if err != nil {
		t.Fatalf("read top_repos.svg: %v", err)
	}
	topReposSVG := string(topReposSVGBytes)
	for _, expectedValue := range []string{
		`<svg id="gh-dark-mode-only" width="360" height="314"`,
		`<foreignObject x="21" y="21" width="318" height="272">`,
		`box-sizing: border-box;`,
		`height: 74px;`,
	} {
		if !strings.Contains(topReposSVG, expectedValue) {
			t.Fatalf("expected top_repos.svg to contain %q, got %s", expectedValue, topReposSVG)
		}
	}
}

func TestLanguagesHeightScalesWithWrappedRows(t *testing.T) {
	temporaryDirectory := t.TempDir()
	currentWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	if err := os.Chdir(temporaryDirectory); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(currentWorkingDirectory); chdirErr != nil {
			t.Fatalf("restore working directory: %v", chdirErr)
		}
	}()

	languages := []internalmodel.LanguageStat{
		{Name: "Go", Color: "#00ADD8", Weighted: 100},
		{Name: "TypeScript", Color: "#3178C6", Weighted: 90},
		{Name: "JavaScript", Color: "#F1E05A", Weighted: 80},
		{Name: "Go Template", Color: "#00ADD8", Weighted: 70},
		{Name: "Objective-C", Color: "#438EFF", Weighted: 60},
		{Name: "Shell", Color: "#89E051", Weighted: 50},
		{Name: "Makefile", Color: "#427819", Weighted: 40},
		{Name: "Python", Color: "#3572A5", Weighted: 30},
		{Name: "Assembly", Color: "#6E4C13", Weighted: 20},
		{Name: "Swift", Color: "#F05138", Weighted: 10},
	}
	expectedTemplateData := buildLanguageTemplateData("Alex Goodkind", languages, "sqrt")
	if expectedTemplateData.SVGHeight <= languagesMinSVGHeight {
		t.Fatalf("expected wrapped languages to exceed min height, got %d", expectedTemplateData.SVGHeight)
	}

	summary := internalmodel.StatsSummary{
		Overview:  internalmodel.OverviewStats{Name: "Alex Goodkind"},
		Languages: languages,
	}
	if err := WriteSVGs(summary, Options{LanguagesCompression: "sqrt"}); err != nil {
		t.Fatalf("WriteSVGs returned error: %v", err)
	}

	languagesSVGBytes, err := os.ReadFile(filepath.Join(generatedDirectory, "languages.svg"))
	if err != nil {
		t.Fatalf("read languages.svg: %v", err)
	}
	languagesSVG := string(languagesSVGBytes)
	for _, expectedValue := range []string{
		fmt.Sprintf(`<svg id="gh-dark-mode-only" width="360" height="%d"`, expectedTemplateData.SVGHeight),
		fmt.Sprintf(`<foreignObject x="21" y="17" width="318" height="%d">`, expectedTemplateData.ForeignObjectHeight),
		`Go Template`,
		`Objective-C`,
		`Assembly`,
	} {
		if !strings.Contains(languagesSVG, expectedValue) {
			t.Fatalf("expected languages.svg to contain %q, got %s", expectedValue, languagesSVG)
		}
	}
}

func TestRenderTemplatesHaveSingleSourceOfTruth(t *testing.T) {
	for _, templatePath := range []string{
		filepath.Join("templates", "overview.svg.tmpl"),
		filepath.Join("templates", "languages.svg.tmpl"),
		filepath.Join("templates", "top_repos.svg.tmpl"),
	} {
		if _, err := os.Stat(templatePath); err != nil {
			t.Fatalf("expected embedded template %q to exist: %v", templatePath, err)
		}
	}

	rootTemplatesPath := filepath.Join("..", "..", "templates")
	rootTemplateEntries, err := os.ReadDir(rootTemplatesPath)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("read root templates directory %q: %v", rootTemplatesPath, err)
	}
	for _, entry := range rootTemplateEntries {
		if strings.HasSuffix(entry.Name(), ".svg.tmpl") {
			t.Fatalf("unexpected root template %q; render embeds internal/render/templates", filepath.Join(rootTemplatesPath, entry.Name()))
		}
	}
}
