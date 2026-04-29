package pipeline

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SarthakShrivastav-a/pluckr/internal/fetch"
	"github.com/SarthakShrivastav-a/pluckr/internal/registry"
	"github.com/SarthakShrivastav-a/pluckr/internal/retriever"
	"github.com/SarthakShrivastav-a/pluckr/internal/retriever/fts5"
	"github.com/SarthakShrivastav-a/pluckr/internal/store"
)

// stubFetcher hands back a canned response per URL. Used to mock the
// headless escalator so pipeline tests can exercise the empty -> retry
// path without launching a real browser.
type stubFetcher struct {
	responses map[string]*fetch.Response
	calls     int
}

func (s *stubFetcher) Fetch(_ context.Context, req fetch.Request) (*fetch.Response, error) {
	s.calls++
	r, ok := s.responses[req.URL]
	if !ok {
		return nil, errors.New("stub: no canned response for " + req.URL)
	}
	return r, nil
}

// TestPipeline_EscalatesEmptyContentToHeadless drives the empty-content
// detection through the renderer and asserts the escalator runs and
// supplies the body the index ends up holding.
func TestPipeline_EscalatesEmptyContentToHeadless(t *testing.T) {
	// HTTP server returns a JS-shell SPA: empty <main>, no real text.
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><main></main><script>/* would render content */</script></body></html>`))
	}))
	t.Cleanup(httpSrv.Close)

	// The "headless" stub returns the post-render HTML containing real
	// text the renderer can index.
	postRender := []byte(`<html><body><main>
<h1>Welcome</h1>
<p>This is the JavaScript-rendered content the agent actually wants to read about the product features and pricing.</p>
<p>Plenty more prose continues below to make sure we comfortably clear the renderer's empty-content threshold.</p>
</main></body></html>`)
	stub := &stubFetcher{responses: map[string]*fetch.Response{
		httpSrv.URL: {
			URL:         httpSrv.URL,
			Status:      200,
			ContentType: "text/html",
			Body:        postRender,
		},
	}}

	cacheRoot := t.TempDir()
	cache, err := store.Open(cacheRoot)
	if err != nil {
		t.Fatalf("Open cache: %v", err)
	}
	if err := cache.EnsureSource("spa-site"); err != nil {
		t.Fatalf("EnsureSource: %v", err)
	}
	idx, err := fts5.Open(cache.IndexDBPath("spa-site"))
	if err != nil {
		t.Fatalf("Open fts5: %v", err)
	}
	t.Cleanup(func() { _ = idx.Close() })

	p := New(cache, idx)
	p.Escalator = stub

	res, err := p.Run(context.Background(), registry.Entry{
		Name: "spa-site",
		Kind: registry.KindWebsite,
		Root: httpSrv.URL,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Pages != 1 {
		t.Fatalf("Pages = %d, want 1 (escalation should have rescued the empty page)", res.Pages)
	}
	if stub.calls == 0 {
		t.Errorf("escalator was never called")
	}

	hits, err := idx.Search(context.Background(), "javascript-rendered", retriever.SearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("expected hit on the post-render content; index appears to have been built from the empty body")
	}
	if !strings.Contains(strings.ToLower(hits[0].Snippet), "javascript-rendered") {
		t.Errorf("snippet missing post-render content: %q", hits[0].Snippet)
	}
}

// TestPipeline_NoEscalatorMeansSkip locks in that empty pages are
// counted as Skipped (not errors) when no escalator is configured.
func TestPipeline_NoEscalatorMeansSkip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><main></main></body></html>`))
	}))
	t.Cleanup(srv.Close)

	cache, _ := store.Open(t.TempDir())
	_ = cache.EnsureSource("empty")
	idx, _ := fts5.Open(cache.IndexDBPath("empty"))
	t.Cleanup(func() { _ = idx.Close() })

	p := New(cache, idx) // no Escalator

	res, _ := p.Run(context.Background(), registry.Entry{
		Name: "empty", Kind: registry.KindWebsite, Root: srv.URL,
	})
	if res.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", res.Skipped)
	}
	if res.Pages != 0 {
		t.Errorf("Pages = %d, want 0", res.Pages)
	}
}
