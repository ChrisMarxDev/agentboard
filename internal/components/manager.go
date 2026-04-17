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
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Props       map[string]PropMeta    `json:"props,omitempty"`
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
				Description: "A single large number with optional trend.",
				Props: map[string]PropMeta{
					"source": {Type: "string", Description: "Data key to read from", Required: true},
					"label":  {Type: "string", Description: "Override label from data"},
					"format": {Type: "string", Description: "number | currency | percent | duration"},
				},
			},
		},
		{
			Name: "Status", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Status",
				Description: "A single state indicator with a label.",
				Props: map[string]PropMeta{
					"source": {Type: "string", Description: "Data key. Expects { state, label, detail? }", Required: true},
				},
			},
		},
		{
			Name: "Progress", Type: "builtin",
			Meta: ComponentMeta{
				Name:        "Progress",
				Description: "A progress bar.",
				Props: map[string]PropMeta{
					"source": {Type: "string", Description: "Data key. Expects { value, max, label? }", Required: true},
					"label":  {Type: "string", Description: "Override label"},
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
				Description: "A boxed container with optional title. Use inside a Deck to build dashboard layouts.",
				Props: map[string]PropMeta{
					"title": {Type: "string", Description: "Optional uppercase label rendered above the content"},
					"span":  {Type: "number", Description: "Number of grid columns this card spans", Default: 1},
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
