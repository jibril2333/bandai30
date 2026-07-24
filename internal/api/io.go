package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/rei/bandai30/internal/auth"
	"github.com/rei/bandai30/internal/store"
)

type exportPayload struct {
	Version   int          `json:"version"`
	ExportedAt string       `json:"exportedAt"`
	Items     []store.Item `json:"items"`
}

func (s *Server) exportJSON(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListItems(ctxOf(r), store.ItemFilter{})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="bandai30-`+time.Now().Format("20060102")+`.json"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(exportPayload{
		Version:    2,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Items:      items,
	})
}

func (s *Server) importJSON(w http.ResponseWriter, r *http.Request) {
	var body exportPayload
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(body.Items) == 0 {
		writeErr(w, http.StatusBadRequest, "no items in payload")
		return
	}
	n := 0
	for i := range body.Items {
		it := body.Items[i]
		if it.ID == "" {
			continue
		}
		if it.Status == "" {
			it.Status = "none"
		}
		if it.Series == "" {
			it.Series = "30MS"
		}
		if err := s.Store.UpsertItem(ctxOf(r), &it); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		n++
	}
	s.Store.Audit(ctxOf(r), auth.CurrentUser(r), "import", "", itoa(n))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "imported": n})
}
