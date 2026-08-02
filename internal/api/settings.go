package api

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/rei/bandai30/internal/store"
)

// settingsView is what the settings page renders: the editable settings plus
// the read-only schedule state that makes them meaningful.
type settingsView struct {
	store.Settings
	LastRun     int64 `json:"lastRun"`     // last incremental (or full), 0 = never
	NextRun     int64 `json:"nextRun"`     // 0 = disabled
	LastFullRun int64 `json:"lastFullRun"` // 0 = never
	NextFullRun int64 `json:"nextFullRun"` // 0 = disabled
	LastBackup  int64 `json:"lastBackup"`  // 0 = never
	NextBackup  int64 `json:"nextBackup"`  // 0 = disabled
}

func (s *Server) envDefaults() (interval, full string) {
	return os.Getenv("BANDAI30_SCRAPE_INTERVAL"), os.Getenv("BANDAI30_FULL_INTERVAL")
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
	if last, err := s.Store.GetMetaTime(ctx, "last_full_scrape_at"); err == nil && !last.IsZero() {
		out.LastFullRun = last.Unix()
		if d := cfg.Full(); d > 0 {
			out.NextFullRun = last.Add(d).Unix()
		}
	}
	if last, err := s.Store.GetMetaTime(ctx, "last_backup_at"); err == nil && !last.IsZero() {
		out.LastBackup = last.Unix()
		if d := cfg.BackupEvery(); d > 0 {
			out.NextBackup = last.Add(d).Unix()
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
	for name, v := range map[string]string{
		"autoInterval":   in.AutoInterval,
		"fullInterval":   in.FullInterval,
		"backupInterval": in.BackupInterval,
	} {
		if v == "" {
			continue // explicit "off"
		}
		if d, err := time.ParseDuration(v); err != nil || d < time.Minute {
			writeErr(w, http.StatusBadRequest, name+" must be a duration ≥ 1m, e.g. 168h")
			return
		}
	}
	if in.BackupKeep < 1 || in.BackupKeep > 365 {
		writeErr(w, http.StatusBadRequest, "backupKeep must be between 1 and 365")
		return
	}
	if err := s.Store.SaveSettings(ctxOf(r), in); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.getSettings(w, r)
}
