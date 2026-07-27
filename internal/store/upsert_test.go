package store

import (
	"context"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// The collection state is irreplaceable — it exists nowhere but this database,
// and a scrape runs over every item every week. If UpsertCatalog ever starts
// writing these columns, the loss is silent and total.
func TestUpsertCatalogKeepsUserFields(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	if err := st.UpsertItem(ctx, &Item{
		ID: "01_1", Series: "30MS", Category: "Sisters", Name: "old name",
		NameZh: "我的中文名", ReleaseDate: "2024-01", Price: "800",
		Status: "sealed", Notes: "在做记录", PhotoURL: "/photos/mine.jpg",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// A scrape: fresh official data, and the zero values a scraper always
	// carries for the user's columns.
	if err := st.UpsertCatalog(ctx, &Item{
		ID: "01_1", Series: "30MS", Category: "Option Body", Name: "新しい名前",
		ReleaseDate: "2024-02", Price: "880", Status: "none",
	}); err != nil {
		t.Fatalf("catalog upsert: %v", err)
	}

	got, err := st.GetItem(ctx, "01_1")
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}
	// Official fields follow the catalogue.
	for _, c := range []struct{ field, got, want string }{
		{"name", got.Name, "新しい名前"},
		{"price", got.Price, "880"},
		{"releaseDate", got.ReleaseDate, "2024-02"},
		{"category", got.Category, "Option Body"},
		// The user's, untouched.
		{"status", got.Status, "sealed"},
		{"nameZh", got.NameZh, "我的中文名"},
		{"notes", got.Notes, "在做记录"},
		{"photoUrl", got.PhotoURL, "/photos/mine.jpg"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}
}

// A listing that renders without a price (a markup change, an "オープン価格"
// placeholder) must not blank the price we already have.
func TestUpsertCatalogIgnoresEmptyOfficialValues(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	if err := st.UpsertCatalog(ctx, &Item{
		ID: "01_2", Series: "30MM", Category: "Mecha", Name: "アルト",
		ReleaseDate: "2024-03", Price: "2200", Status: "none",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.UpsertCatalog(ctx, &Item{
		ID: "01_2", Series: "30MM", Category: "Mecha", Name: "", // all blank
		ReleaseDate: "", Price: "", Status: "none",
	}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, _ := st.GetItem(ctx, "01_2")
	if got.Name != "アルト" || got.Price != "2200" || got.ReleaseDate != "2024-03" {
		t.Errorf("blank scrape overwrote data: name=%q price=%q date=%q",
			got.Name, got.Price, got.ReleaseDate)
	}
}

// The edit form and the JSON import must still be able to set every field,
// including clearing one.
func TestUpsertItemWritesEverything(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	if err := st.UpsertItem(ctx, &Item{
		ID: "01_3", Series: "30MF", Name: "n", Status: "sealed", Notes: "old", NameZh: "旧",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := st.UpsertItem(ctx, &Item{
		ID: "01_3", Series: "30MF", Name: "n", Status: "none", Notes: "", NameZh: "新",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := st.GetItem(ctx, "01_3")
	if got.Status != "none" || got.Notes != "" || got.NameZh != "新" {
		t.Errorf("edit did not apply: status=%q notes=%q nameZh=%q",
			got.Status, got.Notes, got.NameZh)
	}
}
