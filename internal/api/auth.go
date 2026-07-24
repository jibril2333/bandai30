package api

import (
	"net/http"

	"github.com/rei/bandai30/internal/auth"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.NoAuth {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "username": "anon", "noAuth": true})
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Username == "" || body.Password == "" {
		writeErr(w, http.StatusBadRequest, "missing credentials")
		return
	}
	u, err := s.Store.GetUser(ctxOf(r), body.Username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if u == nil || auth.CheckPassword(u.PasswordHash, body.Password) != nil {
		// Same response for both so we don't leak username existence.
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err := auth.IssueSession(ctxOf(r), s.Store, w, r, u.Username); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Store.Audit(ctxOf(r), u.Username, "login", "", "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "username": u.Username})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	auth.RevokeSession(ctxOf(r), s.Store, w, r)
	if user != "" {
		s.Store.Audit(ctxOf(r), user, "logout", "", "")
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	if u == "" {
		writeErr(w, http.StatusUnauthorized, "not signed in")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"username": u, "noAuth": s.NoAuth})
}
