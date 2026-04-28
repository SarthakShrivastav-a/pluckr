package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SarthakShrivastav-a/pluckr/internal/registry"
)

func TestDetectKind(t *testing.T) {
	cases := []struct {
		spec string
		want string
	}{
		{"https://react.dev/reference", registry.KindWebsite},
		{"http://example.com", registry.KindWebsite},
		{"https://example.com/llms.txt", registry.KindLLMSTxt},
		{"https://example.com/llms-full.txt", registry.KindLLMSTxt},
		{"facebook/react", registry.KindGitHub},
		{"facebook/react@v18", registry.KindGitHub},
		{"facebook/react@v18/docs", registry.KindGitHub},
		{"./local-folder", registry.KindLocal},
		{"/abs/path", registry.KindLocal},
		{"~/notes", registry.KindLocal},
		{"completely random nonsense with spaces", ""},
	}
	for _, c := range cases {
		if got := detectKind(c.spec); got != c.want {
			t.Errorf("detectKind(%q) = %q, want %q", c.spec, got, c.want)
		}
	}
}

func TestDeriveName(t *testing.T) {
	cases := []struct {
		spec, kind, want string
	}{
		{"https://react.dev/reference", registry.KindWebsite, "react.dev"},
		{"https://example.com/llms.txt", registry.KindLLMSTxt, "example.com"},
		{"facebook/react", registry.KindGitHub, "facebook/react"},
		{"facebook/react@v18", registry.KindGitHub, "facebook/react"},
		{"facebook/react@v18/docs", registry.KindGitHub, "facebook/react"},
		{"facebook/react/docs", registry.KindGitHub, "facebook/react"},
	}
	for _, c := range cases {
		if got := deriveName(c.spec, c.kind); got != c.want {
			t.Errorf("deriveName(%q, %q) = %q, want %q", c.spec, c.kind, got, c.want)
		}
	}
}

func TestHumanAgo(t *testing.T) {
	now := time.Now()
	cases := []struct {
		t    time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-3 * time.Hour), "3h ago"},
		{now.Add(-2 * 24 * time.Hour), "2d ago"},
	}
	for _, c := range cases {
		if got := humanAgo(c.t); got != c.want {
			t.Errorf("humanAgo(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestSplitFrontmatter(t *testing.T) {
	body := []byte("---\ntitle: \"x\"\nurl: \"https://x\"\n---\n\n# Hello\n\nbody.")
	md, fm := splitFrontmatter(body)
	if !strings.HasPrefix(string(md), "# Hello") {
		t.Errorf("md not stripped: %q", md)
	}
	if !strings.Contains(fm, `url: "https://x"`) {
		t.Errorf("frontmatter missing url: %q", fm)
	}
	if got := urlFromFrontmatter(fm, "fallback"); got != "https://x" {
		t.Errorf("urlFromFrontmatter = %q", got)
	}
}

func TestSplitFrontmatter_NoFrontmatter(t *testing.T) {
	body := []byte("# Hello\n\nbody.")
	md, fm := splitFrontmatter(body)
	if string(md) != string(body) {
		t.Errorf("body should pass through unchanged")
	}
	if fm != "" {
		t.Errorf("expected empty frontmatter, got %q", fm)
	}
}

// integration: add -> list -> search
func TestCLI_AddListSearch(t *testing.T) {
	cacheRoot := t.TempDir()
	docsRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(docsRoot, "intro.md"), []byte("# Intro\n\nHooks let you use state in functional components without writing classes."), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	root := NewRoot("test")
	root.SetOut(new(bytes.Buffer))
	root.SetErr(new(bytes.Buffer))

	// add
	root.SetArgs([]string{"--cache", cacheRoot, "add", docsRoot, "--name", "internal", "--pull"})
	root.SetContext(context.Background())
	if err := root.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}

	// list
	out := new(bytes.Buffer)
	root.SetOut(out)
	root.SetArgs([]string{"--cache", cacheRoot, "list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out.String(), "internal") {
		t.Errorf("list output missing 'internal': %q", out.String())
	}

	// search
	out.Reset()
	root.SetArgs([]string{"--cache", cacheRoot, "search", "hooks"})
	if err := root.Execute(); err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(strings.ToLower(out.String()), "hooks") {
		t.Errorf("search output missing 'hooks': %q", out.String())
	}

	// remove
	out.Reset()
	root.SetArgs([]string{"--cache", cacheRoot, "remove", "internal"})
	if err := root.Execute(); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !strings.Contains(out.String(), "removed internal") {
		t.Errorf("remove output: %q", out.String())
	}
}
