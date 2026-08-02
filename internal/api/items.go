package api

import (
	"net/http"
	"regexp"
	"strconv"

	"github.com/rei/bandai30/internal/auth"
	"github.com/rei/bandai30/internal/store"
)

// catalogIDRE matches IDs created by a scraper (bandai-hobby "01_1234",
// tamashii "tw-1234"). User-added items use a "user-…" id and stay fully editable.
var catalogIDRE = regexp.MustCompile(`^(01_\d+|tw-\d+)$`)

func isCatalogItem(id string) bool { return catalogIDRE.MatchString(id) }

func (s *Server) listItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	f := store.ItemFilter{
		Series:   q.Get("series"),
		Category: q.Get("category"),
		Status:   q.Get("status"),
		Marked:   q.Get("marked") == "1",
		Search:   q.Get("q"),
		Limit:    limit,
		Offset:   offset,
	}
	items, err := s.Store.ListItems(ctxOf(r), f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		items = []store.Item{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) getItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	it, err := s.Store.GetItem(ctxOf(r), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if it == nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	// The detail view shows the whole gallery; the list only ever needs the
	// cover, so this join stays off the /api/items path.
	photos, err := s.Store.ItemPhotos(ctxOf(r), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		*store.Item
		Photos []string `json:"photos"`
	}{it, photos})
}

// createItem is disabled: the catalog is populated only by scraping official
// sources. Manual item creation is not allowed. (Bulk restore goes through
// /api/import instead.)
func (s *Server) createItem(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusForbidden, "manual item creation is disabled; items come from official scraping")
}

func (s *Server) updateItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.Store.GetItem(ctxOf(r), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var patch store.Item
	if err := readJSON(r, &patch); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	patch.ID = id
	patch.CreatedAt = existing.CreatedAt
	if patch.Series == "" {
		patch.Series = existing.Series
	}
	// Catalog items (scraped from an official site) have locked fields — the
	// name/price/release/category/series come from the source of truth and can't
	// be edited. Only the user's own fields (status, nameZh, notes, photo) apply.
	if isCatalogItem(id) {
		patch.Name = existing.Name
		patch.Price = existing.Price
		patch.ReleaseDate = existing.ReleaseDate
		patch.Category = existing.Category
		patch.Series = existing.Series
	}
	if err := s.Store.UpsertItem(ctxOf(r), &patch); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Store.Audit(ctxOf(r), auth.CurrentUser(r), "update", id, patch.Status)
	writeJSON(w, http.StatusOK, &patch)
}

func (s *Server) deleteItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Store.DeleteItem(ctxOf(r), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Store.Audit(ctxOf(r), auth.CurrentUser(r), "delete", id, "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) listCollections(w http.ResponseWriter, r *http.Request) {
	cols, err := s.Store.ListCollections(ctxOf(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cols == nil {
		cols = []store.Collection{}
	}
	writeJSON(w, http.StatusOK, cols)
}

func (s *Server) statsHandler(w http.ResponseWriter, r *http.Request) {
	counts, err := s.Store.SeriesCounts(ctxOf(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, counts)
}

func (s *Server) categoriesHandler(w http.ResponseWriter, r *http.Request) {
	cats, err := s.Store.CategoriesUsed(ctxOf(r), r.URL.Query().Get("series"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cats == nil {
		cats = []string{}
	}
	writeJSON(w, http.StatusOK, cats)
}
