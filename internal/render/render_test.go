package render

import (
	"errors"
	"strings"
	"testing"
)

func TestRender_NativeMarkdownFastPath(t *testing.T) {
	body := []byte("# Hello\n\nThis is **markdown**.\n\n## Section\n\nMore text here.\n")
	r := New()
	doc, err := r.Render(body, "text/markdown; charset=utf-8", "https://x.example/p")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if doc.Title != "Hello" {
		t.Errorf("title = %q, want Hello", doc.Title)
	}
	if !strings.Contains(doc.Markdown, "**markdown**") {
		t.Errorf("expected markdown body to be retained verbatim, got %q", doc.Markdown)
	}
	if len(doc.Outline) != 2 {
		t.Errorf("outline len = %d, want 2", len(doc.Outline))
	}
	if doc.Outline[0].Level != 1 || doc.Outline[1].Level != 2 {
		t.Errorf("unexpected outline levels: %+v", doc.Outline)
	}
}

func TestRender_HTMLBasic(t *testing.T) {
	html := `<!doctype html>
<html><head><title>Hello docs</title></head>
<body>
  <nav>top nav</nav>
  <main>
    <h1>Getting started</h1>
    <p>Install with <code>npm i widget</code>.</p>
    <h2>Configure</h2>
    <p>Add a config file.</p>
  </main>
  <footer>copyright</footer>
</body></html>`
	doc, err := New().Render([]byte(html), "text/html", "https://x.example/start")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if doc.Title != "Hello docs" {
		t.Errorf("title = %q", doc.Title)
	}
	if !strings.Contains(doc.Markdown, "Getting started") {
		t.Errorf("missing main content: %q", doc.Markdown)
	}
	if strings.Contains(doc.Markdown, "top nav") || strings.Contains(doc.Markdown, "copyright") {
		t.Errorf("did not strip nav/footer: %q", doc.Markdown)
	}
	if len(doc.Outline) < 2 {
		t.Errorf("outline len = %d, want >= 2", len(doc.Outline))
	}
}

func TestRender_StripsScriptsAndStyles(t *testing.T) {
	html := `<html><head><title>x</title><style>body{color:red}</style></head>
<body><main><h1>Title</h1>
<p>Real meaningful prose appears here so that we comfortably clear the empty-content threshold for the renderer.</p>
<p>A second paragraph keeps the test resilient to minor changes in the threshold.</p>
<script>alert('xss')</script></main></body></html>`
	doc, err := New().Render([]byte(html), "text/html", "https://x.example")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(doc.Markdown, "alert") || strings.Contains(doc.Markdown, "color:red") {
		t.Errorf("script/style leaked into markdown: %q", doc.Markdown)
	}
}

func TestRender_EmptyContent(t *testing.T) {
	html := `<html><body><main></main></body></html>`
	_, err := New().Render([]byte(html), "text/html", "https://x.example")
	if !errors.Is(err, ErrEmptyContent) {
		t.Fatalf("expected ErrEmptyContent, got %v", err)
	}
}

func TestRender_MarkdownSignal(t *testing.T) {
	body := []byte("# Inferred\n\nNo content type, but looks like markdown.\n")
	doc, err := New().Render(body, "application/octet-stream", "https://x.example")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if doc.Title != "Inferred" {
		t.Errorf("title = %q, want Inferred", doc.Title)
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Hello, World!", "hello-world"},
		{"   spaces   ", "spaces"},
		{"useState — A Hook", "usestate-a-hook"},
		{"!!!", ""},
		{"foo_bar baz", "foo-bar-baz"},
	}
	for _, c := range cases {
		if got := slugify(c.in); got != c.want {
			t.Errorf("slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUniqueSlug(t *testing.T) {
	used := map[string]int{}
	if got := uniqueSlug("Hooks", used); got != "hooks" {
		t.Errorf("first = %q", got)
	}
	if got := uniqueSlug("Hooks", used); got != "hooks-1" {
		t.Errorf("second = %q", got)
	}
	if got := uniqueSlug("Hooks", used); got != "hooks-2" {
		t.Errorf("third = %q", got)
	}
}

func TestExtractOutline_IgnoresFencedCode(t *testing.T) {
	md := "# Title\n\n```\n# not a heading\n```\n\n## Real Heading\n"
	headings := extractOutline(md)
	if len(headings) != 2 {
		t.Fatalf("got %d headings, want 2: %+v", len(headings), headings)
	}
	if headings[0].Text != "Title" || headings[1].Text != "Real Heading" {
		t.Errorf("wrong headings: %+v", headings)
	}
}

func TestStripHeadingSelfLinks(t *testing.T) {
	cases := []struct{ in, want string }{
		{
			"## [Toggling dark mode](#toggling-dark-mode)",
			"## Toggling dark mode",
		},
		{
			"### [Sub heading](#sub-heading) trailing",
			"### Sub heading trailing",
		},
		{
			// Non-fragment link in a heading is preserved.
			"## See [the spec](https://w3.org/spec)",
			"## See [the spec](https://w3.org/spec)",
		},
		{
			// Self-anchor link in body content - left alone (only
			// heading lines are touched).
			"## Pure heading\n\nA paragraph with [a link](#anchor).",
			"## Pure heading\n\nA paragraph with [a link](#anchor).",
		},
		{
			// Indented heading still gets the strip.
			"  ## [Indented](#indented)",
			"  ## Indented",
		},
		{
			"# No links here",
			"# No links here",
		},
	}
	for _, c := range cases {
		if got := stripHeadingSelfLinks(c.in); got != c.want {
			t.Errorf("stripHeadingSelfLinks(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRender_StripsHeadingSelfLinksFromHTML(t *testing.T) {
	// Simulate Tailwind's pattern: each heading wraps its text in a
	// self-anchor link for the copy-link affordance.
	html := `<html><body><main>
		<h1>Page</h1>
		<h2 id="overview"><a href="#overview">Overview</a></h2>
		<p>Plenty of text here so the empty-content threshold is comfortably cleared by the renderer.</p>
		<h2 id="toggling"><a href="#toggling">Toggling dark mode</a></h2>
		<p>More body text continues here for additional padding past the renderer threshold check.</p>
	</main></body></html>`
	doc, err := New().Render([]byte(html), "text/html", "https://x.example/dark")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(doc.Markdown, "](#") {
		t.Errorf("self-anchor links leaked into heading markdown: %q", doc.Markdown)
	}
	for _, h := range doc.Outline {
		if strings.Contains(h.Text, "[") || strings.Contains(h.Text, "](") {
			t.Errorf("outline heading text leaked link syntax: %q", h.Text)
		}
	}
}

func TestNormalizeMarkdown(t *testing.T) {
	in := "Hello\r\n\r\n\r\nworld   \nfoo\t\n\n\n\nend\n"
	got := normalizeMarkdown(in)
	if strings.Contains(got, "\r") {
		t.Errorf("CR not stripped: %q", got)
	}
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("triple newlines not collapsed: %q", got)
	}
	if strings.Contains(got, "world   ") {
		t.Errorf("trailing whitespace not stripped: %q", got)
	}
}
