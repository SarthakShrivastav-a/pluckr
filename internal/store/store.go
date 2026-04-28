// Package store owns the on-disk layout of a pluckr cache:
//
//	<root>/
//	  registry.json
//	  config.toml
//	  sources/<slug>/
//	    pages/<path>.md
//	    manifest.json
//	    index.db        (owned by the retriever, paths only computed here)
//
// Markdown is the source of truth; the FTS5 index and the manifest are
// derived. Users can hand-edit markdown files and a subsequent reindex
// will pick the changes up.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

// DefaultDirName is the directory under the user's home directory where
// pluckr keeps its state when no explicit root is configured.
const DefaultDirName = ".pluckr"

// Root resolves the cache root, honoring an explicit override and falling
// back to ~/.pluckr.
func Root(override string) (string, error) {
	if override != "" {
		return filepath.Clean(override), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("store: locate home dir: %w", err)
	}
	return filepath.Join(home, DefaultDirName), nil
}

// Cache encapsulates one cache root and provides per-source access.
type Cache struct {
	root string
	mu   sync.Mutex
}

// Open returns a Cache rooted at the given directory. The root is created
// if it doesn't exist. It is safe to call Open multiple times against the
// same directory.
func Open(root string) (*Cache, error) {
	if err := os.MkdirAll(filepath.Join(root, "sources"), 0o755); err != nil {
		return nil, fmt.Errorf("store: create root: %w", err)
	}
	return &Cache{root: root}, nil
}

// Root returns the absolute cache root.
func (c *Cache) Root() string { return c.root }

// SourceDir returns <root>/sources/<slug>.
func (c *Cache) SourceDir(name string) string {
	return filepath.Join(c.root, "sources", Slug(name))
}

// PagesDir returns <root>/sources/<slug>/pages.
func (c *Cache) PagesDir(name string) string {
	return filepath.Join(c.SourceDir(name), "pages")
}

// IndexDBPath returns the path the retriever should use for this source.
func (c *Cache) IndexDBPath(name string) string {
	return filepath.Join(c.SourceDir(name), "index.db")
}

// ManifestPath returns the manifest file for this source.
func (c *Cache) ManifestPath(name string) string {
	return filepath.Join(c.SourceDir(name), "manifest.json")
}

// RegistryPath returns the registry file path.
func (c *Cache) RegistryPath() string {
	return filepath.Join(c.root, "registry.json")
}

// EnsureSource creates the <root>/sources/<slug>/pages directory.
func (c *Cache) EnsureSource(name string) error {
	if err := os.MkdirAll(c.PagesDir(name), 0o755); err != nil {
		return fmt.Errorf("store: ensure source %s: %w", name, err)
	}
	return nil
}

// WritePage atomically writes doc to <pagesDir>/<doc.Path>.md, prefixing
// YAML frontmatter so the markdown remains self-describing on disk.
func (c *Cache) WritePage(sourceName string, doc types.Document) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if doc.Path == "" {
		return "", errors.New("store: page path is empty")
	}
	rel := normalizePath(doc.Path)
	if rel == "" {
		return "", errors.New("store: invalid page path")
	}
	full := filepath.Join(c.PagesDir(sourceName), rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", fmt.Errorf("store: mkdir %s: %w", filepath.Dir(full), err)
	}

	body := frontmatter(doc) + doc.Markdown
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("store: write tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, full); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("store: rename %s: %w", full, err)
	}
	return full, nil
}

// ReadPage returns the raw bytes of the markdown file at the given
// source-relative path. The frontmatter, if any, is included verbatim.
func (c *Cache) ReadPage(sourceName, relPath string) ([]byte, error) {
	full := filepath.Join(c.PagesDir(sourceName), normalizePath(relPath))
	return os.ReadFile(full)
}

// ListPages walks the source's pages directory and returns relative paths
// of every .md file it contains, in deterministic order.
func (c *Cache) ListPages(sourceName string) ([]string, error) {
	root := c.PagesDir(sourceName)
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && p == root {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store: list pages for %s: %w", sourceName, err)
	}
	sort.Strings(out)
	return out, nil
}

// RemoveSource deletes <root>/sources/<slug> entirely.
func (c *Cache) RemoveSource(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	dir := c.SourceDir(name)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("store: remove %s: %w", dir, err)
	}
	return nil
}

// Manifest is the per-source state file.
type Manifest struct {
	Source       string                  `json:"source"`
	Kind         string                  `json:"kind"`
	Root         string                  `json:"root"`
	LastSyncedAt time.Time               `json:"last_synced_at"`
	Pages        map[string]ManifestPage `json:"pages"`
}

// ManifestPage records what we know about a single cached page.
type ManifestPage struct {
	URL         string    `json:"url"`
	ContentHash string    `json:"content_hash"`
	FetchedAt   time.Time `json:"fetched_at"`
	TokenCount  int       `json:"token_count"`
}

// LoadManifest reads the manifest file or returns a zero-value manifest
// (with Pages initialized) if the file does not yet exist.
func (c *Cache) LoadManifest(sourceName string) (Manifest, error) {
	path := c.ManifestPath(sourceName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{Source: sourceName, Pages: map[string]ManifestPage{}}, nil
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("store: read manifest %s: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("store: parse manifest %s: %w", path, err)
	}
	if m.Pages == nil {
		m.Pages = map[string]ManifestPage{}
	}
	return m, nil
}

// SaveManifest writes m to disk atomically.
func (c *Cache) SaveManifest(sourceName string, m Manifest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	path := c.ManifestPath(sourceName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("store: mkdir manifest: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("store: marshal manifest: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("store: write tmp manifest: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("store: rename manifest: %w", err)
	}
	return nil
}

// frontmatter returns the YAML header that prefixes every cached page.
func frontmatter(d types.Document) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	if d.Title != "" {
		fmt.Fprintf(&sb, "title: %q\n", d.Title)
	}
	if d.URL != "" {
		fmt.Fprintf(&sb, "url: %q\n", d.URL)
	}
	if d.Source != "" {
		fmt.Fprintf(&sb, "source: %q\n", d.Source)
	}
	if !d.FetchedAt.IsZero() {
		fmt.Fprintf(&sb, "fetched_at: %q\n", d.FetchedAt.UTC().Format(time.RFC3339))
	}
	if d.ContentHash != "" {
		fmt.Fprintf(&sb, "content_hash: %q\n", d.ContentHash)
	}
	sb.WriteString("---\n\n")
	return sb.String()
}

// normalizePath cleans a source-relative path, rejects upward traversal
// that escapes the source directory, and converts backslashes so on-disk
// layout is consistent across platforms. Cleaning is done after slash
// normalization so checks see a single canonical form.
func normalizePath(p string) string {
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimPrefix(p, "\\")
	p = strings.ReplaceAll(p, "\\", "/")
	cleaned := filepath.ToSlash(filepath.Clean(p))
	if cleaned == "." || cleaned == "/" || cleaned == "" {
		return ""
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	if !strings.HasSuffix(cleaned, ".md") {
		cleaned += ".md"
	}
	return filepath.FromSlash(cleaned)
}

// Slug returns a filesystem-safe identifier for a source name.
func Slug(name string) string {
	var sb strings.Builder
	sb.Grow(len(name))
	prevDash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.':
			sb.WriteRune(r)
			prevDash = false
		case r >= 'A' && r <= 'Z':
			sb.WriteRune(r + 32)
			prevDash = false
		case r == '-' || r == '_' || r == ' ' || r == '/':
			if !prevDash && sb.Len() > 0 {
				sb.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(sb.String(), "-")
	if out == "" {
		out = "source"
	}
	return out
}
