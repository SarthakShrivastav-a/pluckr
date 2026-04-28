// Package pipeline wires Source -> Renderer -> Chunker -> Store +
// Retriever into a single end-to-end ingest. It is the only place that
// knows about every other internal package, which keeps the rest of
// the codebase narrow and testable.
//
// Run() is the canonical entry point used by both the CLI 'pluckr pull'
// command and the MCP 'refresh_source' tool. It is goroutine-safe but
// callers are expected to serialize work for a single source name.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SarthakShrivastav-a/pluckr/internal/chunk"
	"github.com/SarthakShrivastav-a/pluckr/internal/fetch"
	"github.com/SarthakShrivastav-a/pluckr/internal/registry"
	"github.com/SarthakShrivastav-a/pluckr/internal/render"
	"github.com/SarthakShrivastav-a/pluckr/internal/retriever"
	"github.com/SarthakShrivastav-a/pluckr/internal/source"
	"github.com/SarthakShrivastav-a/pluckr/internal/source/github"
	"github.com/SarthakShrivastav-a/pluckr/internal/source/llmstxt"
	"github.com/SarthakShrivastav-a/pluckr/internal/source/local"
	"github.com/SarthakShrivastav-a/pluckr/internal/source/website"
	"github.com/SarthakShrivastav-a/pluckr/internal/store"
	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

// Pipeline wires the dependencies together. Construct one per cache
// root and reuse across runs.
type Pipeline struct {
	Cache      *store.Cache
	Retriever  retriever.Retriever
	Renderer   render.Renderer
	Chunker    chunk.Chunker
	Fetcher    fetch.Fetcher
	Lookup     func(string) string
	BufferSize int
}

// New returns a Pipeline with default Renderer and Chunker. The caller
// is responsible for opening the Retriever (which holds a DB handle).
func New(cache *store.Cache, ret retriever.Retriever) *Pipeline {
	return &Pipeline{
		Cache:      cache,
		Retriever:  ret,
		Renderer:   render.New(),
		Chunker:    chunk.New(),
		Fetcher:    fetch.NewHTTP(),
		BufferSize: 64,
	}
}

// Result summarises one Run.
type Result struct {
	Source    string
	Kind      string
	Pages     int
	Chunks    int
	Skipped   int
	Errors    []error
	Elapsed   time.Duration
	StartedAt time.Time
	EndedAt   time.Time
}

// Run executes the full pipeline for one registry entry.
func (p *Pipeline) Run(ctx context.Context, entry registry.Entry) (Result, error) {
	startedAt := time.Now().UTC()
	res := Result{Source: entry.Name, Kind: entry.Kind, StartedAt: startedAt}

	src, err := p.NewSource(entry)
	if err != nil {
		return res, err
	}
	if err := p.Cache.EnsureSource(entry.Name); err != nil {
		return res, err
	}

	manifest, err := p.Cache.LoadManifest(entry.Name)
	if err != nil {
		return res, err
	}
	manifest.Source = entry.Name
	manifest.Kind = entry.Kind
	manifest.Root = entry.Root
	if manifest.Pages == nil {
		manifest.Pages = map[string]store.ManifestPage{}
	}
	// Reset page state - we rebuild from this run's emissions.
	manifest.Pages = map[string]store.ManifestPage{}

	pages := make(chan types.Page, p.BufferSize)
	sourceErr := make(chan error, 1)

	go func() {
		defer close(pages)
		sourceErr <- src.Pull(ctx, pages)
	}()

	var allChunks []types.Chunk
	for page := range pages {
		select {
		case <-ctx.Done():
			res.Errors = append(res.Errors, ctx.Err())
		default:
		}

		doc, err := p.Renderer.Render(page.Body, page.ContentType, page.URL)
		if err != nil {
			if errors.Is(err, render.ErrEmptyContent) {
				res.Skipped++
				continue
			}
			res.Errors = append(res.Errors, fmt.Errorf("render %s: %w", page.URL, err))
			continue
		}
		doc.Source = entry.Name
		doc.Path = page.Path
		if doc.URL == "" {
			doc.URL = page.URL
		}
		if doc.FetchedAt.IsZero() {
			doc.FetchedAt = page.FetchedAt
		}

		if _, err := p.Cache.WritePage(entry.Name, doc); err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("write %s: %w", page.URL, err))
			continue
		}

		chunks := p.Chunker.Chunk(doc)
		for i := range chunks {
			chunks[i].Source = entry.Name
			if chunks[i].Path == "" {
				chunks[i].Path = page.Path
			}
		}
		allChunks = append(allChunks, chunks...)

		manifest.Pages[page.Path] = store.ManifestPage{
			URL:         doc.URL,
			ContentHash: doc.ContentHash,
			FetchedAt:   doc.FetchedAt,
			TokenCount:  doc.TokenCount,
		}
		res.Pages++
	}

	if err := <-sourceErr; err != nil {
		res.Errors = append(res.Errors, err)
	}

	if len(allChunks) > 0 {
		if err := p.Retriever.Reindex(ctx, entry.Name, allChunks); err != nil {
			return res, fmt.Errorf("pipeline: reindex %s: %w", entry.Name, err)
		}
	}
	res.Chunks = len(allChunks)

	manifest.LastSyncedAt = time.Now().UTC()
	if err := p.Cache.SaveManifest(entry.Name, manifest); err != nil {
		return res, fmt.Errorf("pipeline: save manifest %s: %w", entry.Name, err)
	}

	res.EndedAt = time.Now().UTC()
	res.Elapsed = res.EndedAt.Sub(startedAt)
	return res, nil
}

// NewSource constructs the concrete Source implementation for a
// registry entry. Centralising this here keeps every package free of
// awareness of the others.
func (p *Pipeline) NewSource(entry registry.Entry) (source.Source, error) {
	switch entry.Kind {
	case registry.KindWebsite:
		return &website.Website{
			SourceName: entry.Name,
			Root:       entry.Root,
			MaxPages:   entry.MaxPages,
			Headers:    entry.Headers,
			Cookies:    entry.Cookies,
			Fetcher:    p.Fetcher,
			Lookup:     p.Lookup,
		}, nil
	case registry.KindLLMSTxt:
		return &llmstxt.LLMSTxt{
			SourceName: entry.Name,
			Root:       entry.Root,
			MaxPages:   entry.MaxPages,
			Headers:    entry.Headers,
			Cookies:    entry.Cookies,
			Fetcher:    p.Fetcher,
			Lookup:     p.Lookup,
		}, nil
	case registry.KindGitHub:
		return &github.GitHub{
			SourceName: entry.Name,
			Spec:       entry.Root,
			MaxPages:   entry.MaxPages,
			Headers:    entry.Headers,
			Cookies:    entry.Cookies,
			Fetcher:    p.Fetcher,
			Lookup:     p.Lookup,
		}, nil
	case registry.KindLocal:
		return &local.Local{
			SourceName: entry.Name,
			Root:       entry.Root,
			MaxPages:   entry.MaxPages,
		}, nil
	default:
		return nil, fmt.Errorf("pipeline: unknown source kind %q", entry.Kind)
	}
}
