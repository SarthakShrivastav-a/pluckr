package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SarthakShrivastav-a/pluckr/internal/registry"
	"github.com/SarthakShrivastav-a/pluckr/internal/retriever"
	"github.com/SarthakShrivastav-a/pluckr/internal/retriever/fts5"
	"github.com/SarthakShrivastav-a/pluckr/internal/store"
)

func TestPipeline_Run_LocalSource(t *testing.T) {
	// Set up a local source folder.
	sourceRoot := t.TempDir()
	mustWrite(t, filepath.Join(sourceRoot, "intro.md"), "# Intro\n\nWelcome to the docs - this section explains how the project works.")
	mustWrite(t, filepath.Join(sourceRoot, "guide", "advanced.md"), "# Advanced\n\nThis is the advanced guide section, deeper material than the intro.\n\n## Tips\n\nAnd here are some tips that the agent might want to find when searching.")

	cacheRoot := t.TempDir()
	cache, err := store.Open(cacheRoot)
	if err != nil {
		t.Fatalf("Open cache: %v", err)
	}
	if err := cache.EnsureSource("internal-docs"); err != nil {
		t.Fatalf("EnsureSource: %v", err)
	}
	idx, err := fts5.Open(cache.IndexDBPath("internal-docs"))
	if err != nil {
		t.Fatalf("Open fts5: %v", err)
	}
	t.Cleanup(func() { _ = idx.Close() })

	p := New(cache, idx)

	entry := registry.Entry{
		Name: "internal-docs",
		Kind: registry.KindLocal,
		Root: sourceRoot,
	}
	res, err := p.Run(context.Background(), entry)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Pages != 2 {
		t.Errorf("Pages = %d, want 2", res.Pages)
	}
	if res.Chunks == 0 {
		t.Errorf("expected chunks, got 0")
	}
	if len(res.Errors) != 0 {
		t.Errorf("unexpected errors: %v", res.Errors)
	}

	// Verify markdown files written with frontmatter.
	body, err := os.ReadFile(filepath.Join(cache.PagesDir("internal-docs"), "intro.md"))
	if err != nil {
		t.Fatalf("read intro.md: %v", err)
	}
	if !strings.HasPrefix(string(body), "---\n") {
		t.Errorf("expected frontmatter, got %q", string(body[:60]))
	}

	// Verify FTS5 indexed the chunks.
	hits, err := idx.Search(context.Background(), "advanced", retriever.SearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Errorf("expected hits for 'advanced', got 0")
	}

	// Manifest should reflect the run.
	m, err := cache.LoadManifest("internal-docs")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Kind != registry.KindLocal {
		t.Errorf("manifest kind = %q", m.Kind)
	}
	if len(m.Pages) != 2 {
		t.Errorf("manifest pages = %d, want 2", len(m.Pages))
	}
	if m.LastSyncedAt.IsZero() {
		t.Errorf("LastSyncedAt should be set")
	}
}

func TestPipeline_NewSource_UnknownKind(t *testing.T) {
	cache, _ := store.Open(t.TempDir())
	idx, _ := fts5.Open(":memory:")
	t.Cleanup(func() { _ = idx.Close() })
	p := New(cache, idx)
	if _, err := p.NewSource(registry.Entry{Kind: "weird"}); err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestPipeline_NewSource_AllKnownKinds(t *testing.T) {
	cache, _ := store.Open(t.TempDir())
	idx, _ := fts5.Open(":memory:")
	t.Cleanup(func() { _ = idx.Close() })
	p := New(cache, idx)
	kinds := []string{
		registry.KindWebsite,
		registry.KindLLMSTxt,
		registry.KindGitHub,
		registry.KindLocal,
	}
	for _, k := range kinds {
		if _, err := p.NewSource(registry.Entry{Name: "x", Kind: k, Root: "u"}); err != nil {
			t.Errorf("NewSource(%s) err=%v", k, err)
		}
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
