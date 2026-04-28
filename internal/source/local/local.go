// Package local implements the local Source kind: read markdown / text
// files from a directory tree on disk. This is the cheap escape hatch
// for ingesting internal or proprietary docs without standing up a web
// server or wrestling with auth.
//
// The user points at a folder of .md / .markdown / .mdx / .txt files
// and pluckr treats each one as a Page whose path mirrors its position
// under the root.
package local

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SarthakShrivastav-a/pluckr/internal/registry"
	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

// SupportedExts lists the file extensions Local will pick up. The set
// is intentionally small for v0.1; PDF and other binary formats can be
// added behind a separate source kind later.
var SupportedExts = map[string]struct{}{
	".md":       {},
	".markdown": {},
	".mdx":      {},
	".txt":      {},
}

// Local is the Source for a directory tree on disk.
type Local struct {
	SourceName string
	Root       string
	// MaxPages caps the number of files emitted. Zero means no cap.
	MaxPages int
}

// Kind returns registry.KindLocal.
func (l *Local) Kind() string { return registry.KindLocal }

// Name returns the configured source name.
func (l *Local) Name() string { return l.SourceName }

// Pull walks the configured root and emits one Page per supported
// file. Symlinks are followed shallowly; we do not chase out of the
// root tree.
func (l *Local) Pull(ctx context.Context, out chan<- types.Page) error {
	if l.Root == "" {
		return errors.New("local: root is empty")
	}
	root, err := filepath.Abs(l.Root)
	if err != nil {
		return fmt.Errorf("local: resolve root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("local: stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("local: %s is not a directory", root)
	}

	emitted := 0
	walkErr := filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			// Skip dotfile directories (e.g., .git) - these cause
			// almost universal pain when iterating real folders.
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if l.MaxPages > 0 && emitted >= l.MaxPages {
			return filepath.SkipAll
		}

		ext := strings.ToLower(filepath.Ext(p))
		if _, ok := SupportedExts[ext]; !ok {
			return nil
		}

		body, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("local: read %s: %w", p, err)
		}

		rel, err := filepath.Rel(root, p)
		if err != nil {
			return fmt.Errorf("local: rel %s: %w", p, err)
		}
		rel = filepath.ToSlash(rel)
		// Strip the file extension; the store re-attaches .md when it
		// writes the page back out.
		relWithoutExt := strings.TrimSuffix(rel, filepath.Ext(rel))

		page := types.Page{
			URL:         fileURL(p),
			ContentType: contentTypeFor(ext),
			Body:        body,
			FetchedAt:   time.Now().UTC(),
			Path:        relWithoutExt,
		}
		select {
		case out <- page:
			emitted++
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})

	if walkErr != nil && !errors.Is(walkErr, filepath.SkipAll) {
		return fmt.Errorf("local: walk %s: %w", root, walkErr)
	}
	if emitted == 0 {
		return fmt.Errorf("local: no supported files under %s", root)
	}
	return nil
}

// fileURL builds a file:// URL for the absolute path p so downstream
// consumers (frontmatter, hits) have a stable, addressable URL even
// for content that never came from the web.
func fileURL(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	abs = filepath.ToSlash(abs)
	if strings.HasPrefix(abs, "/") {
		return "file://" + abs
	}
	// Windows path: /C:/foo/bar
	return "file:///" + abs
}

// contentTypeFor returns a safe content type for the renderer to use.
// .md / .markdown / .mdx are treated as markdown; .txt is markdown-ish
// (the renderer's signal heuristic will detect it if the file is
// markdown-shaped, otherwise it falls back to a passthrough).
func contentTypeFor(ext string) string {
	switch ext {
	case ".md", ".markdown", ".mdx":
		return "text/markdown; charset=utf-8"
	case ".txt":
		return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

// validateRoot is a small predicate used by callers (CLI / registry)
// before they accept a local root.
func validateRoot(root string) error {
	if root == "" {
		return errors.New("local: root is empty")
	}
	parsed, err := url.Parse(root)
	if err == nil && parsed.Scheme != "" && parsed.Scheme != "file" {
		return fmt.Errorf("local: root must be a path or file:// URL, got scheme %q", parsed.Scheme)
	}
	return nil
}

// ValidateRoot is the public wrapper around validateRoot.
func ValidateRoot(root string) error { return validateRoot(root) }
