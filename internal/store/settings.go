package store

import (
	"context"
	"time"
)

// Settings the owner can change from the UI. They live in the meta table so a
// change survives a redeploy — the container is recreated on every push, and
// anything held only in the environment would revert to the compose default.
type Settings struct {
	// AutoInterval is a Go duration ("168h"). Empty disables the scheduler.
	AutoInterval string `json:"autoInterval"`
	// AutoMode is "incremental" or "full".
	AutoMode string `json:"autoMode"`
}

const (
	keyAutoInterval = "auto_interval"
	keyAutoMode     = "auto_mode"
)

// GetSettings returns the stored settings, falling back to the supplied
// defaults for anything never set (first boot reads them from the
// environment, so an untouched install behaves exactly as before).
func (s *Store) GetSettings(ctx context.Context, defInterval, defMode string) (Settings, error) {
	out := Settings{AutoInterval: defInterval, AutoMode: defMode}
	iv, err := s.GetMeta(ctx, keyAutoInterval)
	if err != nil {
		return out, err
	}
	if iv != "" {
		out.AutoInterval = iv
	}
	// A stored "off" is a deliberate choice and must beat the default.
	if iv == "off" {
		out.AutoInterval = ""
	}
	md, err := s.GetMeta(ctx, keyAutoMode)
	if err != nil {
		return out, err
	}
	if md != "" {
		out.AutoMode = md
	}
	return out, nil
}

// SaveSettings persists both fields. An empty interval is stored as the
// sentinel "off" so it is distinguishable from "never configured".
func (s *Store) SaveSettings(ctx context.Context, in Settings) error {
	iv := in.AutoInterval
	if iv == "" {
		iv = "off"
	}
	if err := s.SetMeta(ctx, keyAutoInterval, iv); err != nil {
		return err
	}
	return s.SetMeta(ctx, keyAutoMode, in.AutoMode)
}

// Interval parses AutoInterval. Zero means "no automatic refresh".
func (st Settings) Interval() time.Duration {
	d, err := time.ParseDuration(st.AutoInterval)
	if err != nil || d < time.Minute {
		return 0
	}
	return d
}
