// Package llmstxt implements the llms.txt Source kind, a fast lane for
// sites that publish the emerging /llms.txt or /llms-full.txt
// convention (see https://llmstxt.org).
//
// Discovery prefers /llms-full.txt: when present it is one big curated
// markdown bundle, no scraping required. Otherwise we parse the
// markdown links from /llms.txt and fetch each one.
package llmstxt

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/SarthakShrivastav-a/pluckr/internal/fetch"
	"github.com/SarthakShrivastav-a/pluckr/internal/registry"
	"github.com/SarthakShrivastav-a/pluckr/internal/source"
	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

// linkRE captures the URL component of markdown inline links. The
// llms.txt format uses these for each documented page.
var linkRE = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)

// LLMSTxt is the Source for the /llms.txt convention.
type LLMSTxt struct {
	SourceName  string
	Root        string // base origin or full /llms.txt URL
	Fetcher     fetch.Fetcher
	Headers     map[string]string
	Cookies     map[string]string
	MaxPages    int
	Concurrency int
	Lookup      func(string) string
}

// Kind returns registry.KindLLMSTxt.
func (l *LLMSTxt) Kind() string { return registry.KindLLMSTxt }

// Name returns the configured source name.
func (l *LLMSTxt) Name() string { return l.SourceName }

// Pull resolves /llms-full.txt if available, otherwise /llms.txt, and
// emits a Page per discovered document.
func (l *LLMSTxt) Pull(ctx context.Context, out chan<- types.Page) error {
	if l.Fetcher == nil {
		l.Fetcher = fetch.NewHTTP()
	}
	if l.MaxPages <= 0 {
		l.MaxPages = 500
	}
	headers := source.ResolveHeaders(l.Headers, l.Lookup)
	cookies := source.ResolveCookies(l.Cookies, l.Lookup)

	fullURL, indexURL := resolveURLs(l.Root)

	// Prefer the bundled full text if the site publishes it.
	if resp, err := l.Fetcher.Fetch(ctx, fetch.Request{URL: fullURL, Headers: headers, Cookies: cookies}); err == nil && resp.IsSuccess() && len(resp.Body) > 0 {
		select {
		case out <- types.Page{
			URL:         resp.URL,
			ContentType: ensureMarkdown(resp.ContentType),
			Body:        resp.Body,
			FetchedAt:   resp.FetchedAt,
			Path:        "llms-full",
		}:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}

	// Fall back to the index file.
	resp, err := l.Fetcher.Fetch(ctx, fetch.Request{URL: indexURL, Headers: headers, Cookies: cookies})
	if err != nil {
		return fmt.Errorf("llmstxt: fetch %s: %w", indexURL, err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("llmstxt: HTTP %d for %s", resp.Status, indexURL)
	}

	links := extractLinks(string(resp.Body), indexURL)
	if len(links) > l.MaxPages {
		links = links[:l.MaxPages]
	}
	if len(links) == 0 {
		// Index has no harvestable links - emit it as a single page so
		// the user at least has the curated descriptions on disk.
		select {
		case out <- types.Page{
			URL:         resp.URL,
			ContentType: ensureMarkdown(resp.ContentType),
			Body:        resp.Body,
			FetchedAt:   resp.FetchedAt,
			Path:        "llms",
		}:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}

	concurrency := l.Concurrency
	if concurrency <= 0 {
		concurrency = runtime.NumCPU() * 2
		if concurrency < 8 {
			concurrency = 8
		}
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	errCh := make(chan error, len(links))

	for _, u := range links {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		default:
		}
		u := u
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			r, err := l.Fetcher.Fetch(ctx, fetch.Request{URL: u, Headers: headers, Cookies: cookies})
			if err != nil {
				errCh <- err
				return
			}
			if !r.IsSuccess() {
				errCh <- fmt.Errorf("llmstxt: HTTP %d for %s", r.Status, u)
				return
			}
			page := types.Page{
				URL:         r.URL,
				ContentType: r.ContentType,
				Body:        r.Body,
				FetchedAt:   r.FetchedAt,
				Path:        pathFromURL(r.URL),
			}
			select {
			case out <- page:
			case <-ctx.Done():
			}
		}()
	}
	wg.Wait()
	close(errCh)

	count := 0
	var firstErr error
	for e := range errCh {
		count++
		if firstErr == nil {
			firstErr = e
		}
	}
	if count > 0 && count == len(links) {
		return fmt.Errorf("llmstxt: every link failed (first: %w)", firstErr)
	}
	return nil
}

// resolveURLs returns (full, index) URLs for a given root. Accepts:
//   - https://example.com           -> (.../llms-full.txt, .../llms.txt)
//   - https://example.com/llms.txt  -> (.../llms-full.txt, .../llms.txt)
//   - https://example.com/llms-full.txt -> (input, .../llms.txt)
func resolveURLs(root string) (string, string) {
	root = strings.TrimRight(root, "/")
	switch {
	case strings.HasSuffix(root, "/llms.txt"):
		base := strings.TrimSuffix(root, "/llms.txt")
		return base + "/llms-full.txt", root
	case strings.HasSuffix(root, "/llms-full.txt"):
		base := strings.TrimSuffix(root, "/llms-full.txt")
		return root, base + "/llms.txt"
	default:
		return root + "/llms-full.txt", root + "/llms.txt"
	}
}

// extractLinks returns absolute URLs found in body, resolved against
// base. Order is preserved and duplicates collapsed.
func extractLinks(body, baseURL string) []string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil
	}
	matches := linkRE.FindAllStringSubmatch(body, -1)
	seen := map[string]struct{}{}
	var out []string
	for _, m := range matches {
		raw := strings.TrimSpace(m[1])
		if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "mailto:") {
			continue
		}
		u, err := base.Parse(raw)
		if err != nil {
			continue
		}
		u.Fragment = ""
		s := u.String()
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// pathFromURL turns a URL into the source-relative file path. Same rules
// as the website source.
func pathFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	p := u.Path
	if p == "" || p == "/" {
		// Use host as the file name when the path is empty.
		return u.Hostname()
	}
	if strings.HasSuffix(p, "/") {
		p += "index"
	}
	if strings.HasSuffix(p, ".html") {
		p = strings.TrimSuffix(p, ".html")
	} else if strings.HasSuffix(p, ".htm") {
		p = strings.TrimSuffix(p, ".htm")
	}
	if strings.HasSuffix(p, ".md") {
		p = strings.TrimSuffix(p, ".md")
	}
	return strings.TrimPrefix(p, "/")
}

// ensureMarkdown forces a markdown content type when the response did
// not declare one - llms.txt files are markdown by convention.
func ensureMarkdown(ct string) string {
	if strings.Contains(ct, "text/markdown") || strings.Contains(ct, "text/x-markdown") {
		return ct
	}
	return "text/markdown; charset=utf-8"
}
