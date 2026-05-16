package permissions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkfile(dir, rel string, body []byte) error {
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, body, 0o644)
}

func member(username string, groups ...string) Actor {
	return Actor{Username: username, Role: RoleMember, Groups: groups}
}

func admin(username string, groups ...string) Actor {
	return Actor{Username: username, Role: RoleAdmin, Groups: groups}
}

func TestEmptyRulesAreOpenByDefault(t *testing.T) {
	r, err := Parse(nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// A non-admin can write anywhere when there are no rules.
	if err := r.Allow(member("hanna"), []string{"marketing/q3.md", "engineering/auth.go", "README.md"}); err != nil {
		t.Errorf("empty rules: expected nil, got %v", err)
	}
	// Whitespace-only YAML is also permissive.
	r2, err := Parse([]byte("\n\n   \n"))
	if err != nil {
		t.Fatalf("parse ws: %v", err)
	}
	if err := r2.Allow(member("hanna"), []string{"marketing/q3.md"}); err != nil {
		t.Errorf("ws-only rules: expected nil, got %v", err)
	}
}

func TestRulesRestrictMatchingPaths(t *testing.T) {
	yamlIn := `version: 1
rules:
  - paths: ["finance/**"]
    write: [finance, admins]
`
	r, err := Parse([]byte(yamlIn))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Non-finance member denied.
	err = r.Allow(member("hanna", "marketing"), []string{"finance/q3.md"})
	if err == nil {
		t.Fatalf("expected deny, got nil")
	}
	de, ok := IsDenyError(err)
	if !ok || de.Reason != "rule" {
		t.Errorf("expected rule DenyError, got %v", err)
	}
	if len(de.Paths) != 1 || de.Paths[0] != "finance/q3.md" {
		t.Errorf("unexpected paths in deny: %+v", de.Paths)
	}

	// Finance member allowed.
	if err := r.Allow(member("lukas", "finance"), []string{"finance/q3.md"}); err != nil {
		t.Errorf("finance member: %v", err)
	}

	// Admin allowed via synthetic admins group.
	if err := r.Allow(admin("chris"), []string{"finance/q3.md"}); err != nil {
		t.Errorf("admin: %v", err)
	}

	// Path outside any rule: open default.
	if err := r.Allow(member("hanna", "marketing"), []string{"marketing/brief.md"}); err != nil {
		t.Errorf("unrestricted path: %v", err)
	}
}

func TestFirstMatchWins(t *testing.T) {
	yamlIn := `version: 1
rules:
  - paths: ["secrets/admin-only.txt"]
    write: [admins]
  - paths: ["secrets/**"]
    write: [security, admins]
`
	r, _ := Parse([]byte(yamlIn))

	// security member: first rule matches the specific path and rejects.
	err := r.Allow(member("alice", "security"), []string{"secrets/admin-only.txt"})
	if err == nil {
		t.Errorf("expected first-match deny for security on admin-only path")
	}
	// security member: second rule matches another path under secrets/.
	if err := r.Allow(member("alice", "security"), []string{"secrets/cert.pem"}); err != nil {
		t.Errorf("security on secrets/cert.pem: %v", err)
	}
}

func TestStructuralAdminOnlyPath(t *testing.T) {
	r, _ := Parse(nil)
	// Non-admin trying to touch .agentboard/anything → structural deny.
	err := r.Allow(member("hanna"), []string{".agentboard/permissions.yaml"})
	de, ok := IsDenyError(err)
	if !ok || de.Reason != "structural" {
		t.Errorf("expected structural deny, got %v", err)
	}
	// Admin OK on the same path.
	if err := r.Allow(admin("chris"), []string{".agentboard/permissions.yaml"}); err != nil {
		t.Errorf("admin on permissions.yaml: %v", err)
	}
	// Structural ban covers anything under .agentboard/, not just the file.
	err = r.Allow(member("hanna"), []string{".agentboard/secrets.json"})
	if _, ok := IsDenyError(err); !ok {
		t.Errorf("structural ban should cover any .agentboard/ path")
	}
}

func TestStructuralBeatsRulesEvenWhenRuleWouldAllow(t *testing.T) {
	// A rule that says "members can write everywhere" doesn't override
	// the structural admin-only ban on .agentboard/.
	yamlIn := `version: 1
rules:
  - paths: ["**"]
    write: [marketing, admins]
`
	r, _ := Parse([]byte(yamlIn))
	err := r.Allow(member("hanna", "marketing"), []string{".agentboard/permissions.yaml"})
	if _, ok := IsDenyError(err); !ok {
		t.Errorf("expected structural deny even with permissive rule, got %v", err)
	}
}

func TestMultiplePaths(t *testing.T) {
	yamlIn := `version: 1
rules:
  - paths: ["finance/**"]
    write: [admins]
`
	r, _ := Parse([]byte(yamlIn))
	// Mix of allowed + denied paths in one push.
	err := r.Allow(member("hanna"), []string{
		"marketing/brief.md",  // allowed (no rule)
		"finance/q3.md",       // denied
		"finance/q4.md",       // denied
		"engineering/auth.go", // allowed
	})
	de, ok := IsDenyError(err)
	if !ok {
		t.Fatalf("expected deny, got %v", err)
	}
	if len(de.Paths) != 2 {
		t.Errorf("expected 2 denied paths, got %+v", de.Paths)
	}
}

func TestEmptyChangedPaths(t *testing.T) {
	yamlIn := `version: 1
rules:
  - paths: ["**"]
    write: [admins]
`
	r, _ := Parse([]byte(yamlIn))
	if err := r.Allow(member("hanna"), nil); err != nil {
		t.Errorf("empty changed paths: %v", err)
	}
	if err := r.Allow(member("hanna"), []string{}); err != nil {
		t.Errorf("empty slice changed paths: %v", err)
	}
	if err := r.Allow(member("hanna"), []string{"", " "}); err != nil {
		t.Errorf("whitespace-only paths: %v", err)
	}
}

func TestGlobSyntax(t *testing.T) {
	yamlIn := `version: 1
rules:
  - paths: ["**/*.secret"]
    write: [admins]
`
	r, _ := Parse([]byte(yamlIn))
	for _, p := range []string{"x.secret", "a/b/c.secret", "deep/path/file.secret"} {
		err := r.Allow(member("hanna"), []string{p})
		if err == nil {
			t.Errorf("expected deny for %s", p)
		}
	}
	for _, p := range []string{"x.txt", "a/b/c.md"} {
		if err := r.Allow(member("hanna"), []string{p}); err != nil {
			t.Errorf("unexpected deny for %s: %v", p, err)
		}
	}
}

func TestPathNormalization(t *testing.T) {
	yamlIn := `version: 1
rules:
  - paths: ["/finance/**"]
    write: [admins]
`
	r, _ := Parse([]byte(yamlIn))
	// Leading slash should have been stripped during parse.
	if got := r.Rules[0].Paths[0]; got != "finance/**" {
		t.Errorf("path normalization: want finance/**, got %q", got)
	}
	// Allow() also strips leading slashes on changed paths.
	err := r.Allow(member("hanna"), []string{"/finance/q3.md"})
	if _, ok := IsDenyError(err); !ok {
		t.Errorf("expected deny for /finance/q3.md, got %v", err)
	}
}

func TestRoleAliasingForBots(t *testing.T) {
	// Bots whose attached user is admin: the auth layer is responsible
	// for resolving the bot's Role to admin before calling Allow. Here
	// we just verify that a bot Actor with Role=admin (not member) is
	// treated as admin.
	yamlIn := `version: 1
rules:
  - paths: ["**"]
    write: [admins]
`
	r, _ := Parse([]byte(yamlIn))
	a := Actor{Username: "deploy-bot", Role: RoleAdmin, Groups: nil}
	if err := r.Allow(a, []string{"anywhere/foo.md"}); err != nil {
		t.Errorf("bot-as-admin should pass: %v", err)
	}

	memberBot := Actor{Username: "scratch-bot", Role: RoleMember, Groups: nil}
	err := r.Allow(memberBot, []string{"anywhere/foo.md"})
	if _, ok := IsDenyError(err); !ok {
		t.Errorf("bot-as-member should be denied without group, got %v", err)
	}
}

func TestParseRejectsBadYAML(t *testing.T) {
	bad := []byte("rules: this should be a list, not a string")
	_, err := Parse(bad)
	if err == nil {
		t.Errorf("expected parse error on malformed YAML")
	}
}

func TestLoadForWorktree(t *testing.T) {
	dir := t.TempDir()

	// Absent file: permissive default.
	r, err := LoadForWorktree(dir)
	if err != nil {
		t.Fatalf("missing file: %v", err)
	}
	if len(r.Rules) != 0 {
		t.Errorf("absent file should give empty Rules, got %+v", r)
	}

	// Write a real file and re-load.
	if err := mkfile(dir, PermissionsFilePath, []byte(`version: 1
rules:
  - paths: ["finance/**"]
    write: [admins]
`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	r2, err := LoadForWorktree(dir)
	if err != nil {
		t.Fatalf("present file: %v", err)
	}
	if len(r2.Rules) != 1 || r2.Rules[0].Paths[0] != "finance/**" {
		t.Errorf("unexpected parsed rules: %+v", r2)
	}

	// Empty file is also permissive.
	if err := mkfile(dir, PermissionsFilePath, []byte("")); err != nil {
		t.Fatalf("empty: %v", err)
	}
	r3, err := LoadForWorktree(dir)
	if err != nil {
		t.Fatalf("empty file: %v", err)
	}
	if len(r3.Rules) != 0 {
		t.Errorf("empty file should give empty Rules, got %+v", r3)
	}
}

func TestDenyErrorMessage(t *testing.T) {
	de := &DenyError{Username: "hanna", Reason: "rule", Paths: []string{"finance/q3.md", "finance/q4.md"}}
	msg := de.Error()
	if !strings.Contains(msg, "@hanna") {
		t.Errorf("message should contain @hanna: %s", msg)
	}
	if !strings.Contains(msg, "finance/q3.md") {
		t.Errorf("message should list offending paths: %s", msg)
	}
	if !errors.Is(de, de) {
		t.Errorf("errors.Is on self should hold")
	}
}
