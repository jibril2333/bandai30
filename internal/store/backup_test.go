package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	if err := st.UpsertItem(ctx, &Item{
		ID: "01_1", Series: "30MS", Name: "n", Status: "sealed", NameZh: "中文名", Notes: "备注",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	dir := filepath.Join(t.TempDir(), "backups")
	path, err := st.Backup(ctx, dir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	// The whole point is that the copy is openable and complete.
	restored, err := Open(path)
	if err != nil {
		t.Fatalf("reopen snapshot: %v", err)
	}
	defer restored.Close()
	got, err := restored.GetItem(ctx, "01_1")
	if err != nil || got == nil {
		t.Fatalf("read from snapshot: %v", err)
	}
	if got.Status != "sealed" || got.NameZh != "中文名" || got.Notes != "备注" {
		t.Errorf("snapshot lost user fields: %+v", got)
	}
}

// A snapshot that cannot be read back is worse than no snapshot, because it is
// only discovered when it is needed. Backup must refuse to leave one behind.
func TestBackupRejectsEmptyDatabase(t *testing.T) {
	ctx := context.Background()
	st := testStore(t) // schema applied, no items

	dir := filepath.Join(t.TempDir(), "backups")
	if _, err := st.Backup(ctx, dir); err == nil {
		t.Fatal("expected an error for a snapshot with no items")
	}
	files, _ := ListBackups(dir)
	if len(files) != 0 {
		t.Errorf("a rejected snapshot was left on disk: %v", files)
	}
}

func TestPruneKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	// Names sort by timestamp; mtime decides order, so stagger them.
	for i, name := range []string{
		"bandai30-20260101-000000.db",
		"bandai30-20260102-000000.db",
		"bandai30-20260103-000000.db",
		"unrelated.txt",
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		// Older index = older file.
		ts := int64(1_700_000_000 + i*3600)
		os.Chtimes(p, timeOf(ts), timeOf(ts))
	}

	n, err := PruneBackups(dir, 2)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 1 {
		t.Errorf("pruned %d, want 1", n)
	}
	left, _ := ListBackups(dir)
	if len(left) != 2 || left[0].Name != "bandai30-20260103-000000.db" {
		t.Errorf("wrong survivors: %+v", left)
	}
	// Anything that isn't ours must be untouched.
	if _, err := os.Stat(filepath.Join(dir, "unrelated.txt")); err != nil {
		t.Error("prune deleted a file it does not own")
	}
}

func timeOf(unix int64) time.Time { return time.Unix(unix, 0) }
