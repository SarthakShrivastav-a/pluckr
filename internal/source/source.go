// Package source defines the abstraction over an ingestible thing - a
// public docs site, an llms.txt endpoint, a GitHub repo, or a local
// folder - and the helper utilities that every implementation needs.
//
// A Source is the producer side of the pipeline: it discovers Pages
// (URL or repo-relative path plus raw bytes plus content type) and
// streams them onto a channel. The pipeline consumer renders, chunks,
// and indexes each Page.
//
// Sources that need to fetch over HTTP receive a fetch.Fetcher via
// their constructor; sources that read from disk ignore it. Auth
// headers and cookies are resolved (env-vars expanded) per fetch by the
// helpers in this package so secrets never enter the registry file.
package source

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

// Source is the operational interface every implementation satisfies.
type Source interface {
	// Kind returns one of the constants in package registry.
	Kind() string
	// Name is the identifier the user (and the registry) uses to refer
	// to this source.
	Name() string
	// Pull discovers and emits Pages onto out. It returns when the
	// source is exhausted, an error has occurred, or ctx is cancelled.
	// Implementations MUST NOT close out - the caller does that.
	Pull(ctx context.Context, out chan<- types.Page) error
}

// PullResult bundles a Source's outcome, useful for callers that want a
// summary independent of the channel transport.
type PullResult struct {
	PagesEmitted int
	Errors       []error
}

// ResolveHeaders expands environment variables of the form ${NAME} in
// header values. Lookup is the function used to read env vars; pass
// os.Getenv in production code and a fake in tests.
func ResolveHeaders(headers map[string]string, lookup func(string) string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	if lookup == nil {
		lookup = os.Getenv
	}
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		out[k] = expandEnv(v, lookup)
	}
	return out
}

// ResolveCookies behaves like ResolveHeaders for cookie values.
func ResolveCookies(cookies map[string]string, lookup func(string) string) map[string]string {
	return ResolveHeaders(cookies, lookup)
}

// expandEnv replaces ${NAME} segments with the looked-up value. Unknown
// names resolve to the empty string, matching shell expansion. $$ is
// reserved for an escaped literal $ but is not commonly needed in
// header values, so it falls through unchanged.
func expandEnv(s string, lookup func(string) string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); {
		if i+1 < len(s) && s[i] == '$' && s[i+1] == '{' {
			end := strings.Index(s[i+2:], "}")
			if end >= 0 {
				name := s[i+2 : i+2+end]
				sb.WriteString(lookup(name))
				i = i + 3 + end
				continue
			}
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String()
}

// ErrNotImplemented is returned by source kinds that are accepted by
// the registry but not yet built into the binary.
var ErrNotImplemented = errors.New("source: kind not yet implemented")
