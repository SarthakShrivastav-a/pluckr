// Package website implements the website Source kind: discover URLs via
// sitemap, then nav extraction, then BFS link crawl, then fetch each
// URL through the supplied Fetcher.
//
// The strategy mirrors webpull's three-tier cascade: sitemap.xml is
// authoritative when present, navigation links are a strong signal for
// docs sites that ship server-rendered HTML, and link crawling is the
// last resort.
package website

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"

	"github.com/SarthakShrivastav-a/pluckr/internal/fetch"
)

// ignoredExt matches asset and binary extensions that we never want to
// pull as docs pages. Mirrors the regex used in webpull's discover.ts.
var ignoredExt = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|svg|webp|ico|pdf|zip|tar|gz|mp4|mp3|woff2?|ttf|eot|css|js|json|xml|rss|atom)$`)

// navSelectors are the CSS selectors consulted when sitemap discovery
// turns up nothing. Order matters: more specific selectors first.
var navSelectors = []string{
	"nav a[href]",
	"aside a[href]",
	`[class*="sidebar"] a[href]`,
	`[class*="Sidebar"] a[href]`,
	`[class*="navigation"] a[href]`,
	`[class*="toc"] a[href]`,
	`[class*="menu"] a[href]`,
	`[role="navigation"] a[href]`,
}

// locRE captures the URL inside <loc>...</loc> tags in sitemap XML.
var locRE = regexp.MustCompile(`(?is)<loc>\s*(.*?)\s*</loc>`)

// hrefRE captures hrefs from raw HTML for the BFS-crawl fallback. Light
// touch by design - we only need same-host links and we follow up with
// goquery for nav extraction.
var hrefRE = regexp.MustCompile(`(?i)href=["']([^"']+)["']`)

// sitemapRE captures Sitemap directives in robots.txt.
var sitemapRE = regexp.MustCompile(`(?im)^Sitemap:\s*(\S+)\s*$`)

// commonSitemapPaths is the list of conventional sitemap filenames we
// probe when robots.txt yields nothing.
var commonSitemapPaths = []string{
	"sitemap.xml",
	"sitemap_index.xml",
	"sitemap-0.xml",
}

// DiscoverOptions tunes discovery. Zero values are safe.
type DiscoverOptions struct {
	MaxPages    int
	Headers     map[string]string
	Cookies     map[string]string
	HTTPClient  *http.Client // for sitemap / nav prefetch when set; otherwise constructed
	Fetcher     fetch.Fetcher
}

// Discover returns the list of URLs to pull for this entry, mirroring
// the sitemap → nav → crawl cascade.
func Discover(ctx context.Context, entry string, opts DiscoverOptions) ([]string, error) {
	if opts.MaxPages <= 0 {
		opts.MaxPages = 500
	}
	if opts.Fetcher == nil {
		opts.Fetcher = fetch.NewHTTP()
	}

	original, err := url.Parse(entry)
	if err != nil {
		return nil, fmt.Errorf("website: bad entry url: %w", err)
	}

	// Resolve redirects up front so all downstream URL math uses the
	// final origin and pathname.
	first, err := opts.Fetcher.Fetch(ctx, fetch.Request{
		URL: entry, Headers: opts.Headers, Cookies: opts.Cookies,
	})
	if err != nil {
		return nil, fmt.Errorf("website: fetch entry: %w", err)
	}
	if !first.IsSuccess() {
		return nil, fmt.Errorf("website: HTTP %d for %s", first.Status, entry)
	}
	actual, err := url.Parse(first.URL)
	if err != nil {
		return nil, fmt.Errorf("website: parse final url: %w", err)
	}

	hosts := map[string]struct{}{original.Hostname(): {}, actual.Hostname(): {}}
	scope := scopePath(actual.Path)

	// Sitemap strategies in parallel: robots.txt + common paths at
	// original and post-redirect origins.
	origins := uniqueStrings(original.Scheme+"://"+original.Host, actual.Scheme+"://"+actual.Host)
	basePaths := uniqueStrings(parentDir(actual.Path), "/")

	type strategyResult []string
	resultsCh := make(chan strategyResult, 32)
	var wg sync.WaitGroup

	run := func(fn func() ([]string, error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			urls, err := fn()
			if err != nil || len(urls) == 0 {
				return
			}
			resultsCh <- urls
		}()
	}

	for _, origin := range origins {
		o := origin
		run(func() ([]string, error) { return sitemapFromRobots(ctx, opts, o) })
		for _, bp := range basePaths {
			bp := bp
			for _, name := range commonSitemapPaths {
				name := name
				url := o + bp + name
				run(func() ([]string, error) { return fetchSitemap(ctx, opts, url, 0) })
			}
		}
	}

	go func() { wg.Wait(); close(resultsCh) }()

	var best []string
	for urls := range resultsCh {
		for _, u := range urls {
			if parsed, perr := url.Parse(u); perr == nil {
				hosts[parsed.Hostname()] = struct{}{}
			}
		}
		filtered := filterAndDedupe(urls, hosts, scope, opts.MaxPages)
		if len(filtered) > len(best) {
			best = filtered
		}
	}
	if len(best) > 0 {
		return best, nil
	}

	// Sitemap empty - try nav extraction on the entry HTML we already have.
	nav, err := extractNav(actual, first.Body)
	if err == nil && len(nav) > 5 {
		filtered := filterAndDedupe(nav, hosts, scope, opts.MaxPages)
		if len(filtered) > 0 {
			return filtered, nil
		}
	}

	// Last resort: BFS crawl.
	return crawl(ctx, opts, actual, hosts, scope)
}

// sitemapFromRobots tries /robots.txt for Sitemap: directives and
// fetches the union of every sitemap they declare.
func sitemapFromRobots(ctx context.Context, opts DiscoverOptions, origin string) ([]string, error) {
	resp, err := opts.Fetcher.Fetch(ctx, fetch.Request{
		URL: origin + "/robots.txt", Headers: opts.Headers, Cookies: opts.Cookies,
	})
	if err != nil || !resp.IsSuccess() {
		return nil, nil
	}
	matches := sitemapRE.FindAllStringSubmatch(string(resp.Body), -1)
	if len(matches) == 0 {
		return nil, nil
	}
	var out []string
	for _, m := range matches {
		urls, _ := fetchSitemap(ctx, opts, strings.TrimSpace(m[1]), 0)
		out = append(out, urls...)
	}
	return out, nil
}

// fetchSitemap loads the sitemap at url and recursively descends into
// any sitemap indexes up to depth 3.
func fetchSitemap(ctx context.Context, opts DiscoverOptions, sitemapURL string, depth int) ([]string, error) {
	if depth > 3 {
		return nil, nil
	}
	resp, err := opts.Fetcher.Fetch(ctx, fetch.Request{
		URL: sitemapURL, Headers: opts.Headers, Cookies: opts.Cookies,
	})
	if err != nil || !resp.IsSuccess() {
		return nil, nil
	}
	body := string(resp.Body)
	if !strings.Contains(body, "<") {
		return nil, nil
	}

	matches := locRE.FindAllStringSubmatch(body, -1)
	locs := make([]string, 0, len(matches))
	for _, m := range matches {
		locs = append(locs, strings.TrimSpace(m[1]))
	}

	isIndex := strings.Contains(body, "<sitemapindex") ||
		(strings.Contains(body, "<sitemap>") && !strings.Contains(body, "<urlset"))

	if !isIndex {
		return locs, nil
	}

	var out []string
	for _, loc := range locs {
		nested, _ := fetchSitemap(ctx, opts, loc, depth+1)
		out = append(out, nested...)
	}
	return out, nil
}

// extractNav parses HTML and pulls hrefs from the configured navigation
// selectors, stripping fragments and query strings.
func extractNav(base *url.URL, body []byte) ([]string, error) {
	doc, err := goquery.NewDocumentFromReader(bytesReader(body))
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	var out []string
	for _, sel := range navSelectors {
		doc.Find(sel).Each(func(_ int, s *goquery.Selection) {
			href, ok := s.Attr("href")
			if !ok {
				return
			}
			href = strings.TrimSpace(href)
			if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "mailto:") {
				return
			}
			abs, err := base.Parse(href)
			if err != nil {
				return
			}
			abs.Fragment = ""
			abs.RawQuery = ""
			if ignoredExt.MatchString(abs.Path) {
				return
			}
			s2 := abs.String()
			if _, dup := seen[s2]; !dup {
				seen[s2] = struct{}{}
				out = append(out, s2)
			}
		})
	}
	out = append(out, base.String())
	return out, nil
}

// crawl walks the entry URL's connected component, same-host only,
// stopping at MaxPages.
func crawl(ctx context.Context, opts DiscoverOptions, base *url.URL, hosts map[string]struct{}, scope string) ([]string, error) {
	visited := map[string]struct{}{}
	queue := []string{base.String()}
	var found []string

	for len(queue) > 0 && len(found) < opts.MaxPages {
		batchSize := 20
		if remaining := opts.MaxPages - len(found); remaining < batchSize {
			batchSize = remaining
		}
		batch := queue[:min(batchSize, len(queue))]
		queue = queue[len(batch):]

		// Filter visited
		var todo []string
		for _, u := range batch {
			if _, dup := visited[u]; !dup {
				visited[u] = struct{}{}
				todo = append(todo, u)
			}
		}

		var (
			wg sync.WaitGroup
			mu sync.Mutex
		)
		links := make([]string, 0, len(todo)*8)
		sem := make(chan struct{}, 20)
		for _, u := range todo {
			u := u
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				resp, err := opts.Fetcher.Fetch(ctx, fetch.Request{
					URL: u, Headers: opts.Headers, Cookies: opts.Cookies,
				})
				if err != nil || !resp.IsSuccess() || !strings.Contains(string(resp.Body), "</html") {
					return
				}
				mu.Lock()
				found = append(found, resp.URL)
				mu.Unlock()
				for _, m := range hrefRE.FindAllStringSubmatch(string(resp.Body), -1) {
					abs, err := base.Parse(strings.TrimSpace(m[1]))
					if err != nil {
						continue
					}
					abs.Fragment = ""
					abs.RawQuery = ""
					if _, ok := hosts[abs.Hostname()]; !ok {
						continue
					}
					if ignoredExt.MatchString(abs.Path) {
						continue
					}
					mu.Lock()
					if _, dup := visited[abs.String()]; !dup {
						links = append(links, abs.String())
					}
					mu.Unlock()
				}
			}()
		}
		wg.Wait()

		seen := map[string]struct{}{}
		for _, l := range links {
			if _, dup := visited[l]; dup {
				continue
			}
			if _, dup := seen[l]; dup {
				continue
			}
			seen[l] = struct{}{}
			if len(found)+len(queue) >= opts.MaxPages {
				break
			}
			queue = append(queue, l)
		}
	}
	out := filterAndDedupe(found, hosts, scope, opts.MaxPages)
	if len(out) == 0 {
		return nil, errors.New("website: discovery turned up no pages")
	}
	return out, nil
}

// filterAndDedupe keeps URLs that are same-host, under the discovery
// scope, and not on the ignore list. Dedupe is by pathname so trivially
// duplicated URLs (with and without trailing slash) collapse.
func filterAndDedupe(urls []string, hosts map[string]struct{}, scope string, max int) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		if _, ok := hosts[u.Hostname()]; !ok {
			continue
		}
		if !strings.HasPrefix(u.Path, scope) && scope != "/" {
			continue
		}
		if ignoredExt.MatchString(u.Path) {
			continue
		}
		u.Fragment = ""
		u.RawQuery = ""
		if _, dup := seen[u.Path]; dup {
			continue
		}
		seen[u.Path] = struct{}{}
		out = append(out, u.String())
		if len(out) >= max {
			break
		}
	}
	return out
}

// scopePath mirrors webpull's getScopePath: collapses the entry pathname
// to the parent directory we use as the discovery prefix.
func scopePath(pathname string) string {
	if pathname == "" || pathname == "/" {
		return "/"
	}
	if matched := regexp.MustCompile(`\.\w+$`).MatchString(pathname); matched {
		return path.Dir(pathname) + "/"
	}
	if strings.HasSuffix(pathname, "/") {
		return pathname
	}
	segs := strings.Split(strings.Trim(pathname, "/"), "/")
	if len(segs) <= 1 {
		return pathname
	}
	return "/" + strings.Join(segs[:len(segs)-1], "/") + "/"
}

func parentDir(p string) string {
	if p == "" || p == "/" {
		return "/"
	}
	dir := path.Dir(p)
	if !strings.HasSuffix(dir, "/") {
		dir += "/"
	}
	return dir
}

func uniqueStrings(values ...string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, v := range values {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

// bytesReader is a thin shim so we can avoid importing bytes in this
// already-busy file purely for one Reader.
func bytesReader(b []byte) io.Reader { return strings.NewReader(string(b)) }
