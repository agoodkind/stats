package render_test

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	internalmodel "github.com/agoodkind/stats/internal/model"
	"github.com/agoodkind/stats/internal/render"
)

type renderedSVG struct {
	XMLName xml.Name `xml:"svg"`
	ID      string   `xml:"id,attr"`
	Width   int      `xml:"width,attr"`
	Height  int      `xml:"height,attr"`
	Frame   struct {
		Width  int `xml:"width,attr"`
		Height int `xml:"height,attr"`
	} `xml:"g>g>foreignObject"`
}

func TestWriteSVGsEscapesDangerousContent(t *testing.T) {
	temporaryDirectory := t.TempDir()
	t.Chdir(temporaryDirectory)
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
	if err := render.WriteSVGs(summary, render.Options{LanguagesCompression: "sqrt"}); err != nil {
		t.Fatalf("WriteSVGs returned error: %v", err)
	}

	topReposSVG, _ := readGeneratedSVG(t, temporaryDirectory, "top_repos.svg")
	for _, unescaped := range []string{`<script>alert("repo")</script>`, `owner/`, `A <script>alert("x")</script>`, `<script>alert("d")</script>`} {
		if strings.Contains(topReposSVG, unescaped) {
			t.Fatalf("top_repos.svg contains unsafe or untrimmed text %q", unescaped)
		}
	}
	for _, expectedValue := range []string{`repo&lt;script&gt;alert(&#34;repo&#34;)&lt;/script&gt;`, `desc &lt;script&gt;alert(&#34;d&#34;)&lt;/script&gt;`, `A &lt;script&gt;`, `&amp;`, `&#9733; 7`, `today`} {
		if !strings.Contains(topReposSVG, expectedValue) {
			t.Fatalf("top_repos.svg does not contain visible value %q: %s", expectedValue, topReposSVG)
		}
	}

	overviewSVG, _ := readGeneratedSVG(t, temporaryDirectory, "overview.svg")
	for _, expectedValue := range []string{"1,000", "2,000", "3,000,000", "4,000,000", "5,000", "6,000", "7,000"} {
		if !strings.Contains(overviewSVG, expectedValue) {
			t.Fatalf("overview.svg does not contain formatted value %q: %s", expectedValue, overviewSVG)
		}
	}
	languagesSVG, _ := readGeneratedSVG(t, temporaryDirectory, "languages.svg")
	if strings.Contains(languagesSVG, `<img src=x onerror=alert(1)>`) || strings.Contains(languagesSVG, `onload=`) {
		t.Fatal("languages.svg contains an unsafe tag or event attribute")
	}
	for _, expectedValue := range []string{`Go &lt;img src=x onerror=alert(1)&gt;`, `#586069`, `100.00%`} {
		if !strings.Contains(languagesSVG, expectedValue) {
			t.Fatalf("languages.svg does not contain escaped visible value %q: %s", expectedValue, languagesSVG)
		}
	}
}

func TestTopReposHeightScalesWithRows(t *testing.T) {
	temporaryDirectory := t.TempDir()
	t.Chdir(temporaryDirectory)
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
		TopRepos: repositories[:1],
	}
	if err := render.WriteSVGs(summary, render.Options{LanguagesCompression: "sqrt"}); err != nil {
		t.Fatalf("render one repository: %v", err)
	}
	_, oneRepository := readGeneratedSVG(t, temporaryDirectory, "top_repos.svg")
	if oneRepository.Height != 150 {
		t.Fatalf("one-repository height = %d, want 150", oneRepository.Height)
	}

	summary.TopRepos = repositories
	if err := render.WriteSVGs(summary, render.Options{LanguagesCompression: "sqrt"}); err != nil {
		t.Fatalf("render six repositories: %v", err)
	}
	topReposSVG, sixRepositories := readGeneratedSVG(t, temporaryDirectory, "top_repos.svg")
	if sixRepositories.Height != 314 || sixRepositories.Height <= oneRepository.Height || sixRepositories.Frame.Height != 272 {
		t.Fatalf("six-repository dimensions = %+v, want height 314 and frame height 272 above baseline %d", sixRepositories, oneRepository.Height)
	}
	if cards := strings.Count(topReposSVG, `class="card"`); cards != len(repositories) {
		t.Fatalf("rendered cards = %d, want %d", cards, len(repositories))
	}
	for _, cardStyle := range []string{`box-sizing: border-box;`, `height: 74px;`} {
		if !strings.Contains(topReposSVG, cardStyle) {
			t.Fatalf("top_repos.svg does not contain card style %q", cardStyle)
		}
	}
}

func TestLanguagesHeightScalesWithWrappedRows(t *testing.T) {
	temporaryDirectory := t.TempDir()
	t.Chdir(temporaryDirectory)
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
	summary := internalmodel.StatsSummary{
		Overview:  internalmodel.OverviewStats{Name: "Alex Goodkind"},
		Languages: languages[:1],
	}
	if err := render.WriteSVGs(summary, render.Options{LanguagesCompression: "sqrt"}); err != nil {
		t.Fatalf("render one language: %v", err)
	}
	_, oneLanguage := readGeneratedSVG(t, temporaryDirectory, "languages.svg")
	if oneLanguage.Height != 140 {
		t.Fatalf("one-language height = %d, want 140", oneLanguage.Height)
	}

	summary.Languages = languages
	if err := render.WriteSVGs(summary, render.Options{LanguagesCompression: "sqrt"}); err != nil {
		t.Fatalf("render ten languages: %v", err)
	}
	languagesSVG, wrappedLanguages := readGeneratedSVG(t, temporaryDirectory, "languages.svg")
	if wrappedLanguages.Height <= oneLanguage.Height || wrappedLanguages.Frame.Height <= oneLanguage.Frame.Height {
		t.Fatalf("wrapped dimensions = %+v, want greater SVG and frame heights than %+v", wrappedLanguages, oneLanguage)
	}
	if labels := strings.Count(languagesSVG, `class="lang"`); labels != len(languages) {
		t.Fatalf("rendered language labels = %d, want %d", labels, len(languages))
	}
	for _, language := range languages {
		if !strings.Contains(languagesSVG, ">"+language.Name+"</span>") {
			t.Fatalf("languages.svg does not display %q", language.Name)
		}
	}
}

func readGeneratedSVG(t *testing.T, directory string, filename string) (string, renderedSVG) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(directory, "generated", filename))
	if err != nil {
		t.Fatalf("read generated SVG %s: %v", filename, err)
	}
	var document renderedSVG
	if err := xml.Unmarshal(contents, &document); err != nil {
		t.Fatalf("decode generated SVG %s: %v", filename, err)
	}
	if document.XMLName.Space != "http://www.w3.org/2000/svg" || document.ID != "gh-dark-mode-only" || document.Width != 360 || document.Height <= 0 || document.Frame.Width != 318 || document.Frame.Height <= 0 {
		t.Fatalf("generated SVG %s has invalid dimensions or root: %+v", filename, document)
	}
	return string(contents), document
}
