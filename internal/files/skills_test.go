package files

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/christophermarx/agentboard/internal/project"
)

// newTestManager builds a Manager rooted at a temp directory. The returned
// path is the project root; files live under <path>/files/.
func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	proj := &project.Project{Path: dir}
	return NewManager(proj, 0), dir
}

// writeSkillFiles creates files/skills/<slug>/<path> with the given contents.
func writeSkillFiles(t *testing.T, root, slug string, files map[string]string) {
	t.Helper()
	base := filepath.Join(root, "files", "skills", slug)
	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		full := filepath.Join(base, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListSkills_Empty(t *testing.T) {
	m, _ := newTestManager(t)
	skills, err := m.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills in empty project, got %d", len(skills))
	}
}

func TestListSkills_ValidSkill(t *testing.T) {
	m, root := newTestManager(t)
	writeSkillFiles(t, root, "greeter", map[string]string{
		"SKILL.md": "---\nname: greeter\ndescription: Say hello\n---\n\nBody here.\n",
	})

	skills, err := m.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	s := skills[0]
	if s.Slug != "greeter" || s.Name != "greeter" || s.Description != "Say hello" {
		t.Errorf("unexpected skill: %+v", s)
	}
	if s.Path != "skills/greeter" {
		t.Errorf("path = %q, want skills/greeter", s.Path)
	}
}

func TestListSkills_SkipsFoldersWithoutManifest(t *testing.T) {
	m, root := newTestManager(t)
	// valid skill
	writeSkillFiles(t, root, "ok", map[string]string{
		"SKILL.md": "---\nname: ok\ndescription: fine\n---\n",
	})
	// folder with no SKILL.md
	if err := os.MkdirAll(filepath.Join(root, "files", "skills", "empty"), 0755); err != nil {
		t.Fatal(err)
	}
	// SKILL.md without required fields
	writeSkillFiles(t, root, "incomplete", map[string]string{
		"SKILL.md": "---\nname: incomplete\n---\n", // missing description
	})
	// SKILL.md with malformed frontmatter (no closing marker)
	writeSkillFiles(t, root, "bad", map[string]string{
		"SKILL.md": "---\nname: bad\ndescription: oops\nno close\n",
	})

	skills, err := m.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 valid skill, got %d: %+v", len(skills), skills)
	}
	if skills[0].Slug != "ok" {
		t.Errorf("expected slug=ok, got %q", skills[0].Slug)
	}
}

func TestListSkills_SkipsHiddenSlugs(t *testing.T) {
	m, root := newTestManager(t)
	// Folder starting with a dot should be rejected by ValidateName.
	if err := os.MkdirAll(filepath.Join(root, "files", "skills", ".hidden"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "files", "skills", ".hidden", "SKILL.md"),
		[]byte("---\nname: h\ndescription: d\n---\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	skills, err := m.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills (hidden folder must be ignored), got %d", len(skills))
	}
}

func TestListSkills_Sorted(t *testing.T) {
	m, root := newTestManager(t)
	writeSkillFiles(t, root, "zebra", map[string]string{
		"SKILL.md": "---\nname: zebra\ndescription: z\n---\n",
	})
	writeSkillFiles(t, root, "alpha", map[string]string{
		"SKILL.md": "---\nname: alpha\ndescription: a\n---\n",
	})
	writeSkillFiles(t, root, "mango", map[string]string{
		"SKILL.md": "---\nname: mango\ndescription: m\n---\n",
	})

	skills, _ := m.ListSkills()
	if len(skills) != 3 {
		t.Fatalf("expected 3 skills, got %d", len(skills))
	}
	got := []string{skills[0].Slug, skills[1].Slug, skills[2].Slug}
	want := []string{"alpha", "mango", "zebra"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("slug[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGetSkill_NotFound(t *testing.T) {
	m, _ := newTestManager(t)
	_, err := m.GetSkill("missing")
	if !errors.Is(err, ErrSkillNotFound) {
		t.Errorf("expected ErrSkillNotFound, got %v", err)
	}
}

func TestGetSkill_InvalidSlug(t *testing.T) {
	m, _ := newTestManager(t)
	cases := []string{"../traversal", "with/slash", ".hidden", ""}
	for _, slug := range cases {
		if _, err := m.GetSkill(slug); !errors.Is(err, ErrInvalidName) && !errors.Is(err, ErrSkillNotFound) {
			t.Errorf("slug %q: expected ErrInvalidName or ErrSkillNotFound, got %v", slug, err)
		}
	}
}

func TestGetSkill_InlinesTextAndBinary(t *testing.T) {
	m, root := newTestManager(t)
	// Text file (SKILL.md) and a binary-ish file (fake PNG signature).
	writeSkillFiles(t, root, "multi", map[string]string{
		"SKILL.md":   "---\nname: multi\ndescription: multi-file skill\n---\n\nBody\n",
		"helper.py":  "print('hi')\n",
		"README.txt": "notes",
	})
	// Add a file with a null byte so it's treated as binary.
	binPath := filepath.Join(root, "files", "skills", "multi", "logo.bin")
	if err := os.WriteFile(binPath, []byte{0x00, 0x01, 0x02, 0x03}, 0644); err != nil {
		t.Fatal(err)
	}

	bundle, err := m.GetSkill("multi")
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Name != "multi" || bundle.Description != "multi-file skill" {
		t.Errorf("unexpected bundle metadata: %+v", bundle)
	}
	// SKILL.md should be first in the file list.
	if len(bundle.Files) == 0 || bundle.Files[0].Path != "SKILL.md" {
		t.Fatalf("expected SKILL.md first, got files=%v", bundle.Files)
	}

	byPath := map[string]SkillFile{}
	for _, f := range bundle.Files {
		byPath[f.Path] = f
	}

	if byPath["SKILL.md"].Encoding != "text" {
		t.Errorf("SKILL.md encoding = %q, want text", byPath["SKILL.md"].Encoding)
	}
	if byPath["helper.py"].Encoding != "text" {
		t.Errorf("helper.py encoding = %q, want text", byPath["helper.py"].Encoding)
	}
	if byPath["logo.bin"].Encoding != "base64" {
		t.Errorf("logo.bin encoding = %q, want base64", byPath["logo.bin"].Encoding)
	}
	// Base64 should decode back to the original bytes.
	raw, err := base64.StdEncoding.DecodeString(byPath["logo.bin"].Content)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, []byte{0x00, 0x01, 0x02, 0x03}) {
		t.Errorf("base64 roundtrip mismatch: %v", raw)
	}
}

func TestWriteSkillZip_ContainsAllFiles(t *testing.T) {
	m, root := newTestManager(t)
	writeSkillFiles(t, root, "packable", map[string]string{
		"SKILL.md":     "---\nname: packable\ndescription: zipped\n---\n\nBody\n",
		"examples.md":  "# examples\n",
		"nested/tool":  "sub-file",
	})

	var buf bytes.Buffer
	if err := m.WriteSkillZip("packable", &buf); err != nil {
		t.Fatal(err)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		got[f.Name] = string(body)
	}

	for _, want := range []string{
		"packable/SKILL.md",
		"packable/examples.md",
		"packable/nested/tool",
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("zip missing entry %q (has: %v)", want, keys(got))
		}
	}
	if got["packable/examples.md"] != "# examples\n" {
		t.Errorf("examples.md contents mismatch: %q", got["packable/examples.md"])
	}
}

func TestWriteSkillZip_NotFound(t *testing.T) {
	m, _ := newTestManager(t)
	var buf bytes.Buffer
	err := m.WriteSkillZip("absent", &buf)
	if !errors.Is(err, ErrSkillNotFound) {
		t.Errorf("expected ErrSkillNotFound, got %v", err)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
