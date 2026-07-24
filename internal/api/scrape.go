package api

import (
	"net/http"
	"strings"

	"github.com/rei/bandai30/internal/auth"
)

// runScrape refreshes a single collection identified by its slug.
func (s *Server) runScrape(w http.ResponseWriter, r *http.Request) {
	slug := strings.ToLower(r.PathValue("slug"))
	col, err := s.Store.GetCollectionBySlug(ctxOf(r), slug)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if col == nil {
		writeErr(w, http.StatusNotFound, "unknown collection: "+slug)
		return
	}
	if col.Scraper == "" {
		writeErr(w, http.StatusBadRequest, "collection "+col.Code+" is manual-entry (no scraper)")
		return
	}
	rep, err := s.Scraper.ScrapeCollection(ctxOf(r), col)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Store.Audit(ctxOf(r), auth.CurrentUser(r), "scrape", col.Code,
		"upserted="+itoa(rep.Upserted)+" newPhotos="+itoa(rep.NewPhotos))
	writeJSON(w, http.StatusOK, rep)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
