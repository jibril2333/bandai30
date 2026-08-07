package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/rei/bandai30/internal/auth"
	"github.com/rei/bandai30/internal/store"
)

// The owner's own photos are the one kind this app cannot re-fetch, so they
// are stored apart from the scraped gallery and audited like a status change.

func (s *Server) listUserPhotos(w http.ResponseWriter, r *http.Request) {
	photos, err := s.Store.UserPhotos(ctxOf(r), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if photos == nil {
		photos = []store.UserPhoto{}
	}
	writeJSON(w, http.StatusOK, photos)
}

func (s *Server) addUserPhoto(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := ctxOf(r)
	it, err := s.Store.GetItem(ctx, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if it == nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}

	var in struct {
		URL     string `json:"url"`
		Caption string `json:"caption"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	// Only files this server stored. An arbitrary URL would leave the picture
	// on someone else's host, where it can vanish or change under us — the
	// opposite of what "my photo" is supposed to mean.
	if !strings.HasPrefix(in.URL, "/photos/") || strings.Contains(in.URL, "..") {
		writeErr(w, http.StatusBadRequest, "url must be an uploaded /photos/ path")
		return
	}

	p, err := s.Store.AddUserPhoto(ctx, id, in.URL, strings.TrimSpace(in.Caption))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Store.Audit(ctx, auth.CurrentUser(r), "photo-add", id, in.URL)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deleteUserPhoto(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pid, err := strconv.ParseInt(r.PathValue("photoID"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad photo id")
		return
	}
	gone, err := s.Store.DeleteUserPhoto(ctxOf(r), id, pid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !gone {
		// Either already deleted or the id belongs to another item. Say so
		// rather than writing an audit row for a deletion that didn't happen —
		// that log is the record of last resort when the database is damaged.
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	// The file itself stays: the cover may point at it, and an orphaned image
	// costs a few hundred KB where a broken cover costs a visible bug.
	s.Store.Audit(ctxOf(r), auth.CurrentUser(r), "photo-del", id, r.PathValue("photoID"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
