package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/rei/bandai30/internal/notify"
	"github.com/rei/bandai30/internal/store"
)

// settingsView is what the settings page renders: the editable settings plus
// the read-only schedule state that makes them meaningful.
type settingsView struct {
	store.Settings
	// Whether a token is stored, never the token itself. The settings page has
	// no reason to see it, and this endpoint is served over a tunnel.
	NtfyTokenSet bool  `json:"ntfyTokenSet"`
	LastRun      int64 `json:"lastRun"`     // last incremental (or full), 0 = never
	NextRun      int64 `json:"nextRun"`     // 0 = disabled
	LastFullRun  int64 `json:"lastFullRun"` // 0 = never
	NextFullRun  int64 `json:"nextFullRun"` // 0 = disabled
	LastBackup   int64 `json:"lastBackup"`  // 0 = never
	NextBackup   int64 `json:"nextBackup"`  // 0 = disabled
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
	out := settingsView{Settings: cfg, NtfyTokenSet: cfg.NtfyToken != ""}
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

// settingsInput mirrors store.Settings except for the token, which is a
// pointer so three cases stay distinguishable: absent (leave the stored one
// alone — the page never receives it, so it cannot echo it back), empty
// string (clear it), and a value (replace it).
type settingsInput struct {
	AutoInterval   string  `json:"autoInterval"`
	FullInterval   string  `json:"fullInterval"`
	BackupInterval string  `json:"backupInterval"`
	BackupKeep     int     `json:"backupKeep"`
	NtfyServer     string  `json:"ntfyServer"`
	NtfyTopic      string  `json:"ntfyTopic"`
	NtfyToken      *string `json:"ntfyToken"`
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var raw settingsInput
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	in := store.Settings{
		AutoInterval:   raw.AutoInterval,
		FullInterval:   raw.FullInterval,
		BackupInterval: raw.BackupInterval,
		BackupKeep:     raw.BackupKeep,
		NtfyServer:     strings.TrimSpace(raw.NtfyServer),
		NtfyTopic:      strings.TrimSpace(raw.NtfyTopic),
	}
	// Carry the stored token forward unless this request names one.
	cur, err := s.Store.GetSettings(ctxOf(r), "", "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if raw.NtfyToken != nil {
		in.NtfyToken = strings.TrimSpace(*raw.NtfyToken)
	} else {
		in.NtfyToken = cur.NtfyToken
	}
	if in.NtfyServer != "" {
		u, err := url.Parse(in.NtfyServer)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			writeErr(w, http.StatusBadRequest, "ntfyServer must be an http(s) URL, e.g. https://ntfy.sh")
			return
		}
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

// testNtfy sends one notification with the settings as they are stored right
// now. Without it a wrong token is only discovered the next time something
// worth announcing happens — which is exactly when you want the notification,
// and the failure would sit in a container log nobody reads. Send() checks the
// response status, so a rejected token surfaces here as an error.
func (s *Server) testNtfy(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.Store.GetSettings(ctxOf(r), "", "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cfg.NtfyTopic == "" {
		writeErr(w, http.StatusBadRequest, "先填主题再测试")
		return
	}
	n := notify.New(cfg.NtfyServer, cfg.NtfyTopic, cfg.NtfyToken)
	if err := n.Send(ctxOf(r), "Bandai30: 测试推送", "设置页发出的测试通知。收到这条就说明配置是对的。", "white_check_mark,robot"); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
