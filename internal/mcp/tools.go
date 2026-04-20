package mcp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/christophermarx/agentboard/internal/components"
	"github.com/christophermarx/agentboard/internal/files"
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
