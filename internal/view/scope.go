// Package view implements the read-only broker for SPA data access.
// Every page rendered in the browser flows through the view broker:
// the client posts a view.Open request, the server resolves the page's
// static dependency graph, and returns the bundle of data keys + files
// the page needs. Subsequent live updates arrive on a scoped SSE stream.
//
// The broker is the single read-path chokepoint — /api/data/*,
// /api/files/* and /api/content/* still exist for agents and CLI but
// the SPA never calls them. Share visitors literally can't: their
// session cookie is rejected everywhere except /api/view/*.
package view

import (
	"net/http"
	"strings"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/publicroutes"
	"github.com/christophermarx/agentboard/internal/store"
)

// AuthorityKind is how a view session got authorised. It determines
// what the session can read WITHIN the page's ref set. The ref set
// itself is always derived from the AST — that's the upper bound,
// regardless of authority.
type AuthorityKind int

const (
	// AuthorityAnonymous means no user, no share token. Only reachable
	// when the requested path is in public.paths. Reads are restricted
	// to keys that are ALSO in public.paths.
	AuthorityAnonymous AuthorityKind = iota
	// AuthorityAdmin reads anything in the ref set.
	AuthorityAdmin
	// AuthorityAgent reads keys in the ref set that pass the per-user
	// access rules.
	AuthorityAgent
	// AuthorityShare reads anything in the ref set. Scope is the set;
	// inside it everything is fair game because the operator authored
	// the page with that in mind.
	AuthorityShare
)

// Scope is the per-request answer to "what can this caller read?". It
// combines a reference-set (the page's static dep graph, transitive
// over the subtree for shares/public) with an authority that decides
// what subset of that set is actually reachable.
type Scope struct {
	// Path is the canonical page path this scope is computed for.
	// Shares / public mode get a full subtree anchored here.
	Path string

	// Authority is how this caller got through the door.
	Authority AuthorityKind

	// User is non-nil for Admin and Agent; nil for Share and Anonymous.
	User *auth.User

	// DataKeys / Files / Subpages are the upper bound — the set of
	// resources this view can *potentially* see. Whether any specific
	// read succeeds also depends on Authority.
	DataKeys map[string]bool
	Files    map[string]bool
	Subpages map[string]bool

	// publicMatcher is consulted for AuthorityAnonymous reads. nil
	// means "nothing is public" (default for zero-config boards).
	publicMatcher *publicroutes.Matcher
}

// CanReadData reports whether this scope's caller may read a data key.
func (s *Scope) CanReadData(key string) bool {
	if !s.DataKeys[key] {
		return false
	}
	switch s.Authority {
	case AuthorityAdmin, AuthorityShare:
		return true
	case AuthorityAgent:
		if s.User == nil {
			return false
		}
		return auth.Authorize(s.User.AccessMode, s.User.Rules, http.MethodGet, "/api/"+key)
	case AuthorityAnonymous:
		if s.publicMatcher == nil {
			return false
		}
		return s.publicMatcher.IsPubliclyReadable(http.MethodGet, "/api/"+key)
	}
	return false
}

// CanReadFile reports whether this scope's caller may read a file path.
// The path is the `/api/files/<name>` form stored in page_refs.
func (s *Scope) CanReadFile(path string) bool {
	if !s.Files[path] {
		return false
	}
	switch s.Authority {
	case AuthorityAdmin, AuthorityShare:
		return true
	case AuthorityAgent:
		if s.User == nil {
			return false
		}
		return auth.Authorize(s.User.AccessMode, s.User.Rules, http.MethodGet, path)
	case AuthorityAnonymous:
		if s.publicMatcher == nil {
			return false
		}
		return s.publicMatcher.IsPubliclyReadable(http.MethodGet, path)
	}
	return false
}

// CanReadSubpage reports whether this scope's caller may traverse to a
// subpage. This gates navigation — the SPA uses it to decide which
// sidebar entries to render, and the view/open handler refuses to
// bundle a page whose path isn't reachable.
func (s *Scope) CanReadSubpage(path string) bool {
	// Normalise the path so `handbook` and `/handbook` both work.
	n := "/" + strings.TrimPrefix(path, "/")
	rootFull := "/" + strings.TrimPrefix(s.Path, "/")
	if n == rootFull || strings.HasPrefix(n, rootFull+"/") {
		// Implicit yes for the subtree — share/public visitors walk
		// their granted subtree. For Admin/Agent the subtree rule is
		// augmented by the per-user rule evaluation below.
		switch s.Authority {
		case AuthorityShare, AuthorityAnonymous:
			return true
		}
	}
	switch s.Authority {
	case AuthorityAdmin:
		return true
	case AuthorityAgent:
		if s.User == nil {
			return false
		}
		return auth.Authorize(s.User.AccessMode, s.User.Rules, http.MethodGet, "/api"+n)
	}
	return false
}

// ScopeBuilder assembles scopes for incoming requests. Injected into
// the broker and view middleware so tests can stub dependencies.
type ScopeBuilder struct {
	Refs          *store.RefStore
	PublicMatcher *publicroutes.Matcher
}

// Build computes the scope for a given page path + authority pair.
// The caller is responsible for having already authenticated; Build
// just synthesises the reachable resource sets.
func (b *ScopeBuilder) Build(path string, authority AuthorityKind, user *auth.User) (*Scope, error) {
	norm := normalisePath(path)
	s := &Scope{
		Path:          norm,
		Authority:     authority,
		User:          user,
		DataKeys:      map[string]bool{},
		Files:         map[string]bool{},
		Subpages:      map[string]bool{},
		publicMatcher: b.PublicMatcher,
	}
	// Share and public visitors always see the subtree under their
	// anchored path. Admin/Agent can technically see anything — we
	// still pull the subtree so SSE filtering has a concrete set to
	// work against (cheap upper bound).
	if b.Refs != nil {
		refs, pages, err := b.Refs.GetForSubtree(norm)
		if err != nil {
			return nil, err
		}
		for _, k := range refs.Data {
			s.DataKeys[k] = true
		}
		for _, f := range refs.Files {
			s.Files[f] = true
		}
		for _, p := range pages {
			s.Subpages[p] = true
		}
	}
	// Ensure the root page itself is in Subpages even when no refs
	// were recorded for it (e.g. a skeleton page with only prose).
	s.Subpages[norm] = true
	return s, nil
}

// normalisePath mirrors the PageManager's key shape: no leading slash,
// no ".md" suffix, "" → "index".
func normalisePath(p string) string {
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, ".md")
	if p == "" {
		p = "index"
	}
	return p
}
