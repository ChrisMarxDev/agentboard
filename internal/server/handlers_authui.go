package server

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/go-chi/chi/v5"
)

// Human-facing auth UI. These pages exist because the JSON API alone
// is unusable from a browser — a browser hitting GET /_api/auth/login
// gets a 405, and a token-redemption flow demands a real form.
//
// Pages render server-side HTML (no SPA), post to themselves, and on
// success mint the same session cookies the JSON handlers do.
//
// Surface:
//
//	GET  /login                 — form
//	POST /login                 — credentials → session cookies → redirect
//	GET  /logout                — revoke session + redirect
//	GET  /invite/{id}           — redemption form
//	POST /invite/{id}           — claim user + mint session + redirect

// authUIPage is the layout shared by login, invite, and other
// logged-out surfaces. Mobile-first, dark-mode aware via the same
// design tokens the dashboard shell uses.
const authUIPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} · {{.WorkspaceName}}</title>
<link rel="stylesheet" href="/_static/design-system.css">
<style>
  body{margin:0;min-height:100vh;display:grid;place-items:center;padding:1.5rem;
    background:var(--bg);color:var(--text);
    font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif}
  .card{width:100%;max-width:420px;background:var(--bg-secondary);
    border:1px solid var(--border);border-radius:var(--ab-radius,8px);
    padding:1.75rem;box-shadow:var(--ab-shadow,0 1px 2px rgba(0,0,0,.04))}
  .brand{font-weight:600;font-size:.85rem;color:var(--text-secondary);
    text-transform:uppercase;letter-spacing:.05em;margin-bottom:.75rem}
  .brand a{color:inherit;text-decoration:none}
  h1{font-size:1.5rem;margin:0 0 .25rem;letter-spacing:-.015em}
  .subtitle{color:var(--text-secondary);margin:0 0 1.5rem;font-size:.95rem}
  label{display:block;margin:.85rem 0 .35rem;font-size:.85rem;font-weight:500}
  input[type=text],input[type=password]{
    width:100%;padding:.6rem .75rem;background:var(--bg);color:var(--text);
    border:1px solid var(--border);border-radius:6px;font-size:1rem;
    -webkit-appearance:none}
  input:focus{outline:none;border-color:var(--accent);box-shadow:0 0 0 3px var(--accent-light)}
  button{margin-top:1.25rem;width:100%;padding:.7rem 1rem;background:var(--accent);
    color:#fff;border:0;border-radius:6px;font-size:1rem;font-weight:500;cursor:pointer}
  button:hover{filter:brightness(1.05)}
  .err{margin-top:1rem;padding:.6rem .85rem;background:rgba(220,38,38,.08);
    border:1px solid rgba(220,38,38,.25);border-radius:6px;
    color:var(--error,#dc2626);font-size:.85rem}
  .foot{margin-top:1.25rem;font-size:.8rem;color:var(--text-secondary);text-align:center}
  .foot a{color:var(--accent);text-decoration:none}
  .field-hint{font-size:.75rem;color:var(--text-secondary);margin-top:.35rem}
</style>
</head>
<body>
<form class="card" method="post" action="{{.Action}}" autocomplete="on">
  <div class="brand"><a href="/">{{.WorkspaceName}}</a></div>
  <h1>{{.Heading}}</h1>
  {{if .Subtitle}}<p class="subtitle">{{.Subtitle}}</p>{{end}}
  {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
  {{range .Fields}}
    <label for="{{.Name}}">{{.Label}}</label>
    <input id="{{.Name}}" name="{{.Name}}" type="{{.Type}}"
           {{if .Autocomplete}}autocomplete="{{.Autocomplete}}"{{end}}
           {{if .Value}}value="{{.Value}}"{{end}}
           {{if .Required}}required{{end}}
           {{if .Pattern}}pattern="{{.Pattern}}"{{end}}>
    {{if .Hint}}<div class="field-hint">{{.Hint}}</div>{{end}}
  {{end}}
  {{if .Hidden}}{{range $k, $v := .Hidden}}<input type="hidden" name="{{$k}}" value="{{$v}}">{{end}}{{end}}
  <button type="submit">{{.Submit}}</button>
  {{if .Foot}}<div class="foot">{{.Foot}}</div>{{end}}
</form>
</body>
</html>`

var authUITemplate = template.Must(template.New("auth-ui").Parse(authUIPage))

type authUIField struct {
	Name         string
	Label        string
	Type         string // "text" or "password"
	Autocomplete string
	Value        string
	Required     bool
	Pattern      string
	Hint         string
}

type authUIData struct {
	Title         string
	WorkspaceName string
	Heading       string
	Subtitle      string
	Action        string
	Fields        []authUIField
	Hidden        map[string]string
	Submit        string
	Foot          template.HTML
	Error         string
}

// registerAuthUIRoutes wires the human-facing auth pages. Mounted as
// top-level paths so the URLs are bookmarkable + obvious. Called from
// buildRouter before the HTML catch-all so /login etc. take precedence.
func (s *Server) registerAuthUIRoutes(r chi.Router) {
	r.Get("/login", s.handleLoginPage)
	r.Post("/login", s.handleLoginSubmit)
	r.Get("/logout", s.handleLogoutPage)
	r.Get("/invite/{id}", s.handleInvitePage)
	r.Post("/invite/{id}", s.handleInviteSubmit)
}

// ----- /login -----

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	// If already signed in via a session cookie, hop home.
	if user := auth.UserFromContext(r.Context()); user != nil {
		http.Redirect(w, r, redirectTarget(r, "/"), http.StatusFound)
		return
	}
	// If the board has no users yet, /login is the wrong UI — there's
	// nothing to log into. Send the visitor at the active bootstrap
	// invitation instead so they can claim the first admin in one
	// click. If somehow there's no active bootstrap invite, fall
	// through to the regular login form (with a hint in the foot).
	if s.Auth != nil && s.Invitations != nil {
		if has, _ := s.Auth.HasAnyUser(); !has {
			if inv, err := s.Invitations.BootstrapActive(); err == nil && inv != nil {
				http.Redirect(w, r, "/invite/"+inv.ID, http.StatusFound)
				return
			}
		}
	}
	data := loginViewData(r, "", "")
	s.renderAuthUI(w, data)
}

func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderAuthUI(w, loginViewData(r, "", "Invalid form submission."))
		return
	}
	username := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	password := r.FormValue("password")
	if username == "" || password == "" {
		s.renderAuthUI(w, loginViewData(r, username, "Enter your username and password."))
		return
	}
	user, err := s.Auth.VerifyLogin(username, password)
	if err != nil {
		s.renderAuthUI(w, loginViewData(r, username, "Username or password incorrect."))
		return
	}
	_, plain, err := s.Auth.CreateSession(user.Username, r.UserAgent(), clientIP(r), 0)
	if err != nil {
		s.renderAuthUI(w, loginViewData(r, username, "Could not start a session — try again."))
		return
	}
	csrf, err := auth.GenerateCSRFToken()
	if err != nil {
		s.renderAuthUI(w, loginViewData(r, username, "Could not start a session — try again."))
		return
	}
	setSessionCookies(w, plain, csrf, auth.DefaultSessionTTL, requestIsSecure(r))
	http.Redirect(w, r, redirectTarget(r, "/"), http.StatusFound)
}

func loginViewData(r *http.Request, username, errMsg string) authUIData {
	next := r.URL.Query().Get("next")
	hidden := map[string]string{}
	if next != "" && strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") {
		hidden["next"] = next
	}
	return authUIData{
		Title:         "Sign in",
		WorkspaceName: "AgentBoard",
		Heading:       "Sign in to AgentBoard",
		Subtitle: "AgentBoard is a shared git workspace humans and AI agents " +
			"collaborate inside. Use the username + password you set when " +
			"claiming this board.",
		Action: "/login",
		Submit: "Sign in",
		Error:  errMsg,
		Hidden: hidden,
		Fields: []authUIField{
			{Name: "username", Label: "Username", Type: "text",
				Autocomplete: "username", Value: username, Required: true,
				Pattern: "[a-z0-9_-]+"},
			{Name: "password", Label: "Password", Type: "password",
				Autocomplete: "current-password", Required: true},
		},
		Foot: template.HTML(`No account? Ask an admin for an invite URL ` +
			`(<code>/invite/&lt;id&gt;</code>).<br>` +
			`Curious what this is? ` +
			`<a href="/_api/introduction">Read the introduction</a>.`),
	}
}

// ----- /logout -----

func (s *Server) handleLogoutPage(w http.ResponseWriter, r *http.Request) {
	// Mirror handleAuthLogout: best-effort revoke + clear cookies.
	if sess := auth.SessionFromContext(r.Context()); sess != nil {
		_ = s.Auth.RevokeSession(sess.ID)
	} else if cookie, err := r.Cookie(auth.SessionCookieName); err == nil && cookie.Value != "" {
		if _, sess, err := s.Auth.ResolveSession(cookie.Value); err == nil && sess != nil {
			_ = s.Auth.RevokeSession(sess.ID)
		}
	}
	clearSessionCookies(w, requestIsSecure(r))
	http.Redirect(w, r, "/", http.StatusFound)
}

// ----- /invite/{id} -----

func (s *Server) handleInvitePage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	inv, err := s.Invitations.Get(id)
	if err != nil || inv == nil {
		s.renderAuthUI(w, authUIData{
			Title:         "Invitation",
			WorkspaceName: "AgentBoard",
			Heading:       "Invitation not found",
			Subtitle:      "The link is wrong, expired, or already used. Ask an admin for a fresh one.",
			Action:        "/",
			Submit:        "Back to dashboard",
		})
		return
	}
	s.renderAuthUI(w, inviteViewData(id, string(inv.Role), "", ""))
}

func (s *Server) handleInviteSubmit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		s.renderAuthUI(w, inviteViewData(id, "", "", "Invalid form submission."))
		return
	}
	username := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	password := r.FormValue("password")
	if username == "" || password == "" {
		s.renderAuthUI(w, inviteViewData(id, "", username, "Pick a username and password."))
		return
	}

	// Reuse the same Auth + Invitations machinery the JSON handler uses
	// rather than POSTing through the HTTP loop — clearer error
	// surfacing, no double-cookie risk.
	inv, err := s.Invitations.Get(id)
	if err != nil || inv == nil {
		s.renderAuthUI(w, inviteViewData(id, "", username, "Invitation not found."))
		return
	}
	if inv.Status() != "active" {
		s.renderAuthUI(w, inviteViewData(id, string(inv.Role), username, "Invitation already used, revoked, or expired."))
		return
	}

	kind := auth.KindMember
	switch string(inv.Role) {
	case "admin":
		kind = auth.KindAdmin
	case "bot":
		kind = auth.KindBot
	}
	user, createErr := s.Auth.CreateUser(auth.CreateUserParams{
		Username: username,
		Kind:     kind,
	})
	if createErr != nil {
		msg := "Could not create the account."
		if strings.Contains(createErr.Error(), "username") {
			msg = "Username is taken or invalid (use only a-z, 0-9, _, -)."
		}
		s.renderAuthUI(w, inviteViewData(id, string(inv.Role), username, msg))
		return
	}
	if err := s.Auth.SetPassword(user.Username, password); err != nil {
		s.renderAuthUI(w, inviteViewData(id, string(inv.Role), username,
			"Password is too short (minimum 12 characters)."))
		return
	}
	// Mint a personal token + persist on the user, so they can later
	// pull it from /_api/me/tokens. Mirrors the JSON redeem path.
	token, _ := auth.GenerateToken()
	_, _ = s.Auth.CreateToken(auth.CreateTokenParams{
		Username:  user.Username,
		TokenHash: auth.HashToken(token),
		Label:     "initial",
	})
	if _, err := s.Invitations.Redeem(id, user.Username); err != nil {
		s.renderAuthUI(w, inviteViewData(id, string(inv.Role), username,
			"Could not redeem the invitation — it may have been used in another tab."))
		return
	}
	_, plain, sessErr := s.Auth.CreateSession(user.Username, r.UserAgent(), clientIP(r), 0)
	if sessErr != nil {
		// Account exists; user can sign in manually.
		http.Redirect(w, r, "/login?next=/", http.StatusFound)
		return
	}
	csrf, _ := auth.GenerateCSRFToken()
	setSessionCookies(w, plain, csrf, auth.DefaultSessionTTL, requestIsSecure(r))
	http.Redirect(w, r, "/", http.StatusFound)
}

func inviteViewData(id, role, username, errMsg string) authUIData {
	heading := "Claim your account"
	subtitle := "Pick a username and a password to finish setting up this board."
	if role == "admin" {
		subtitle = "You've been invited as an admin. Pick a username + password."
	} else if role == "bot" {
		heading = "Claim a bot account"
		subtitle = "Bot identities exist for automation — the password lets you sign in to manage their tokens."
	}
	return authUIData{
		Title:         heading,
		WorkspaceName: "AgentBoard",
		Heading:       heading,
		Subtitle:      subtitle,
		Action:        "/invite/" + id,
		Submit:        "Create account",
		Error:         errMsg,
		Fields: []authUIField{
			{Name: "username", Label: "Username", Type: "text",
				Autocomplete: "username", Value: username, Required: true,
				Pattern: "[a-z0-9_-]+",
				Hint:    "Lowercase letters, digits, underscore, hyphen. Usernames are permanent."},
			{Name: "password", Label: "Password", Type: "password",
				Autocomplete: "new-password", Required: true,
				Hint: "Minimum 12 characters. Used for browser sign-in only."},
		},
		Foot: template.HTML(fmt.Sprintf(`Role: <strong>%s</strong>.`, template.HTMLEscapeString(role))),
	}
}

// ----- shared -----

func (s *Server) renderAuthUI(w http.ResponseWriter, data authUIData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if data.WorkspaceName == "" {
		data.WorkspaceName = "AgentBoard"
	}
	_ = authUITemplate.Execute(w, data)
}

// redirectTarget returns a safe post-login destination. Honors the
// `next` query/form param only when it's a relative path; falls back
// to `def` otherwise. Prevents open-redirect via `next=https://evil`.
func redirectTarget(r *http.Request, def string) string {
	candidates := []string{r.URL.Query().Get("next"), r.FormValue("next")}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if !strings.HasPrefix(c, "/") || strings.HasPrefix(c, "//") {
			continue
		}
		if _, err := url.Parse(c); err != nil {
			continue
		}
		return c
	}
	return def
}
