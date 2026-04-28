package fts5

import (
	"context"
	"strings"
	"testing"

	"github.com/SarthakShrivastav-a/pluckr/internal/retriever"
	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sampleChunks() []types.Chunk {
	return []types.Chunk{
		{URL: "https://react.dev/learn/state", Path: "learn/state", Title: "React", HeadingPath: []string{"State", "useState"}, Anchor: "usestate", Body: "useState lets you add state to your function components.", TokenCount: 14, Order: 0},
		{URL: "https://react.dev/learn/effect", Path: "learn/effect", Title: "React", HeadingPath: []string{"Effects", "useEffect"}, Anchor: "useeffect", Body: "useEffect lets you synchronize a component with an external system.", TokenCount: 16, Order: 1},
		{URL: "https://tailwindcss.com/docs", Path: "docs/index", Title: "Tailwind", HeadingPath: []string{"Configuration"}, Anchor: "configuration", Body: "Tailwind generates utility classes from your config file.", TokenCount: 12, Order: 0},
	}
}

func TestStore_IndexAndSearch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.Index(ctx, "react.dev", sampleChunks()[:2]); err != nil {
		t.Fatalf("Index react: %v", err)
	}
	if err := s.Index(ctx, "tailwindcss.com", sampleChunks()[2:]); err != nil {
		t.Fatalf("Index tailwind: %v", err)
	}

	hits, err := s.Search(ctx, "useState", retriever.SearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least one hit")
	}
	if hits[0].Source != "react.dev" {
		t.Errorf("first hit source = %q, want react.dev", hits[0].Source)
	}
	if !strings.Contains(strings.ToLower(hits[0].Snippet), "usestate") {
		t.Errorf("snippet missing the matched term: %q", hits[0].Snippet)
	}
}

func TestStore_SearchFiltersBySource(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_ = s.Index(ctx, "react.dev", sampleChunks()[:2])
	_ = s.Index(ctx, "tailwindcss.com", sampleChunks()[2:])

	hits, err := s.Search(ctx, "config", retriever.SearchOptions{Sources: []string{"react.dev"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, h := range hits {
		if h.Source != "react.dev" {
			t.Errorf("expected only react.dev, got %q", h.Source)
		}
	}
}

func TestStore_Reindex(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_ = s.Index(ctx, "react.dev", sampleChunks()[:2])

	hits, _ := s.Search(ctx, "useState", retriever.SearchOptions{})
	if len(hits) == 0 {
		t.Fatal("expected initial hit before reindex")
	}

	// Reindex with a single different chunk.
	replacement := []types.Chunk{
		{URL: "https://react.dev/replaced", Path: "replaced", Title: "React", HeadingPath: []string{"Replaced"}, Body: "totally different content about widgets and gadgets.", TokenCount: 11},
	}
	if err := s.Reindex(ctx, "react.dev", replacement); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	hits, _ = s.Search(ctx, "useState", retriever.SearchOptions{})
	if len(hits) != 0 {
		t.Errorf("expected no hits for old content after reindex, got %d", len(hits))
	}
	hits, _ = s.Search(ctx, "widgets", retriever.SearchOptions{})
	if len(hits) == 0 {
		t.Errorf("expected hits for new content after reindex")
	}
}

func TestStore_Sources(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_ = s.Index(ctx, "react.dev", sampleChunks()[:1])
	_ = s.Index(ctx, "tailwindcss.com", sampleChunks()[2:])

	got, err := s.Sources(ctx)
	if err != nil {
		t.Fatalf("Sources: %v", err)
	}
	if len(got) != 2 || got[0] != "react.dev" || got[1] != "tailwindcss.com" {
		t.Errorf("Sources = %v, want [react.dev tailwindcss.com]", got)
	}
}

func TestStore_Remove(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_ = s.Index(ctx, "react.dev", sampleChunks()[:2])
	if err := s.Remove(ctx, "react.dev"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	got, _ := s.Sources(ctx)
	if len(got) != 0 {
		t.Errorf("expected no sources after remove, got %v", got)
	}
}

func TestSanitizeQuery(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello world", `"hello" "world"`},
		{"useState()", `"useState"`},
		{`how to "use"  hooks`, `"how" "to" "use" "hooks"`},
		{"", ""},
	}
	for _, c := range cases {
		if got := sanitizeQuery(c.in); got != c.want {
			t.Errorf("sanitizeQuery(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStore_EmptyQueryRejected(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Search(context.Background(), "  ", retriever.SearchOptions{}); err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestStore_HeadingPathRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_ = s.Index(ctx, "react.dev", sampleChunks()[:1])
	hits, _ := s.Search(ctx, "useState", retriever.SearchOptions{})
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	if len(hits[0].HeadingPath) != 2 || hits[0].HeadingPath[0] != "State" || hits[0].HeadingPath[1] != "useState" {
		t.Errorf("heading_path round trip = %v, want [State useState]", hits[0].HeadingPath)
	}
}
