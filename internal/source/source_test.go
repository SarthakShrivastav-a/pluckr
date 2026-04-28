package source

import "testing"

func TestExpandEnv(t *testing.T) {
	lookup := func(name string) string {
		switch name {
		case "TOKEN":
			return "abc123"
		case "EMPTY":
			return ""
		}
		return ""
	}
	cases := []struct{ in, want string }{
		{"Bearer ${TOKEN}", "Bearer abc123"},
		{"plain text", "plain text"},
		{"${TOKEN}/${TOKEN}", "abc123/abc123"},
		{"${MISSING}", ""},
		{"hello ${EMPTY} world", "hello  world"},
		{"unterminated ${TOKEN", "unterminated ${TOKEN"},
	}
	for _, c := range cases {
		if got := expandEnv(c.in, lookup); got != c.want {
			t.Errorf("expandEnv(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolveHeaders(t *testing.T) {
	in := map[string]string{
		"Authorization": "Bearer ${TOKEN}",
		"X-Custom":      "static",
	}
	out := ResolveHeaders(in, func(string) string { return "secret" })
	if out["Authorization"] != "Bearer secret" {
		t.Errorf("auth = %q", out["Authorization"])
	}
	if out["X-Custom"] != "static" {
		t.Errorf("custom = %q", out["X-Custom"])
	}
	if &out == &in {
		t.Errorf("ResolveHeaders should return a new map")
	}
}

func TestResolveHeaders_NilLookupFallsBackToOSGetenv(t *testing.T) {
	in := map[string]string{"X": "value"}
	out := ResolveHeaders(in, nil)
	if out["X"] != "value" {
		t.Errorf("unexpected = %q", out["X"])
	}
}

func TestResolveHeaders_NilInputReturnsNil(t *testing.T) {
	if got := ResolveHeaders(nil, nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}
