package render

import (
	"strings"
	"unicode"

	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

// extractOutline walks the markdown looking for ATX headings (#, ##, ...)
// and produces an ordered slice of types.Heading. Anchors follow the
// GitHub-flavored slug convention so that links from the rendered docs
// site usually still resolve to chunk anchors.
func extractOutline(md string) []types.Heading {
	var out []types.Heading
	used := map[string]int{}
	offset := 0
	inFence := false
	for _, line := range strings.Split(md, "\n") {
		lineLen := len(line) + 1 // include trailing newline

		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(line, "#") {
			level := 0
			for level < len(line) && line[level] == '#' && level < 6 {
				level++
			}
			if level >= 1 && level <= 6 && level < len(line) && line[level] == ' ' {
				text := strings.TrimSpace(line[level+1:])
				text = strings.TrimRight(text, "#")
				text = strings.TrimSpace(text)
				if text != "" {
					anchor := uniqueSlug(text, used)
					out = append(out, types.Heading{
						Level:  level,
						Text:   text,
						Anchor: anchor,
						Offset: offset,
					})
				}
			}
		}
		offset += lineLen
	}
	return out
}

// slugify replicates the common GitHub-flavored slug algorithm: lowercase,
// alphanumerics and dashes pass through, spaces become dashes, everything
// else is dropped, leading/trailing dashes are trimmed.
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
		default:
			// drop punctuation
		}
	}
	out := strings.Trim(b.String(), "-")
	return out
}

// uniqueSlug ensures collisions get suffixes (-1, -2, ...) per the
// GitHub-flavored convention.
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
	negative := i < 0
	if negative {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
