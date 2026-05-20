package render

import (
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
			Name:               `A <script>alert("x")</script> & "Owner"`,
			Stars:              1000,
			Forks:              2000,
			TotalContributions: 3000000,
			LinesChanged:       4000000,
			Views:              5000,
			RepositoryCount:    6000,
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

	if err := WriteSVGs(summary); err != nil {
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
	for _, expectedValue := range []string{"1,000", "2,000", "3,000,000", "4,000,000", "5,000", "6,000"} {
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
