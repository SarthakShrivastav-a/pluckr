// Package render turns the bytes a fetcher returned into a clean Document.
//
// Two fast paths exist on top of the regular HTML pipeline:
//
//   - Native markdown: when the response advertises a markdown content
//     type (or its body looks like markdown), it bypasses HTML parsing
//     entirely. This is the major shortcut for docs platforms that
//     already serve text/markdown.
//   - Empty-page detection: when the picked main subtree contains less
//     than EmptyContentMin bytes of real text, ErrEmptyContent is
//     returned so the pipeline can decide to escalate to a headless
//     fetcher (planned).
package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/PuerkitoBio/goquery"

	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

// ErrEmptyContent signals that the renderer could not find a substantive
// content block. Callers should treat it as a soft failure - the page
// fetched, parsing succeeded, but there is nothing to index.
var ErrEmptyContent = errors.New("render: empty content")

// EmptyContentMin is the visible-text byte threshold below which a page
// is considered empty for ErrEmptyContent purposes.
const EmptyContentMin = 80

// markdownSignal matches the first line of common markdown structures so we
// can identify markdown bodies that aren't advertised via Content-Type.
var markdownSignal = regexp.MustCompile("(?m)^(#{1,6}\\s|[-*]\\s|\\d+\\.\\s|```|>\\s|\\[.+\\]\\(.+\\))")

// headingSelfLink matches a markdown link whose target is an in-page
// fragment, e.g. [Toggling dark mode](#toggling-dark-mode). Modern docs
// engines wrap each heading text in such an anchor for the copy-link
// hover affordance, and html-to-markdown faithfully serialises it. We
// strip these links from heading lines so chunked heading paths and
// the on-disk markdown both read like plain prose.
var headingSelfLink = regexp.MustCompile(`\[([^\]]+)\]\(#[^)]*\)`)

// mainSelectors is consulted in order; the first selector that matches a
// node carrying more than EmptyContentMin bytes of text wins. This mirrors
// what most docs platforms render.
var mainSelectors = []string{
	"main",
	"[role=main]",
	"article",
	".markdown-body",
	".prose",
	".docs-content",
	".doc-content",
	".content",
	"#content",
	"#main-content",
	"#__next main",
}

// Renderer is the operational interface; HTMLRenderer is the only impl.
type Renderer interface {
	Render(body []byte, contentType, url string) (types.Document, error)
}

// HTMLRenderer is the default renderer. It is safe for concurrent use.
type HTMLRenderer struct{}

// New returns a Renderer with default config.
func New() *HTMLRenderer { return &HTMLRenderer{} }

// Render converts body to a Document. URL is used as the canonical URL on
// the returned Document.
func (r *HTMLRenderer) Render(body []byte, contentType, sourceURL string) (types.Document, error) {
	if isMarkdown(contentType, body) {
		return r.renderMarkdown(body, sourceURL), nil
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return types.Document{}, fmt.Errorf("render: parse html: %w", err)
	}

	doc.Find("script, style, noscript, iframe, link[rel=stylesheet], svg, [aria-hidden=true]").Remove()

	title := firstNonEmpty(
		strings.TrimSpace(doc.Find("title").First().Text()),
		strings.TrimSpace(doc.Find("h1").First().Text()),
	)

	main := pickMainContent(doc)
	if main == nil || strings.TrimSpace(main.Text()) == "" {
		return types.Document{Title: title, URL: sourceURL, FetchedAt: time.Now().UTC()}, ErrEmptyContent
	}
	if len(strings.TrimSpace(main.Text())) < EmptyContentMin {
		return types.Document{Title: title, URL: sourceURL, FetchedAt: time.Now().UTC()}, ErrEmptyContent
	}

	html, err := goquery.OuterHtml(main)
	if err != nil {
		return types.Document{}, fmt.Errorf("render: serialise main: %w", err)
	}
	md, err := htmltomarkdown.ConvertString(html)
	if err != nil {
		return types.Document{}, fmt.Errorf("render: convert: %w", err)
	}
	md = stripHeadingSelfLinks(md)
	md = normalizeMarkdown(md)

	outline := extractOutline(md)
	return types.Document{
		URL:         sourceURL,
		Title:       title,
		Markdown:    md,
		Outline:     outline,
		TokenCount:  estimateTokens(md),
		FetchedAt:   time.Now().UTC(),
		ContentHash: hashOf(md),
	}, nil
}

func (r *HTMLRenderer) renderMarkdown(body []byte, sourceURL string) types.Document {
	md := stripHeadingSelfLinks(string(body))
	md = normalizeMarkdown(md)
	title := titleFromMarkdown(md)
	outline := extractOutline(md)
	return types.Document{
		URL:         sourceURL,
		Title:       title,
		Markdown:    md,
		Outline:     outline,
		TokenCount:  estimateTokens(md),
		FetchedAt:   time.Now().UTC(),
		ContentHash: hashOf(md),
	}
}

func isMarkdown(contentType string, body []byte) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "text/markdown") || strings.Contains(ct, "text/x-markdown") {
		return true
	}
	// If the server didn't tell us anything HTMLish and the body looks
	// like markdown, trust it.
	if ct != "" && !strings.Contains(ct, "text/html") && markdownSignal.Match(body) {
		return true
	}
	return false
}

func pickMainContent(doc *goquery.Document) *goquery.Selection {
	for _, sel := range mainSelectors {
		s := doc.Find(sel).First()
		if s.Length() > 0 && len(strings.TrimSpace(s.Text())) >= EmptyContentMin {
			return s
		}
	}
	body := doc.Find("body").First()
	if body.Length() > 0 {
		return body
	}
	return doc.Selection
}

// titleFromMarkdown returns the text of the first H1, or empty string.
func titleFromMarkdown(md string) string {
	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(t, "# "))
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// estimateTokens is a fast char-based approximation that avoids pulling in
// a real tokenizer. Roughly 4 characters per token holds up well enough
// for the 800-token chunk cap. Real tokenization is a future build-tagged
// extension.
func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	n := len(s) / 4
	if n == 0 {
		n = 1
	}
	return n
}

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// stripHeadingSelfLinks removes [text](#anchor) markdown links found on
// heading lines. External-target links are left alone - the user might
// legitimately link out from a heading and we shouldn't break that.
func stripHeadingSelfLinks(md string) string {
	if !strings.Contains(md, "](#") {
		return md
	}
	lines := strings.Split(md, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			lines[i] = headingSelfLink.ReplaceAllString(line, "$1")
		}
	}
	return strings.Join(lines, "\n")
}

// normalizeMarkdown collapses runs of blank lines and trims trailing
// whitespace per line so the on-disk artifact diffs cleanly across runs.
func normalizeMarkdown(md string) string {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	lines := strings.Split(md, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	out := strings.Join(lines, "\n")
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(out) + "\n"
}
