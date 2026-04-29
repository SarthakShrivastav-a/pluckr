package fetch

import (
	"context"
	"errors"
	"testing"
)

func TestHeadlessFetcher_EmptyURLRejected(t *testing.T) {
	h := NewHeadless()
	t.Cleanup(func() { _ = h.Close() })

	_, err := h.Fetch(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
	if !errContains(err, "empty url") {
		t.Errorf("error = %v, want one mentioning 'empty url'", err)
	}
}

func TestHeadlessFetcher_DefaultsAreSensible(t *testing.T) {
	h := NewHeadless()
	if h.Timeout <= 0 {
		t.Errorf("Timeout default not set: %v", h.Timeout)
	}
	if h.StableWait <= 0 {
		t.Errorf("StableWait default not set: %v", h.StableWait)
	}
	if h.UserAgent == "" {
		t.Errorf("UserAgent default not set")
	}
	if h.MaxBody <= 0 {
		t.Errorf("MaxBody default not set: %d", h.MaxBody)
	}
}

func errContains(err error, substr string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for i := 0; i+len(substr) <= len(msg); i++ {
		if msg[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Ensure the type satisfies the Fetcher interface at compile time.
var _ Fetcher = (*HeadlessFetcher)(nil)

// Smoke-test the lazy ensure path: calling Close on a never-launched
// fetcher must not error or panic.
func TestHeadlessFetcher_CloseBeforeFetch(t *testing.T) {
	h := NewHeadless()
	if err := h.Close(); err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("Close before any Fetch should be a no-op, got %v", err)
	}
}
