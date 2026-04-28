package registry

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestRegistry_AddListGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	r, err := Load(path)
	if err != nil {
		t.Fatalf("Load empty: %v", err)
	}

	if err := r.Add(Entry{Name: "react.dev", Kind: KindWebsite, Root: "https://react.dev/reference", Refresh: "7d"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.Add(Entry{Name: "tailwind", Kind: KindWebsite, Root: "https://tailwindcss.com"}); err != nil {
		t.Fatalf("Add second: %v", err)
	}

	list := r.List()
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}
	if list[0].Name != "react.dev" || list[1].Name != "tailwind" {
		t.Errorf("List = %v, want sorted [react.dev tailwind]", list)
	}

	got, ok := r.Get("react.dev")
	if !ok {
		t.Fatal("Get react.dev not found")
	}
	if got.Refresh != "7d" {
		t.Errorf("Refresh = %q, want 7d", got.Refresh)
	}
	if got.AddedAt.IsZero() {
		t.Errorf("AddedAt should be set")
	}
}

func TestRegistry_AddRejectsDuplicates(t *testing.T) {
	r, _ := Load(filepath.Join(t.TempDir(), "registry.json"))
	if err := r.Add(Entry{Name: "x", Kind: KindWebsite, Root: "u"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	err := r.Add(Entry{Name: "x", Kind: KindWebsite, Root: "u"})
	if err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestRegistry_Persistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	r1, _ := Load(path)
	if err := r1.Add(Entry{Name: "react.dev", Kind: KindWebsite, Root: "https://react.dev"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	r2, err := Load(path)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if got, ok := r2.Get("react.dev"); !ok || got.Root != "https://react.dev" {
		t.Errorf("expected react.dev to survive reload, got %+v ok=%v", got, ok)
	}
}

func TestRegistry_Update(t *testing.T) {
	r, _ := Load(filepath.Join(t.TempDir(), "registry.json"))
	if err := r.Add(Entry{Name: "react.dev", Kind: KindWebsite, Root: "u"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if err := r.Update("react.dev", func(e Entry) Entry {
		e.LastSyncedAt = now
		return e
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := r.Get("react.dev")
	if !got.LastSyncedAt.Equal(now) {
		t.Errorf("LastSyncedAt = %v, want %v", got.LastSyncedAt, now)
	}

	if err := r.Update("missing", func(e Entry) Entry { return e }); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update missing = %v, want ErrNotFound", err)
	}
}

func TestRegistry_Remove(t *testing.T) {
	r, _ := Load(filepath.Join(t.TempDir(), "registry.json"))
	_ = r.Add(Entry{Name: "x", Kind: KindWebsite, Root: "u"})
	if err := r.Remove("x"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok := r.Get("x"); ok {
		t.Errorf("expected x to be gone")
	}
	if err := r.Remove("x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Remove missing = %v, want ErrNotFound", err)
	}
}

func TestEntry_Validate(t *testing.T) {
	cases := []struct {
		name  string
		entry Entry
		ok    bool
	}{
		{"valid", Entry{Name: "x", Kind: KindWebsite, Root: "u"}, true},
		{"missing name", Entry{Kind: KindWebsite, Root: "u"}, false},
		{"missing kind", Entry{Name: "x", Root: "u"}, false},
		{"missing root", Entry{Name: "x", Kind: KindWebsite}, false},
		{"unknown kind", Entry{Name: "x", Kind: "weird", Root: "u"}, false},
		{"local", Entry{Name: "x", Kind: KindLocal, Root: "/tmp"}, true},
		{"github", Entry{Name: "x", Kind: KindGitHub, Root: "owner/repo"}, true},
		{"llms_txt", Entry{Name: "x", Kind: KindLLMSTxt, Root: "https://x/llms.txt"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.entry.Validate()
			if (err == nil) != c.ok {
				t.Errorf("Validate err=%v, want ok=%v", err, c.ok)
			}
		})
	}
}
