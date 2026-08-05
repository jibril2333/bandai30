package scrape

import (
	"strings"
	"testing"

	"github.com/rei/bandai30/internal/store"
)

func TestDiffItem(t *testing.T) {
	base := func() *store.Item {
		return &store.Item{ID: "01_1", Name: "30MS テスト", Price: "2200", ReleaseDate: "2026-11"}
	}

	t.Run("nothing changed", func(t *testing.T) {
		if d := diffItem(base(), base()); d != "" {
			t.Errorf("reported a change where there is none: %q", d)
		}
	})

	t.Run("brand new item", func(t *testing.T) {
		if d := diffItem(nil, base()); d != "" {
			t.Errorf("a new item is not a change: %q", d)
		}
	})

	t.Run("price rise", func(t *testing.T) {
		n := base()
		n.Price = "2420"
		d := diffItem(base(), n)
		if !strings.Contains(d, "¥2200 → ¥2420") {
			t.Errorf("got %q, want the old and new price", d)
		}
	})

	t.Run("release slips", func(t *testing.T) {
		n := base()
		n.ReleaseDate = "2027-02"
		d := diffItem(base(), n)
		if !strings.Contains(d, "2026-11 → 2027-02") {
			t.Errorf("got %q, want both dates", d)
		}
	})

	// UpsertCatalog keeps the old value when the catalogue serves a blank, so
	// reporting "¥2200 → (nothing)" would describe a change that never happened.
	t.Run("blank incoming value is not a change", func(t *testing.T) {
		n := base()
		n.Price, n.ReleaseDate = "", ""
		if d := diffItem(base(), n); d != "" {
			t.Errorf("blank fields reported as a change: %q", d)
		}
	})

	t.Run("several at once", func(t *testing.T) {
		n := base()
		n.Price = "3000"
		n.ReleaseDate = "2027-01"
		d := diffItem(base(), n)
		if !strings.Contains(d, "价格") || !strings.Contains(d, "发售日") {
			t.Errorf("got %q, want both fields", d)
		}
	})

	t.Run("long name is trimmed", func(t *testing.T) {
		o, n := base(), base()
		o.Name = strings.Repeat("あ", 80)
		n.Name = o.Name
		n.Price = "1"
		d := diffItem(o, n)
		if !strings.Contains(d, "…") {
			t.Errorf("long name was not trimmed: %q", d)
		}
	})
}
