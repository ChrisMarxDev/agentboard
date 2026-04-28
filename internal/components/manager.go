package components

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/christophermarx/agentboard/internal/project"
	"github.com/fsnotify/fsnotify"
)

// MaxComponentSourceBytes caps user-uploaded component source to prevent
// pathological inputs. ~100 KB covers any reasonable hand-written component.
const MaxComponentSourceBytes = 100 * 1024

// componentNamePattern restricts uploaded component names to valid React
// component identifiers. This also blocks path traversal (no `/`, no `.`,
// no `..`) since the name is joined with ComponentsDir() and given a .jsx suffix.
var componentNamePattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9]{0,63}$`)

// ErrInvalidComponentName is returned when a name fails validation.
var ErrInvalidComponentName = errors.New("component name must start with an uppercase letter and contain only letters and digits (max 64 chars)")

// ErrComponentTooLarge is returned when source exceeds MaxComponentSourceBytes.
var ErrComponentTooLarge = fmt.Errorf("component source exceeds %d bytes", MaxComponentSourceBytes)

// ErrBuiltinComponent is returned when the caller tries to mutate a built-in.
var ErrBuiltinComponent = errors.New("cannot modify built-in component; user components with the same name will override at runtime")

// ErrComponentNotFound is returned when deleting a component that doesn't exist.
var ErrComponentNotFound = errors.New("component not found")

// ComponentMeta describes a component's metadata.
type ComponentMeta struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Props       map[string]PropMeta `json:"props,omitempty"`
}

// PropMeta describes a component prop.
type PropMeta struct {
	Type        string      `json:"type"`
	Description string      `json:"description,omitempty"`
	Required    bool        `json:"required,omitempty"`
	Default     interface{} `json:"default,omitempty"`
}

// ComponentInfo represents a registered component.
type ComponentInfo struct {
	Name   string        `json:"name"`
	Type   string        `json:"type"` // "builtin" or "user"
	File   string        `json:"file,omitempty"`
	Meta   ComponentMeta `json:"meta"`
	Source string        `json:"-"` // raw source (not included in list)
}

// Manager manages the component catalog and compilation.
type Manager struct {
	project    *project.Project
	mu         sync.RWMutex
	components map[string]*ComponentInfo
	bundle     string // compiled JS bundle of user components
}

// NewManager creates a new component manager.
func NewManager(proj *project.Project) *Manager {
	m := &Manager{
		project:    proj,
		components: make(map[string]*ComponentInfo),
	}

	// Register built-in components
	m.registerBuiltins()

	// Scan user components
	m.ScanUserComponents()

	return m
}

func (m *Manager) registerBuiltins() {
	builtins := []ComponentInfo{
		{
			Name: "Metric", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Metric",
				Description: "A single large number with optional trend. Prefer inline props for hand-authored pages; `source` stays available for values written by agents into the KV store.",
				Props: map[string]PropMeta{
					"value":      {Type: "number", Description: "Inline value. Wins over `source` when present."},
					"label":      {Type: "string", Description: "Label rendered below the number."},
					"trend":      {Type: "number", Description: "Percent change — positive green up arrow, negative red down arrow."},
					"comparison": {Type: "string", Description: "Small text shown next to the trend (e.g. 'last week')."},
					"format":     {Type: "string", Description: "number | currency | percent | duration"},
					"source":     {Type: "string", Description: "Optional KV key. Use for agent-driven numbers; hand-authored pages should use `value`."},
				},
			},
		},
		{
			Name: "Status", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Status",
				Description: "A state indicator with colored pill. Prefer inline props; `source` is available for agent-driven status.",
				Props: map[string]PropMeta{
					"state":  {Type: "string", Description: "running | passing | failing | waiting | stale"},
					"label":  {Type: "string", Description: "Primary text in the pill."},
					"detail": {Type: "string", Description: "Optional trailing context text."},
					"source": {Type: "string", Description: "Optional KV key. Expects { state, label, detail? }."},
				},
			},
		},
		{
			Name: "Progress", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Progress",
				Description: "A progress bar. Prefer inline props; `source` is available for agent-driven values.",
				Props: map[string]PropMeta{
					"value":  {Type: "number", Description: "Current value (inline)."},
					"max":    {Type: "number", Description: "Upper bound (inline). Default 100."},
					"label":  {Type: "string", Description: "Label above the bar."},
					"source": {Type: "string", Description: "Optional KV key. Expects { value, max, label? }."},
				},
			},
		},
		{
			Name: "Table", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Table",
				Description: "A table of rows and columns.",
				Props: map[string]PropMeta{
					"source":    {Type: "string", Description: "Data key. Expects { columns, rows } or array of objects", Required: true},
					"linkField": {Type: "string", Description: "Field containing URLs"},
				},
			},
		},
		{
			Name: "Chart", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Chart",
				Description: "A static chart rendered from a snapshot.",
				Props: map[string]PropMeta{
					"source":  {Type: "string", Description: "Data key", Required: true},
					"variant": {Type: "string", Description: "bar | pie | donut | horizontal_bar"},
				},
			},
		},
		{
			Name: "TimeSeries", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "TimeSeries",
				Description: "A time series line or bar chart.",
				Props: map[string]PropMeta{
					"source":  {Type: "string", Description: "Data key. Expects array of points", Required: true},
					"variant": {Type: "string", Description: "line | bar"},
					"x":       {Type: "string", Description: "Field name for x axis"},
					"y":       {Type: "string", Description: "Field name for y axis"},
				},
			},
		},
		{
			Name: "Log", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Log",
				Description: "An append-only text log.",
				Props: map[string]PropMeta{
					"source": {Type: "string", Description: "Data key. Expects array of { timestamp, level?, message }", Required: true},
					"limit":  {Type: "number", Description: "Max entries to show", Default: 50},
				},
			},
		},
		{
			Name: "List", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "List",
				Description: "An ordered or unordered list with optional status badges. Pass `source` for an array key, or omit it on a folder-index page to auto-attach to the page's own children.",
				Props: map[string]PropMeta{
					"source":  {Type: "string", Description: "Frontmatter array key, OR a folder path with trailing slash (e.g. \"items/\"). Omit on a folder-index page to auto-attach."},
					"variant": {Type: "string", Description: "ordered | unordered"},
				},
			},
		},
		{
			Name: "Kanban", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Kanban",
				Description: "A kanban board. Pass `source` as a folder path with trailing slash (e.g. \"tasks/\") for a folder-collection of cards, or as a frontmatter array key for inline cards. Omit `source` on a folder-index page (`/tasks.md`) to auto-attach to the page's own folder.",
				Props: map[string]PropMeta{
					"source":     {Type: "string", Description: "Folder path (\"tasks/\") or frontmatter array key. Omit on a folder-index page to auto-attach."},
					"groupBy":    {Type: "string", Description: "Field to group cards by (e.g. \"col\" or \"status\").", Required: true},
					"columns":    {Type: "array", Description: "Explicit column order. Without this, columns appear in the order encountered."},
					"titleField": {Type: "string", Description: "Card field rendered as the card title.", Default: "title"},
				},
			},
		},
		{
			Name: "Deck", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Deck",
				Description: "A responsive grid container for Cards. Children wrap to new rows based on viewport width.",
				Props: map[string]PropMeta{
					"min":     {Type: "number", Description: "Minimum card width in pixels before wrapping", Default: 280},
					"gap":     {Type: "number", Description: "Spacing between cards in pixels", Default: 16},
					"columns": {Type: "number", Description: "Force a fixed number of columns (overrides min-width auto-fit)"},
				},
			},
		},
		{
			Name: "Card", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Card",
				Description: "A boxed container with optional title. Use inside a Deck to build dashboard layouts. Pass `href` to make the entire card a navigation target — inner links/buttons keep working.",
				Props: map[string]PropMeta{
					"title": {Type: "string", Description: "Optional uppercase label rendered above the content"},
					"span":  {Type: "number", Description: "Number of grid columns this card spans", Default: 1},
					"href":  {Type: "string", Description: "Optional navigation target. Relative paths navigate in-app; http(s) URLs open in a new tab. Inner interactive elements still work."},
				},
			},
		},
		{
			Name: "Stack", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Stack",
				Description: "A vertical flex container. Children flow top-to-bottom with configurable gap.",
				Props: map[string]PropMeta{
					"gap":   {Type: "number", Description: "Spacing between children in pixels", Default: 16},
					"align": {Type: "string", Description: "start | center | end | stretch", Default: "stretch"},
				},
			},
		},
		{
			Name: "Markdown", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Markdown",
				Description: "Renders markdown text. Prefer passing text via children or `text`; `source` reads a KV string and re-renders on SSE updates.",
				Props: map[string]PropMeta{
					"text":   {Type: "string", Description: "Raw markdown string (compiled to MDX)."},
					"source": {Type: "string", Description: "Optional KV key. Expects a string, or { text } object."},
				},
			},
		},
		{
			Name: "Badge", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Badge",
				Description: "A small inline pill for versions, environment labels, or inline status. Prefer inline `text` for hand-authored pages; `source` is for agent-driven labels.",
				Props: map[string]PropMeta{
					"text":    {Type: "string", Description: "Label text (inline). Wins over `source` when present."},
					"variant": {Type: "string", Description: "default | accent | success | warning | error"},
					"source":  {Type: "string", Description: "Optional frontmatter key. Expects a string, or { text, variant? } object."},
				},
			},
		},
		{
			Name: "Counter", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Counter",
				Description: "Like Metric but flashes green on increase / red on decrease when the value updates. Prefer inline `value` for hand-authored pages; `source` is for agent-driven counters.",
				Props: map[string]PropMeta{
					"value":  {Type: "number", Description: "Inline value (preferred). Wins over `source` when present."},
					"label":  {Type: "string", Description: "Optional label rendered below the number."},
					"format": {Type: "string", Description: "number | currency | percent"},
					"source": {Type: "string", Description: "Optional frontmatter key. Expects a number or { value } object."},
				},
			},
		},
		{
			Name: "Code", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Code",
				Description: "A syntax-highlighted code block. Prefer inline `value` (or children) for hand-authored pages; `source` is for agent-driven snippets.",
				Props: map[string]PropMeta{
					"value":    {Type: "string", Description: "Inline code text (preferred). Wins over `source` when present."},
					"language": {Type: "string", Description: "Prism language id (e.g. js, ts, json, bash, sql). Default: text."},
					"source":   {Type: "string", Description: "Optional frontmatter key. Expects a string, or { code, language? } object."},
				},
			},
		},
		{
			Name: "Mermaid", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Mermaid",
				Description: "Renders a Mermaid diagram. Prefer inline `value` for hand-authored pages; `source` is for agent-generated diagrams. The library loads lazily on first render.",
				Props: map[string]PropMeta{
					"value":  {Type: "string", Description: "Inline Mermaid source (preferred). Wins over `source` when present."},
					"theme":  {Type: "string", Description: "default | dark | forest | neutral | base (follows system color scheme by default)"},
					"source": {Type: "string", Description: "Optional frontmatter key. Expects a Mermaid source string, or { code } object."},
				},
			},
		},
		{
			Name: "Image", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Image",
				Description: "Renders an uploaded image inline. Resolves source/src to /api/files/<name> for bare names, passes through absolute URLs.",
				Props: map[string]PropMeta{
					"source": {Type: "string", Description: "Data key. Expects a filename string or { file, alt?, width?, height? } object."},
					"src":    {Type: "string", Description: "Direct URL override (takes precedence over source)."},
					"alt":    {Type: "string", Description: "Alt text."},
					"width":  {Type: "number", Description: "Width in pixels."},
					"height": {Type: "number", Description: "Height in pixels."},
					"fit":    {Type: "string", Description: "contain | cover | fill | none (default: contain)"},
				},
			},
		},
		{
			Name: "File", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "File",
				Description: "Renders a downloadable file as a card with filename, size, and MIME type. Falls back gracefully for remote URLs.",
				Props: map[string]PropMeta{
					"source": {Type: "string", Description: "Data key. Expects a filename string or { file, label? } object."},
					"src":    {Type: "string", Description: "Direct URL override."},
					"label":  {Type: "string", Description: "Override the display name on the card."},
				},
			},
		},
		{
			Name: "Errors", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Errors",
				Description: "Renders the live render-error buffer from /api/errors. Shows Mermaid parse failures, Image 404s, Markdown errors — all with dedupe counts and per-entry Clear buttons. No source prop — fetches the endpoint directly.",
				Props: map[string]PropMeta{
					"limit": {Type: "number", Description: "Max entries to show (default 10)"},
				},
			},
		},
		{
			Name: "ApiList", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "ApiList",
				Description: "Generic list renderer for any REST endpoint that returns a JSON array of objects (e.g. /api/skills, /api/errors, /api/content). Picks a title, description, and id from each row (configurable, with sensible fallbacks). Optionally shows a download link per row. Use this before adding a type-specific page — principle 9.",
				Props: map[string]PropMeta{
					"src":            {Type: "string", Description: "Endpoint returning a JSON array of objects (required)."},
					"titleKey":       {Type: "string", Description: "Field to use as row title (defaults: title, name, slug, id, key)."},
					"descriptionKey": {Type: "string", Description: "Field to use as row description (defaults: description, summary, detail)."},
					"idKey":          {Type: "string", Description: "Field used for React keys and URL building (defaults: slug, id, key, name)."},
					"downloadPrefix": {Type: "string", Description: "If set, each row shows a download button to `{downloadPrefix}{idKey}`."},
					"empty":          {Type: "string", Description: "Text shown when the endpoint returns an empty array."},
					"refreshOn":      {Type: "string", Description: "Optional window event name to re-fetch on (e.g. agentboard:file-updated)."},
				},
			},
		},
		{
			Name: "Button", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Button",
				Description: "Fires a webhook event on click. The component does exactly one thing — POST /api/webhooks/fire with `{event, payload}`. Subscribers decide what happens. Use it to wire dashboards to ops actions (deploy, alert, rebuild) without inventing per-button endpoints.",
				Props: map[string]PropMeta{
					"fires":    {Type: "string", Description: "Outbound event name (e.g. 'deploy.prod'). Required for the click to do anything."},
					"payload":  {Type: "object", Description: "Structured payload passed to subscribers as event.data."},
					"confirm":  {Type: "string", Description: "Confirm-before-firing prompt text. If set, click first opens a window.confirm() dialog."},
					"variant":  {Type: "string", Description: "default | accent | danger"},
					"label":    {Type: "string", Description: "Button label. Falls back to children, then to 'Fire {fires}'."},
					"disabled": {Type: "boolean", Description: "Disable the button."},
				},
			},
		},
		{
			Name: "Inbox", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Inbox",
				Description: "Renders the current user's reminder queue (mentions, assignments, approval requests, webhook failures). Polls /api/inbox every 30s. Click a row to navigate to its subject; per-row mark-read / archive / delete plus a top-level 'mark all read'. No source prop — it always shows the authed user's own inbox.",
				Props: map[string]PropMeta{
					"unreadOnly": {Type: "boolean", Description: "Show only unread items. Handy for nav-adjacent widgets."},
					"limit":      {Type: "number", Description: "Max items to load."},
				},
			},
		},
		{
			Name: "Mention", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Mention",
				Description: "Renders a compact `@username` pill backed by the user's avatar color, or a team pill for stored teams and reserved pseudo-teams (@all, @admins, @agents, @here). Unknown usernames render as plain text so the original intent survives until the user is created.",
				Props: map[string]PropMeta{
					"username": {Type: "string", Description: "Username or team slug (without the leading @).", Required: true},
					"display":  {Type: "string", Description: "Override the visual label; defaults to '@username'."},
					"plain":    {Type: "boolean", Description: "Drop the colored pill and render plain text with the leading @."},
				},
			},
		},
		{
			Name: "RichText", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "RichText",
				Description: "Renders a plain string with inline `@username` patterns parsed into Mention pills. Used for free-text values from the data store (log entries, task descriptions, comments) where agents write strings and the dashboard wants them rich. Wire format stays 'just a string'.",
				Props: map[string]PropMeta{
					"text":       {Type: "string", Description: "Inline string (from MDX prose or another component's data)."},
					"source":     {Type: "string", Description: "Data store key to subscribe to; overrides `text` when both are given."},
					"emptyLabel": {Type: "string", Description: "Placeholder rendered when the resolved text is empty or missing."},
				},
			},
		},
		{
			Name: "Sheet", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Sheet",
				Description: "Renders an array-of-objects data key as an editable grid. Authed users edit cells inline; public/share mode is read-only. Saves go through the standard data API (PATCH/POST/DELETE) so every edit fans out on the webhook bus as data.* events — no new server plumbing.",
				Props: map[string]PropMeta{
					"source":  {Type: "string", Description: "Data key resolving to an array of objects. Each row should have an `id` field to be editable.", Required: true},
					"columns": {Type: "array", Description: "Explicit column order. Inferred from the first row's keys when omitted."},
					"title":   {Type: "string", Description: "Optional header rendered above the grid."},
				},
			},
		},
		{
			Name: "SkillInstall", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "SkillInstall",
				Description: "Renders a single copy-paste prompt that any coding agent (Claude Code, Cursor, Aider) can follow to safely install the skill hosted on this AgentBoard. Designed as the safer alternative to `curl | bash` — the agent first reads the manifest, judges harm, then installs into its own framework's skill directory.",
				Props: map[string]PropMeta{
					"slug":  {Type: "string", Description: "Skill slug — folder name under content/skills/.", Required: true},
					"label": {Type: "string", Description: "Optional label override for the copy button."},
				},
			},
		},
		{
			Name: "TeamRoster", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "TeamRoster",
				Description: "Renders a card-style roster for one team: the team pill as header, the description line, and a wrapped list of member Mention pills. Renders a subtle inline warning when the slug is unknown.",
				Props: map[string]PropMeta{
					"slug": {Type: "string", Description: "Team slug (without the leading @).", Required: true},
				},
			},
		},
		{
			Name: "TasksSummary", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "TasksSummary",
				Description: "Three-up status strip for a task collection. Aggregate counts (open / blocked / due-this-week) — no chronological feed. Designed for the Home page; works on any folder collection of task-shaped rows.",
				Props: map[string]PropMeta{
					"source": {Type: "string", Description: "Folder collection path. Default 'tasks/'."},
					"href":   {Type: "string", Description: "Where each tile links. Default '/tasks'."},
				},
			},
		},
		{
			Name: "InboxPreview", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "InboxPreview",
				Description: "Compact preview of the current user's inbox — top N items + 'view all' link. Polls /api/inbox every 30s. Per-user; nobody else sees these items.",
				Props: map[string]PropMeta{
					"limit":      {Type: "number", Description: "Max items to show inline. Default 3."},
					"unreadOnly": {Type: "boolean", Description: "Only surface unread items. Default true."},
				},
			},
		},
		{
			Name: "PinnedPages", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "PinnedPages",
				Description: "Operator-curated shortcut grid. Reads a `pinned: [{href, title, summary?}]` array from the page's frontmatter; each entry renders as a card linking to its href.",
				Props: map[string]PropMeta{
					"source":   {Type: "string", Description: "Frontmatter key holding the pinned array. Default 'pinned'."},
					"fallback": {Type: "array", Description: "Items rendered when the frontmatter key is empty/missing."},
				},
			},
		},
		{
			Name: "SkillsStrip", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "SkillsStrip",
				Description: "Row of skill cards — either curated (frontmatter list of slugs via `source`) or auto (first N skills from /api/skills). Each card links to /skills/<slug>.",
				Props: map[string]PropMeta{
					"source": {Type: "string", Description: "Frontmatter key holding a list of skill slugs. Omit to auto-list from /api/skills."},
					"limit":  {Type: "number", Description: "Max cards to render. Default 6."},
				},
			},
		},
	}

	for _, b := range builtins {
		info := b // copy
		m.components[b.Name] = &info
	}
}

// ScanUserComponents reads .jsx files from the components/ directory.
func (m *Manager) ScanUserComponents() {
	m.mu.Lock()
	defer m.mu.Unlock()

	componentsDir := m.project.ComponentsDir()
	entries, err := os.ReadDir(componentsDir)
	if err != nil {
		return // no components dir or empty
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsx") {
			continue
		}

		filePath := filepath.Join(componentsDir, entry.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			log.Printf("Warning: could not read component %s: %v", entry.Name(), err)
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".jsx")
		m.components[name] = &ComponentInfo{
			Name:   name,
			Type:   "user",
			File:   filepath.Join("components", entry.Name()),
			Source: string(content),
			Meta: ComponentMeta{
				Name:        name,
				Description: "User component: " + name,
			},
		}
	}
}

// ListComponents returns all registered components.
func (m *Manager) ListComponents() []ComponentInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]ComponentInfo, 0, len(m.components))
	for _, c := range m.components {
		result = append(result, *c)
	}
	return result
}

// GetComponentSource returns the source of a component.
func (m *Manager) GetComponentSource(name string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	c, ok := m.components[name]
	if !ok {
		return ""
	}
	return c.Source
}

// GetBundle returns the compiled JS bundle of user components.
func (m *Manager) GetBundle() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.bundle
}

// WriteComponent creates or replaces a user component .jsx file.
// The caller must have gated this behind the AllowComponentUpload flag.
// Validation: name must match componentNamePattern, source must be within
// MaxComponentSourceBytes, and the name must not collide with a built-in.
func (m *Manager) WriteComponent(name, source string) error {
	if !componentNamePattern.MatchString(name) {
		return ErrInvalidComponentName
	}
	if len(source) > MaxComponentSourceBytes {
		return ErrComponentTooLarge
	}

	m.mu.RLock()
	existing, ok := m.components[name]
	m.mu.RUnlock()
	if ok && existing.Type == "builtin" {
		return ErrBuiltinComponent
	}

	dir := m.project.ComponentsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("ensure components dir: %w", err)
	}

	filePath := filepath.Join(dir, name+".jsx")
	if err := os.WriteFile(filePath, []byte(source), 0644); err != nil {
		return fmt.Errorf("write component: %w", err)
	}

	m.ScanUserComponents()
	return nil
}

// DeleteComponent removes a user component .jsx file. Built-ins cannot be
// deleted. The caller must have gated this behind AllowComponentUpload.
func (m *Manager) DeleteComponent(name string) error {
	if !componentNamePattern.MatchString(name) {
		return ErrInvalidComponentName
	}

	m.mu.RLock()
	existing, ok := m.components[name]
	m.mu.RUnlock()
	if ok && existing.Type == "builtin" {
		return ErrBuiltinComponent
	}

	filePath := filepath.Join(m.project.ComponentsDir(), name+".jsx")
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return ErrComponentNotFound
		}
		return fmt.Errorf("delete component: %w", err)
	}

	m.mu.Lock()
	delete(m.components, name)
	m.mu.Unlock()

	m.ScanUserComponents()
	return nil
}

// StartWatcher watches the components/ directory for changes.
func (m *Manager) StartWatcher(onChange func(names []string)) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	componentsDir := m.project.ComponentsDir()
	if err := watcher.Add(componentsDir); err != nil {
		log.Printf("Warning: could not watch %s: %v", componentsDir, err)
		return nil
	}

	go func() {
		defer watcher.Close()
		var debounceTimer *time.Timer

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if !strings.HasSuffix(event.Name, ".jsx") {
					continue
				}

				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) {
					if debounceTimer != nil {
						debounceTimer.Stop()
					}

					debounceTimer = time.AfterFunc(500*time.Millisecond, func() {
						m.ScanUserComponents()
						if onChange != nil {
							names := make([]string, 0)
							m.mu.RLock()
							for name, c := range m.components {
								if c.Type == "user" {
									names = append(names, name)
								}
							}
							m.mu.RUnlock()
							onChange(names)
						}
					})
				}

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("Component watcher error: %v", err)
			}
		}
	}()

	return nil
}
