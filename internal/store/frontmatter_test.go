package store

import "testing"

func TestSplitFrontmatter_HappyPath(t *testing.T) {
	body := []byte("---\ntitle: \"Dark mode - Tailwind CSS\"\nurl: \"https://tailwindcss.com/docs/dark-mode\"\nsource: \"tailwindcss.com\"\nfetched_at: \"2026-04-28T14:58:25Z\"\ncontent_hash: \"abc123\"\n---\n\n## Overview\n\nbody.")
	md, fm := SplitFrontmatter(body)
	if string(md) != "## Overview\n\nbody." {
		t.Errorf("md = %q", md)
	}
	wants := map[string]string{
		"title":        "Dark mode - Tailwind CSS",
		"url":          "https://tailwindcss.com/docs/dark-mode",
		"source":       "tailwindcss.com",
		"fetched_at":   "2026-04-28T14:58:25Z",
		"content_hash": "abc123",
	}
	for k, v := range wants {
		if fm[k] != v {
			t.Errorf("fm[%q] = %q, want %q", k, fm[k], v)
		}
	}
}

func TestSplitFrontmatter_NoFrontmatter(t *testing.T) {
	body := []byte("# Hello\n\nbody.")
	md, fm := SplitFrontmatter(body)
	if string(md) != string(body) {
		t.Errorf("md should be unchanged")
	}
	if len(fm) != 0 {
		t.Errorf("fm should be empty, got %v", fm)
	}
	if fm == nil {
		t.Errorf("fm must not be nil even when empty")
	}
}

func TestSplitFrontmatter_UnterminatedFrontmatter(t *testing.T) {
	body := []byte("---\ntitle: x\nbody never ends with closing dashes")
	md, fm := SplitFrontmatter(body)
	if string(md) != string(body) {
		t.Errorf("body should be returned untouched if frontmatter is unterminated")
	}
	if len(fm) != 0 {
		t.Errorf("fm should be empty for unterminated input")
	}
}

func TestSplitFrontmatter_IgnoresCommentsAndBlanks(t *testing.T) {
	body := []byte("---\n# this is a comment\n\ntitle: \"x\"\n---\n\nbody")
	_, fm := SplitFrontmatter(body)
	if fm["title"] != "x" {
		t.Errorf("title = %q", fm["title"])
	}
}
