package files

import (
	"archive/zip"
	"encoding/base64"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// skillsSubdir is the conventional folder under the content tree (see
// Project.FilesDir → ContentDir per CORE_GUIDELINES §9) where Anthropic-style
// skills live. Each direct child of this folder is one skill; each skill
// folder contains a SKILL.md with YAML frontmatter (name + description) plus
// any supporting files. Matches the Anthropic skill format so skills are
// portable.
const skillsSubdir = "skills"

// skillManifest is the canonical filename inside a skill folder.
const skillManifest = "SKILL.md"

// Skill errors. Handlers translate these to HTTP codes.
var (
	ErrSkillNotFound = errors.New("skill not found")
)

// SkillSummary is one entry in the skills index.
type SkillSummary struct {
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Path        string    `json:"path"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SkillFile is one file inside a skill bundle, returned by GetSkill for MCP
// callers. Binary files are base64-encoded.
type SkillFile struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"` // "text" or "base64"
	Size     int64  `json:"size"`
}

// SkillBundle is the full inline contents of one skill, used by the MCP
// get_skill tool. Humans downloading via REST get a zip instead (see
// WriteSkillZip).
type SkillBundle struct {
	Slug        string      `json:"slug"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Path        string      `json:"path"`
	Files       []SkillFile `json:"files"`
}

// SkillsDir returns the filesystem path where skill folders live.
func (m *Manager) SkillsDir() string {
	return filepath.Join(m.FilesDir(), skillsSubdir)
}

// ListSkills walks content/skills/ one level deep and returns each folder that
// contains a valid SKILL.md with name + description frontmatter. Folders
// without a manifest or with malformed frontmatter are silently skipped —
// storage stays ignorant of skill semantics.
func (m *Manager) ListSkills() ([]SkillSummary, error) {
	root := m.SkillsDir()
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []SkillSummary{}, nil
	}
	if err != nil {
		return nil, err
	}

	out := make([]SkillSummary, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		slug := entry.Name()
		if _, err := ValidateName(slug); err != nil {
			continue
		}
		summary, ok := loadSkillSummary(root, slug)
		if !ok {
			continue
		}
		out = append(out, summary)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

// GetSkill returns the full contents of one skill as inlined files. Text-y
// files (detected via extension and content sniff) are returned as UTF-8
// strings; everything else is base64.
func (m *Manager) GetSkill(slug string) (*SkillBundle, error) {
	if _, err := ValidateName(slug); err != nil {
		return nil, ErrInvalidName
	}
	// Reject slugs with separators — skills are direct children of content/skills/.
	if strings.Contains(slug, "/") {
		return nil, ErrInvalidName
	}

	root := m.SkillsDir()
	summary, ok := loadSkillSummary(root, slug)
	if !ok {
		return nil, ErrSkillNotFound
	}

	skillDir := filepath.Join(root, slug)
	var files []SkillFile
	err := filepath.WalkDir(skillDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Skip temp upload files (same guard as List).
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".upload-") && strings.HasSuffix(base, ".tmp") {
			return nil
		}
		rel, err := filepath.Rel(skillDir, path)
		if err != nil {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		sf := buildSkillFile(filepath.ToSlash(rel), body, info.Size())
		files = append(files, sf)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort for deterministic output; SKILL.md first so agents see it immediately.
	sort.Slice(files, func(i, j int) bool {
		if files[i].Path == skillManifest {
			return true
		}
		if files[j].Path == skillManifest {
			return false
		}
		return files[i].Path < files[j].Path
	})

	return &SkillBundle{
		Slug:        summary.Slug,
		Name:        summary.Name,
		Description: summary.Description,
		Path:        summary.Path,
		Files:       files,
	}, nil
}

// WriteSkillZip streams a zip of the skill folder to w. Used by the REST GET
// endpoint so humans can download a skill as one file.
func (m *Manager) WriteSkillZip(slug string, w io.Writer) error {
	if _, err := ValidateName(slug); err != nil {
		return ErrInvalidName
	}
	if strings.Contains(slug, "/") {
		return ErrInvalidName
	}

	root := m.SkillsDir()
	if _, ok := loadSkillSummary(root, slug); !ok {
		return ErrSkillNotFound
	}

	skillDir := filepath.Join(root, slug)
	zw := zip.NewWriter(w)
	defer zw.Close()

	return filepath.WalkDir(skillDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".upload-") && strings.HasSuffix(base, ".tmp") {
			return nil
		}
		rel, err := filepath.Rel(skillDir, path)
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		// Place entries under <slug>/ inside the zip so unzipping yields a named folder.
		header.Name = slug + "/" + filepath.ToSlash(rel)
		header.Method = zip.Deflate
		writer, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(writer, f)
		return err
	})
}

// loadSkillSummary reads <root>/<slug>/SKILL.md, parses the frontmatter, and
// returns a populated SkillSummary. Returns ok=false when the folder is not a
// valid skill (missing manifest, bad frontmatter, missing required fields).
func loadSkillSummary(root, slug string) (SkillSummary, bool) {
	manifestPath := filepath.Join(root, slug, skillManifest)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return SkillSummary{}, false
	}
	fm := parseFrontmatter(data)
	if fm == nil {
		return SkillSummary{}, false
	}
	name, _ := fm["name"].(string)
	desc, _ := fm["description"].(string)
	if strings.TrimSpace(name) == "" || strings.TrimSpace(desc) == "" {
		return SkillSummary{}, false
	}
	stat, err := os.Stat(manifestPath)
	if err != nil {
		return SkillSummary{}, false
	}
	return SkillSummary{
		Slug:        slug,
		Name:        name,
		Description: desc,
		Path:        "skills/" + slug,
		UpdatedAt:   stat.ModTime().UTC(),
	}, true
}

// buildSkillFile decides whether a file should be returned as text or base64
// and builds the SkillFile. Text detection mirrors spec-files.md: trust common
// text extensions, otherwise check for valid UTF-8 without null bytes.
func buildSkillFile(relPath string, body []byte, size int64) SkillFile {
	if isTextual(relPath, body) {
		return SkillFile{
			Path:     relPath,
			Content:  string(body),
			Encoding: "text",
			Size:     size,
		}
	}
	return SkillFile{
		Path:     relPath,
		Content:  base64.StdEncoding.EncodeToString(body),
		Encoding: "base64",
		Size:     size,
	}
}

// textExtensions is a small whitelist of extensions we return inline as text
// without sniffing. Anything outside this list falls through to byte-content
// sniffing.
var textExtensions = map[string]bool{
	".md":   true,
	".mdx":  true,
	".txt":  true,
	".yaml": true,
	".yml":  true,
	".json": true,
	".csv":  true,
	".tsv":  true,
	".xml":  true,
	".html": true,
	".css":  true,
	".js":   true,
	".ts":   true,
	".jsx":  true,
	".tsx":  true,
	".py":   true,
	".sh":   true,
	".go":   true,
	".rs":   true,
	".toml": true,
	".ini":  true,
}

func isTextual(path string, body []byte) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if textExtensions[ext] {
		return true
	}
	// Heuristic: if there are no null bytes in the first few KB, assume text.
	head := body
	if len(head) > 2048 {
		head = head[:2048]
	}
	for _, b := range head {
		if b == 0 {
			return false
		}
	}
	// Empty or short ASCII-only — treat as text.
	return len(head) > 0
}
