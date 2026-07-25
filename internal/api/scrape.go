package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/rei/bandai30/internal/auth"
	"github.com/rei/bandai30/internal/scrape"
)

// A refresh takes minutes — well past the server's write timeout — so these
// handlers start it in the background and return immediately. The UI polls
// scrapeStatus for progress. scrape.Client allows only one run at a time, so
// pressing refresh while the weekly job is going returns 409 instead of
// starting a competing pass.

// startScrape kicks off a refresh of one collection, or of everything when no
// slug is given.
func (s *Server) startScrape(w http.ResponseWriter, r *http.Request) {
	scope, label := "", "all collections"
	if slug := strings.ToLower(r.PathValue("slug")); slug != "" {
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
		scope, label = col.Code, col.Code
	}

	user := auth.CurrentUser(r)
	// Deliberately NOT the request context: that is cancelled the moment this
	// response is written, which would kill the scrape a few milliseconds in.
	// BaseCtx lives as long as the process, so the run survives the request
	// and still stops on shutdown.
	err := s.Scraper.Start(s.BaseCtx, scope, "manual", func(fresh []string, err error) {
		if err != nil {
			log.Printf("manual scrape %s: %v", label, err)
			return
		}
		log.Printf("manual scrape %s: done, %d new item(s)", label, len(fresh))
		s.Store.Audit(context.WithoutCancel(s.BaseCtx), user, "scrape", scope,
			"manual newItems="+itoa(len(fresh)))
	})
	if err != nil {
		if errors.Is(err, scrape.ErrBusy) {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, s.Scraper.State())
}

// scrapeStatus reports the in-flight (or last finished) refresh.
func (s *Server) scrapeStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Scraper.State())
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
