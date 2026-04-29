package project

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// NavItem represents a navigation entry.
type NavItem struct {
	Path  string `yaml:"path" json:"path"`
	Label string `yaml:"label" json:"label"`
}

// PublicConfig controls anonymous read access. See
// internal/publicroutes for the matcher semantics.
type PublicConfig struct {
	// Paths is a list of glob patterns. A GET/HEAD/OPTIONS request whose
	// path matches at least one include pattern (and no `!exclude`) is
	// served without authentication. Writes always require auth; the
	// `writes_require_auth` field is informational — the invariant is
	// enforced in code, not config.
	Paths []string `yaml:"paths,omitempty" json:"paths,omitempty"`
	// WritesRequireAuth is informational. The real invariant lives in
	// internal/publicroutes — flipping this to false has no effect on
	// security, it's here so the intent is documented in the config file.
	WritesRequireAuth bool `yaml:"writes_require_auth" json:"writes_require_auth"`
}

// Config holds the agentboard.yaml configuration.
type Config struct {
	Title                string    `yaml:"title" json:"title"`
	Port                 int       `yaml:"port" json:"port"`
	Theme                string    `yaml:"theme" json:"theme"`
	HistoryRetentionDays int       `yaml:"history_retention_days" json:"history_retention_days"`
	Nav                  []NavItem `yaml:"nav,omitempty" json:"nav,omitempty"`
	// AllowComponentUpload gates PUT/DELETE /api/components/:name and the
	// matching MCP tools. Default false: user components can only be added
	// by writing .jsx files directly to the components/ folder. Enable only
	// when you trust every caller of localhost:3000 — component source runs
	// as arbitrary JavaScript in every dashboard visitor's browser.
	AllowComponentUpload bool `yaml:"allow_component_upload" json:"allow_component_upload"`
	// MaxFileSizeMB caps per-file uploads to /api/files/:name. Default 50,
	// hard upper bound 500 (enforced by internal/files). See docs/archive/spec-files.md §5.
	MaxFileSizeMB int `yaml:"max_file_size_mb" json:"max_file_size_mb"`
	// Public exposes selected read endpoints without auth. Empty = fully
	// private instance (default).
	Public PublicConfig `yaml:"public" json:"public"`
}

// DefaultConfig returns configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Title:                "AgentBoard",
		Port:                 3000,
		Theme:                "auto",
		HistoryRetentionDays: 30,
	}
}

// LoadConfig loads configuration from agentboard.yaml, merged with defaults.
func LoadConfig(projectPath string) (*Config, error) {
	cfg := DefaultConfig()

	// Use folder name as default title
	cfg.Title = filepath.Base(projectPath)

	configPath := filepath.Join(projectPath, "agentboard.yaml")
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}

	var fileCfg Config
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return nil, err
	}

	// Merge file config over defaults
	if fileCfg.Title != "" {
		cfg.Title = fileCfg.Title
	}
	if fileCfg.Port != 0 {
		cfg.Port = fileCfg.Port
	}
	if fileCfg.Theme != "" {
		cfg.Theme = fileCfg.Theme
	}
	if fileCfg.HistoryRetentionDays != 0 {
		cfg.HistoryRetentionDays = fileCfg.HistoryRetentionDays
	}
	if len(fileCfg.Nav) > 0 {
		cfg.Nav = fileCfg.Nav
	}
	if fileCfg.AllowComponentUpload {
		cfg.AllowComponentUpload = true
	}
	if fileCfg.MaxFileSizeMB > 0 {
		cfg.MaxFileSizeMB = fileCfg.MaxFileSizeMB
	}
	if len(fileCfg.Public.Paths) > 0 {
		cfg.Public.Paths = fileCfg.Public.Paths
	}
	// writes_require_auth is informational only — always surface the
	// configured value so /api/config accurately reflects what the
	// operator wrote.
	cfg.Public.WritesRequireAuth = fileCfg.Public.WritesRequireAuth

	return cfg, nil
}
