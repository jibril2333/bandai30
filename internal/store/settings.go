package store

import (
	"context"
	"strconv"
	"time"
)

// Settings the owner can change from the UI. They live in the meta table so a
// change survives a redeploy — the container is recreated on every push, and
// anything held only in the environment would revert to the compose default.
type Settings struct {
	// AutoInterval is how often the cheap listing-only refresh runs, as a Go
	// duration ("168h"). Empty disables it.
	AutoInterval string `json:"autoInterval"`
	// FullInterval is how often the detail-page pass runs. It is far more
	// expensive, so it gets its own, usually much longer, schedule. Empty
	// disables it. A full run also covers everything the incremental one does.
	FullInterval string `json:"fullInterval"`
	// BackupInterval is how often a verified snapshot is taken. Empty disables
	// it, which is only sensible if something else backs this file up.
	BackupInterval string `json:"backupInterval"`
	// BackupKeep is how many snapshots to retain. They are ~300 KB each.
	BackupKeep int `json:"backupKeep"`

	// ntfy push. These used to arrive from the environment, which meant the
	// container had to be recreated to change a topic — and after the move to
	// the NAS, where CI no longer touches the host, changing one meant editing
	// YAML on the box. They live here for the same reason the intervals do.
	//
	// NtfyToken never leaves the server: the API reports whether one is set,
	// not what it is.
	NtfyServer string `json:"ntfyServer"`
	NtfyTopic  string `json:"ntfyTopic"`
	NtfyToken  string `json:"-"`
}

const (
	keyAutoInterval   = "auto_interval"
	keyFullInterval   = "full_interval"
	keyBackupInterval = "backup_interval"
	keyBackupKeep     = "backup_keep"
	keyNtfyServer     = "ntfy_server"
	keyNtfyTopic      = "ntfy_topic"
	keyNtfyToken      = "ntfy_token"

	// defaultBackupInterval/Keep apply when nothing was ever configured: daily,
	// a fortnight of history. At ~300 KB a snapshot that is ~4 MB total.
	defaultBackupInterval = "24h"
	defaultBackupKeep     = 14
)

// GetSettings returns the stored settings, falling back to the supplied
// defaults for anything never set (first boot reads them from the
// environment, so an untouched install behaves exactly as before).
func (s *Store) GetSettings(ctx context.Context, defInterval, defFull string) (Settings, error) {
	out := Settings{
		AutoInterval:   defInterval,
		FullInterval:   defFull,
		BackupInterval: defaultBackupInterval,
		BackupKeep:     defaultBackupKeep,
	}
	for _, f := range []struct {
		key string
		dst *string
	}{
		{keyAutoInterval, &out.AutoInterval},
		{keyFullInterval, &out.FullInterval},
		{keyBackupInterval, &out.BackupInterval},
	} {
		v, err := s.GetMeta(ctx, f.key)
		if err != nil {
			return out, err
		}
		// "off" is a deliberate choice and must beat the default; "" means
		// never configured, so the default stands.
		if v == "off" {
			*f.dst = ""
		} else if v != "" {
			*f.dst = v
		}
	}
	if v, err := s.GetMeta(ctx, keyBackupKeep); err == nil && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			out.BackupKeep = n
		}
	}
	// No "off" sentinel for these: an empty topic already means "don't push",
	// so there is nothing a default would override.
	for _, f := range []struct {
		key string
		dst *string
	}{
		{keyNtfyServer, &out.NtfyServer},
		{keyNtfyTopic, &out.NtfyTopic},
		{keyNtfyToken, &out.NtfyToken},
	} {
		v, err := s.GetMeta(ctx, f.key)
		if err != nil {
			return out, err
		}
		*f.dst = v
	}
	return out, nil
}

// SaveSettings persists both fields. An empty interval is stored as the
// sentinel "off" so it is distinguishable from "never configured".
func (s *Store) SaveSettings(ctx context.Context, in Settings) error {
	blank := func(v string) string {
		if v == "" {
			return "off"
		}
		return v
	}
	if err := s.SetMeta(ctx, keyAutoInterval, blank(in.AutoInterval)); err != nil {
		return err
	}
	if err := s.SetMeta(ctx, keyFullInterval, blank(in.FullInterval)); err != nil {
		return err
	}
	if err := s.SetMeta(ctx, keyBackupInterval, blank(in.BackupInterval)); err != nil {
		return err
	}
	if err := s.SetMeta(ctx, keyBackupKeep, strconv.Itoa(in.BackupKeep)); err != nil {
		return err
	}
	if err := s.SetMeta(ctx, keyNtfyServer, in.NtfyServer); err != nil {
		return err
	}
	if err := s.SetMeta(ctx, keyNtfyTopic, in.NtfyTopic); err != nil {
		return err
	}
	return s.SetMeta(ctx, keyNtfyToken, in.NtfyToken)
}

func parseInterval(v string) time.Duration {
	d, err := time.ParseDuration(v)
	if err != nil || d < time.Minute {
		return 0
	}
	return d
}

// Interval is the incremental cadence. Zero means "never".
func (st Settings) Interval() time.Duration { return parseInterval(st.AutoInterval) }

// Full is the detail-page cadence. Zero means "never".
func (st Settings) Full() time.Duration { return parseInterval(st.FullInterval) }

// BackupEvery is the snapshot cadence. Zero means "never".
func (st Settings) BackupEvery() time.Duration { return parseInterval(st.BackupInterval) }
