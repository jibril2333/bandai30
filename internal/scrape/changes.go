package scrape

import (
	"fmt"
	"strings"

	"github.com/rei/bandai30/internal/store"
)

// Bandai revises the catalogue after announcing a product: prices creep up,
// release months slip by a quarter, provisional names get finalised. Those
// edits were applied silently — the item simply looked different next time you
// opened it. For anything on a wishlist or already ordered they are the news.

// diffItem describes what changed between the stored item and the freshly
// scraped one, in a form fit for a phone notification. Empty when nothing of
// interest moved.
//
// Only the fields a collector acts on are compared. Category is derived from
// the name, so it would just echo a rename, and photo changes are far too
// noisy — every re-shoot would ping.
func diffItem(old, new *store.Item) string {
	if old == nil {
		return ""
	}
	var parts []string
	if c := changed("价格", fmtYen(old.Price), fmtYen(new.Price)); c != "" {
		parts = append(parts, c)
	}
	if c := changed("发售日", old.ReleaseDate, new.ReleaseDate); c != "" {
		parts = append(parts, c)
	}
	if c := changed("名称", old.Name, new.Name); c != "" {
		parts = append(parts, c)
	}
	if len(parts) == 0 {
		return ""
	}
	name := new.Name
	if name == "" {
		name = old.Name
	}
	return fmt.Sprintf("%s\n  %s", trimName(name), strings.Join(parts, "\n  "))
}

// changed renders "label: old → new", but says nothing when the new value is
// empty: the catalogue occasionally serves a blank field, and UpsertCatalog
// keeps the old value in that case, so reporting it would be a lie.
func changed(label, old, new string) string {
	if new == "" || old == new {
		return ""
	}
	if old == "" {
		return fmt.Sprintf("%s: %s", label, new)
	}
	return fmt.Sprintf("%s: %s → %s", label, old, new)
}

func fmtYen(v string) string {
	if v == "" {
		return ""
	}
	return "¥" + v
}

// trimName keeps a notification readable on a phone.
func trimName(s string) string {
	r := []rune(s)
	if len(r) <= 40 {
		return s
	}
	return string(r[:40]) + "…"
}
