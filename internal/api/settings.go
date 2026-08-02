package api

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/rei/bandai30/internal/scrape"
	"github.com/rei/bandai30/internal/store"
)

// settingsView is what the settings page renders: the editable settings plus
// the read-only schedule state that makes them meaningful.
type settingsView struct {
	store.Settings
	LastRun int64 `json:"lastRun"` // unix seconds, 0 = never
	NextRun int64 `json:"nextRun"` // unix seconds, 0 = disabled or unknown
}

func (s *Server) envDefaults() (interval, mode string) {
	mode = os.Getenv("BANDAI30_SCRAPE_MODE")
	if mode == "" {
		mode = string(scrape.ModeIncremental)
	}
	return os.Getenv("BANDAI30_SCRAPE_INTERVAL"), mode
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	ctx := ctxOf(r)
	di, dm := s.envDefaults()
	cfg, err := s.Store.GetSettings(ctx, di, dm)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := settingsView{Settings: cfg}
	if last, err := s.Store.GetMetaTime(ctx, "last_scrape_at"); err == nil && !last.IsZero() {
		out.LastRun = last.Unix()
		if d := cfg.Interval(); d > 0 {
			out.NextRun = last.Add(d).Unix()
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in store.Settings
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	// Validate here rather than letting a typo silently disable the scheduler.
	if in.AutoInterval != "" {
		d, err := time.ParseDuration(in.AutoInterval)
		if err != nil || d < time.Minute {
			writeErr(w, http.StatusBadRequest, "autoInterval must be a duration ≥ 1m, e.g. 168h")
			return
		}
	}
	if in.AutoMode != string(scrape.ModeIncremental) && in.AutoMode != string(scrape.ModeFull) {
		writeErr(w, http.StatusBadRequest, "autoMode must be incremental or full")
		return
	}
	if err := s.Store.SaveSettings(ctxOf(r), in); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.getSettings(w, r)
}
