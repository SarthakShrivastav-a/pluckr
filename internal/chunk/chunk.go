// Package chunk splits a rendered Document into the indexable units used
// by the retriever. Two passes:
//
//  1. Split at heading boundaries up to SplitLevel (default H2). Each
//     resulting chunk carries the heading_path that led to it so search
//     hits are self-locating.
//  2. Any chunk whose token count exceeds MaxTokens is re-split at
//     paragraph boundaries while preserving heading_path. This guards
//     against pathologically large API-reference pages without losing
//     structural signal.
package chunk

import (
	"strings"
	"unicode"

	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

// Chunker is the operational interface; HeadingChunker is the only impl.
type Chunker interface {
	Chunk(doc types.Document) []types.Chunk
}

// HeadingChunker is the default heading-bounded chunker.
type HeadingChunker struct {
	// MaxTokens is the soft cap above which a chunk is re-split at
	// paragraph boundaries. Defaults to 800.
	MaxTokens int
	// SplitLevel is the deepest heading level (1..6) that triggers a
	// new chunk. Defaults to 2 (H2). H1 is treated as the document's
	// title and never splits a chunk on its own.
	SplitLevel int
	// MinBody drops chunks whose body has fewer characters than this
	// after trimming. Prevents emitting empty stub chunks for headings
	// with no following content. Defaults to 24.
	MinBody int
}

// New returns a HeadingChunker with default settings.
func New() *HeadingChunker { return &HeadingChunker{} }

// defaults fills any zero fields with their default values.
func (c *HeadingChunker) defaults() {
	if c.MaxTokens <= 0 {
		c.MaxTokens = 800
	}
	if c.SplitLevel <= 0 || c.SplitLevel > 6 {
		c.SplitLevel = 2
	}
	if c.MinBody <= 0 {
		c.MinBody = 24
	}
}

// Chunk splits doc into chunks. Order is contiguous starting at 0.
func (c *HeadingChunker) Chunk(doc types.Document) []types.Chunk {
	c.defaults()
	first := c.splitByLevel(doc)
	var out []types.Chunk
	for _, ch := range first {
		if ch.TokenCount <= c.MaxTokens {
			out = append(out, ch)
			continue
		}
		out = append(out, c.splitByParagraph(ch)...)
	}
	for i := range out {
		out[i].Order = i
	}
	return out
}

func (c *HeadingChunker) splitByLevel(doc types.Document) []types.Chunk {
	type frame struct {
		Level  int
		Text   string
		Anchor string
	}
	var stack []frame
	var path []string
	var anchor string
	var current strings.Builder
	var chunks []types.Chunk
	used := map[string]int{}
	inFence := false

	flush := func() {
		body := strings.TrimSpace(current.String())
		current.Reset()
		if len(body) < c.MinBody {
			return
		}
		chunks = append(chunks, types.Chunk{
			Source:      doc.Source,
			URL:         doc.URL,
			Path:        doc.Path,
			Title:       doc.Title,
			HeadingPath: append([]string(nil), path...),
			Anchor:      anchor,
			Body:        body,
			TokenCount:  estimateTokens(body),
		})
	}

	pop := func(level int) {
		for len(stack) > 0 && stack[len(stack)-1].Level >= level {
			stack = stack[:len(stack)-1]
		}
	}
	rebuildPath := func() {
		path = path[:0]
		for _, f := range stack {
			path = append(path, f.Text)
		}
		if len(stack) > 0 {
			anchor = stack[len(stack)-1].Anchor
		} else {
			anchor = ""
		}
	}

	for _, line := range strings.Split(doc.Markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
		}

		level, text := parseHeading(line)
		if level == 0 || inFence {
			current.WriteString(line)
			current.WriteByte('\n')
			continue
		}

		// H1 is treated as the document title - already exposed via
		// Document.Title and Chunk.Title - so it never participates in
		// heading_path or splitting.
		if level == 1 {
			current.WriteString(line)
			current.WriteByte('\n')
			continue
		}

		if level <= c.SplitLevel {
			flush()
			pop(level)
			stack = append(stack, frame{Level: level, Text: text, Anchor: uniqueSlug(text, used)})
			rebuildPath()
		} else {
			pop(level)
			stack = append(stack, frame{Level: level, Text: text, Anchor: uniqueSlug(text, used)})
		}
		current.WriteString(line)
		current.WriteByte('\n')
	}
	flush()
	return chunks
}

func (c *HeadingChunker) splitByParagraph(ch types.Chunk) []types.Chunk {
	paragraphs := splitParagraphs(ch.Body)
	var out []types.Chunk
	var sb strings.Builder
	tokens := 0

	flush := func() {
		body := strings.TrimSpace(sb.String())
		sb.Reset()
		tokens = 0
		if len(body) < c.MinBody {
			return
		}
		out = append(out, types.Chunk{
			Source:      ch.Source,
			URL:         ch.URL,
			Path:        ch.Path,
			Title:       ch.Title,
			HeadingPath: append([]string(nil), ch.HeadingPath...),
			Anchor:      ch.Anchor,
			Body:        body,
			TokenCount:  estimateTokens(body),
		})
	}

	for _, p := range paragraphs {
		t := estimateTokens(p)
		if tokens+t > c.MaxTokens && sb.Len() > 0 {
			flush()
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(p)
		tokens += t
	}
	flush()
	if len(out) == 0 {
		// Fallback: oversized chunk had no exploitable structure;
		// keep it as-is so we never silently drop content.
		out = []types.Chunk{ch}
	}
	return out
}

// parseHeading returns the heading level (1..6) and trimmed text if line is
// an ATX heading. Returns (0, "") otherwise.
func parseHeading(line string) (int, string) {
	if !strings.HasPrefix(line, "#") {
		return 0, ""
	}
	level := 0
	for level < len(line) && line[level] == '#' && level < 7 {
		level++
	}
	if level < 1 || level > 6 {
		return 0, ""
	}
	if level == len(line) {
		return 0, ""
	}
	if line[level] != ' ' && line[level] != '\t' {
		return 0, ""
	}
	text := strings.TrimSpace(strings.TrimRight(line[level+1:], "#"))
	if text == "" {
		return 0, ""
	}
	return level, text
}

func splitParagraphs(s string) []string {
	var out []string
	var sb strings.Builder
	inFence := false
	flush := func() {
		t := strings.TrimSpace(sb.String())
		sb.Reset()
		if t != "" {
			out = append(out, t)
		}
	}
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			sb.WriteString(line)
			sb.WriteByte('\n')
			continue
		}
		if !inFence && trimmed == "" {
			flush()
			continue
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	flush()
	return out
}

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

func slugify(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevDash := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			prevDash = false
		case r == ' ' || r == '\t' || r == '-' || r == '_':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func uniqueSlug(text string, used map[string]int) string {
	base := slugify(text)
	if base == "" {
		base = "section"
	}
	count := used[base]
	used[base] = count + 1
	if count == 0 {
		return base
	}
	return base + "-" + itoa(count)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
