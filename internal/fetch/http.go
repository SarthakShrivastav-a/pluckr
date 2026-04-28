package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultTimeout = 30 * time.Second
	defaultUA      = "pluckr/0.1 (+https://github.com/SarthakShrivastav-a/pluckr)"
	defaultAccept  = "text/markdown, text/html;q=0.9, */*;q=0.1"
	maxBody        = 10 << 20 // 10 MiB; docs pages should never approach this
)

// HTTPFetcher is the default plain-HTTP fetcher.
type HTTPFetcher struct {
	Client    HTTPClient
	UserAgent string
	Accept    string
	MaxBody   int64
}

// NewHTTP returns an HTTPFetcher with sensible defaults.
func NewHTTP() *HTTPFetcher {
	return &HTTPFetcher{
		Client: &http.Client{
			Timeout: defaultTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) > 10 {
					return errors.New("too many redirects")
				}
				return nil
			},
		},
		UserAgent: defaultUA,
		Accept:    defaultAccept,
		MaxBody:   maxBody,
	}
}

// Fetch performs the request.
func (f *HTTPFetcher) Fetch(ctx context.Context, req Request) (*Response, error) {
	if req.URL == "" {
		return nil, errors.New("fetch: empty url")
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch: build request: %w", err)
	}

	accept := req.Accept
	if accept == "" {
		accept = f.Accept
	}
	r.Header.Set("Accept", accept)
	r.Header.Set("User-Agent", f.UserAgent)
	for k, v := range req.Headers {
		r.Header.Set(k, v)
	}
	for name, value := range req.Cookies {
		r.AddCookie(&http.Cookie{Name: name, Value: value})
	}

	resp, err := f.Client.Do(r)
	if err != nil {
		return nil, fmt.Errorf("fetch: %s: %w", req.URL, err)
	}
	defer resp.Body.Close()

	limit := f.MaxBody
	if limit <= 0 {
		limit = maxBody
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("fetch: read %s: %w", req.URL, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("fetch: body for %s exceeded %d bytes", req.URL, limit)
	}

	return &Response{
		URL:         resp.Request.URL.String(),
		Status:      resp.StatusCode,
		ContentType: strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type"))),
		Body:        body,
		FetchedAt:   time.Now().UTC(),
	}, nil
}
