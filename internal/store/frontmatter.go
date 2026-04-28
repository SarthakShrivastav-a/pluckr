package store

import "strings"

// SplitFrontmatter peels off a leading YAML block delimited by --- on its
// own line and returns the markdown body, plus the parsed key/value
// pairs. If body has no frontmatter, the body is returned unchanged and
// the map is empty (never nil).
//
// The parser is deliberately tiny - it only handles the flat string-only
// shape that store.WritePage writes (title / url / source / fetched_at /
// content_hash). Lists, nested maps, and multi-line values are not
// supported because we never produce them.
func SplitFrontmatter(body []byte) ([]byte, map[string]string) {
	s := string(body)
	if !strings.HasPrefix(s, "---\n") {
		return body, map[string]string{}
	}
	rest := s[4:]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return body, map[string]string{}
	}
	fm := parseFlatYAML(rest[:end])
	markdown := strings.TrimLeft(rest[end+5:], "\n")
	return []byte(markdown), fm
}

func parseFlatYAML(block string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		val := strings.TrimSpace(line[colon+1:])
		val = strings.Trim(val, `"`)
		if key != "" {
			out[key] = val
		}
	}
	return out
}
