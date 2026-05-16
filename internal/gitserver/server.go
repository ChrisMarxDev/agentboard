package gitserver

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Hooks is the optional event callback set the server hands to the
// caller after a successful push. Server-side code wires these to:
//   - the working-tree mirror (re-checkout HEAD of default branch)
//   - the SSE broadcaster (page-updated events)
//   - the activity log (if anything still cares post-§4)
type Hooks struct {
	OnPush func(ctx context.Context, workspace string, refs []PushedRef)
}

// PushedRef captures one ref update from the receive-pack output.
type PushedRef struct {
	Ref     string
	OldHash string
	NewHash string
}

// Server is the HTTP-facing entry point. Mount with `r.Handle("/git/*", server)`.
type Server struct {
	Store          *Store
	Hooks          Hooks
	GitHTTPBackend string // path to git-http-backend (defaults via discoverBackend)
	// ProjectPath is propagated to the pre-receive hook as
	// AGENTBOARD_PROJECT_PATH so the hook subprocess knows which
	// project DB to open for permission resolution.
	ProjectPath string
}

// New builds a Server pointed at the workspace registry. Discovers
// `git-http-backend` on PATH; returns an error if it can't be found.
func New(store *Store, hooks Hooks) (*Server, error) {
	path, err := discoverBackend()
	if err != nil {
		return nil, err
	}
	return &Server{Store: store, Hooks: hooks, GitHTTPBackend: path}, nil
}

// discoverBackend asks `git --exec-path` for the directory containing
// the dashed plumbing binaries and locates git-http-backend within it.
func discoverBackend() (string, error) {
	out, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		return "", fmt.Errorf("git --exec-path: %w", err)
	}
	dir := strings.TrimSpace(string(out))
	candidate := filepath.Join(dir, "git-http-backend")
	if _, err := os.Stat(candidate); err != nil {
		return "", fmt.Errorf("git-http-backend not found at %s", candidate)
	}
	return candidate, nil
}

// ServeHTTP handles the smart-HTTPS protocol. The chi router mounts
// this on /git/*; the path tail starts with `<workspace>.git/...`.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws, gitPath, ok := parseGitPath(r.URL.Path)
	if !ok {
		http.Error(w, "bad git path", http.StatusBadRequest)
		return
	}

	workspace, err := s.Store.Get(r.Context(), ws)
	if err != nil {
		http.Error(w, "registry error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if workspace == nil {
		http.Error(w, "no such workspace: "+ws, http.StatusNotFound)
		return
	}

	bare := s.Store.BarePath(ws)
	if _, statErr := os.Stat(filepath.Join(bare, "HEAD")); statErr != nil {
		http.Error(w, "bare repo missing on disk", http.StatusInternalServerError)
		return
	}

	// Username for git's receive-pack reflog entry. The chi-mounted
	// auth middleware has already verified the bearer; we just pull
	// the resolved user off the context if present.
	remoteUser := "anonymous"
	if u, ok := userFromRequest(r); ok {
		remoteUser = u
	}

	// CGI invocation. PATH_INFO is the suffix git-http-backend uses
	// to figure out repo + service; GIT_PROJECT_ROOT bounds the
	// filesystem scope.
	cmd := exec.Command(s.GitHTTPBackend)
	cmd.Env = []string{
		"GIT_PROJECT_ROOT=" + filepath.Join(s.Store.Root(), "repos"),
		"GIT_HTTP_EXPORT_ALL=1",
		"REQUEST_METHOD=" + r.Method,
		"QUERY_STRING=" + r.URL.RawQuery,
		"CONTENT_TYPE=" + r.Header.Get("Content-Type"),
		"PATH_INFO=/" + ws + ".git" + gitPath,
		"REMOTE_USER=" + remoteUser,
		"REMOTE_ADDR=" + r.RemoteAddr,
		"HTTP_GIT_PROTOCOL=" + r.Header.Get("Git-Protocol"),
		"PATH=" + os.Getenv("PATH"),
		// Threaded through to the pre-receive hook installed by
		// InstallPreReceiveHook. The hook needs to know which user
		// authenticated (for permission resolution) and which project
		// directory holds the auth DB.
		"AGENTBOARD_USER=" + remoteUser,
		"AGENTBOARD_PROJECT_PATH=" + s.ProjectPath,
		"AGENTBOARD_WORKSPACE=" + ws,
	}
	// Some git clients send `Content-Encoding: gzip` which git-http-backend
	// handles natively if the env var is set.
	if enc := r.Header.Get("Content-Encoding"); enc != "" {
		cmd.Env = append(cmd.Env, "HTTP_CONTENT_ENCODING="+enc)
	}
	cmd.Stdin = r.Body

	// Capture stdout into a buffer; CGI mixes headers + body and we
	// need to peel the headers off before relaying. (Streaming via
	// PipeWriter would be cleaner but the body sizes here are small
	// — packfiles for our workspaces fit comfortably in memory.)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			http.Error(w, "git-http-backend failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		http.Error(w, "git-http-backend not runnable: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := relayCGI(stdout.Bytes(), w); err != nil {
		// Headers may have already been written; nothing useful to do
		// except log. (We don't have a logger here; use stderr.)
		fmt.Fprintln(os.Stderr, "git CGI relay:", err)
		return
	}

	// Best-effort push hook: parse the receive-pack capture if this
	// was a push. The CGI output for receive-pack contains a series
	// of report-status lines; we re-derive the refs from the bare
	// repo's reflog as a more reliable signal.
	if strings.HasSuffix(gitPath, "/git-receive-pack") && s.Hooks.OnPush != nil {
		refs := readRecentRefUpdates(bare)
		s.Hooks.OnPush(r.Context(), ws, refs)
	}
}

// parseGitPath splits a `/git/<workspace>.git/<rest>` URL into the
// workspace name and the rest of the path. Returns ok=false for
// anything that doesn't match the shape.
func parseGitPath(p string) (workspace, rest string, ok bool) {
	if !strings.HasPrefix(p, "/git/") {
		return "", "", false
	}
	tail := strings.TrimPrefix(p, "/git/")
	idx := strings.Index(tail, ".git")
	if idx < 0 {
		return "", "", false
	}
	workspace = tail[:idx]
	rest = tail[idx+len(".git"):]
	if workspace == "" {
		return "", "", false
	}
	return workspace, rest, true
}

// relayCGI splits CGI output into headers + body and writes them to
// the http.ResponseWriter.
func relayCGI(raw []byte, w http.ResponseWriter) error {
	rd := bufio.NewReader(bytes.NewReader(raw))
	status := http.StatusOK
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read CGI headers: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		k := strings.TrimSpace(line[:colon])
		v := strings.TrimSpace(line[colon+1:])
		if strings.EqualFold(k, "Status") {
			// "Status: 200 OK" → 200
			fields := strings.Fields(v)
			if len(fields) > 0 {
				if code, err := strconv.Atoi(fields[0]); err == nil {
					status = code
				}
			}
			continue
		}
		w.Header().Add(k, v)
	}
	w.WriteHeader(status)
	_, err := io.Copy(w, rd)
	return err
}

// readRecentRefUpdates is a heuristic that returns the last refs to
// move in the bare repo. Used by the push hook so the working-tree
// mirror and SSE broadcaster know what just changed without re-
// parsing the receive-pack protocol. Quick + cheap: parses the last
// few lines of HEAD's reflog and any branch reflog under
// `logs/refs/heads/`.
func readRecentRefUpdates(bare string) []PushedRef {
	var refs []PushedRef
	logsDir := filepath.Join(bare, "logs", "refs", "heads")
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return refs
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ref := "refs/heads/" + e.Name()
		oldHash, newHash, ok := tailReflog(filepath.Join(logsDir, e.Name()))
		if !ok {
			continue
		}
		refs = append(refs, PushedRef{Ref: ref, OldHash: oldHash, NewHash: newHash})
	}
	return refs
}

func tailReflog(path string) (oldHash, newHash string, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 0 {
		return "", "", false
	}
	last := lines[len(lines)-1]
	fields := strings.Fields(last)
	if len(fields) < 2 {
		return "", "", false
	}
	return fields[0], fields[1], true
}
