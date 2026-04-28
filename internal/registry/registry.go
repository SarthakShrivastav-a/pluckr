// Package registry owns the user-managed list of subscribed sources.
// The on-disk file is registry.json under the cache root and is the
// source of truth for "what sources exist". The store and retriever are
// derived from this list; deleting a registry entry implies deleting the
// matching cache directory and FTS5 rows.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Kind is one of the supported source kinds.
const (
	KindWebsite  = "website"
	KindLLMSTxt  = "llms_txt"
	KindGitHub   = "github"
	KindLocal    = "local"
)

// Entry is one subscribed source.
type Entry struct {
	Name         string            `json:"name"`
	Kind         string            `json:"kind"`
	Root         string            `json:"root"`
	Refresh      string            `json:"refresh,omitempty"` // "7d" | "manual" | "never"
	MaxPages     int               `json:"max_pages,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Cookies      map[string]string `json:"cookies,omitempty"`
	AddedAt      time.Time         `json:"added_at"`
	LastSyncedAt time.Time         `json:"last_synced_at,omitempty"`
}

// Validate returns a non-nil error if e is missing fields the registry
// considers required.
func (e Entry) Validate() error {
	if strings.TrimSpace(e.Name) == "" {
		return errors.New("registry: entry name is required")
	}
	if e.Kind == "" {
		return errors.New("registry: entry kind is required")
	}
	if e.Root == "" {
		return errors.New("registry: entry root is required")
	}
	switch e.Kind {
	case KindWebsite, KindLLMSTxt, KindGitHub, KindLocal:
	default:
		return fmt.Errorf("registry: unknown kind %q", e.Kind)
	}
	return nil
}

// Registry is loaded from / saved to disk. Use methods, not direct field
// access, for thread safety.
type Registry struct {
	path    string
	mu      sync.Mutex
	entries []Entry
}

// fileShape is the JSON wire format on disk.
type fileShape struct {
	Sources []Entry `json:"sources"`
}

// Load opens the registry at path. A missing file yields an empty
// registry, not an error.
func Load(path string) (*Registry, error) {
	r := &Registry{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, fmt.Errorf("registry: read %s: %w", path, err)
	}
	var shape fileShape
	if err := json.Unmarshal(data, &shape); err != nil {
		return nil, fmt.Errorf("registry: parse %s: %w", path, err)
	}
	r.entries = shape.Sources
	r.sortLocked()
	return r, nil
}

// Save writes the registry to disk atomically.
func (r *Registry) Save() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saveLocked()
}

func (r *Registry) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return fmt.Errorf("registry: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(fileShape{Sources: r.entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("registry: marshal: %w", err)
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("registry: write tmp: %w", err)
	}
	if err := os.Rename(tmp, r.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("registry: rename: %w", err)
	}
	return nil
}

// List returns a copy of the entries.
func (r *Registry) List() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Entry, len(r.entries))
	copy(out, r.entries)
	return out
}

// Get returns the entry with the given name, or false if absent.
func (r *Registry) Get(name string) (Entry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

// Add inserts a new entry. Returns an error if an entry with the same
// name already exists.
func (r *Registry) Add(e Entry) error {
	if err := e.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.entries {
		if existing.Name == e.Name {
			return fmt.Errorf("registry: source %q already exists", e.Name)
		}
	}
	if e.AddedAt.IsZero() {
		e.AddedAt = time.Now().UTC()
	}
	r.entries = append(r.entries, e)
	r.sortLocked()
	return r.saveLocked()
}

// Update mutates the entry with the given name in place. The mutator
// receives a copy and returns the new value; the registry persists the
// result on success. Returns ErrNotFound when no such entry exists.
func (r *Registry) Update(name string, fn func(Entry) Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, e := range r.entries {
		if e.Name == name {
			r.entries[i] = fn(e)
			return r.saveLocked()
		}
	}
	return ErrNotFound
}

// Remove deletes the entry with the given name.
func (r *Registry) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, e := range r.entries {
		if e.Name == name {
			r.entries = append(r.entries[:i], r.entries[i+1:]...)
			return r.saveLocked()
		}
	}
	return ErrNotFound
}

// ErrNotFound is returned by Update and Remove when the named entry is
// absent.
var ErrNotFound = errors.New("registry: source not found")

func (r *Registry) sortLocked() {
	sort.Slice(r.entries, func(i, j int) bool {
		return r.entries[i].Name < r.entries[j].Name
	})
}
