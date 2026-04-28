// Package types defines the small set of value types that flow through the
// fetch -> render -> chunk -> index -> serve pipeline. Keeping them in one
// dependency-free package avoids import cycles between the operational
// packages.
package types

import "time"

// Page is the raw artifact a Source emits during discovery: bytes plus the
// metadata needed to render and address them. The renderer turns a Page
// into a Document.
type Page struct {
	URL         string
	ContentType string
	Body        []byte
	FetchedAt   time.Time
	// Path is the source-relative key used to write a file on disk.
	// For websites it is derived from the URL pathname; for local and
	// github sources it is the original repo-relative path.
	Path string
}

// Document is post-rendering: clean markdown plus title and an outline of
// headings. The chunker reads Document; the indexer writes it (whole) to
// disk and the chunks (split) to the retriever.
type Document struct {
	Source      string  // logical source name from the registry
	URL         string  // canonical URL (post-redirect)
	Path        string  // disk-relative path under sources/<source>/pages/
	Title       string
	Markdown    string
	Outline     []Heading
	TokenCount  int
	FetchedAt   time.Time
	ContentHash string
}

// Heading is a node in the document's outline. Order matches document order;
// Level is 1..6 mirroring HTML h1..h6 / markdown #..######.
type Heading struct {
	Level   int
	Text    string
	Anchor  string
	Offset  int // byte offset in Document.Markdown where the heading starts
}

// Chunk is the indexable unit. A Document yields one or more chunks; each
// chunk carries the full heading path so retrieval results are
// self-locating without needing to re-render the parent document.
type Chunk struct {
	Source      string
	URL         string
	Path        string   // document path (chunks within the same doc share this)
	Title       string   // document title
	HeadingPath []string // ["React", "Hooks", "useState"]
	Anchor      string   // last heading's anchor, used to deep-link
	Body        string   // markdown body of just this section
	TokenCount  int
	Order       int // 0-based position within the parent document
}

// Hit is a single retrieval result.
type Hit struct {
	Source        string    `json:"source"`
	URL           string    `json:"url"`
	HeadingPath   []string  `json:"heading_path"`
	Anchor        string    `json:"anchor,omitempty"`
	Snippet       string    `json:"snippet"`
	TokenCount    int       `json:"token_count"`
	LastSyncedAt  time.Time `json:"last_synced_at"`
	Stale         bool      `json:"stale"`
	Score         float64   `json:"score"`
}

// SourceInfo summarises one entry in the registry plus its on-disk state.
type SourceInfo struct {
	Name           string    `json:"name"`
	Kind           string    `json:"kind"` // "website" | "llms_txt" | "github" | "local"
	Root           string    `json:"root"` // entry URL, repo, or path
	PageCount      int       `json:"page_count"`
	LastSyncedAt   time.Time `json:"last_synced_at"`
	RefreshAfter   string    `json:"refresh_after,omitempty"` // "7d" | "manual" | "never"
	Stale          bool      `json:"stale"`
}

// FreshnessThreshold returns the duration after which a source is
// considered stale. Empty / "manual" / "never" return 0, signalling
// "do not auto-refresh".
func ParseRefresh(s string) time.Duration {
	if s == "" || s == "manual" || s == "never" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		// Tolerate the human-friendly "7d" / "30d" shorthand that
		// time.ParseDuration rejects.
		if len(s) > 1 && s[len(s)-1] == 'd' {
			if days, e2 := time.ParseDuration(s[:len(s)-1] + "h"); e2 == nil {
				return days * 24
			}
		}
		return 0
	}
	return d
}
