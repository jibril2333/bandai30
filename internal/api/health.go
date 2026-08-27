package api

import (
	"net/http"
)

// health is the one endpoint that does NOT require a session.
//
// Two callers need it and neither can log in: Docker's healthcheck, which
// runs inside the container, and the deploy workflow, which polls the NAS
// over Tailscale to confirm the box is serving THIS build rather than the
// container that was already running. Waiting for "something answers 200"
// passes instantly against the old container — the commit is what makes the
// check mean anything.
//
// It reports the commit and nothing else. A count of items would make the
// "deployed onto an empty data dir" failure visible here too, but this is the
// only route reachable without auth, and the tunnel publishes it; the deploy
// checks the catalogue separately over Tailscale, where auth is off anyway.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	// A round-trip through SQLite, so a corrupt or unreadable database reports
	// unhealthy instead of merely proving the process still holds the port.
	if _, err := s.Store.ItemCount(ctxOf(r)); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok": false, "version": s.Version, "error": "database unreadable",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": s.Version})
}
