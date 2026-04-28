package llmstxt

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SarthakShrivastav-a/pluckr/internal/fetch"
	"github.com/SarthakShrivastav-a/pluckr/internal/registry"
	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

func TestResolveURLs(t *testing.T) {
	cases := []struct {
		in            string
		wantFull      string
		wantIndex     string
	}{
		{"https://example.com", "https://example.com/llms-full.txt", "https://example.com/llms.txt"},
		{"https://example.com/", "https://example.com/llms-full.txt", "https://example.com/llms.txt"},
		{"https://example.com/llms.txt", "https://example.com/llms-full.txt", "https://example.com/llms.txt"},
		{"https://example.com/llms-full.txt", "https://example.com/llms-full.txt", "https://example.com/llms.txt"},
	}
	for _, c := range cases {
		full, idx := resolveURLs(c.in)
		if full != c.wantFull || idx != c.wantIndex {
			t.Errorf("resolveURLs(%q) = (%q, %q), want (%q, %q)", c.in, full, idx, c.wantFull, c.wantIndex)
		}
	}
}

func TestExtractLinks(t *testing.T) {
	body := `# Project

> Description

## Docs
- [Intro](https://example.com/intro): the basics
- [Advanced](/advanced): deeper dive
- [Skip mailto](mailto:no@x)
- [Skip anchor](#section)
- [Dup](https://example.com/intro)
`
	got := extractLinks(body, "https://example.com/llms.txt")
	want := []string{"https://example.com/intro", "https://example.com/advanced"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("link[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPathFromURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://example.com/", "example.com"},
		{"https://example.com/foo", "foo"},
		{"https://example.com/foo/", "foo/index"},
		{"https://example.com/foo.md", "foo"},
		{"https://example.com/a/b.html", "a/b"},
	}
	for _, c := range cases {
		if got := pathFromURL(c.in); got != c.want {
			t.Errorf("pathFromURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPull_PrefersFullText(t *testing.T) {
	full := "# Bundle\n\nbundled markdown body."
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/llms-full.txt":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, full)
		case "/llms.txt":
			t.Errorf("Pull should not reach /llms.txt when llms-full.txt exists")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	l := &LLMSTxt{
		SourceName: "test",
		Root:       srv.URL,
		Fetcher:    fetch.NewHTTP(),
	}
	pages := make(chan types.Page, 4)
	go func() {
		_ = l.Pull(context.Background(), pages)
		close(pages)
	}()
	var got []types.Page
	for p := range pages {
		got = append(got, p)
	}
	if len(got) != 1 {
		t.Fatalf("got %d pages, want 1", len(got))
	}
	if !strings.Contains(string(got[0].Body), "bundled markdown body") {
		t.Errorf("body = %q", got[0].Body)
	}
	if got[0].Path != "llms-full" {
		t.Errorf("path = %q, want llms-full", got[0].Path)
	}
	if !strings.Contains(got[0].ContentType, "markdown") {
		t.Errorf("expected markdown content-type, got %q", got[0].ContentType)
	}
}

func TestPull_FallsBackToIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/llms-full.txt":
			http.NotFound(w, r)
		case "/llms.txt":
			fmt.Fprintf(w, "# Project\n\n## Docs\n- [Intro](%[1]s/intro)\n- [Adv](%[1]s/adv)\n", "")
		case "/intro":
			fmt.Fprint(w, "intro body")
		case "/adv":
			fmt.Fprint(w, "adv body")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	// Bake the live URL into a fresh handler.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/llms-full.txt":
			http.NotFound(w, r)
		case "/llms.txt":
			fmt.Fprintf(w, "# Project\n\n## Docs\n- [Intro](%[1]s/intro)\n- [Adv](%[1]s/adv)\n", srv.URL)
		case "/intro":
			fmt.Fprint(w, "intro body")
		case "/adv":
			fmt.Fprint(w, "adv body")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv2.Close)

	l := &LLMSTxt{
		SourceName: "test",
		Root:       srv2.URL,
		Fetcher:    fetch.NewHTTP(),
	}
	pages := make(chan types.Page, 4)
	go func() { _ = l.Pull(context.Background(), pages); close(pages) }()
	var bodies []string
	for p := range pages {
		bodies = append(bodies, string(p.Body))
	}
	if len(bodies) != 2 {
		t.Fatalf("got %d pages, want 2", len(bodies))
	}
}

func TestEnsureMarkdown(t *testing.T) {
	cases := []struct{ in, want string }{
		{"text/markdown", "text/markdown"},
		{"text/markdown; charset=utf-8", "text/markdown; charset=utf-8"},
		{"text/x-markdown", "text/x-markdown"},
		{"text/plain", "text/markdown; charset=utf-8"},
		{"", "text/markdown; charset=utf-8"},
	}
	for _, c := range cases {
		if got := ensureMarkdown(c.in); got != c.want {
			t.Errorf("ensureMarkdown(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestKindAndName(t *testing.T) {
	l := &LLMSTxt{SourceName: "x"}
	if l.Kind() != registry.KindLLMSTxt {
		t.Errorf("Kind = %q", l.Kind())
	}
	if l.Name() != "x" {
		t.Errorf("Name = %q", l.Name())
	}
}
