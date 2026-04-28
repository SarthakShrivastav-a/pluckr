package website

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/SarthakShrivastav-a/pluckr/internal/fetch"
	"github.com/SarthakShrivastav-a/pluckr/internal/registry"
	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

func TestPagePathFromURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://x.example/", "index"},
		{"https://x.example/foo", "foo"},
		{"https://x.example/foo/", "foo/index"},
		{"https://x.example/foo/bar.html", "foo/bar"},
		{"https://x.example/foo/bar.htm", "foo/bar"},
		{"https://x.example/a/b/c/", "a/b/c/index"},
	}
	for _, c := range cases {
		if got := pagePathFromURL(c.in); got != c.want {
			t.Errorf("pagePathFromURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestScopePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/", "/"},
		{"/docs/", "/docs/"},
		{"/docs/intro", "/docs/"},
		{"/foo/bar.html", "/foo/"},
		{"/single", "/single"},
	}
	for _, c := range cases {
		if got := scopePath(c.in); got != c.want {
			t.Errorf("scopePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDiscover_HonorsOriginalScopeOverRedirect locks in the fix for
// docs sites that redirect /docs -> /docs/<deep-page>. The discovery
// scope must stay at /docs so the rest of the docs tree remains in
// range.
func TestDiscover_HonorsOriginalScopeOverRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/installation/using-vite", http.StatusFound)
	})
	mux.HandleFunc("/docs/installation/using-vite", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><nav>
			<a href="/docs/installation/using-vite">Vite</a>
			<a href="/docs/responsive-design">Responsive Design</a>
			<a href="/docs/dark-mode">Dark Mode</a>
			<a href="/docs/colors">Colors</a>
			<a href="/docs/spacing">Spacing</a>
			<a href="/docs/typography">Typography</a>
			<a href="/docs/effects">Effects</a>
		</nav><main><h1>Vite</h1><p>Use Vite for installation.</p></main></body></html>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	urls, err := Discover(context.Background(), srv.URL+"/docs", DiscoverOptions{
		MaxPages: 50, Fetcher: fetch.NewHTTP(),
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(urls) < 5 {
		t.Fatalf("expected discovery to keep /docs as the scope and find sibling pages, got %d urls: %v", len(urls), urls)
	}
}

func TestFilterAndDedupe(t *testing.T) {
	hosts := map[string]struct{}{"docs.example.com": {}}
	urls := []string{
		"https://docs.example.com/intro",
		"https://docs.example.com/intro/",     // dup pathname-ish
		"https://docs.example.com/img.png",    // ignored ext
		"https://other.example.com/page",      // wrong host
		"https://docs.example.com/blog/post1", // out of scope
		"https://docs.example.com/docs/a",
		"https://docs.example.com/docs/b",
	}
	got := filterAndDedupe(urls, hosts, "/docs/", 100)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2: %v", len(got), got)
	}
}

func TestWebsite_PullSitemap(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<?xml version="1.0"?>
<urlset><url><loc>BASE/docs/intro</loc></url><url><loc>BASE/docs/advanced</loc></url></urlset>`)
	})
	mux.HandleFunc("/docs/intro", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><main><h1>Intro</h1><p>Hi</p></main></body></html>`)
	})
	mux.HandleFunc("/docs/advanced", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><main><h1>Adv</h1><p>Hi</p></main></body></html>`)
	})
	mux.HandleFunc("/docs/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><main><h1>Docs</h1></main></body></html>`)
	})
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Rewrite the BASE placeholder in the sitemap with the live URL.
	rewritingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sitemap.xml" {
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<?xml version="1.0"?>
<urlset><url><loc>%[1]s/docs/intro</loc></url><url><loc>%[1]s/docs/advanced</loc></url></urlset>`, srv.URL)
			return
		}
		mux.ServeHTTP(w, r)
	})
	srv2 := httptest.NewServer(rewritingHandler)
	t.Cleanup(srv2.Close)

	w := &Website{
		SourceName: "test",
		Root:       srv.URL + "/docs/",
		Fetcher:    fetch.NewHTTP(),
		MaxPages:   10,
	}
	// Override via local fetcher routing - simplest is to point to srv2
	// for sitemap.xml. We just point Root at srv2 so the discovery path
	// finds the rewritten sitemap.
	w.Root = srv2.URL + "/docs/"

	pages := make(chan types.Page, 8)
	done := make(chan error, 1)
	go func() { done <- w.Pull(context.Background(), pages) }()

	var got []types.Page
	var mu sync.Mutex
	collectDone := make(chan struct{})
	go func() {
		defer close(collectDone)
		for p := range pages {
			mu.Lock()
			got = append(got, p)
			mu.Unlock()
		}
	}()

	if err := <-done; err != nil {
		t.Fatalf("Pull: %v", err)
	}
	close(pages)
	<-collectDone

	mu.Lock()
	defer mu.Unlock()
	if len(got) < 2 {
		t.Errorf("expected at least 2 pages, got %d", len(got))
	}
	for _, p := range got {
		if !strings.Contains(string(p.Body), "<main>") {
			t.Errorf("body missing main: %q", p.Body)
		}
		if p.Path == "" {
			t.Errorf("empty page path for %s", p.URL)
		}
	}
}

func TestWebsite_KindAndName(t *testing.T) {
	w := &Website{SourceName: "react.dev"}
	if w.Kind() != registry.KindWebsite {
		t.Errorf("Kind = %q", w.Kind())
	}
	if w.Name() != "react.dev" {
		t.Errorf("Name = %q", w.Name())
	}
}
