package tools

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebFetchReadsPlainText(t *testing.T) {
	tool := testFetch(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("hello from the page"))
	})
	raw, err := tool.Execute(t.Context(), []byte(`{"url":"http://example.com/notes"}`))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		URL  string `json:"url"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.URL != "http://example.com/notes" || got.Text != "hello from the page" {
		t.Fatalf("result = %#v", got)
	}
}

func TestWebFetchReadsHTMLAsText(t *testing.T) {
	tool := testFetch(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>Docs</title><style>p{color:red}</style></head><body><script>alert(1)</script><p>Read this</p></body></html>`))
	})
	raw, err := tool.Execute(t.Context(), []byte(`{"url":"http://example.com/docs"}`))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Title string `json:"title"`
		Text  string `json:"text"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Title != "Docs" || got.Text != "Read this" || strings.Contains(got.Text, "alert") || strings.Contains(got.Text, "color") {
		t.Fatalf("result = %#v", got)
	}
}

func TestWebFetchRefusesCrossHostRedirect(t *testing.T) {
	tool := testFetch(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://other.example/page", http.StatusFound)
	})
	_, err := tool.Execute(t.Context(), []byte(`{"url":"http://example.com/start"}`))
	if err == nil || !strings.Contains(err.Error(), "other.example") {
		t.Fatalf("error = %v", err)
	}
}

func TestWebFetchRefusesLoopbackAndMetadata(t *testing.T) {
	tool := &WebFetch{Lookup: func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("1.2.3.4")}, nil
	}}
	if _, err := tool.Execute(t.Context(), []byte(`{"url":"http://127.0.0.1/"}`)); err == nil || !strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("loopback error = %v", err)
	}
	if _, err := tool.Execute(t.Context(), []byte(`{"url":"https://metadata.google.internal/latest"}`)); err == nil || !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("metadata error = %v", err)
	}
}

func TestWebFetchRefusesNonHTTP(t *testing.T) {
	tool := &WebFetch{}
	if _, err := tool.Execute(t.Context(), []byte(`{"url":"file:///etc/passwd"}`)); err == nil || !strings.Contains(err.Error(), "http") {
		t.Fatalf("error = %v", err)
	}
}

func testFetch(t *testing.T, handler http.HandlerFunc) *WebFetch {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &WebFetch{
		Lookup: func(context.Context, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("1.2.3.4")}, nil
		},
		Client: &http.Client{Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			},
		}},
	}
}
