package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPFetcher_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "pluckr/") {
			t.Errorf("missing pluckr User-Agent, got %q", got)
		}
		if got := r.Header.Get("Accept"); !strings.Contains(got, "text/markdown") {
			t.Errorf("expected text/markdown in Accept, got %q", got)
		}
		if got := r.Header.Get("X-Test"); got != "yes" {
			t.Errorf("custom header missing, got %q", got)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>hello</body></html>"))
	}))
	t.Cleanup(srv.Close)

	f := NewHTTP()
	resp, err := f.Fetch(context.Background(), Request{
		URL:     srv.URL + "/page",
		Headers: map[string]string{"X-Test": "yes"},
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !resp.IsSuccess() {
		t.Fatalf("expected 2xx, got %d", resp.Status)
	}
	if !strings.Contains(string(resp.Body), "hello") {
		t.Errorf("unexpected body: %q", resp.Body)
	}
	if !strings.HasPrefix(resp.ContentType, "text/html") {
		t.Errorf("unexpected content-type: %q", resp.ContentType)
	}
}

func TestHTTPFetcher_FollowsRedirects(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("final"))
	}))
	t.Cleanup(final.Close)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	f := NewHTTP()
	resp, err := f.Fetch(context.Background(), Request{URL: srv.URL})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(resp.Body) != "final" {
		t.Errorf("expected redirect to be followed, got body %q", resp.Body)
	}
	if resp.URL != final.URL {
		t.Errorf("expected URL to be the post-redirect URL, got %q", resp.URL)
	}
}

func TestHTTPFetcher_BodyLimit(t *testing.T) {
	big := strings.Repeat("x", 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(big))
	}))
	t.Cleanup(srv.Close)

	f := NewHTTP()
	f.MaxBody = 100
	_, err := f.Fetch(context.Background(), Request{URL: srv.URL})
	if err == nil {
		t.Fatal("expected error from body limit, got nil")
	}
	if !strings.Contains(err.Error(), "exceeded") {
		t.Errorf("expected exceeded error, got %v", err)
	}
}

func TestHTTPFetcher_EmptyURL(t *testing.T) {
	if _, err := NewHTTP().Fetch(context.Background(), Request{}); err == nil {
		t.Fatal("expected error for empty URL")
	}
}
