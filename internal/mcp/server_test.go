package mcp

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/SarthakShrivastav-a/pluckr/internal/registry"
	"github.com/SarthakShrivastav-a/pluckr/internal/store"
)

func TestBuildServer_ToolsReadOnly(t *testing.T) {
	cache, _ := store.Open(t.TempDir())
	reg, _ := registry.Load(filepath.Join(t.TempDir(), "registry.json"))
	srv := buildServer(cache, reg, false)

	got := toolNames(srv.ListTools())
	want := []string{"get_outline", "get_page", "list_sources", "search_docs"}
	sort.Strings(got)
	sort.Strings(want)
	if !equalStringSlices(got, want) {
		t.Errorf("read-only tools = %v, want %v", got, want)
	}
}

func TestBuildServer_ToolsWithMutations(t *testing.T) {
	cache, _ := store.Open(t.TempDir())
	reg, _ := registry.Load(filepath.Join(t.TempDir(), "registry.json"))
	srv := buildServer(cache, reg, true)

	got := toolNames(srv.ListTools())
	want := []string{
		"add_source", "get_outline", "get_page", "list_sources",
		"refresh_source", "remove_source", "search_docs",
	}
	sort.Strings(got)
	sort.Strings(want)
	if !equalStringSlices(got, want) {
		t.Errorf("full tools = %v, want %v", got, want)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "a", "b"); got != "a" {
		t.Errorf("got %q, want a", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// toolNames extracts the keys of the tools map without importing the
// concrete *server.ServerTool type directly. Reflection avoids the
// transitive import surface in tests.
func toolNames(tools any) []string {
	v := reflect.ValueOf(tools)
	if v.Kind() != reflect.Map {
		return nil
	}
	out := make([]string, 0, v.Len())
	for _, k := range v.MapKeys() {
		out = append(out, k.String())
	}
	return out
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
