// headless.go: Chromium-based Fetcher implementation.
//
// Used by the pipeline as an opt-in escalation target when the HTTP
// renderer reports ErrEmptyContent (typically a JS-shell SPA that
// ships near-empty HTML and renders content client-side). The user
// must have Chrome / Chromium installed on PATH; we never bundle a
// browser. The launcher locates it via standard probe paths the rod
// library ships with.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// HeadlessFetcher renders pages through a real browser before returning
// the post-render HTML. It is safe for concurrent use; one browser
// process is shared across calls and only spawns on first Fetch.
type HeadlessFetcher struct {
	UserAgent string
	// Timeout caps the total per-page time including navigation, network
	// settle wait, and HTML extraction. Defaults to 30 seconds.
	Timeout time.Duration
	// StableWait is how long the page must be quiet (no in-flight
	// requests) before HTML is harvested. Defaults to 1 second.
	StableWait time.Duration
	// MaxBody bounds the returned HTML byte size. Defaults to maxBody.
	MaxBody int64

	once    sync.Once
	browser *rod.Browser
	closer  func()
	initErr error
}

// NewHeadless returns an unconnected HeadlessFetcher. The browser is
// launched lazily on the first Fetch call so callers do not pay the
// startup cost when the escalation never triggers.
func NewHeadless() *HeadlessFetcher {
	return &HeadlessFetcher{
		UserAgent:  defaultUA,
		Timeout:    30 * time.Second,
		StableWait: time.Second,
		MaxBody:    maxBody,
	}
}

// Close releases the underlying browser process. Safe to call multiple
// times.
func (h *HeadlessFetcher) Close() error {
	if h.closer != nil {
		h.closer()
	}
	if h.browser != nil {
		return h.browser.Close()
	}
	return nil
}

func (h *HeadlessFetcher) ensure() error {
	h.once.Do(func() {
		path, found := launcher.LookPath()
		if !found {
			h.initErr = errors.New("headless: no Chrome/Chromium found in PATH; install Chrome or set ROD_BROWSER_BIN")
			return
		}
		l := launcher.New().Bin(path).Headless(true)
		u, err := l.Launch()
		if err != nil {
			h.initErr = fmt.Errorf("headless: launch browser: %w", err)
			return
		}
		b := rod.New().ControlURL(u)
		if err := b.Connect(); err != nil {
			h.initErr = fmt.Errorf("headless: connect: %w", err)
			return
		}
		h.browser = b
		h.closer = func() { l.Cleanup() }
	})
	return h.initErr
}

// Fetch navigates to req.URL, waits for the network to settle, then
// returns the post-render HTML.
func (h *HeadlessFetcher) Fetch(ctx context.Context, req Request) (*Response, error) {
	if req.URL == "" {
		return nil, errors.New("headless: empty url")
	}
	if err := h.ensure(); err != nil {
		return nil, err
	}

	timeout := h.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	stable := h.StableWait
	if stable <= 0 {
		stable = time.Second
	}

	page, err := h.browser.Page(proto.TargetCreateTarget{URL: ""})
	if err != nil {
		return nil, fmt.Errorf("headless: open page: %w", err)
	}
	page = page.Context(ctx).Timeout(timeout)
	defer func() { _ = page.Close() }()

	if h.UserAgent != "" {
		_ = page.SetUserAgent(&proto.NetworkSetUserAgentOverride{UserAgent: h.UserAgent})
	}
	if len(req.Headers) > 0 {
		flat := make([]string, 0, len(req.Headers)*2)
		for k, v := range req.Headers {
			flat = append(flat, k, v)
		}
		_, _ = page.SetExtraHeaders(flat)
	}

	if err := page.Navigate(req.URL); err != nil {
		return nil, fmt.Errorf("headless: navigate %s: %w", req.URL, err)
	}
	if err := page.WaitStable(stable); err != nil {
		return nil, fmt.Errorf("headless: wait stable %s: %w", req.URL, err)
	}

	html, err := page.HTML()
	if err != nil {
		return nil, fmt.Errorf("headless: read html %s: %w", req.URL, err)
	}
	limit := h.MaxBody
	if limit <= 0 {
		limit = maxBody
	}
	if int64(len(html)) > limit {
		html = html[:limit]
	}

	finalURL := req.URL
	if info, ierr := page.Info(); ierr == nil && info != nil && info.URL != "" {
		finalURL = info.URL
	}

	return &Response{
		URL:         finalURL,
		Status:      200,
		ContentType: "text/html; charset=utf-8",
		Body:        []byte(html),
		FetchedAt:   time.Now().UTC(),
	}, nil
}
