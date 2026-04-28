package local

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/SarthakShrivastav-a/pluckr/internal/registry"
	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestLocal_Pull_BasicWalk(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "# A\n\nbody A.")
	writeFile(t, filepath.Join(root, "sub", "b.markdown"), "# B\n\nbody B.")
	writeFile(t, filepath.Join(root, "ignored.png"), "binary")
	writeFile(t, filepath.Join(root, ".git", "config"), "should be skipped")
	writeFile(t, filepath.Join(root, "notes.txt"), "plain notes here.")

	l := &Local{SourceName: "internal", Root: root}
	pages := make(chan types.Page, 8)
	go func() { _ = l.Pull(context.Background(), pages); close(pages) }()

	var got []types.Page
	for p := range pages {
		got = append(got, p)
	}
	if len(got) != 3 {
		t.Fatalf("got %d pages, want 3", len(got))
	}

	paths := make([]string, len(got))
	for i, p := range got {
		paths[i] = p.Path
	}
	sort.Strings(paths)
	want := []string{"a", "notes", "sub/b"}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("path[%d] = %q, want %q", i, paths[i], want[i])
		}
	}

	for _, p := range got {
		if !strings.HasPrefix(p.URL, "file://") {
			t.Errorf("URL should be file:// for local pages, got %q", p.URL)
		}
		if p.FetchedAt.IsZero() {
			t.Errorf("FetchedAt should be set")
		}
		if len(p.Body) == 0 {
			t.Errorf("body should not be empty")
		}
	}
}

func TestLocal_Pull_RespectsMaxPages(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 10; i++ {
		writeFile(t, filepath.Join(root, "doc"+string(rune('a'+i))+".md"), "# x\nbody.")
	}
	l := &Local{SourceName: "x", Root: root, MaxPages: 3}
	pages := make(chan types.Page, 16)
	go func() { _ = l.Pull(context.Background(), pages); close(pages) }()
	count := 0
	for range pages {
		count++
	}
	if count != 3 {
		t.Errorf("got %d pages, want 3", count)
	}
}

func TestLocal_Pull_MissingRoot(t *testing.T) {
	l := &Local{SourceName: "x", Root: filepath.Join(t.TempDir(), "does-not-exist")}
	if err := l.Pull(context.Background(), make(chan types.Page, 1)); err == nil {
		t.Fatal("expected error for missing root")
	}
}

func TestLocal_Pull_EmptyRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "binary.png"), "x") // no supported files
	l := &Local{SourceName: "x", Root: root}
	if err := l.Pull(context.Background(), make(chan types.Page, 1)); err == nil {
		t.Fatal("expected error when no supported files found")
	}
}

func TestKindAndName(t *testing.T) {
	l := &Local{SourceName: "x"}
	if l.Kind() != registry.KindLocal {
		t.Errorf("Kind = %q", l.Kind())
	}
	if l.Name() != "x" {
		t.Errorf("Name = %q", l.Name())
	}
}

func TestValidateRoot(t *testing.T) {
	cases := []struct {
		root string
		ok   bool
	}{
		{"", false},
		{"/some/path", true},
		{"file:///some/path", true},
		{"https://example.com", false},
	}
	for _, c := range cases {
		err := ValidateRoot(c.root)
		if (err == nil) != c.ok {
			t.Errorf("ValidateRoot(%q) err=%v, want ok=%v", c.root, err, c.ok)
		}
	}
}

func TestContentTypeFor(t *testing.T) {
	if got := contentTypeFor(".md"); !strings.Contains(got, "markdown") {
		t.Errorf(".md = %q", got)
	}
	if got := contentTypeFor(".txt"); !strings.Contains(got, "plain") {
		t.Errorf(".txt = %q", got)
	}
	if got := contentTypeFor(".bin"); got != "application/octet-stream" {
		t.Errorf(".bin = %q", got)
	}
}
