package server

import (
	"html/template"
	"net/http"
	"os"
	"path/filepath"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/permissions"
)

// /admin/permissions — server-rendered editor for
// .agentboard/permissions.yaml. The rules table renders the parsed
// current state; the textarea is the YAML escape hatch. POSTing the
// textarea validates the YAML, then commits the new body via the
// EditFn (= gitserver.PutFile) the rest of the dashboard's edit form
// uses. The structural admin-only ban on .agentboard/* (§5.2) is
// what keeps members from sneaking changes through here.

const permissionsUIPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Permissions · {{.WorkspaceName}}</title>
<link rel="stylesheet" href="/_static/design-system.css">
<style>
  body{margin:0;padding:1.5rem;font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;color:var(--text);background:var(--bg);max-width:980px;margin-left:auto;margin-right:auto}
  header{display:flex;align-items:baseline;justify-content:space-between;margin-bottom:1.5rem;padding-bottom:.75rem;border-bottom:1px solid var(--border)}
  header h1{margin:0;font-size:1.5rem;letter-spacing:-.01em}
  header a{color:var(--text-secondary);text-decoration:none;font-size:.85rem}
  header a:hover{text-decoration:underline}
  h2{font-size:1rem;text-transform:uppercase;letter-spacing:.05em;color:var(--text-secondary);margin:2rem 0 .85rem;font-weight:500}
  .panel{background:var(--bg-secondary);border:1px solid var(--border);border-radius:var(--ab-radius,8px);padding:1rem 1.25rem;margin-bottom:1rem}
  .notice{background:rgba(34,197,94,.1);border-left:3px solid var(--success);padding:.75rem 1rem;border-radius:6px;font-size:.9rem;margin-bottom:1rem}
  .error{background:rgba(220,38,38,.1);border-left:3px solid var(--error);padding:.75rem 1rem;border-radius:6px;font-size:.9rem;margin-bottom:1rem}
  table{width:100%;border-collapse:collapse;font-size:.9rem}
  th,td{text-align:left;padding:.5rem .65rem;border-bottom:1px solid var(--border);vertical-align:top}
  th{font-size:.75rem;text-transform:uppercase;letter-spacing:.05em;color:var(--text-secondary);font-weight:500}
  tr:last-child td{border-bottom:0}
  td code{background:var(--bg);padding:.1rem .35rem;border-radius:3px;font-size:.85em}
  .pill{display:inline-block;padding:.05rem .5rem;border-radius:9999px;font-size:.7rem;font-weight:500;background:var(--bg);border:1px solid var(--border);margin-right:.25rem}
  .pill.admins{background:rgba(220,38,38,.1);color:var(--error);border-color:transparent}
  form.yaml-edit{display:flex;flex-direction:column;gap:.75rem}
  form.yaml-edit textarea{width:100%;min-height:24rem;padding:.85rem;
    font-family:var(--ab-mono,monospace);font-size:.85rem;line-height:1.45;
    border:1px solid var(--border);border-radius:6px;
    background:var(--bg);color:var(--text);resize:vertical}
  form.yaml-edit textarea:focus{outline:none;border-color:var(--accent);box-shadow:0 0 0 3px var(--accent-light)}
  form.yaml-edit .actions{display:flex;gap:.5rem;justify-content:flex-end}
  form.yaml-edit button{padding:.55rem 1.25rem;background:var(--accent);color:#fff;border:0;border-radius:6px;font-weight:500;cursor:pointer}
  .empty{color:var(--text-secondary);font-style:italic;padding:1rem 0}
  .ab-muted{color:var(--text-secondary);font-size:.85rem}
</style>
</head>
<body>
<header>
  <h1>Permissions</h1>
  <span><a href="/admin">← back to admin</a> · @{{.User}}</span>
</header>

{{if .Notice}}<div class="notice">{{.Notice}}</div>{{end}}
{{if .Error}}<div class="error"><strong>Save failed:</strong> {{.Error}}</div>{{end}}

<p class="ab-muted">
  Permission rules in <code>.agentboard/permissions.yaml</code> restrict
  writes to specific paths. The file is open-by-default — every workspace
  member can write everywhere unless a rule explicitly narrows access.
  Writes to <code>.agentboard/permissions.yaml</code> itself are always
  admin-only, regardless of what the YAML says.
</p>

<h2>Current rules ({{len .Rules}})</h2>
{{if .Rules}}
<div class="panel" style="padding:.25rem 0">
  <table>
    <thead><tr><th>Order</th><th>Paths</th><th>Allowed writers</th></tr></thead>
    <tbody>
    {{range $i, $rule := .Rules}}
      <tr>
        <td><code>{{$i}}</code></td>
        <td>
          {{range $rule.Paths}}<code>{{.}}</code><br>{{end}}
        </td>
        <td>
          {{range $rule.Write}}<span class="pill{{if eq . "admins"}} admins{{end}}">{{.}}</span>{{end}}
        </td>
      </tr>
    {{end}}
    </tbody>
  </table>
</div>
{{else}}
<div class="panel"><p class="empty">No rules — workspace is fully open to every member. Add rules below to restrict specific paths.</p></div>
{{end}}

<h2>Edit YAML</h2>
<div class="panel">
  <form class="yaml-edit" method="post" action="/admin/permissions/save">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <textarea name="body" spellcheck="false">{{.Source}}</textarea>
    <p class="ab-muted">
      Schema: <code>version: 1</code> at the top, then a
      <code>rules:</code> list. Each rule has <code>paths:</code>
      (doublestar globs) + <code>write:</code> (group names).
      <code>admins</code> is the synthetic group of every admin-kind user.
    </p>
    <div class="actions">
      <a class="ab-muted" href="/admin" style="padding:.55rem 1.25rem;align-self:center">Cancel</a>
      <button type="submit">Save rules</button>
    </div>
  </form>
</div>

</body>
</html>`

var permissionsUITemplate = template.Must(template.New("perm-ui").Parse(permissionsUIPage))

type permissionsUIData struct {
	WorkspaceName string
	User          string
	CSRF          string
	Source        string
	Rules         []permissions.Rule
	Notice        string
	Error         string
}

func (s *Server) handleAdminPermissionsPage(w http.ResponseWriter, r *http.Request) {
	notice := ""
	if r.URL.Query().Get("saved") != "" {
		notice = "Permissions saved. Changes are live on the next push or edit."
	}
	s.renderPermissionsPage(w, r, notice, "")
}

func (s *Server) renderPermissionsPage(w http.ResponseWriter, r *http.Request, notice, errMsg string) {
	user := auth.UserFromContext(r.Context())
	username := ""
	if user != nil {
		username = user.Username
	}
	csrf := ""
	if c, err := r.Cookie(auth.CSRFCookieName); err == nil {
		csrf = c.Value
	}
	source, rules := s.loadPermissionsSourceAndParsed()

	data := permissionsUIData{
		WorkspaceName: "AgentBoard",
		User:          username,
		CSRF:          csrf,
		Source:        source,
		Rules:         rules,
		Notice:        notice,
		Error:         errMsg,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = permissionsUITemplate.Execute(w, data)
}

// loadPermissionsSourceAndParsed reads .agentboard/permissions.yaml
// from the dogfood worktree mirror and returns (raw body, parsed
// rules). Errors silently degrade to empty strings + empty rules —
// the editor still renders so the operator can recover.
func (s *Server) loadPermissionsSourceAndParsed() (string, []permissions.Rule) {
	if s.Project == nil {
		return "", nil
	}
	full := filepath.Join(s.Project.Path, ".agentboard", "worktrees", "dogfood", permissions.PermissionsFilePath)
	body, err := os.ReadFile(full)
	if err != nil {
		return "", nil
	}
	r, err := permissions.Parse(body)
	if err != nil {
		return string(body), nil
	}
	return string(body), r.Rules
}

func (s *Server) handleAdminPermissionsSave(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdminCSRF(w, r) {
		return
	}
	if s.EditFn == nil {
		s.renderPermissionsPage(w, r, "", "edit not configured")
		return
	}
	body := r.FormValue("body")
	// Validate by parsing before committing. The structural admin-only
	// invariant means this file path is gated by the WriteCheck on
	// EditFn's downstream — but we don't need to short-circuit; admin
	// callers pass through anyway.
	if _, err := permissions.Parse([]byte(body)); err != nil {
		s.renderPermissionsPage(w, r, "", "Invalid YAML: "+err.Error())
		return
	}
	actor := resolveActor(r)
	msg := "Update permissions"
	if err := s.EditFn(r.Context(), "dogfood", permissions.PermissionsFilePath, body, actor, msg); err != nil {
		s.renderPermissionsPage(w, r, "", err.Error())
		return
	}
	// PRG: redirect back to GET so reloads don't re-POST.
	http.Redirect(w, r, "/admin/permissions?saved=1", http.StatusFound)
}

