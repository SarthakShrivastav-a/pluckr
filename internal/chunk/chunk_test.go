package chunk

import (
	"strings"
	"testing"

	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

func TestChunk_SingleSectionDoc(t *testing.T) {
	doc := types.Document{
		Title:    "Intro",
		URL:      "https://x.example/a",
		Markdown: "# Intro\n\nA paragraph that is long enough to clear the minimum body threshold easily.\n",
	}
	got := New().Chunk(doc)
	if len(got) != 1 {
		t.Fatalf("got %d chunks, want 1", len(got))
	}
	if got[0].Order != 0 {
		t.Errorf("order = %d, want 0", got[0].Order)
	}
	if !strings.Contains(got[0].Body, "long enough") {
		t.Errorf("body missing content: %q", got[0].Body)
	}
}

func TestChunk_SplitsAtH2(t *testing.T) {
	md := strings.Join([]string{
		"# React",
		"",
		"Lead paragraph that introduces the doc with enough text to count.",
		"",
		"## Hooks",
		"",
		"Hooks let you use state and other React features without writing a class.",
		"",
		"### useState",
		"",
		"useState lets you add state to your function components.",
		"",
		"## Components",
		"",
		"Components are reusable building blocks for your UI.",
	}, "\n")

	got := New().Chunk(types.Document{Title: "React", URL: "https://x.example/r", Markdown: md})
	if len(got) != 3 {
		t.Fatalf("got %d chunks, want 3 (H1 lead, Hooks, Components)", len(got))
	}

	wantPaths := [][]string{
		{},          // intro chunk - H1 is stored as Title, not in heading_path
		{"Hooks"},
		{"Components"},
	}
	for i, c := range got {
		if !pathEqual(c.HeadingPath, wantPaths[i]) {
			t.Errorf("chunk %d heading_path = %v, want %v", i, c.HeadingPath, wantPaths[i])
		}
		if c.Title != "React" {
			t.Errorf("chunk %d Title = %q, want React", i, c.Title)
		}
	}

	if !strings.Contains(got[1].Body, "useState") {
		t.Errorf("Hooks chunk should keep its H3 child, got %q", got[1].Body)
	}
}

func TestChunk_DoesNotSplitInsideFencedCode(t *testing.T) {
	md := strings.Join([]string{
		"# Title",
		"",
		"Intro paragraph long enough to count for the threshold check.",
		"",
		"## Real",
		"",
		"```",
		"# this looks like a heading but is code",
		"## also code",
		"```",
		"",
		"More prose for the section.",
	}, "\n")

	got := New().Chunk(types.Document{Title: "Title", URL: "u", Markdown: md})
	if len(got) != 2 {
		t.Fatalf("got %d chunks, want 2 (intro + Real)", len(got))
	}
	if !strings.Contains(got[1].Body, "this looks like") {
		t.Errorf("fenced content should be preserved in chunk body: %q", got[1].Body)
	}
}

func TestChunk_OversizedSplitsByParagraph(t *testing.T) {
	paragraph := strings.Repeat("Sentence about hooks. ", 30) // ~150 tokens
	md := "# T\n\n## Big\n\n" + strings.Join([]string{
		paragraph, paragraph, paragraph, paragraph, paragraph,
	}, "\n\n") + "\n"

	c := New()
	c.MaxTokens = 200
	got := c.Chunk(types.Document{Title: "T", URL: "u", Markdown: md})
	if len(got) < 2 {
		t.Fatalf("expected oversized section to split, got %d chunks", len(got))
	}
	for _, ch := range got {
		if !pathEqual(ch.HeadingPath, []string{"Big"}) {
			t.Errorf("split chunks must keep parent heading path [Big], got %v", ch.HeadingPath)
		}
		// We allow some slack: a chunk may include one paragraph that
		// itself is bigger than MaxTokens, but never two.
		if ch.TokenCount > c.MaxTokens*2 {
			t.Errorf("chunk still oversized: token=%d max=%d", ch.TokenCount, c.MaxTokens)
		}
	}
}

func TestChunk_AnchorsAreUniqueSlugs(t *testing.T) {
	md := strings.Join([]string{
		"# Top",
		"",
		"## Reference",
		"",
		"first body paragraph long enough to count.",
		"",
		"## Reference",
		"",
		"second body paragraph long enough to count.",
	}, "\n")
	got := New().Chunk(types.Document{Title: "Top", URL: "u", Markdown: md})
	if len(got) != 2 {
		t.Fatalf("got %d chunks, want 2", len(got))
	}
	if got[0].Anchor != "reference" || got[1].Anchor != "reference-1" {
		t.Errorf("anchors = (%q, %q), want (reference, reference-1)", got[0].Anchor, got[1].Anchor)
	}
}

func TestChunk_EmptyMarkdownYieldsNothing(t *testing.T) {
	got := New().Chunk(types.Document{Title: "x"})
	if len(got) != 0 {
		t.Errorf("expected no chunks, got %d", len(got))
	}
}

func TestParseHeading(t *testing.T) {
	cases := []struct {
		line  string
		level int
		text  string
	}{
		{"# Hello", 1, "Hello"},
		{"## With trailing #", 2, "With trailing"},
		{"###    spaced", 3, "spaced"},
		{"#####", 0, ""},
		{"#nospace", 0, ""},
		{"plain", 0, ""},
		{"####### too many", 0, ""},
		// Self-anchor link from html-to-markdown is unwrapped.
		{"## [Toggling dark mode](#toggling-dark-mode)", 2, "Toggling dark mode"},
		{"### [a](#a) and [b](#b)", 3, "a and b"},
		// External link in a heading is preserved.
		{"## See [spec](https://w3.org)", 2, "See [spec](https://w3.org)"},
	}
	for _, c := range cases {
		gotLevel, gotText := parseHeading(c.line)
		if gotLevel != c.level || gotText != c.text {
			t.Errorf("parseHeading(%q) = (%d,%q), want (%d,%q)", c.line, gotLevel, gotText, c.level, c.text)
		}
	}
}

func pathEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func pathStartsWith(a, prefix []string) bool {
	if len(a) < len(prefix) {
		return false
	}
	for i := range prefix {
		if a[i] != prefix[i] {
			return false
		}
	}
	return true
}
