// Package fts5 implements retriever.Retriever on top of SQLite FTS5 via
// the pure-Go modernc.org/sqlite driver. The schema is a single virtual
// table: searchable text columns (title, heading_path, body) sit
// alongside UNINDEXED metadata so a single SELECT returns everything the
// MCP layer needs without a join.
//
// The unicode61 + porter tokenizer combination gives reasonable
// stemming for English-language docs without external dependencies.
package fts5

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/SarthakShrivastav-a/pluckr/internal/retriever"
	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

// HeadingSep is the visible separator used to serialize a heading_path
// into a single FTS5 column. The unicode61 tokenizer treats '›' as
// punctuation, so words from each heading are tokenized independently
// while the on-disk form remains human-readable.
const HeadingSep = " › "

const schema = `
CREATE VIRTUAL TABLE IF NOT EXISTS chunks USING fts5(
    source UNINDEXED,
    url UNINDEXED,
    path UNINDEXED,
    title,
    heading_path,
    anchor UNINDEXED,
    body,
    token_count UNINDEXED,
    ord UNINDEXED,
    tokenize='porter unicode61'
);
`

// Store is an FTS5-backed retriever.
type Store struct {
	db *sql.DB
}

// Open opens (and creates if necessary) a SQLite database at path with
// the FTS5 schema applied. Use ":memory:" for tests.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("fts5: open %s: %w", path, err)
	}
	// Single connection avoids SQLite "database is locked" pain on
	// concurrent writers - the chunker is the only writer anyway and
	// reads through Search go through the same pool.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("fts5: create schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// Index appends chunks for sourceName.
func (s *Store) Index(ctx context.Context, sourceName string, chunks []types.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("fts5: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO chunks (source, url, path, title, heading_path, anchor, body, token_count, ord)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`)
	if err != nil {
		return fmt.Errorf("fts5: prepare insert: %w", err)
	}
	defer stmt.Close()

	for _, c := range chunks {
		hp := strings.Join(c.HeadingPath, HeadingSep)
		if _, err := stmt.ExecContext(ctx,
			sourceName, c.URL, c.Path, c.Title, hp, c.Anchor, c.Body, c.TokenCount, c.Order,
		); err != nil {
			return fmt.Errorf("fts5: insert chunk: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("fts5: commit: %w", err)
	}
	return nil
}

// Reindex wipes existing chunks for sourceName and inserts the new set in
// a single transaction so search never sees a partial index.
func (s *Store) Reindex(ctx context.Context, sourceName string, chunks []types.Chunk) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("fts5: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE source = ?`, sourceName); err != nil {
		return fmt.Errorf("fts5: clear source: %w", err)
	}

	if len(chunks) > 0 {
		stmt, err := tx.PrepareContext(ctx, `
INSERT INTO chunks (source, url, path, title, heading_path, anchor, body, token_count, ord)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`)
		if err != nil {
			return fmt.Errorf("fts5: prepare insert: %w", err)
		}
		defer stmt.Close()

		for _, c := range chunks {
			hp := strings.Join(c.HeadingPath, HeadingSep)
			if _, err := stmt.ExecContext(ctx,
				sourceName, c.URL, c.Path, c.Title, hp, c.Anchor, c.Body, c.TokenCount, c.Order,
			); err != nil {
				return fmt.Errorf("fts5: insert chunk: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("fts5: commit: %w", err)
	}
	return nil
}

// Remove deletes every chunk for sourceName.
func (s *Store) Remove(ctx context.Context, sourceName string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM chunks WHERE source = ?`, sourceName); err != nil {
		return fmt.Errorf("fts5: remove source %s: %w", sourceName, err)
	}
	return nil
}

// Sources returns the distinct source names.
func (s *Store) Sources(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT source FROM chunks ORDER BY source`)
	if err != nil {
		return nil, fmt.Errorf("fts5: list sources: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("fts5: scan source: %w", err)
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// Search runs a BM25-ranked FTS5 query against the body, title, and
// heading_path columns. Snippets are computed from the body column.
func (s *Store) Search(ctx context.Context, query string, opts retriever.SearchOptions) ([]types.Hit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("fts5: empty query")
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	args := []any{sanitizeQuery(query)}
	where := "chunks MATCH ?"
	if len(opts.Sources) > 0 {
		placeholders := make([]string, len(opts.Sources))
		for i, src := range opts.Sources {
			placeholders[i] = "?"
			args = append(args, src)
		}
		where += " AND source IN (" + strings.Join(placeholders, ",") + ")"
	}
	args = append(args, limit, opts.Offset)

	q := `
SELECT source, url, heading_path, anchor, token_count,
       snippet(chunks, 6, '[', ']', ' ... ', 24) AS snip,
       bm25(chunks) AS score
FROM chunks
WHERE ` + where + `
ORDER BY score
LIMIT ? OFFSET ?
`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("fts5: query: %w", err)
	}
	defer rows.Close()

	var out []types.Hit
	for rows.Next() {
		var (
			source, url, hp, anchor, snip string
			tokens                        int
			score                         float64
		)
		if err := rows.Scan(&source, &url, &hp, &anchor, &tokens, &snip, &score); err != nil {
			return nil, fmt.Errorf("fts5: scan hit: %w", err)
		}
		var path []string
		if hp != "" {
			path = strings.Split(hp, HeadingSep)
		}
		out = append(out, types.Hit{
			Source:      source,
			URL:         url,
			HeadingPath: path,
			Anchor:      anchor,
			Snippet:     snip,
			TokenCount:  tokens,
			Score:       score,
		})
	}
	return out, rows.Err()
}

// sanitizeQuery turns user input into a valid FTS5 MATCH expression.
// Each term is wrapped in double quotes (so punctuation can't break the
// parser) and the terms are joined with OR so BM25 can rank documents
// by how many of the terms they hit, weighted by rarity. Bare AND
// behaviour drops most natural-language queries to zero hits the moment
// a single word is missing from the corpus, which is the wrong default
// for an agent-driven search surface.
func sanitizeQuery(q string) string {
	q = strings.ReplaceAll(q, "\"", "")
	parts := strings.Fields(q)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimFunc(p, func(r rune) bool {
			switch r {
			case '(', ')', ':', '*', '"', '\'':
				return true
			}
			return false
		})
		if p == "" {
			continue
		}
		out = append(out, "\""+p+"\"")
	}
	if len(out) == 0 {
		return ""
	}
	if len(out) == 1 {
		return out[0]
	}
	return strings.Join(out, " OR ")
}
