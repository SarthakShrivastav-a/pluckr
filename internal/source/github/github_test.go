package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/SarthakShrivastav-a/pluckr/internal/fetch"
	"github.com/SarthakShrivastav-a/pluckr/internal/registry"
	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

func TestParseSpec(t *testing.T) {
	cases := []struct {
		in   string
		want Spec
		ok   bool
	}{
		{"facebook/react", Spec{Owner: "facebook", Repo: "react"}, true},
		{"facebook/react@main", Spec{Owner: "facebook", Repo: "react", Ref: "main"}, true},
		{"facebook/react/docs", Spec{Owner: "facebook", Repo: "react", Subdir: "docs"}, true},
		{"facebook/react@v18/docs/api", Spec{Owner: "facebook", Repo: "react", Ref: "v18", Subdir: "docs/api"}, true},
		{"github.com/owner/repo", Spec{Owner: "owner", Repo: "repo"}, true},
		{"single", Spec{}, false},
		{"", Spec{}, false},
		{"/missing-owner", Spec{}, false},
	}
	for _, c := range cases {
		got, err := ParseSpec(c.in)
		if (err == nil) != c.ok {
			t.Errorf("ParseSpec(%q) err=%v, want ok=%v", c.in, err, c.ok)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("ParseSpec(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestSpec_String(t *testing.T) {
	cases := []struct {
		spec Spec
		want string
	}{
		{Spec{Owner: "a", Repo: "b"}, "a/b"},
		{Spec{Owner: "a", Repo: "b", Ref: "main"}, "a/b@main"},
		{Spec{Owner: "a", Repo: "b", Subdir: "docs"}, "a/b/docs"},
		{Spec{Owner: "a", Repo: "b", Ref: "v1", Subdir: "docs/api"}, "a/b@v1/docs/api"},
	}
	for _, c := range cases {
		if got := c.spec.String(); got != c.want {
			t.Errorf("String(%+v) = %q, want %q", c.spec, got, c.want)
		}
	}
}

func TestInSubdir(t *testing.T) {
	cases := []struct {
		path, subdir string
		want         bool
	}{
		{"docs/intro.md", "docs", true},
		{"docs/sub/page.md", "docs", true},
		{"src/index.md", "docs", false},
		{"docs", "docs", true},
		{"anything", "", true},
	}
	for _, c := range cases {
		if got := inSubdir(c.path, c.subdir); got != c.want {
			t.Errorf("inSubdir(%q, %q) = %v, want %v", c.path, c.subdir, got, c.want)
		}
	}
}

// TestPull_EndToEnd uses a fake API + raw server to exercise the full flow.
func TestPull_EndToEnd(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/git/trees/main"):
			fmt.Fprint(w, `{"tree":[
				{"path":"README.md","type":"blob","size":10},
				{"path":"docs/intro.md","type":"blob","size":12},
				{"path":"docs/api.md","type":"blob","size":12},
				{"path":"src/index.ts","type":"blob","size":50},
				{"path":"docs","type":"tree"}
			]}`)
		case strings.HasSuffix(r.URL.Path, "/owner/repo"):
			// repo info: default branch
			fmt.Fprint(w, `{"default_branch":"main"}`)
		default:
			http.NotFound(w, r)
		}
	})
	api := httptest.NewServer(mux)
	t.Cleanup(api.Close)

	rawMux := http.NewServeMux()
	rawMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "# %s\n\nbody for %s.\n", r.URL.Path, r.URL.Path)
	})
	raw := httptest.NewServer(rawMux)
	t.Cleanup(raw.Close)

	// We need to re-route the github URLs the source builds to our two
	// test servers. Wrap the default fetcher with a host-rewriting one.
	rewriting := &rewritingFetcher{
		api:  api.URL,
		raw:  raw.URL,
		base: fetch.NewHTTP(),
	}

	g := &GitHub{
		SourceName: "react",
		Spec:       "owner/repo/docs",
		Fetcher:    rewriting,
		Lookup:     func(string) string { return "" }, // no token
	}
	pages := make(chan types.Page, 8)
	go func() { _ = g.Pull(context.Background(), pages); close(pages) }()
	var got []types.Page
	for p := range pages {
		got = append(got, p)
	}
	if len(got) != 2 {
		t.Fatalf("got %d pages, want 2 (docs/intro and docs/api)", len(got))
	}
	for _, p := range got {
		if !strings.HasPrefix(p.URL, "https://github.com/owner/repo/blob/main/docs/") {
			t.Errorf("URL = %q, want github.com docs URL", p.URL)
		}
		if !strings.HasPrefix(p.Path, "docs/") {
			t.Errorf("Path = %q, want docs/ prefix", p.Path)
		}
	}
}

func TestKindAndName(t *testing.T) {
	g := &GitHub{SourceName: "x"}
	if g.Kind() != registry.KindGitHub {
		t.Errorf("Kind = %q", g.Kind())
	}
	if g.Name() != "x" {
		t.Errorf("Name = %q", g.Name())
	}
}

// rewritingFetcher rewrites api.github.com and raw.githubusercontent.com
// requests to point at the supplied test servers.
type rewritingFetcher struct {
	api, raw string
	base     fetch.Fetcher
}

func (r *rewritingFetcher) Fetch(ctx context.Context, req fetch.Request) (*fetch.Response, error) {
	u, err := url.Parse(req.URL)
	if err != nil {
		return nil, err
	}
	switch u.Host {
	case "api.github.com":
		req.URL = r.api + u.Path
		if u.RawQuery != "" {
			req.URL += "?" + u.RawQuery
		}
	case "raw.githubusercontent.com":
		req.URL = r.raw + u.Path
	}
	return r.base.Fetch(ctx, req)
}
