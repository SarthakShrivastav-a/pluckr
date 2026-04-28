package website

import (
	"context"
	"fmt"
	"net/url"
	"runtime"
	"strings"
	"sync"

	"github.com/SarthakShrivastav-a/pluckr/internal/fetch"
	"github.com/SarthakShrivastav-a/pluckr/internal/registry"
	"github.com/SarthakShrivastav-a/pluckr/internal/source"
	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

// Website is the Source implementation for plain docs sites.
type Website struct {
	SourceName string
	Root       string
	Fetcher    fetch.Fetcher
	MaxPages   int
	Headers    map[string]string
	Cookies    map[string]string
	// Concurrency controls the number of fetch goroutines used during
	// Pull. Zero falls back to runtime.NumCPU()*2 with a floor of 8.
	Concurrency int
	// Lookup is consulted to expand ${ENV} references in headers and
	// cookies. Tests inject a fake; production code passes os.Getenv.
	Lookup func(string) string
}

// Kind returns registry.KindWebsite.
func (w *Website) Kind() string { return registry.KindWebsite }

// Name returns the source name.
func (w *Website) Name() string { return w.SourceName }

// Pull discovers URLs and emits a Page per fetched URL onto out. Caller
// closes out.
func (w *Website) Pull(ctx context.Context, out chan<- types.Page) error {
	if w.Fetcher == nil {
		w.Fetcher = fetch.NewHTTP()
	}
	if w.MaxPages <= 0 {
		w.MaxPages = 500
	}

	headers := source.ResolveHeaders(w.Headers, w.Lookup)
	cookies := source.ResolveCookies(w.Cookies, w.Lookup)

	urls, err := Discover(ctx, w.Root, DiscoverOptions{
		MaxPages: w.MaxPages,
		Headers:  headers,
		Cookies:  cookies,
		Fetcher:  w.Fetcher,
	})
	if err != nil {
		return fmt.Errorf("website: %s: %w", w.SourceName, err)
	}
	if len(urls) == 0 {
		return fmt.Errorf("website: %s: no pages discovered", w.SourceName)
	}

	concurrency := w.Concurrency
	if concurrency <= 0 {
		concurrency = runtime.NumCPU() * 2
		if concurrency < 8 {
			concurrency = 8
		}
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	errCh := make(chan error, len(urls))

	for _, u := range urls {
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

			resp, err := w.Fetcher.Fetch(ctx, fetch.Request{
				URL: u, Headers: headers, Cookies: cookies,
			})
			if err != nil {
				errCh <- fmt.Errorf("website: fetch %s: %w", u, err)
				return
			}
			if !resp.IsSuccess() {
				errCh <- fmt.Errorf("website: HTTP %d for %s", resp.Status, u)
				return
			}
			page := types.Page{
				URL:         resp.URL,
				ContentType: resp.ContentType,
				Body:        resp.Body,
				FetchedAt:   resp.FetchedAt,
				Path:        pagePathFromURL(resp.URL),
			}
			select {
			case out <- page:
			case <-ctx.Done():
			}
		}()
	}
	wg.Wait()
	close(errCh)

	// Collect errors but don't fail the whole pull on individual page
	// failures; the pipeline can decide how strict to be.
	var firstErr error
	count := 0
	for e := range errCh {
		count++
		if firstErr == nil {
			firstErr = e
		}
	}
	if count > 0 && count == len(urls) {
		return fmt.Errorf("website: every URL failed (first: %w)", firstErr)
	}
	return nil
}

// pagePathFromURL turns a URL into the source-relative file path used
// on disk and in the FTS5 index. Mirrors webpull's logic: trailing
// slashes become 'index', .html / .htm extensions are stripped, and the
// leading '/' is dropped.
func pagePathFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	p := u.Path
	if p == "" || p == "/" {
		return "index"
	}
	if strings.HasSuffix(p, "/") {
		p += "index"
	}
	if strings.HasSuffix(p, ".html") {
		p = strings.TrimSuffix(p, ".html")
	} else if strings.HasSuffix(p, ".htm") {
		p = strings.TrimSuffix(p, ".htm")
	}
	p = strings.TrimPrefix(p, "/")
	return p
}
