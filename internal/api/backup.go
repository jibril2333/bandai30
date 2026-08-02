package api

import (
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/rei/bandai30/internal/store"
)

// Snapshots live next to the database they protect, which guards against the
// failure that actually happened (one file losing pages) but not against
// losing the disk. Hence the download endpoint: a copy the owner keeps
// elsewhere is the only real off-site protection.

func (s *Server) backupDir() string { return filepath.Join(filepath.Dir(s.PhotosDir), "backups") }

func (s *Server) listBackups(w http.ResponseWriter, r *http.Request) {
	files, err := store.ListBackups(s.backupDir())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if files == nil {
		files = []store.BackupFile{}
	}
	writeJSON(w, http.StatusOK, files)
}

// runBackup takes a snapshot now. It is fast (a few hundred KB), so unlike a
// scrape it can answer synchronously.
func (s *Server) runBackup(w http.ResponseWriter, r *http.Request) {
	ctx := ctxOf(r)
	path, err := s.Store.Backup(ctx, s.backupDir())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	di, df := s.envDefaults()
	if cfg, err := s.Store.GetSettings(ctx, di, df); err == nil {
		_, _ = store.PruneBackups(s.backupDir(), cfg.BackupKeep)
	}
	_ = s.Store.SetMetaTime(ctx, "last_backup_at", time.Now())
	writeJSON(w, http.StatusOK, map[string]string{"name": filepath.Base(path)})
}

func (s *Server) downloadBackup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	// Same containment rule as photos: the name is a path segment, never a path.
	if name == "" || strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) ||
		!strings.HasSuffix(name, ".db") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"\"")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, filepath.Join(s.backupDir(), name))
}
