package mcp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/christophermarx/agentboard/internal/components"
	"github.com/christophermarx/agentboard/internal/files"
	"github.com/christophermarx/agentboard/internal/grab"
	"github.com/christophermarx/agentboard/internal/mdx"
)

func (s *Server) toolDefinitions() []ToolDef {
	tools := []ToolDef{
		// Legacy KV tools (agentboard_set, _merge, _append, _delete,
		// _get, _list_keys, _get_data_schema, _upsert_by_id,
		// _merge_by_id, _delete_by_id, _get_by_id) were removed in
		// Cut 1 of the rewrite. The agentboard_* family is the
		// data surface; Cut 3 collapses it back into the unprefixed
		// names.
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
			Name:        "agentboard_patch_page",
			Description: "Merge into an existing page's YAML frontmatter and/or replace its body, without touching the rest. Use this to flip a kanban card's column, stamp a `shipped` date, or rewrite prose without re-emitting the structured fields. Frontmatter merge is shallow + RFC-7396: a `null` value deletes the key; missing keys are preserved. Use `agentboard_write_page` for full replacement.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":              map[string]string{"type": "string", "description": "Page path (e.g. tasks/ship-v2)"},
					"frontmatter_patch": map[string]interface{}{"type": "object", "description": "Top-level keys to merge into the page's frontmatter. null deletes the key."},
					"body":              map[string]string{"type": "string", "description": "Optional. Replaces the page body verbatim. Omit to leave the body untouched."},
				},
				"required": []string{"path"},
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
		// agentboard_get_data_schema removed in Cut 1 — schema lookup
		// happens via agentboard_index now (every catalog entry
		// includes its inferred type).
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
			Name:        "agentboard_search_pages",
			Description: "Full-text search restricted to dashboard pages. Returns ranked hits with path, title, and a short snippet highlighting the match. For a unified search across pages + store data + components, use agentboard_search.",
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

	// Page-lock tools — admin-only. Non-admins will get a structured
	// error from the handler before the store is touched. The HTTP
	// handler at /api/locks applies the same gate as belt + suspenders.
	if s.Locks != nil {
		tools = append(tools,
			ToolDef{
				Name:        "agentboard_lock_page",
				Description: "Lock a page so non-admins cannot edit it. Requires admin. Locked pages render normally but reject PUT/DELETE from members/bots.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path":   map[string]string{"type": "string", "description": "Page path (with or without leading slash, with or without .md)"},
						"reason": map[string]string{"type": "string", "description": "Optional — shown to non-admins trying to edit"},
					},
					"required": []string{"path"},
				},
			},
			ToolDef{
				Name:        "agentboard_unlock_page",
				Description: "Remove a page's lock. Requires admin.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]string{"type": "string", "description": "Page path"},
					},
					"required": []string{"path"},
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

	tools = append(tools, s.storeToolDefs()...)

	return tools
}

func (s *Server) handleToolCall(r *http.Request, params json.RawMessage) (interface{}, *RPCError) {
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

	if result, err, ok := s.dispatchStore(call.Name, args); ok {
		return result, err
	}

	switch call.Name {
	// Legacy KV tools removed in Cut 1 of the rewrite. Callers using
	// agentboard_set / _merge / _append / _delete / _get / _list_keys /
	// _get_data_schema fall through to the unknown-tool branch below
	// and get a clear error.
	case "agentboard_list_pages":
		return s.toolListPages()
	case "agentboard_read_page":
		return s.toolReadPage(args)
	case "agentboard_write_page":
		return s.toolWritePage(args)
	case "agentboard_patch_page":
		return s.toolPatchPage(args)
	case "agentboard_delete_page":
		return s.toolDeletePage(args)
	case "agentboard_list_components":
		return s.toolListComponents()
	case "agentboard_read_component":
		return s.toolReadComponent(args)
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
	case "agentboard_search_pages":
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
	case "agentboard_lock_page":
		return s.toolLockPage(r, args)
	case "agentboard_unlock_page":
		return s.toolUnlockPage(r, args)
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

// toolSet / toolMerge / toolAppend / toolDelete / toolGet / toolListKeys
// were the legacy KV implementations. Cut 1 of the rewrite removed
// them along with the Store: data.DataStore field. The agentboard_*
// tools cover the same surface against the files-first store; Cut 3
// drops the v2 prefix.

func (s *Server) toolListPages() (interface{}, *RPCError) {
	pages := s.Pages.ListPages()
	// Decorate with lock metadata if the locks store is wired. Keeps
	// pages that are normal undecorated so the output stays small.
	type pageWithLock struct {
		Path       string `json:"path"`
		File       string `json:"file"`
		Title      string `json:"title,omitempty"`
		Order      int    `json:"order,omitempty"`
		Locked     bool   `json:"locked,omitempty"`
		LockedBy   string `json:"locked_by,omitempty"`
		LockedAt   string `json:"locked_at,omitempty"`
		LockReason string `json:"lock_reason,omitempty"`
	}
	out := make([]pageWithLock, 0, len(pages))
	for _, p := range pages {
		row := pageWithLock{Path: p.Path, File: p.File, Title: p.Title, Order: p.Order}
		if s.Locks != nil {
			normalized := p.Path
			if len(normalized) > 0 && normalized[0] == '/' {
				normalized = normalized[1:]
			}
			if l, err := s.Locks.Get(normalized); err == nil && l != nil {
				row.Locked = true
				row.LockedBy = l.LockedBy
				row.LockedAt = l.LockedAt.Format("2006-01-02T15:04:05Z07:00")
				row.LockReason = l.Reason
			}
		}
		out = append(out, row)
	}
	pretty, _ := json.MarshalIndent(out, "", "  ")
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

// toolPatchPage merges frontmatter and/or replaces the body of an
// existing page. Mirrors PATCH /api/content/<path>. RFC-7396 semantics:
// null in frontmatter_patch deletes the key; missing keys are
// preserved. Body field replaces the whole body when set; omitted
// means body stays as-is.
func (s *Server) toolPatchPage(args map[string]json.RawMessage) (interface{}, *RPCError) {
	path := getString(args, "path")
	if path == "" {
		return nil, &RPCError{Code: -32602, Message: "path required"}
	}

	var fmPatch map[string]any
	if raw, ok := args["frontmatter_patch"]; ok {
		if err := json.Unmarshal(raw, &fmPatch); err != nil {
			return nil, &RPCError{Code: -32602, Message: "frontmatter_patch must be a JSON object: " + err.Error()}
		}
	}
	var bodyPtr *string
	if raw, ok := args["body"]; ok {
		var b string
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, &RPCError{Code: -32602, Message: "body must be a string: " + err.Error()}
		}
		bodyPtr = &b
	}
	if fmPatch == nil && bodyPtr == nil {
		return nil, &RPCError{Code: -32602, Message: "frontmatter_patch and/or body required"}
	}

	normalizedPath := mdx.NormalizePagePath(path)
	current := s.Pages.GetPage(normalizedPath)
	if current == nil {
		return nil, &RPCError{Code: -32000, Message: "page not found: " + normalizedPath}
	}

	merged := map[string]any{}
	for k, v := range current.Frontmatter {
		merged[k] = v
	}
	for k, v := range fmPatch {
		if v == nil {
			delete(merged, k)
		} else {
			merged[k] = v
		}
	}

	body := current.Source
	if bodyPtr != nil {
		body = *bodyPtr
	}

	newSource, err := mdx.AssemblePageSource(merged, body)
	if err != nil {
		return nil, &RPCError{Code: -32000, Message: "assemble source: " + err.Error()}
	}
	if err := s.Pages.WritePage(path, newSource); err != nil {
		return nil, &RPCError{Code: -32000, Message: err.Error()}
	}
	return mcpContent(fmt.Sprintf("Page %s patched", normalizedPath)), nil
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

// toolGetSchema removed in Cut 1 — schema inference now comes through
// the v2 catalog (every catalog entry has its inferred type alongside
// shape + version). Callers use agentboard_index instead.
