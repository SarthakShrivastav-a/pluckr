// Package github implements the github Source kind: ingest the
// markdown files from a public GitHub repository via the public API
// (tree listing) and raw.githubusercontent.com (file contents).
//
// Spec grammar:
//
//	owner/repo                   - default branch, every .md / .mdx
//	owner/repo@branch            - explicit branch / tag / SHA
//	owner/repo/docs              - default branch, files under /docs
//	owner/repo@v1.2/docs/api     - explicit branch + subdir
//
// A GITHUB_TOKEN environment variable raises the rate limit from 60 to
// 5000 requests per hour and lets users target their private repos.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/SarthakShrivastav-a/pluckr/internal/fetch"
	"github.com/SarthakShrivastav-a/pluckr/internal/registry"
	"github.com/SarthakShrivastav-a/pluckr/internal/source"
	"github.com/SarthakShrivastav-a/pluckr/internal/types"
)

// SupportedExts lists the extensions github source treats as docs.
var SupportedExts = map[string]struct{}{
	".md":       {},
	".markdown": {},
	".mdx":      {},
}

// Spec captures a parsed GitHub source spec.
type Spec struct {
	Owner  string
	Repo   string
	Ref    string // branch / tag / SHA; empty means "default branch"
	Subdir string // optional path prefix, no leading or trailing slash
}

// ParseSpec parses a github spec into its components.
func ParseSpec(raw string) (Spec, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Spec{}, errors.New("github: empty spec")
	}
	raw = strings.TrimPrefix(raw, "github.com/")

	var ref string
	if i := strings.Index(raw, "@"); i >= 0 {
		// Find the ref segment, which ends at the next '/' (subdir) or
		// end of string.
		rest := raw[i+1:]
		if slash := strings.Index(rest, "/"); slash >= 0 {
			ref = rest[:slash]
			raw = raw[:i] + "/" + rest[slash+1:]
		} else {
			ref = rest
			raw = raw[:i]
		}
	}

	parts := strings.SplitN(raw, "/", 3)
	if len(parts) < 2 {
		return Spec{}, fmt.Errorf("github: spec %q must be owner/repo[/subdir][@ref]", raw)
	}
	s := Spec{Owner: parts[0], Repo: parts[1], Ref: ref}
	if len(parts) == 3 {
		s.Subdir = strings.Trim(parts[2], "/")
	}
	if s.Owner == "" || s.Repo == "" {
		return Spec{}, fmt.Errorf("github: spec %q missing owner or repo", raw)
	}
	return s, nil
}

// String formats the spec back into the canonical string form.
func (s Spec) String() string {
	out := s.Owner + "/" + s.Repo
	if s.Ref != "" {
		out += "@" + s.Ref
	}
	if s.Subdir != "" {
		out += "/" + s.Subdir
	}
	return out
}

// GitHub is the Source impl.
type GitHub struct {
	SourceName  string
	Spec        string
	Fetcher     fetch.Fetcher
	Headers     map[string]string
	Cookies     map[string]string
	MaxPages    int
	Concurrency int
	Lookup      func(string) string

	// Token is consulted before falling back to GITHUB_TOKEN env var.
	Token string
}

// Kind returns registry.KindGitHub.
func (g *GitHub) Kind() string { return registry.KindGitHub }

// Name returns the configured source name.
func (g *GitHub) Name() string { return g.SourceName }

// Pull lists the repo's tree, filters to docs files, and emits a Page
// per file.
func (g *GitHub) Pull(ctx context.Context, out chan<- types.Page) error {
	if g.Fetcher == nil {
		g.Fetcher = fetch.NewHTTP()
	}
	if g.MaxPages <= 0 {
		g.MaxPages = 1000
	}
	spec, err := ParseSpec(g.Spec)
	if err != nil {
		return err
	}

	headers := source.ResolveHeaders(g.Headers, g.Lookup)
	if headers == nil {
		headers = map[string]string{}
	}
	if token := g.resolveToken(); token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	headers["Accept"] = "application/vnd.github+json"
	cookies := source.ResolveCookies(g.Cookies, g.Lookup)

	ref := spec.Ref
	if ref == "" {
		ref, err = g.defaultBranch(ctx, spec, headers, cookies)
		if err != nil {
			return err
		}
	}

	tree, err := g.fetchTree(ctx, spec, ref, headers, cookies)
	if err != nil {
		return err
	}

	var paths []string
	for _, n := range tree.Tree {
		if n.Type != "blob" {
			continue
		}
		if !inSubdir(n.Path, spec.Subdir) {
			continue
		}
		ext := strings.ToLower(extOf(n.Path))
		if _, ok := SupportedExts[ext]; !ok {
			continue
		}
		paths = append(paths, n.Path)
		if len(paths) >= g.MaxPages {
			break
		}
	}
	if len(paths) == 0 {
		return fmt.Errorf("github: no markdown files under %s", spec)
	}

	concurrency := g.Concurrency
	if concurrency <= 0 {
		concurrency = runtime.NumCPU() * 2
		if concurrency < 8 {
			concurrency = 8
		}
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	errCh := make(chan error, len(paths))

	for _, p := range paths {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		default:
		}
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s",
				url.PathEscape(spec.Owner), url.PathEscape(spec.Repo),
				url.PathEscape(ref), p,
			)
			r, err := g.Fetcher.Fetch(ctx, fetch.Request{
				URL:    rawURL,
				Accept: "text/markdown, text/plain, */*",
			})
			if err != nil {
				errCh <- err
				return
			}
			if !r.IsSuccess() {
				errCh <- fmt.Errorf("github: HTTP %d for %s", r.Status, rawURL)
				return
			}
			pagePath := strings.TrimSuffix(p, extOf(p))
			page := types.Page{
				URL:         "https://github.com/" + spec.Owner + "/" + spec.Repo + "/blob/" + ref + "/" + p,
				ContentType: "text/markdown; charset=utf-8",
				Body:        r.Body,
				FetchedAt:   r.FetchedAt,
				Path:        pagePath,
			}
			select {
			case out <- page:
			case <-ctx.Done():
			}
		}()
	}
	wg.Wait()
	close(errCh)

	count := 0
	var firstErr error
	for e := range errCh {
		count++
		if firstErr == nil {
			firstErr = e
		}
	}
	if count > 0 && count == len(paths) {
		return fmt.Errorf("github: every file failed (first: %w)", firstErr)
	}
	return nil
}

func (g *GitHub) resolveToken() string {
	if g.Token != "" {
		return g.Token
	}
	if g.Lookup != nil {
		if t := g.Lookup("GITHUB_TOKEN"); t != "" {
			return t
		}
		return g.Lookup("PLUCKR_GITHUB_TOKEN")
	}
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	return os.Getenv("PLUCKR_GITHUB_TOKEN")
}

type repoInfo struct {
	DefaultBranch string `json:"default_branch"`
}

func (g *GitHub) defaultBranch(ctx context.Context, spec Spec, headers, cookies map[string]string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", spec.Owner, spec.Repo)
	resp, err := g.Fetcher.Fetch(ctx, fetch.Request{URL: url, Headers: headers, Cookies: cookies})
	if err != nil {
		return "", fmt.Errorf("github: fetch repo info: %w", err)
	}
	if !resp.IsSuccess() {
		return "", fmt.Errorf("github: repo info HTTP %d for %s/%s", resp.Status, spec.Owner, spec.Repo)
	}
	var info repoInfo
	if err := json.Unmarshal(resp.Body, &info); err != nil {
		return "", fmt.Errorf("github: parse repo info: %w", err)
	}
	if info.DefaultBranch == "" {
		return "main", nil
	}
	return info.DefaultBranch, nil
}

type treeResponse struct {
	Tree     []treeNode `json:"tree"`
	Truncated bool      `json:"truncated"`
}

type treeNode struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int    `json:"size"`
}

func (g *GitHub) fetchTree(ctx context.Context, spec Spec, ref string, headers, cookies map[string]string) (treeResponse, error) {
	u := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1", spec.Owner, spec.Repo, ref)
	resp, err := g.Fetcher.Fetch(ctx, fetch.Request{URL: u, Headers: headers, Cookies: cookies})
	if err != nil {
		return treeResponse{}, fmt.Errorf("github: fetch tree: %w", err)
	}
	if !resp.IsSuccess() {
		return treeResponse{}, fmt.Errorf("github: tree HTTP %d for %s@%s", resp.Status, spec, ref)
	}
	var tr treeResponse
	if err := json.Unmarshal(resp.Body, &tr); err != nil {
		return treeResponse{}, fmt.Errorf("github: parse tree: %w", err)
	}
	return tr, nil
}

func inSubdir(p, subdir string) bool {
	if subdir == "" {
		return true
	}
	return strings.HasPrefix(p, subdir+"/") || p == subdir
}

func extOf(p string) string {
	i := strings.LastIndex(p, ".")
	if i < 0 {
		return ""
	}
	return strings.ToLower(p[i:])
}
