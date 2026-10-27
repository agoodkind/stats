package profilecounter_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/agoodkind/stats/internal/profilecounter"
)

func TestFetchProfileViewsParsesBadgeAndPreservesQuery(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Path != "/badge" || request.URL.Query().Get("username") != "sample" ||
			request.URL.Query().Get("style") != "flat" {
			t.Errorf("badge request = %s", request.URL.String())
		}
		_, _ = io.WriteString(writer, `<svg><text>Profile views</text><text fill-opacity=".3">1,159</text><text>1,159</text></svg>`)
	}))
	defer server.Close()
	client := profilecounter.New(profilecounter.Options{HTTPClient: server.Client(), Endpoint: server.URL + "/badge?style=flat"})
	count, err := client.FetchProfileViews(context.Background(), " sample ")
	if err != nil || count != 1159 || requests.Load() != 1 {
		t.Fatalf("profile views = %d, error=%v, requests=%d", count, err, requests.Load())
	}
}

func TestFetchProfileViewsRejectsInvalidResponses(t *testing.T) {
	for _, test := range []struct {
		name         string
		actor        string
		status       int
		body         string
		want         string
		wantRequests int32
	}{
		{name: "missing value", actor: "sample", status: http.StatusOK, body: "<svg><text>Profile views</text></svg>", want: "parse profile counter: badge value not found", wantRequests: 1},
		{name: "HTTP failure", actor: "sample", status: http.StatusServiceUnavailable, body: "unavailable", want: "fetch profile counter: status 503", wantRequests: 1},
		{name: "empty actor", actor: " ", status: http.StatusOK, body: "<svg><text>42</text></svg>", want: "profile counter actor is empty", wantRequests: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				writer.WriteHeader(test.status)
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()
			client := profilecounter.New(profilecounter.Options{HTTPClient: server.Client(), Endpoint: server.URL})
			count, err := client.FetchProfileViews(context.Background(), test.actor)
			if err == nil || !strings.Contains(err.Error(), test.want) || count != 0 || requests.Load() != test.wantRequests {
				t.Fatalf("profile views = %d, error=%v, requests=%d; want %q and %d requests", count, err, requests.Load(), test.want, test.wantRequests)
			}
		})
	}
}
