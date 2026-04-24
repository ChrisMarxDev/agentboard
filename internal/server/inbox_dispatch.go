package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/christophermarx/agentboard/internal/auth"
	"github.com/christophermarx/agentboard/internal/inbox"
)

// dispatchInboxForValueWrite is the shared data-handler hook. Accepts
// the raw JSON bytes of a write value (PUT/PATCH) and scans every
// string leaf for mentions. `rowID` is non-empty when the write was
// row-scoped (`/api/data/<key>/<id>`).
func (s *Server) dispatchInboxForValueWrite(key string, body []byte, actor, rowID string) {
	if s.Inbox == nil || len(body) == 0 {
		return
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return
	}
	subjectPath := "/api/data/" + key
	subjectRef := rowID
	title := "You were mentioned in " + key
	if rowID != "" {
		title = fmt.Sprintf("You were mentioned in %s[%s]", key, rowID)
	}
	s.emitInboxForMentionsInAny(v, actor, subjectPath, subjectRef, title)
}

// dispatchInboxForRowUpdate fires both mention detection on the new
// row AND assignee-diff detection. Called by the row-scoped handlers
// (`PUT/PATCH /api/data/<key>/<id>`).
//
// `prev` is the row before the write (may be nil if it didn't exist).
// `next` is the row after.
func (s *Server) dispatchInboxForRowUpdate(key string, prev, next map[string]any, actor string) {
	if s.Inbox == nil || next == nil {
		return
	}
	rowID := ""
	if id, ok := next["id"]; ok {
		if s, ok := id.(string); ok {
			rowID = s
		} else {
			rowID = fmt.Sprintf("%v", id)
		}
	}
	subjectPath := "/api/data/" + key
	rowTitle := ""
	if t, ok := next["title"].(string); ok {
		rowTitle = t
	}
	// Assignment diff.
	s.emitInboxForAssignments(prev, next, actor, subjectPath, rowID, rowTitle)
	// Mention detection on every string leaf in the new row.
	s.emitInboxForMentionsInAny(next, actor, subjectPath, rowID, fmt.Sprintf("You were mentioned in %s[%s]", key, rowID))
}

// emitInboxForMentions runs on every content or data write that plausibly
// carries prose. It scans the supplied text for `@username` occurrences,
// verifies each against the user directory, and creates an inbox item
// per mentioned user (minus the actor, so people don't ping themselves
// in every write).
//
// `subjectPath` + `subjectRef` tell the recipient *where* the mention
// happened. The UI hyperlinks to `subjectPath` so clicking an inbox
// entry goes straight to the context.
//
// Best-effort: any error is logged and swallowed — a broken inbox must
// not block the underlying write.
func (s *Server) emitInboxForMentions(text, actor, subjectPath, subjectRef, title string) {
	if s.Inbox == nil || text == "" {
		return
	}
	names := inbox.ExtractMentions(text)
	if len(names) == 0 {
		return
	}
	s.dispatchInboxNotifications(names, inbox.KindMention, actor, subjectPath, subjectRef, title)
}

// emitInboxForMentionsInAny is the data-write variant: it walks an
// arbitrary JSON-shaped value, extracting mentions from every string
// leaf. The title we produce for the inbox entry gets enriched with
// the data key so recipients understand the origin even when the
// value itself is anonymous ("the value you were mentioned in").
func (s *Server) emitInboxForMentionsInAny(v any, actor, subjectPath, subjectRef, title string) {
	if s.Inbox == nil {
		return
	}
	names := inbox.ExtractMentionsInAny(v)
	if len(names) == 0 {
		return
	}
	s.dispatchInboxNotifications(names, inbox.KindMention, actor, subjectPath, subjectRef, title)
}

// emitInboxForAssignments fires when an `assignees` field gains new
// members on an array row. `prevRow` and `nextRow` are the row-shaped
// maps before and after the write; we only look at their "assignees"
// key. The title describes the assignment and hyperlinks back to the
// kanban the row belongs to.
func (s *Server) emitInboxForAssignments(prevRow, nextRow map[string]any, actor, subjectPath, subjectRef, rowTitle string) {
	if s.Inbox == nil || nextRow == nil {
		return
	}
	prev := any(nil)
	if prevRow != nil {
		prev = prevRow["assignees"]
	}
	added := inbox.DiffAssigneesAny(prev, nextRow["assignees"])
	if len(added) == 0 {
		return
	}
	title := fmt.Sprintf("Assigned: %s", rowTitle)
	if rowTitle == "" {
		title = "You were assigned a new row"
	}
	s.dispatchInboxNotifications(added, inbox.KindAssignment, actor, subjectPath, subjectRef, title)
}

// dispatchInboxNotifications is the low-level helper. Shared by the
// mention + assignment wrappers.
//
// Resolution precedence for each raw name:
//  1. If it's a user — a single item for that user.
//  2. Else if it's a reserved pseudo-team (@all, @admins, @agents) —
//     expand to the matching set of active users.
//  3. Else if it's a stored team — expand to every member.
//  4. Otherwise — drop silently (prose rendering still shows it).
//
// The actor is filtered out after expansion (nobody pings themselves).
// Deduplication happens at the inbox.Store level via the 60-second
// dedupe window, so `@alice @marketing` where alice is on marketing
// still produces exactly one item for alice.
//
// Fire-and-forget: we run the actual inserts in a goroutine so the
// data write's HTTP response isn't blocked by SQLite's writer lock
// (the caller's transaction may still be finishing). Best-effort —
// inbox is a reminder queue, not a system of record.
func (s *Server) dispatchInboxNotifications(recipients []string, kind inbox.Kind, actor, subjectPath, subjectRef, title string) {
	if len(recipients) == 0 {
		return
	}
	go func() {
		actorLower := strings.ToLower(actor)
		// Expand every raw reference into a deduplicated set of usernames
		// BEFORE hitting the inbox store. Cheaper than relying on the
		// 60-second dedupe window when a team has dozens of members and
		// the same recipient is referenced multiple ways.
		seen := map[string]struct{}{}
		order := make([]string, 0, len(recipients))
		add := func(name string) {
			n := strings.ToLower(strings.TrimSpace(name))
			if n == "" || n == actorLower {
				return
			}
			if _, ok := seen[n]; ok {
				return
			}
			seen[n] = struct{}{}
			order = append(order, n)
		}
		for _, raw := range recipients {
			for _, name := range s.expandMention(raw) {
				add(name)
			}
		}
		for _, name := range order {
			if s.Auth != nil {
				u, err := s.Auth.GetUser(name)
				if err != nil || u == nil {
					continue
				}
				if u.DeactivatedAt != nil {
					continue
				}
			}
			// Simple retry loop for SQLITE_BUSY contention during
			// the surrounding write's commit. Bounded; logs + drops
			// after a few tries.
			if err := insertWithRetry(s.Inbox, inbox.CreateParams{
				Recipient:   name,
				Kind:        kind,
				Title:       title,
				SubjectPath: subjectPath,
				SubjectRef:  subjectRef,
				Actor:       actor,
			}); err != nil {
				fmt.Printf("inbox: create for %q failed: %v\n", name, err)
			}
		}
	}()
}

// expandMention turns a raw slug ("alice", "marketing", "all") into a
// list of concrete usernames. Users win over teams; reserved pseudo-
// teams (@all, @admins, @agents) resolve to dynamic sets from the auth
// store. Unknown slugs return an empty slice — the caller drops them.
func (s *Server) expandMention(raw string) []string {
	name := strings.ToLower(strings.TrimSpace(raw))
	if name == "" {
		return nil
	}
	// User-first: if it's a known user, return it verbatim.
	if s.Auth != nil {
		if u, err := s.Auth.GetUser(name); err == nil && u != nil {
			return []string{name}
		}
	}
	// Reserved pseudo-teams pull from the user directory.
	switch name {
	case "all":
		return s.allActiveUsernames("")
	case "admins":
		return s.allActiveUsernames(string(auth.KindAdmin))
	case "agents":
		return s.allActiveUsernames(string(auth.KindAgent))
	case "here":
		// Presence isn't tracked in v0; treated as unknown.
		return nil
	}
	// Stored team — expand to members.
	if s.Teams != nil {
		members, err := s.Teams.MemberUsernames(name)
		if err == nil && len(members) > 0 {
			return members
		}
	}
	return nil
}

// allActiveUsernames returns every non-deactivated user's username.
// `kind` filters by user kind (admin / agent); "" returns both.
func (s *Server) allActiveUsernames(kind string) []string {
	if s.Auth == nil {
		return nil
	}
	users, err := s.Auth.ListUsers(false)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(users))
	for _, u := range users {
		if u.DeactivatedAt != nil {
			continue
		}
		if kind != "" && string(u.Kind) != kind {
			continue
		}
		out = append(out, strings.ToLower(u.Username))
	}
	return out
}

// insertWithRetry retries inbox.Create a few times on SQLITE_BUSY.
// The underlying data write holds the writer lock for a few ms around
// commit; a short retry absorbs that without blocking the HTTP path.
func insertWithRetry(store *inbox.Store, p inbox.CreateParams) error {
	const attempts = 6
	var lastErr error
	for i := 0; i < attempts; i++ {
		if _, err := store.Create(p); err == nil {
			return nil
		} else {
			lastErr = err
			if !strings.Contains(err.Error(), "database is locked") {
				return err
			}
			time.Sleep(time.Duration(10*(i+1)) * time.Millisecond)
		}
	}
	return lastErr
}
