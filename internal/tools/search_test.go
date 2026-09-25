package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebSearchTruncatesAndDropsEmpty(t *testing.T) {
	var results []map[string]string
	for i := 0; i < 6; i++ {
		results = append(results, map[string]string{
			"title":       "Title",
			"url":         "https://example.com/" + string(rune('a'+i)),
			"description": strings.Repeat("x", 400),
		})
	}
	results = append(results, map[string]string{"title": "skip", "url": "", "description": "no url"})
	body, err := json.Marshal(map[string]any{"web": map[string]any{"results": results}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") != "secret" {
			t.Errorf("token = %q", r.Header.Get("X-Subscription-Token"))
		}
		if r.URL.Query().Get("count") != "5" {
			t.Errorf("count = %q", r.URL.Query().Get("count"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	raw, err := (&WebSearch{Endpoint: server.URL, APIKey: "secret"}).Execute(context.Background(), json.RawMessage(`{"query":"gopi"}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Results []searchHit `json:"results"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Results) != 5 {
		t.Fatalf("results = %d", len(payload.Results))
	}
	if len([]rune(payload.Results[0].Snippet)) != maxSnippetRunes {
		t.Fatalf("snippet runes = %d", len([]rune(payload.Results[0].Snippet)))
	}
}

func TestWebSearchRefusesOffHostRedirect(t *testing.T) {
	otherHits := 0
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		otherHits++
	}))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/gone", http.StatusFound)
	}))
	defer server.Close()

	_, err := (&WebSearch{Endpoint: server.URL, APIKey: "secret"}).Execute(context.Background(), json.RawMessage(`{"query":"gopi"}`))
	if err == nil || !strings.Contains(err.Error(), "refusing redirect") {
		t.Fatalf("err = %v", err)
	}
	if otherHits != 0 {
		t.Fatalf("other host hits = %d", otherHits)
	}
}

func TestWebSearchMissingKeySkipsHTTP(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer server.Close()
	_, err := (&WebSearch{Endpoint: server.URL}).Execute(context.Background(), json.RawMessage(`{"query":"gopi"}`))
	if err == nil || !strings.Contains(err.Error(), "search_api_key") {
		t.Fatalf("err = %v", err)
	}
	if called {
		t.Fatal("request was sent without a key")
	}
}

func TestWebSearchDefaultsToBrave(t *testing.T) {
	transport := &captureTransport{}
	tool := &WebSearch{APIKey: "secret", Client: &http.Client{Transport: transport}}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"gopi docs"}`)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(transport.url, "https://api.search.brave.com/res/v1/web/search") || !strings.Contains(transport.url, "q=gopi+docs") {
		t.Fatalf("url = %q", transport.url)
	}
}

type captureTransport struct {
	url string
}

func (c *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.url = req.URL.String()
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"web":{"results":[]}}`)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}
