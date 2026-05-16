package server

import (
	"fmt"
	"html/template"
	"net/http"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/go-chi/chi/v5"
)

// /me — server-rendered "your account" page for any signed-in user.
// Unlike /admin (which manages everybody), /me only exposes
// self-managing surfaces: list tokens, mint a fresh one, revoke one.
// Mounted in the same group as /admin so anonymous → /login redirect.
//
// Routes:
//   GET  /me                                      — landing page
//   POST /me/tokens/new                           — mint a personal token
//   POST /me/tokens/{id}/revoke                   — revoke a personal token

const meUIPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Your account · {{.WorkspaceName}}</title>
<link rel="stylesheet" href="/_static/design-system.css">
<style>
  body{margin:0;padding:1.5rem;font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;color:var(--text);background:var(--bg);max-width:760px;margin-left:auto;margin-right:auto}
  header{display:flex;align-items:baseline;justify-content:space-between;margin-bottom:1.5rem;padding-bottom:.75rem;border-bottom:1px solid var(--border)}
  header h1{margin:0;font-size:1.5rem;letter-spacing:-.01em}
  header a{color:var(--text-secondary);text-decoration:none;font-size:.85rem}
  header a:hover{text-decoration:underline}
  h2{font-size:1rem;text-transform:uppercase;letter-spacing:.05em;color:var(--text-secondary);margin:2rem 0 .85rem;font-weight:500}
  .panel{background:var(--bg-secondary);border:1px solid var(--border);border-radius:var(--ab-radius,8px);padding:1rem 1.25rem;margin-bottom:1rem}
  .me-row{display:flex;align-items:center;gap:1rem;font-size:.95rem}
  .me-row strong{color:var(--text)}
  .me-row .kind{display:inline-block;padding:.05rem .5rem;border-radius:9999px;font-size:.7rem;font-weight:500;background:var(--accent-light);color:var(--accent)}
  .me-row .kind.admin{background:rgba(220,38,38,.1);color:var(--error)}
  .me-row .kind.bot{background:rgba(168,85,247,.12);color:#a855f7}
  table{width:100%;border-collapse:collapse;font-size:.9rem}
  th,td{text-align:left;padding:.5rem .65rem;border-bottom:1px solid var(--border)}
  th{font-size:.75rem;text-transform:uppercase;letter-spacing:.05em;color:var(--text-secondary);font-weight:500}
  tr:last-child td{border-bottom:0}
  .new-token{background:rgba(34,197,94,.10);border:1px solid transparent;border-left:3px solid var(--success);padding:.85rem 1rem;border-radius:6px;margin-bottom:1rem;word-break:break-all;font-family:var(--ab-mono,monospace);font-size:.85rem}
  .new-token .label{font-family:system-ui;color:var(--success);font-weight:500;margin-bottom:.25rem;font-size:.8rem;text-transform:uppercase;letter-spacing:.05em}
  form.new-tok{display:flex;gap:.5rem;align-items:flex-end;flex-wrap:wrap}
  form.new-tok label{font-size:.75rem;color:var(--text-secondary);text-transform:uppercase;letter-spacing:.05em;display:flex;flex-direction:column;gap:.3rem}
  form.new-tok input{padding:.45rem .65rem;border:1px solid var(--border);border-radius:6px;background:var(--bg);color:var(--text);font-size:.9rem;min-width:14rem}
  form.new-tok button{padding:.5rem 1.25rem;background:var(--accent);color:#fff;border:0;border-radius:6px;font-weight:500;cursor:pointer}
  form.inline{display:inline}
  form.inline button{background:none;border:0;color:var(--accent);cursor:pointer;font:inherit;padding:0;font-size:.85rem}
  form.inline button:hover{text-decoration:underline}
  .empty{color:var(--text-secondary);font-style:italic;padding:1rem 0}
  .help{font-size:.85rem;color:var(--text-secondary);margin:.5rem 0 0}
  .help code{background:var(--bg);padding:.1rem .3rem;border-radius:3px}
</style>
</head>
<body>
<header>
  <h1>Your account</h1>
  <span><a href="/">← back to workspace</a> · @{{.Username}} · <a href="/logout">sign out</a></span>
</header>

<div class="panel">
  <div class="me-row">
    <strong>@{{.Username}}</strong>
    <span class="kind {{.Kind}}">{{.Kind}}</span>
    {{if .DisplayName}}<span class="ab-muted">{{.DisplayName}}</span>{{end}}
  </div>
</div>

{{if .NewToken}}
<div class="new-token">
  <div class="label">New token — copy it now, it won't show again</div>
  {{.NewToken}}
</div>
{{end}}

<h2>Tokens</h2>
<div class="panel">
  <p class="help">
    Tokens authenticate you for git (<code>http://_:&lt;token&gt;@{{.Host}}/git/dogfood.git</code>),
    REST (<code>Authorization: Bearer &lt;token&gt;</code>), and MCP.
    One per device or context (laptop, CI, an agent runtime). You can
    revoke any token without affecting the others.
  </p>
  <form class="new-tok" method="post" action="/me/tokens/new" style="margin-top:1rem">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <label>Label
      <input type="text" name="label" placeholder="laptop, mcp-agent, ci-…" required>
    </label>
    <button type="submit">Create token</button>
  </form>
</div>

{{if .Tokens}}
<div class="panel" style="padding:.25rem 0">
  <table>
    <thead><tr><th>Label</th><th>Created</th><th>Last used</th><th></th></tr></thead>
    <tbody>
    {{range .Tokens}}
      <tr>
        <td>{{if .Label}}{{.Label}}{{else}}<span class="empty">—</span>{{end}}</td>
        <td><span class="ab-muted">{{.CreatedAt}}</span></td>
        <td><span class="ab-muted">{{if .LastUsedAt}}{{.LastUsedAt}}{{else}}never{{end}}</span></td>
        <td style="text-align:right">
          <form class="inline" method="post" action="/me/tokens/{{.ID}}/revoke" onsubmit="return confirm('Revoke this token? Anything using it will get 401.')">
            <input type="hidden" name="csrf" value="{{$.CSRF}}">
            <button type="submit">revoke</button>
          </form>
        </td>
      </tr>
    {{end}}
    </tbody>
  </table>
</div>
{{else}}
<div class="panel"><p class="empty">You don't have any tokens yet. Create one above.</p></div>
{{end}}

<h2>Connect from a terminal</h2>
<div class="panel">
  <p class="help">Once you've minted a token above, wire your working
     directory to this workspace:</p>
  <pre style="background:var(--bg);padding:.85rem 1rem;border-radius:6px;font-family:var(--ab-mono,monospace);font-size:.85rem;overflow-x:auto;margin:.5rem 0 0"><code>git init -q
git remote add origin http://_:&lt;token&gt;@{{.Host}}/git/dogfood.git
git fetch -q origin
git checkout -B main origin/main</code></pre>
</div>

</body>
</html>`

var meUITemplate = template.Must(template.New("me-ui").Parse(meUIPage))

type meUIToken struct {
	ID         string
	Label      string
	CreatedAt  string
	LastUsedAt string
}

type meUIData struct {
	WorkspaceName string
	Username      string
	Kind          string
	DisplayName   string
	Host          string
	CSRF          string
	Tokens        []meUIToken
	NewToken      string
}

func (s *Server) registerMeUIRoutes(r chi.Router) {
	r.Get("/me", s.handleMeHome)
	r.Post("/me/tokens/new", s.handleMeCreateToken)
	r.Post("/me/tokens/{id}/revoke", s.handleMeRevokeToken)
}

func (s *Server) handleMeHome(w http.ResponseWriter, r *http.Request) {
	s.renderMeHome(w, r, "")
}

func (s *Server) renderMeHome(w http.ResponseWriter, r *http.Request, newToken string) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login?next=/me", http.StatusFound)
		return
	}
	csrf := ""
	if c, err := r.Cookie(auth.CSRFCookieName); err == nil {
		csrf = c.Value
	}

	var toks []meUIToken
	if s.Auth != nil {
		list, err := s.Auth.ListTokensForUser(user.Username)
		if err == nil {
			for _, t := range list {
				row := meUIToken{
					ID:        t.ID,
					Label:     t.Label,
					CreatedAt: t.CreatedAt.UTC().Format("2006-01-02 15:04"),
				}
				if t.LastUsedAt != nil {
					row.LastUsedAt = t.LastUsedAt.UTC().Format("2006-01-02 15:04")
				}
				toks = append(toks, row)
			}
		}
	}

	data := meUIData{
		WorkspaceName: "AgentBoard",
		Username:      user.Username,
		Kind:          string(user.Kind),
		DisplayName:   user.DisplayName,
		Host:          r.Host,
		CSRF:          csrf,
		Tokens:        toks,
		NewToken:      newToken,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, private")
	_ = meUITemplate.Execute(w, data)
}

func (s *Server) handleMeCreateToken(w http.ResponseWriter, r *http.Request) {
	if !s.checkMeCSRF(w, r) {
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login?next=/me", http.StatusFound)
		return
	}
	label := r.FormValue("label")
	plain, err := auth.GenerateToken()
	if err != nil {
		http.Error(w, "token gen failed", http.StatusInternalServerError)
		return
	}
	if _, err := s.Auth.CreateToken(auth.CreateTokenParams{
		Username:  user.Username,
		TokenHash: auth.HashToken(plain),
		Label:     label,
		CreatedBy: user.Username,
	}); err != nil {
		http.Error(w, "token persist failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.renderMeHome(w, r, plain)
}

func (s *Server) handleMeRevokeToken(w http.ResponseWriter, r *http.Request) {
	if !s.checkMeCSRF(w, r) {
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login?next=/me", http.StatusFound)
		return
	}
	id := chi.URLParam(r, "id")
	// Confirm the token belongs to the signed-in user before revoking.
	tok, err := s.Auth.GetToken(id)
	if err != nil || tok == nil || tok.Username != user.Username {
		http.Error(w, "token not found on your account", http.StatusNotFound)
		return
	}
	if err := s.Auth.RevokeToken(id); err != nil {
		http.Error(w, "revoke failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/me", http.StatusFound)
}

// checkMeCSRF validates the double-submit CSRF token on form-encoded
// /me/* actions. Mirrors checkAdminCSRF.
func (s *Server) checkMeCSRF(w http.ResponseWriter, r *http.Request) bool {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return false
	}
	formCSRF := r.FormValue("csrf")
	cookie, err := r.Cookie(auth.CSRFCookieName)
	if err != nil || cookie.Value == "" || formCSRF == "" || cookie.Value != formCSRF {
		http.Error(w, "CSRF check failed; reload the form and try again", http.StatusForbidden)
		return false
	}
	return true
}

// Suppress unused-import noise from `fmt` until a future helper needs it.
var _ = fmt.Sprintf
