package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Backups exist because the catalogue is reproducible and the collection is
// not. Statuses, Chinese names, notes and uploaded photos live nowhere but
// this file; on 2026-07-31 it lost 18 of its 64 pages, including the header,
// and only a byte-level salvage of the audit log got the marks back.
//
// The snapshot is taken from INSIDE the process, through SQLite's own
// VACUUM INTO: it writes a fresh, fully-checkpointed database with no torn
// pages, needs no lock the app doesn't already hold, and never involves an
// outside process touching the bind-mounted file — the layer that most likely
// lost those pages in the first place.

const backupPrefix = "bandai30-"

// Backup writes a verified snapshot into dir and returns its path.
func (s *Store) Backup(ctx context.Context, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := backupPrefix + time.Now().Format("20060102-150405") + ".db"
	path := filepath.Join(dir, name)

	// VACUUM INTO refuses to overwrite, which is what we want: a name clash
	// means something is wrong, not something to silently clobber.
	if _, err := s.DB.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return "", fmt.Errorf("vacuum into %s: %w", name, err)
	}

	// An unverified backup is worse than none: the failure this guards against
	// was silent, and a corrupt copy would be discovered only when needed.
	if err := verifyDB(ctx, path); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("backup %s failed verification: %w", name, err)
	}
	return path, nil
}

func verifyDB(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	var res string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("integrity_check: %s", res)
	}
	// A structurally valid but empty file would also pass integrity_check.
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items`).Scan(&n); err != nil {
		return fmt.Errorf("read items: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("no items in snapshot")
	}
	return nil
}

// BackupFile describes one snapshot on disk.
type BackupFile struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Taken int64  `json:"taken"` // unix seconds, from the file's mtime
}

// ListBackups returns the snapshots in dir, newest first.
func ListBackups(dir string) ([]BackupFile, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []BackupFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), backupPrefix) || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupFile{Name: e.Name(), Size: info.Size(), Taken: info.ModTime().Unix()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Taken > out[j].Taken })
	return out, nil
}

// PruneBackups deletes all but the keep newest snapshots and reports how many
// it removed. keep <= 0 means keep everything.
func PruneBackups(dir string, keep int) (int, error) {
	if keep <= 0 {
		return 0, nil
	}
	files, err := ListBackups(dir)
	if err != nil || len(files) <= keep {
		return 0, err
	}
	n := 0
	for _, f := range files[keep:] {
		if os.Remove(filepath.Join(dir, f.Name)) == nil {
			n++
		}
	}
	return n, nil
}
