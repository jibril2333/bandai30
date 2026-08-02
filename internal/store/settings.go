package store

import (
	"context"
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
}

const (
	keyAutoInterval = "auto_interval"
	keyFullInterval = "full_interval"
)

// GetSettings returns the stored settings, falling back to the supplied
// defaults for anything never set (first boot reads them from the
// environment, so an untouched install behaves exactly as before).
func (s *Store) GetSettings(ctx context.Context, defInterval, defFull string) (Settings, error) {
	out := Settings{AutoInterval: defInterval, FullInterval: defFull}
	for _, f := range []struct {
		key string
		dst *string
	}{{keyAutoInterval, &out.AutoInterval}, {keyFullInterval, &out.FullInterval}} {
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
	return s.SetMeta(ctx, keyFullInterval, blank(in.FullInterval))
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
