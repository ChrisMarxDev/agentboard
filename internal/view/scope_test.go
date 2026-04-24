package view

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/mdx"
	"github.com/christophermarx/agentboard/internal/publicroutes"
)

func openScopeDB(t *testing.T) (*mdx.RefStore, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	rs, err := mdx.NewRefStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return rs, db
}

func seedScope(t *testing.T) *ScopeBuilder {
	t.Helper()
	rs, _ := openScopeDB(t)
	_ = rs.Record("handbook", mdx.RefSet{Data: []string{"hb.main"}, Files: []string{"/api/files/banner.svg"}})
	_ = rs.Record("handbook/faq", mdx.RefSet{Data: []string{"hb.faq"}})
	_ = rs.Record("other", mdx.RefSet{Data: []string{"other.secret"}})
	return &ScopeBuilder{Refs: rs}
}

func TestScope_ShareCoversSubtree(t *testing.T) {
	b := seedScope(t)
	s, err := b.Build("handbook", AuthorityShare, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !s.CanReadData("hb.main") || !s.CanReadData("hb.faq") {
		t.Errorf("share should cover subtree data keys")
	}
	if s.CanReadData("other.secret") {
		t.Errorf("share leaked sibling subtree key")
	}
	if !s.CanReadFile("/api/files/banner.svg") {
		t.Errorf("share should cover subtree file refs")
	}
	if !s.CanReadSubpage("handbook/faq") || !s.CanReadSubpage("handbook") {
		t.Errorf("share should cover subpages under its root")
	}
	if s.CanReadSubpage("other") {
		t.Errorf("share leaked sibling subpage")
	}
}

func TestScope_AnonymousNeedsPublicMatcher(t *testing.T) {
	b := seedScope(t)

	// No matcher → anonymous sees nothing.
	sNoMatcher, _ := b.Build("handbook", AuthorityAnonymous, nil)
	if sNoMatcher.CanReadData("hb.main") {
		t.Errorf("anonymous without matcher should read nothing")
	}

	// Matcher allows data/hb.main → anonymous can read that key only.
	b.PublicMatcher = publicroutes.New([]string{"/api/data/hb.main"})
	sWithMatcher, _ := b.Build("handbook", AuthorityAnonymous, nil)
	if !sWithMatcher.CanReadData("hb.main") {
		t.Errorf("anonymous with matching public rule should read hb.main")
	}
	if sWithMatcher.CanReadData("hb.faq") {
		t.Errorf("anonymous should NOT read a key that isn't in public.paths")
	}
}

func TestScope_AdminReadsAnythingInRefSet(t *testing.T) {
	b := seedScope(t)
	admin := &auth.User{Username: "root", Kind: auth.KindAdmin}
	s, _ := b.Build("handbook", AuthorityAdmin, admin)
	if !s.CanReadData("hb.main") || !s.CanReadData("hb.faq") {
		t.Errorf("admin should read all subtree keys")
	}
	// Out-of-scope (not in any subtree page_refs) is still blocked —
	// upper bound is the ref set.
	if s.CanReadData("other.secret") {
		t.Errorf("admin shouldn't see keys outside the subtree's ref set")
	}
}

func TestScope_AgentRespectsRules(t *testing.T) {
	b := seedScope(t)
	agent := &auth.User{
		Username:   "alice",
		Kind:       auth.KindAgent,
		AccessMode: auth.ModeRestrictToList,
		Rules: []auth.Rule{
			auth.Allow("/api/data/hb.main", "GET"),
		},
	}
	s, _ := b.Build("handbook", AuthorityAgent, agent)
	if !s.CanReadData("hb.main") {
		t.Errorf("agent with matching allow rule should read hb.main")
	}
	if s.CanReadData("hb.faq") {
		t.Errorf("agent without matching rule should NOT read hb.faq")
	}
}

func TestScope_SubpageNormalisation(t *testing.T) {
	b := seedScope(t)
	s, _ := b.Build("handbook", AuthorityShare, nil)
	if !s.CanReadSubpage("/handbook/faq") {
		t.Errorf("leading slash should not break subpage check")
	}
}
