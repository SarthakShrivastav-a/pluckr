// Package fetch defines a minimal Fetcher abstraction. Sources hand it a
// URL and a header bag; it returns the response body or an error.
//
// Two fetcher implementations are planned:
//
//   - http (this package): plain net/http with redirect handling and
//     a per-request timeout. Default for everything.
//   - headless (future): rod-driven Chromium for SPA-only docs sites.
//     The Fetcher interface lets the pipeline auto-escalate without
//     either side knowing about the other.
package fetch

import (
	"context"
	"net/http"
	"time"
)

// Request describes one fetch attempt.
type Request struct {
	URL     string
	Headers map[string]string // resolved (env-vars already expanded)
	Cookies map[string]string
	// Accept overrides the Accept header; if empty fetchers send their
	// own preferred default (HTML+markdown for the http fetcher).
	Accept string
}

// Response is what the fetcher hands back. Body is owned by the caller and
// must be released - currently fetchers buffer the whole body in memory,
// which is fine for docs pages but not for binary downloads (those are
// filtered out earlier in discovery).
type Response struct {
	URL         string // final URL after redirects
	Status      int
	ContentType string
	Body        []byte
	FetchedAt   time.Time
}

// Fetcher is the operational interface implementations satisfy.
type Fetcher interface {
	Fetch(ctx context.Context, req Request) (*Response, error)
}

// Status helpers used by callers that want to special-case 4xx vs 5xx
// without touching net/http directly.

// IsSuccess reports whether r is a 2xx response.
func (r *Response) IsSuccess() bool { return r != nil && r.Status >= 200 && r.Status < 300 }

// HTTPClient is the small subset of *http.Client that fetchers need;
// extracted so tests can swap in an in-memory transport.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}
