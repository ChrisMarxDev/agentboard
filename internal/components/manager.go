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
				Description: "An ordered or unordered list with optional status badges.",
				Props: map[string]PropMeta{
					"source":  {Type: "string", Description: "Data key. Expects array of strings or objects", Required: true},
					"variant": {Type: "string", Description: "ordered | unordered"},
				},
			},
		},
		{
			Name: "Kanban", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Kanban",
				Description: "A non-interactive kanban board.",
				Props: map[string]PropMeta{
					"source":     {Type: "string", Description: "Data key. Expects array of objects", Required: true},
					"groupBy":    {Type: "string", Description: "Field to group by", Required: true},
					"columns":    {Type: "array", Description: "Explicit column order"},
					"titleField": {Type: "string", Description: "Field for card title", Default: "title"},
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
				Description: "A small inline pill for versions, environment labels, or inline status.",
				Props: map[string]PropMeta{
					"text":    {Type: "string", Description: "Label text (inline)."},
					"variant": {Type: "string", Description: "default | accent | success | warning | error"},
					"source":  {Type: "string", Description: "Optional KV key. Expects a string, or { text, variant? } object."},
				},
			},
		},
		{
			Name: "Counter", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Counter",
				Description: "Like Metric but flashes green on increase / red on decrease when the value updates.",
				Props: map[string]PropMeta{
					"value":  {Type: "number", Description: "Inline value. Wins over `source` when present."},
					"label":  {Type: "string", Description: "Optional label rendered below the number."},
					"format": {Type: "string", Description: "number | currency | percent"},
					"source": {Type: "string", Description: "Optional KV key. Expects a number or { value } object."},
				},
			},
		},
		{
			Name: "Code", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Code",
				Description: "A syntax-highlighted code block.",
				Props: map[string]PropMeta{
					"value":    {Type: "string", Description: "Inline code text."},
					"language": {Type: "string", Description: "Prism language id (e.g. js, ts, json, bash, sql). Default: text."},
					"source":   {Type: "string", Description: "Optional KV key. Expects a string, or { code, language? } object."},
				},
			},
		},
		{
			Name: "Mermaid", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Mermaid",
				Description: "Renders a Mermaid diagram. The library loads lazily on first render.",
				Props: map[string]PropMeta{
					"value":  {Type: "string", Description: "Inline Mermaid source (preferred when hand-authoring)."},
					"theme":  {Type: "string", Description: "default | dark | forest | neutral | base (follows system color scheme by default)"},
					"source": {Type: "string", Description: "Optional KV key. Expects a Mermaid source string, or { code } object."},
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
