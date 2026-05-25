package profilecounter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseBadgeCount(t *testing.T) {
	badgeSVG := `<svg>
<text x="40.6" y="14">Profile views</text>
<text x="99" y="15" fill="#010101" fill-opacity=".3">1,159</text>
<text x="99" y="14">1,159</text>
</svg>`

	count, err := parseBadgeCount(badgeSVG)
	if err != nil {
		t.Fatalf("parseBadgeCount returned error: %v", err)
	}
	if count != 1159 {
		t.Fatalf("expected 1159, got %d", count)
	}
}

func TestFetchProfileViews(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("username") != "agoodkind" {
			t.Fatalf("expected username agoodkind, got %q", request.URL.Query().Get("username"))
		}
		_, _ = fmt.Fprint(responseWriter, `<svg><text>Profile views</text><text>1,234</text></svg>`)
	}))
	defer server.Close()

	client := &Client{
		httpClient: server.Client(),
		endpoint:   server.URL,
	}

	count, err := client.FetchProfileViews(context.Background(), "agoodkind")
	if err != nil {
		t.Fatalf("FetchProfileViews returned error: %v", err)
	}
	if count != 1234 {
		t.Fatalf("expected 1234, got %d", count)
	}
}
