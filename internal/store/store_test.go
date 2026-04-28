package store

import (
	"strings"
	"testing"
	"time"

	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

func TestSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"react.dev", "react.dev"},
		{"My Internal Wiki", "my-internal-wiki"},
		{"https://example.com/foo", "https-example.com-foo"},
		{"---???---", "source"},
	}
	for _, c := range cases {
		if got := Slug(c.in); got != c.want {
			t.Errorf("Slug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"foo/bar", "foo/bar.md"},
		{"foo/bar.md", "foo/bar.md"},
		{"/foo/bar", "foo/bar.md"},
		{"./foo", "foo.md"},
		{"../escape", ""},
		{"foo/../escape", "escape.md"},     // foo/../escape resolves to "escape" - inside dir, fine
		{"foo/bar/../../../bad", ""},        // resolves above the source root, reject
		{"", ""},
		{"/", ""},
	}
	for _, c := range cases {
		got := normalizePath(c.in)
		// Compare using forward slashes for cross-platform stability.
		got = strings.ReplaceAll(got, "\\", "/")
		if got != c.want {
			t.Errorf("normalizePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWritePage_AndReadBack(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.EnsureSource("react.dev"); err != nil {
		t.Fatalf("EnsureSource: %v", err)
	}
	doc := types.Document{
		Source:    "react.dev",
		URL:       "https://react.dev/reference/useState",
		Path:      "reference/useState",
		Title:     "useState",
		Markdown:  "# useState\n\nReturns a stateful value...",
		FetchedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	full, err := c.WritePage("react.dev", doc)
	if err != nil {
		t.Fatalf("WritePage: %v", err)
	}
	if !strings.HasSuffix(full, "useState.md") {
		t.Errorf("expected .md suffix, got %q", full)
	}
	body, err := c.ReadPage("react.dev", "reference/useState")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	s := string(body)
	if !strings.HasPrefix(s, "---\n") {
		t.Errorf("missing frontmatter: %q", s[:60])
	}
	if !strings.Contains(s, `title: "useState"`) {
		t.Errorf("title missing: %q", s)
	}
	if !strings.Contains(s, "Returns a stateful value") {
		t.Errorf("body missing: %q", s)
	}
}

func TestWritePage_RejectsTraversal(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = c.EnsureSource("s")
	_, err = c.WritePage("s", types.Document{Path: "../../escape", Markdown: "x"})
	if err == nil {
		t.Fatal("expected error for traversal path, got nil")
	}
}

func TestListPages_Sorted(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = c.EnsureSource("s")
	for _, p := range []string{"b/index", "a/intro", "c"} {
		if _, err := c.WritePage("s", types.Document{
			Path: p, Title: p, Markdown: "# x\n\ncontent " + p,
		}); err != nil {
			t.Fatalf("WritePage %s: %v", p, err)
		}
	}
	got, err := c.ListPages("s")
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	want := []string{"a/intro.md", "b/index.md", "c.md"}
	if len(got) != len(want) {
		t.Fatalf("ListPages = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ListPages[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestManifest_RoundTrip(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = c.EnsureSource("s")

	m, err := c.LoadManifest("s")
	if err != nil {
		t.Fatalf("LoadManifest empty: %v", err)
	}
	if m.Pages == nil {
		t.Fatal("expected Pages map to be initialized")
	}

	m.Source = "s"
	m.Kind = "website"
	m.Root = "https://example.com"
	m.LastSyncedAt = time.Now().UTC().Truncate(time.Second)
	m.Pages["a.md"] = ManifestPage{URL: "https://example.com/a", ContentHash: "h", FetchedAt: time.Now().UTC().Truncate(time.Second), TokenCount: 42}

	if err := c.SaveManifest("s", m); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	back, err := c.LoadManifest("s")
	if err != nil {
		t.Fatalf("LoadManifest back: %v", err)
	}
	if back.Kind != "website" || back.Root != "https://example.com" || len(back.Pages) != 1 {
		t.Errorf("round trip lost data: %+v", back)
	}
	if back.Pages["a.md"].TokenCount != 42 {
		t.Errorf("expected token count 42, got %d", back.Pages["a.md"].TokenCount)
	}
}

func TestRoot_HonorsOverride(t *testing.T) {
	got, err := Root("/tmp/example")
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	if !strings.Contains(got, "example") {
		t.Errorf("Root override = %q, expected to contain 'example'", got)
	}
}

func TestRemoveSource(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = c.EnsureSource("s")
	if _, err := c.WritePage("s", types.Document{Path: "x", Markdown: "x"}); err != nil {
		t.Fatalf("WritePage: %v", err)
	}
	if err := c.RemoveSource("s"); err != nil {
		t.Fatalf("RemoveSource: %v", err)
	}
	pages, _ := c.ListPages("s")
	if len(pages) != 0 {
		t.Errorf("expected source to be gone, got %v", pages)
	}
}
