package mcp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/christophermarx/agentboard/internal/components"
	"github.com/christophermarx/agentboard/internal/files"
	"github.com/christophermarx/agentboard/internal/grab"
)

func (s *Server) toolDefinitions() []ToolDef {
	tools := []ToolDef{
		{
			Name:        "agentboard_set",
			Description: "Set a data value at a key. Value can be any JSON.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"key":   map[string]string{"type": "string", "description": "Dotted path key (e.g. analytics.dau)"},
					"value": map[string]string{"description": "Any valid JSON value"},
				},
				"required": []string{"key", "value"},
			},
		},
		{
			Name:        "agentboard_merge",
			Description: "Deep merge JSON into an existing data value. Creates key if it doesn't exist.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"key":   map[string]string{"type": "string", "description": "Dotted path key"},
					"value": map[string]string{"description": "JSON object to merge"},
				},
				"required": []string{"key", "value"},
			},
		},
		{
			Name:        "agentboard_append",
			Description: "Append an item to a data array. Creates array if key doesn't exist.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"key":  map[string]string{"type": "string", "description": "Dotted path key"},
					"item": map[string]string{"description": "JSON value to append"},
				},
				"required": []string{"key", "item"},
			},
		},
		{
			Name:        "agentboard_delete",
			Description: "Delete a data key entirely.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"key": map[string]string{"type": "string", "description": "Dotted path key to delete"},
				},
				"required": []string{"key"},
			},
		},
		{
			Name:        "agentboard_get",
			Description: "Read the current value at a data key.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"key": map[string]string{"type": "string", "description": "Dotted path key to read"},
				},
				"required": []string{"key"},
			},
		},
		{
			Name:        "agentboard_list_keys",
			Description: "List all data keys with their current values.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "agentboard_list_pages",
			Description: "List all dashboard pages.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "agentboard_read_page",
			Description: "Read a page's MDX source.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]string{"type": "string", "description": "Page path (e.g. index, ops, metrics)"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "agentboard_write_page",
			Description: "Create or update a dashboard page with MDX content.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":   map[string]string{"type": "string", "description": "Page path (e.g. index, ops)"},
					"source": map[string]string{"type": "string", "description": "MDX source content"},
				},
				"required": []string{"path", "source"},
			},
		},
		{
			Name:        "agentboard_delete_page",
			Description: "Delete a dashboard page. Cannot delete index page.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]string{"type": "string", "description": "Page path to delete"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "agentboard_list_components",
			Description: "List all available dashboard components with their props and descriptions.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "agentboard_read_component",
			Description: "Read a component's source code.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]string{"type": "string", "description": "Component name"},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "agentboard_get_data_schema",
			Description: "Get the inferred JSON schema of all current data keys.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "agentboard_write_file",
			Description: "Upload (or replace) a binary file under the project's files/ folder. Pass the bytes base64-encoded. File is served at /api/files/<name>; reference from pages with <Image source=\"...\"> or <File source=\"...\">.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":           map[string]string{"type": "string", "description": "Relative file path (e.g. hero.png, exports/q1.csv). Letters/digits/._- only, no leading dot, no ..)"},
					"content_base64": map[string]string{"type": "string", "description": "File contents encoded as base64."},
				},
				"required": []string{"name", "content_base64"},
			},
		},
		{
			Name:        "agentboard_list_files",
			Description: "List all files in the project with size, MIME type, and URL.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "agentboard_delete_file",
			Description: "Delete a file from the project's files/ folder.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]string{"type": "string", "description": "File name to delete (same format as agentboard_write_file)"},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "agentboard_list_skills",
			Description: "List skills hosted by this project. A skill is any folder under content/skills/<slug>/ containing a SKILL.md with `name` and `description` in YAML frontmatter (Anthropic skill format). Returns slug, name, description, path, and updated_at per skill. Use this to discover what skills are available before fetching one with agentboard_get_skill.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "agentboard_get_skill",
			Description: "Fetch the complete contents of one skill, inlined. Returns slug, name, description, path, and every file in the skill folder (SKILL.md first). Text files come back as `encoding: \"text\"`; anything else as `encoding: \"base64\"`. Use agentboard_list_skills first to discover valid slugs.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"slug": map[string]string{"type": "string", "description": "Folder name under content/skills/ (no path separators)."},
				},
				"required": []string{"slug"},
			},
		},
		{
			Name:        "agentboard_list_errors",
			Description: "List recent render-time errors reported by frontend components (Mermaid parse failures, Image 404s, Markdown syntax errors, etc.). Returns entries sorted newest-first with component, source key, page, error text, count, first_seen, last_seen. Use this after writing a page or data key to confirm nothing is broken.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "agentboard_clear_errors",
			Description: "Clear errors from the buffer. Pass `key` to clear a single entry (e.g. after fixing one diagram); omit for a full clear.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"key": map[string]string{"type": "string", "description": "Optional: dedupe key from agentboard_list_errors. Omit to clear all."},
				},
			},
		},
		{
			Name:        "agentboard_search",
			Description: "Full-text search over every page in the project. Returns ranked hits with path, title, and a short snippet highlighting the match. Prefer this over list_pages + read_page when you know what you're looking for but not where it lives.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"q":     map[string]string{"type": "string", "description": "Query. Whitespace-separated terms are ANDed. Quote for exact phrases."},
					"limit": map[string]any{"type": "number", "description": "Max hits (default 20, cap 100)"},
				},
				"required": []string{"q"},
			},
		},
		{
			Name:        "agentboard_grab",
			Description: "Materialize a set of Card picks across pages into a single agent-ready payload. Each pick is {page, card_id} where card_id is the kebab-slug of the Card's title. Returns the formatted text (markdown | xml | json) plus the resolved sections. Use this to pull cross-page context from the dashboard when responding to the user.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"picks": map[string]interface{}{
						"type":        "array",
						"description": "List of {page, card_id} picks in the order you want them rendered",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"page":    map[string]string{"type": "string", "description": "Page URL path, e.g. /features/auth"},
								"card_id": map[string]string{"type": "string", "description": "Kebab-case slug of the Card's title"},
							},
							"required": []string{"page", "card_id"},
						},
					},
					"format": map[string]string{"type": "string", "description": "markdown (default) | xml | json"},
				},
				"required": []string{"picks"},
			},
		},
	}

	// Webhook tools — only advertised when the webhook store is live.
	// Agents use these to subscribe (for themselves or for downstream
	// systems) and to fire user-triggered events.
	if s.Webhooks != nil {
		tools = append(tools,
			ToolDef{
				Name:        "agentboard_list_webhooks",
				Description: "List every unrevoked webhook subscription on this instance. Returns id, event_pattern, destination_url, label, status, and success/failure counts. Use to audit what's being observed before adding a new subscription.",
				InputSchema: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
			ToolDef{
				Name:        "agentboard_create_webhook",
				Description: "Subscribe to outbound events matching `event_pattern`. Every matching event POSTs a signed JSON body to `destination_url`. Returns the subscription record plus a plaintext `secret` shown ONCE — save it or rotate via revoke+create. Patterns: `*` = everything, `data.*` = any key write, `content.updated.*` = any page update, etc.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"event_pattern":   map[string]string{"type": "string", "description": "Match pattern, e.g. 'data.*' or 'content.updated.handbook'"},
						"destination_url": map[string]string{"type": "string", "description": "Absolute URL the matching events are POSTed to"},
						"label":           map[string]string{"type": "string", "description": "Optional human-readable description"},
					},
					"required": []string{"event_pattern", "destination_url"},
				},
			},
			ToolDef{
				Name:        "agentboard_revoke_webhook",
				Description: "Revoke a webhook subscription by id. Deliveries stop immediately. Idempotent — revoking an already-revoked subscription is a no-op.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]string{"type": "string", "description": "Subscription id from agentboard_list_webhooks or the create response"},
					},
					"required": []string{"id"},
				},
			},
			ToolDef{
				Name:        "agentboard_fire_event",
				Description: "Emit a user-triggered event onto the webhook bus. Every subscription whose pattern matches `event` receives a signed POST. Use for agent-triggered signals (\"deploy started\", \"runbook completed\") when data writes don't capture the intent. Returns the number of subscribers the event reached.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"event":   map[string]string{"type": "string", "description": "Event name, e.g. 'deploy.prod' or 'alert.pager'"},
						"payload": map[string]interface{}{"type": "object", "description": "Structured payload carried as event.data"},
					},
					"required": []string{"event"},
				},
			},
		)
	}

	// Component upload tools are only advertised when the server was started
	// with --allow-component-upload. See CORE_GUIDELINES and spec §7.6 —
	// user component source runs as arbitrary JS in every visitor's browser.
	if s.AllowComponentUpload {
		tools = append(tools,
			ToolDef{
				Name:        "agentboard_write_component",
				Description: "Create or replace a user component (.jsx) in the project's components/ folder. Requires --allow-component-upload.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name":   map[string]string{"type": "string", "description": "Component name (PascalCase, letters+digits only)"},
						"source": map[string]string{"type": "string", "description": "JSX source. Must export a default React component."},
					},
					"required": []string{"name", "source"},
				},
			},
			ToolDef{
				Name:        "agentboard_delete_component",
				Description: "Delete a user component. Cannot delete built-ins. Requires --allow-component-upload.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]string{"type": "string", "description": "Component name to delete"},
					},
					"required": []string{"name"},
				},
			},
		)
	}

	// Team tools — role groups that expand @mentions to members. Admin-
	// gated writes are enforced at the HTTP layer; the MCP surface
	// advertises them unconditionally so tool discovery works.
	if s.Teams != nil {
		tools = append(tools,
			ToolDef{
				Name:        "agentboard_list_teams",
				Description: "List every team with its members. Use this to discover @team mentions that exist on this instance.",
				InputSchema: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
			ToolDef{
				Name:        "agentboard_get_team",
				Description: "Get one team by slug, including its members.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"slug": map[string]string{"type": "string", "description": "Team slug (e.g. marketing)"},
					},
					"required": []string{"slug"},
				},
			},
			ToolDef{
				Name:        "agentboard_create_team",
				Description: "Create a new team. Requires admin. Reserved slugs (all, admins, agents, here) are refused.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"slug":         map[string]string{"type": "string", "description": "Lowercase slug; same grammar as a username"},
						"display_name": map[string]string{"type": "string", "description": "Optional pretty name"},
						"description":  map[string]string{"type": "string", "description": "Optional one-liner"},
						"members": map[string]interface{}{
							"type":        "array",
							"items":       map[string]string{"type": "string"},
							"description": "Optional initial member usernames",
						},
					},
					"required": []string{"slug"},
				},
			},
			ToolDef{
				Name:        "agentboard_delete_team",
				Description: "Delete a team and its members. Requires admin. Assignees on kanban rows that reference the deleted team will render as unknown.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"slug": map[string]string{"type": "string", "description": "Team slug to delete"},
					},
					"required": []string{"slug"},
				},
			},
			ToolDef{
				Name:        "agentboard_add_team_member",
				Description: "Add a user to a team. Requires admin. Idempotent — re-adding updates the role.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"slug":     map[string]string{"type": "string", "description": "Team slug"},
						"username": map[string]string{"type": "string", "description": "Existing username to add"},
						"role":     map[string]string{"type": "string", "description": "Optional role tag (lead, rotation, ...)"},
					},
					"required": []string{"slug", "username"},
				},
			},
			ToolDef{
				Name:        "agentboard_remove_team_member",
				Description: "Remove a user from a team. Requires admin.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"slug":     map[string]string{"type": "string", "description": "Team slug"},
						"username": map[string]string{"type": "string", "description": "Username to remove"},
					},
					"required": []string{"slug", "username"},
				},
			},
		)
	}

	return tools
}

func (s *Server) handleToolCall(params json.RawMessage) (interface{}, *RPCError) {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params"}
	}

	var args map[string]json.RawMessage
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid arguments"}
	}

	switch call.Name {
	case "agentboard_set":
		return s.toolSet(args)
	case "agentboard_merge":
		return s.toolMerge(args)
	case "agentboard_append":
		return s.toolAppend(args)
	case "agentboard_delete":
		return s.toolDelete(args)
	case "agentboard_get":
		return s.toolGet(args)
	case "agentboard_list_keys":
		return s.toolListKeys()
	case "agentboard_list_pages":
		return s.toolListPages()
	case "agentboard_read_page":
		return s.toolReadPage(args)
	case "agentboard_write_page":
		return s.toolWritePage(args)
	case "agentboard_delete_page":
		return s.toolDeletePage(args)
	case "agentboard_list_components":
		return s.toolListComponents()
	case "agentboard_read_component":
		return s.toolReadComponent(args)
	case "agentboard_get_data_schema":
		return s.toolGetSchema()
	case "agentboard_write_component":
		return s.toolWriteComponent(args)
	case "agentboard_delete_component":
		return s.toolDeleteComponent(args)
	case "agentboard_write_file":
		return s.toolWriteFile(args)
	case "agentboard_list_files":
		return s.toolListFiles()
	case "agentboard_delete_file":
		return s.toolDeleteFile(args)
	case "agentboard_list_skills":
		return s.toolListSkills()
	case "agentboard_get_skill":
		return s.toolGetSkill(args)
	case "agentboard_list_errors":
		return s.toolListErrors()
	case "agentboard_clear_errors":
		return s.toolClearErrors(args)
	case "agentboard_grab":
		return s.toolGrab(args)
	case "agentboard_search":
		return s.toolSearch(args)
	case "agentboard_list_webhooks":
		return s.toolListWebhooks()
	case "agentboard_create_webhook":
		return s.toolCreateWebhook(args)
	case "agentboard_revoke_webhook":
		return s.toolRevokeWebhook(args)
	case "agentboard_fire_event":
		return s.toolFireEvent(args)
	case "agentboard_list_teams":
		return s.toolListTeams()
	case "agentboard_get_team":
		return s.toolGetTeam(args)
	case "agentboard_create_team":
		return s.toolCreateTeam(args)
	case "agentboard_delete_team":
		return s.toolDeleteTeam(args)
	case "agentboard_add_team_member":
		return s.toolAddTeamMember(args)
	case "agentboard_remove_team_member":
		return s.toolRemoveTeamMember(args)
	default:
		return nil, &RPCError{Code: -32601, Message: fmt.Sprintf("Unknown tool: %s", call.Name)}
	}
}

func (s *Server) toolWriteComponent(args map[string]json.RawMessage) (interface{}, *RPCError) {
	if !s.AllowComponentUpload {
		return nil, &RPCError{Code: -32000, Message: "component upload is disabled; start the server with --allow-component-upload"}
	}
	name := getString(args, "name")
	source := getString(args, "source")
	if name == "" || source == "" {
		return nil, &RPCError{Code: -32602, Message: "name and source required"}
	}
	if err := s.Components.WriteComponent(name, source); err != nil {
		code := -32000
		if errors.Is(err, components.ErrInvalidComponentName) ||
			errors.Is(err, components.ErrComponentTooLarge) ||
			errors.Is(err, components.ErrBuiltinComponent) {
			code = -32602
		}
		return nil, &RPCError{Code: code, Message: err.Error()}
	}
	return mcpContent(fmt.Sprintf("Component %s written successfully", name)), nil
}

func (s *Server) toolWriteFile(args map[string]json.RawMessage) (interface{}, *RPCError) {
	name := getString(args, "name")
	content := getString(args, "content_base64")
	if name == "" || content == "" {
		return nil, &RPCError{Code: -32602, Message: "name and content_base64 required"}
	}
	raw, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, &RPCError{Code: -32602, Message: "content_base64 is not valid base64"}
	}
	info, err := s.Files.Write(name, bytes.NewReader(raw))
	if err != nil {
		code := -32000
		if errors.Is(err, files.ErrInvalidName) || errors.Is(err, files.ErrTooLarge) {
			code = -32602
		}
		return nil, &RPCError{Code: code, Message: err.Error()}
	}
	pretty, _ := json.MarshalIndent(info, "", "  ")
	return mcpContent(string(pretty)), nil
}

func (s *Server) toolListFiles() (interface{}, *RPCError) {
	list, err := s.Files.List()
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	pretty, _ := json.MarshalIndent(list, "", "  ")
	return mcpContent(string(pretty)), nil
}

func (s *Server) toolDeleteFile(args map[string]json.RawMessage) (interface{}, *RPCError) {
	name := getString(args, "name")
	if name == "" {
		return nil, &RPCError{Code: -32602, Message: "name required"}
	}
	if err := s.Files.Delete(name); err != nil {
		code := -32000
		if errors.Is(err, files.ErrInvalidName) || errors.Is(err, files.ErrNotFound) {
			code = -32602
		}
		return nil, &RPCError{Code: code, Message: err.Error()}
	}
	return mcpContent(fmt.Sprintf("File %s deleted", name)), nil
}

func (s *Server) toolListSkills() (interface{}, *RPCError) {
	list, err := s.Files.ListSkills()
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	if len(list) == 0 {
		return mcpContent("No skills found. Hosts skills by writing SKILL.md (YAML frontmatter with `name` and `description`) into content/skills/<slug>/ via PUT /api/files/skills/<slug>/SKILL.md."), nil
	}
	pretty, _ := json.MarshalIndent(list, "", "  ")
	return mcpContent(string(pretty)), nil
}

func (s *Server) toolGetSkill(args map[string]json.RawMessage) (interface{}, *RPCError) {
	slug := getString(args, "slug")
	if slug == "" {
		return nil, &RPCError{Code: -32602, Message: "slug required"}
	}
	bundle, err := s.Files.GetSkill(slug)
	if err != nil {
		code := -32000
		if errors.Is(err, files.ErrInvalidName) || errors.Is(err, files.ErrSkillNotFound) {
			code = -32602
		}
		return nil, &RPCError{Code: code, Message: err.Error()}
	}
	pretty, _ := json.MarshalIndent(bundle, "", "  ")
	return mcpContent(string(pretty)), nil
}

func (s *Server) toolListErrors() (interface{}, *RPCError) {
	if s.Errors == nil {
		return nil, &RPCError{Code: -32000, Message: "errors buffer not configured"}
	}
	entries := s.Errors.List()
	if len(entries) == 0 {
		return mcpContent("No render errors recorded."), nil
	}
	pretty, _ := json.MarshalIndent(entries, "", "  ")
	return mcpContent(string(pretty)), nil
}

func (s *Server) toolSearch(args map[string]json.RawMessage) (interface{}, *RPCError) {
	if s.Search == nil {
		// Search unavailable (FTS5 not built into the sqlite driver, or the
		// store doesn't expose a *sql.DB). Return an empty result rather
		// than erroring — callers can treat this as "nothing found."
		return map[string]any{"hits": []any{}}, nil
	}
	q := getString(args, "q")
	if q == "" {
		return nil, &RPCError{Code: -32602, Message: "q is required"}
	}
	limit := 20
	if raw, ok := args["limit"]; ok {
		var n int
		if err := json.Unmarshal(raw, &n); err == nil && n > 0 {
			limit = n
		}
	}
	hits, err := s.Search.Query(q, limit)
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	return map[string]any{"hits": hits}, nil
}

func (s *Server) toolGrab(args map[string]json.RawMessage) (interface{}, *RPCError) {
	if s.Grab == nil {
		return nil, &RPCError{Code: -32000, Message: "grab materializer not configured"}
	}
	var picks []grab.Pick
	if raw, ok := args["picks"]; ok {
		if err := json.Unmarshal(raw, &picks); err != nil {
			return nil, &RPCError{Code: -32602, Message: "picks must be an array of {page, card_id}"}
		}
	}
	if len(picks) == 0 {
		return nil, &RPCError{Code: -32602, Message: "at least one pick is required"}
	}
	format := grab.Format(getString(args, "format"))
	if format != grab.FormatXML && format != grab.FormatJSON {
		format = grab.FormatMarkdown
	}
	sections := s.Grab.Materialize(picks)
	text := grab.Render(sections, format)
	return mcpContent(text), nil
}

func (s *Server) toolClearErrors(args map[string]json.RawMessage) (interface{}, *RPCError) {
	if s.Errors == nil {
		return nil, &RPCError{Code: -32000, Message: "errors buffer not configured"}
	}
	key := getString(args, "key")
	if key != "" {
		if ok := s.Errors.ClearByKey(key); !ok {
			return nil, &RPCError{Code: -32602, Message: fmt.Sprintf("no error with key %s", key)}
		}
		return mcpContent(fmt.Sprintf("Cleared error %s.", key)), nil
	}
	n := s.Errors.Clear()
	return mcpContent(fmt.Sprintf("Cleared %d errors.", n)), nil
}

func (s *Server) toolDeleteComponent(args map[string]json.RawMessage) (interface{}, *RPCError) {
	if !s.AllowComponentUpload {
		return nil, &RPCError{Code: -32000, Message: "component upload is disabled; start the server with --allow-component-upload"}
	}
	name := getString(args, "name")
	if name == "" {
		return nil, &RPCError{Code: -32602, Message: "name required"}
	}
	if err := s.Components.DeleteComponent(name); err != nil {
		code := -32000
		if errors.Is(err, components.ErrInvalidComponentName) ||
			errors.Is(err, components.ErrBuiltinComponent) ||
			errors.Is(err, components.ErrComponentNotFound) {
			code = -32602
		}
		return nil, &RPCError{Code: code, Message: err.Error()}
	}
	return mcpContent(fmt.Sprintf("Component %s deleted", name)), nil
}

func getString(args map[string]json.RawMessage, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	var s string
	json.Unmarshal(v, &s)
	return s
}

func mcpContent(text string) interface{} {
	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}
}

func (s *Server) toolSet(args map[string]json.RawMessage) (interface{}, *RPCError) {
	key := getString(args, "key")
	value := args["value"]
	if key == "" || value == nil {
		return nil, &RPCError{Code: -32602, Message: "key and value required"}
	}
	if err := s.Store.Set(key, value, "mcp"); err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	return mcpContent(fmt.Sprintf("Set %s successfully", key)), nil
}

func (s *Server) toolMerge(args map[string]json.RawMessage) (interface{}, *RPCError) {
	key := getString(args, "key")
	value := args["value"]
	if key == "" || value == nil {
		return nil, &RPCError{Code: -32602, Message: "key and value required"}
	}
	if err := s.Store.Merge(key, value, "mcp"); err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	return mcpContent(fmt.Sprintf("Merged into %s successfully", key)), nil
}

func (s *Server) toolAppend(args map[string]json.RawMessage) (interface{}, *RPCError) {
	key := getString(args, "key")
	item := args["item"]
	if key == "" || item == nil {
		return nil, &RPCError{Code: -32602, Message: "key and item required"}
	}
	if err := s.Store.Append(key, item, "mcp"); err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	return mcpContent(fmt.Sprintf("Appended to %s successfully", key)), nil
}

func (s *Server) toolDelete(args map[string]json.RawMessage) (interface{}, *RPCError) {
	key := getString(args, "key")
	if key == "" {
		return nil, &RPCError{Code: -32602, Message: "key required"}
	}
	if err := s.Store.Delete(key, "mcp"); err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	return mcpContent(fmt.Sprintf("Deleted %s", key)), nil
}

func (s *Server) toolGet(args map[string]json.RawMessage) (interface{}, *RPCError) {
	key := getString(args, "key")
	if key == "" {
		return nil, &RPCError{Code: -32602, Message: "key required"}
	}
	value, err := s.Store.Get(key)
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	if value == nil {
		return mcpContent(fmt.Sprintf("Key %s not found", key)), nil
	}
	pretty, _ := json.MarshalIndent(json.RawMessage(value), "", "  ")
	return mcpContent(string(pretty)), nil
}

func (s *Server) toolListKeys() (interface{}, *RPCError) {
	all, err := s.Store.GetAll("", nil)
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}

	type keyInfo struct {
		Key  string `json:"key"`
		Type string `json:"type"`
	}
	keys := make([]keyInfo, 0)
	for k, v := range all {
		var val interface{}
		json.Unmarshal(v, &val)
		t := "unknown"
		switch val.(type) {
		case float64:
			t = "number"
		case string:
			t = "string"
		case bool:
			t = "boolean"
		case []interface{}:
			t = "array"
		case map[string]interface{}:
			t = "object"
		}
		keys = append(keys, keyInfo{Key: k, Type: t})
	}

	pretty, _ := json.MarshalIndent(keys, "", "  ")
	return mcpContent(string(pretty)), nil
}

func (s *Server) toolListPages() (interface{}, *RPCError) {
	pages := s.Pages.ListPages()
	pretty, _ := json.MarshalIndent(pages, "", "  ")
	return mcpContent(string(pretty)), nil
}

func (s *Server) toolReadPage(args map[string]json.RawMessage) (interface{}, *RPCError) {
	path := getString(args, "path")
	if path == "" {
		return nil, &RPCError{Code: -32602, Message: "path required"}
	}
	page := s.Pages.GetPage(path)
	if page == nil {
		return mcpContent(fmt.Sprintf("Page %s not found", path)), nil
	}
	return mcpContent(page.Source), nil
}

func (s *Server) toolWritePage(args map[string]json.RawMessage) (interface{}, *RPCError) {
	path := getString(args, "path")
	source := getString(args, "source")
	if path == "" || source == "" {
		return nil, &RPCError{Code: -32602, Message: "path and source required"}
	}
	if err := s.Pages.WritePage(path, source); err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	return mcpContent(fmt.Sprintf("Page %s written successfully", path)), nil
}

func (s *Server) toolDeletePage(args map[string]json.RawMessage) (interface{}, *RPCError) {
	path := getString(args, "path")
	if path == "" {
		return nil, &RPCError{Code: -32602, Message: "path required"}
	}
	if err := s.Pages.DeletePage(path); err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	return mcpContent(fmt.Sprintf("Page %s deleted", path)), nil
}

func (s *Server) toolListComponents() (interface{}, *RPCError) {
	comps := s.Components.ListComponents()
	pretty, _ := json.MarshalIndent(comps, "", "  ")
	return mcpContent(string(pretty)), nil
}

func (s *Server) toolReadComponent(args map[string]json.RawMessage) (interface{}, *RPCError) {
	name := getString(args, "name")
	if name == "" {
		return nil, &RPCError{Code: -32602, Message: "name required"}
	}
	source := s.Components.GetComponentSource(name)
	if source == "" {
		return mcpContent(fmt.Sprintf("Component %s not found or is a built-in", name)), nil
	}
	return mcpContent(source), nil
}

func (s *Server) toolGetSchema() (interface{}, *RPCError) {
	schema, err := s.Store.InferSchema()
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	pretty, _ := json.MarshalIndent(schema, "", "  ")
	return mcpContent(string(pretty)), nil
}
