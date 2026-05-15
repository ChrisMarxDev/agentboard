package server

import (
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/invitations"
	"github.com/go-chi/chi/v5"
)

// /admin — server-rendered admin home for human admins. JSON-API
// machinery already exists under /_api/admin/*; this is the UI that
// drives the most-used operation (inviting new users) without
// requiring an admin to know the curl shape.
//
// Routes:
//
//	GET  /admin                                — landing page (users + invites)
//	POST /admin/invitations/new                — mint a new invitation
//	POST /admin/invitations/{id}/revoke        — revoke an invitation
//
// All routes admin-gated upstream; this file assumes the request has
// already passed AdminRequired.

const adminUIPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Admin · {{.WorkspaceName}}</title>
<link rel="stylesheet" href="/_static/design-system.css">
<style>
  body{margin:0;padding:1.5rem;font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;color:var(--text);background:var(--bg);max-width:980px;margin-left:auto;margin-right:auto}
  header{display:flex;align-items:baseline;justify-content:space-between;margin-bottom:1.5rem;padding-bottom:.75rem;border-bottom:1px solid var(--border)}
  header h1{margin:0;font-size:1.5rem;letter-spacing:-.01em}
  header a{color:var(--text-secondary);text-decoration:none;font-size:.85rem}
  header a:hover{text-decoration:underline}
  h2{font-size:1rem;text-transform:uppercase;letter-spacing:.05em;color:var(--text-secondary);margin:2rem 0 .85rem;font-weight:500}
  .panel{background:var(--bg-secondary);border:1px solid var(--border);border-radius:var(--ab-radius,8px);padding:1rem 1.25rem;margin-bottom:1rem}
  .notice{background:var(--accent-light);border:1px solid transparent;border-left:3px solid var(--accent);padding:.75rem 1rem;border-radius:6px;font-size:.9rem;margin-bottom:1rem;word-break:break-all}
  .notice code{background:rgba(0,0,0,.1);padding:.1rem .3rem;border-radius:3px}
  table{width:100%;border-collapse:collapse;font-size:.9rem}
  th,td{text-align:left;padding:.5rem .65rem;border-bottom:1px solid var(--border)}
  th{font-size:.75rem;text-transform:uppercase;letter-spacing:.05em;color:var(--text-secondary);font-weight:500}
  tr:last-child td{border-bottom:0}
  .kind,.status{display:inline-block;padding:.05rem .5rem;border-radius:9999px;font-size:.7rem;font-weight:500;background:var(--bg);border:1px solid var(--border)}
  .kind.admin{background:rgba(220,38,38,.1);color:var(--error);border-color:transparent}
  .kind.member{background:var(--accent-light);color:var(--accent);border-color:transparent}
  .kind.bot{background:rgba(168,85,247,.12);color:#a855f7;border-color:transparent}
  .status.active{background:rgba(34,197,94,.12);color:var(--success);border-color:transparent}
  .status.redeemed{background:var(--bg);color:var(--text-secondary)}
  .status.expired,.status.revoked{background:rgba(245,158,11,.12);color:var(--warning);border-color:transparent}
  form.inline{display:inline}
  form.inline button{background:none;border:0;color:var(--accent);cursor:pointer;font:inherit;padding:0;font-size:.85rem}
  form.inline button:hover{text-decoration:underline}
  form.new-invite{display:grid;grid-template-columns:auto 1fr auto auto;gap:.5rem;align-items:end;margin-top:.5rem}
  form.new-invite label{font-size:.75rem;color:var(--text-secondary);text-transform:uppercase;letter-spacing:.05em;display:flex;flex-direction:column;gap:.3rem}
  form.new-invite select,form.new-invite input{padding:.45rem .65rem;border:1px solid var(--border);border-radius:6px;background:var(--bg);color:var(--text);font-size:.9rem}
  form.new-invite button{padding:.5rem 1.25rem;background:var(--accent);color:#fff;border:0;border-radius:6px;font-weight:500;cursor:pointer}
  .copy-link{font-family:var(--ab-mono,monospace);font-size:.85rem;color:var(--accent);text-decoration:none}
  .empty{color:var(--text-secondary);font-style:italic;padding:1rem 0}
  @media (max-width: 640px){
    form.new-invite{grid-template-columns:1fr 1fr}
    table{display:block;overflow-x:auto}
  }
</style>
</head>
<body>
<header>
  <h1>Admin</h1>
  <span><a href="/">← back to workspace</a> · @{{.User}} · <a href="/logout">sign out</a></span>
</header>

{{if .NewInviteURL}}
<div class="notice">
  <strong>Invitation created.</strong> Send this URL to the person you want to invite:<br>
  <a class="copy-link" href="{{.NewInviteURL}}">{{.NewInviteURL}}</a>
</div>
{{end}}

<h2>Invite a new user</h2>
<div class="panel">
  <form class="new-invite" method="post" action="/admin/invitations/new">
    <input type="hidden" name="csrf" value="{{.CSRF}}">
    <label>Role
      <select name="role">
        <option value="member" selected>member</option>
        <option value="admin">admin</option>
        <option value="bot">bot</option>
      </select>
    </label>
    <label>Label (optional)
      <input type="text" name="label" placeholder="e.g. alice's laptop, ci-bot">
    </label>
    <label>Expires (days)
      <input type="number" name="expires_in_days" value="7" min="1" max="365" style="width:5rem">
    </label>
    <button type="submit">Create invite</button>
  </form>
</div>

<h2>Invitations ({{len .Invitations}})</h2>
{{if .Invitations}}
<div class="panel" style="padding:.25rem 0">
  <table>
    <thead><tr><th>Role</th><th>Label</th><th>Status</th><th>Created</th><th>Expires</th><th>URL / Action</th></tr></thead>
    <tbody>
    {{range .Invitations}}
      <tr>
        <td><span class="kind {{.Role}}">{{.Role}}</span></td>
        <td>{{if .Label}}{{.Label}}{{else}}<span class="empty">—</span>{{end}}</td>
        <td><span class="status {{.Status}}">{{.Status}}</span></td>
        <td><span class="ab-muted">{{.CreatedAt}}</span></td>
        <td><span class="ab-muted">{{.ExpiresAt}}</span></td>
        <td>
          {{if eq .Status "active"}}
            <a class="copy-link" href="/invite/{{.ID}}">/invite/{{.ID}}</a>
            ·
            <form class="inline" method="post" action="/admin/invitations/{{.ID}}/revoke" onsubmit="return confirm('Revoke this invitation?')">
              <input type="hidden" name="csrf" value="{{$.CSRF}}">
              <button type="submit">revoke</button>
            </form>
          {{else if eq .Status "redeemed"}}
            <span class="ab-muted">redeemed by @{{.RedeemedBy}}</span>
          {{else}}
            <span class="ab-muted">{{.Status}}</span>
          {{end}}
        </td>
      </tr>
    {{end}}
    </tbody>
  </table>
</div>
{{else}}
<div class="panel"><p class="empty">No invitations yet.</p></div>
{{end}}

<h2>Users ({{len .Users}})</h2>
{{if .Users}}
<div class="panel" style="padding:.25rem 0">
  <table>
    <thead><tr><th>Username</th><th>Kind</th><th>Display name</th><th>Created</th></tr></thead>
    <tbody>
    {{range .Users}}
      <tr>
        <td>@{{.Username}}</td>
        <td><span class="kind {{.Kind}}">{{.Kind}}</span></td>
        <td>{{if .DisplayName}}{{.DisplayName}}{{else}}<span class="empty">—</span>{{end}}</td>
        <td><span class="ab-muted">{{.CreatedAt}}</span></td>
      </tr>
    {{end}}
    </tbody>
  </table>
</div>
{{end}}

</body>
</html>`

var adminUITemplate = template.Must(template.New("admin-ui").Parse(adminUIPage))

type adminUIInvitation struct {
	ID         string
	Role       string
	Label      string
	Status     string
	CreatedAt  string
	ExpiresAt  string
	RedeemedBy string
}

type adminUIUser struct {
	Username    string
	Kind        string
	DisplayName string
	CreatedAt   string
}

type adminUIData struct {
	WorkspaceName string
	User          string
	CSRF          string
	Invitations   []adminUIInvitation
	Users         []adminUIUser
	NewInviteURL  string
}

// registerAdminUIRoutes wires /admin/* — runs inside the admin-gated
// chain so AdminRequired has already approved the caller.
func (s *Server) registerAdminUIRoutes(r chi.Router) {
	r.Get("/admin", s.handleAdminHome)
	r.Post("/admin/invitations/new", s.handleAdminCreateInvitation)
	r.Post("/admin/invitations/{id}/revoke", s.handleAdminRevokeInvitation)
}

func (s *Server) handleAdminHome(w http.ResponseWriter, r *http.Request) {
	s.renderAdminHome(w, r, "")
}

func (s *Server) renderAdminHome(w http.ResponseWriter, r *http.Request, newInviteURL string) {
	user := auth.UserFromContext(r.Context())
	csrf := ""
	if c, err := r.Cookie(auth.CSRFCookieName); err == nil {
		csrf = c.Value
	}

	var invs []adminUIInvitation
	if s.Invitations != nil {
		list, err := s.Invitations.List(true)
		if err == nil {
			for _, inv := range list {
				invs = append(invs, adminUIInvitation{
					ID:         inv.ID,
					Role:       string(inv.Role),
					Label:      inv.Label,
					Status:     inv.Status(),
					CreatedAt:  inv.CreatedAt.UTC().Format("2006-01-02 15:04"),
					ExpiresAt:  inv.ExpiresAt.UTC().Format("2006-01-02 15:04"),
					RedeemedBy: inv.RedeemedBy,
				})
			}
		}
	}

	var users []adminUIUser
	if s.Auth != nil {
		list, err := s.Auth.ListUsers(true)
		if err == nil {
			for _, u := range list {
				users = append(users, adminUIUser{
					Username:    u.Username,
					Kind:        string(u.Kind),
					DisplayName: u.DisplayName,
					CreatedAt:   u.CreatedAt.UTC().Format("2006-01-02 15:04"),
				})
			}
		}
	}

	data := adminUIData{
		WorkspaceName: "AgentBoard",
		User:          "",
		CSRF:          csrf,
		Invitations:   invs,
		Users:         users,
		NewInviteURL:  newInviteURL,
	}
	if user != nil {
		data.User = user.Username
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = adminUITemplate.Execute(w, data)
}

func (s *Server) handleAdminCreateInvitation(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdminCSRF(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	role := invitations.Role(r.FormValue("role"))
	if !invitations.ValidRole(role) {
		http.Error(w, "role must be admin, member, or bot", http.StatusBadRequest)
		return
	}
	days := 7
	if v := r.FormValue("expires_in_days"); v != "" {
		var n int
		_, _ = fmt.Sscanf(v, "%d", &n)
		if n > 0 && n <= 365 {
			days = n
		}
	}
	caller := auth.UserFromContext(r.Context())
	createdBy := ""
	if caller != nil {
		createdBy = caller.Username
	}
	inv, err := s.Invitations.Create(invitations.CreateParams{
		Role:      role,
		CreatedBy: createdBy,
		ExpiresIn: time.Duration(days) * 24 * time.Hour,
		Label:     r.FormValue("label"),
	})
	if err != nil {
		http.Error(w, "create failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Build a same-origin URL so admins can copy it straight from the
	// success notice. Honour X-Forwarded-Proto for tunnel deploys.
	scheme := "http"
	if requestIsSecure(r) {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	inviteURL := fmt.Sprintf("%s://%s/invite/%s", scheme, r.Host, inv.ID)
	s.renderAdminHome(w, r, inviteURL)
}

func (s *Server) handleAdminRevokeInvitation(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdminCSRF(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.Invitations.Revoke(id); err != nil {
		http.Error(w, "revoke failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusFound)
}

// checkAdminCSRF validates the double-submit CSRF token on a form-
// encoded admin action. Mirrors handleEditSubmit's pattern. Returns
// false (and writes an error response) on failure.
func (s *Server) checkAdminCSRF(w http.ResponseWriter, r *http.Request) bool {
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
