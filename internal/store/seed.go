package store

import "context"

// DefaultCollections is the built-in catalog of product lines. Seeded on first
// run (and any new ones added in later versions get inserted-if-absent), but
// user edits to existing rows are never overwritten.
var DefaultCollections = []Collection{
	{Code: "30MS", Slug: "30ms", Name: "30 Minutes Sisters", Family: "30 Minutes",
		Tagline: "原创美少女", Color: "#e8467a", Scraper: "bandai-hobby", ScraperArg: "30ms", Type: "kit", SortOrder: 1},
	{Code: "30MP", Slug: "30mp", Name: "30 Minutes Preference", Family: "30 Minutes",
		Tagline: "IP 联动美少女", Color: "#6366f1", Scraper: "bandai-hobby", ScraperArg: "30mp", Type: "kit", SortOrder: 2},
	{Code: "30MM", Slug: "30mm", Name: "30 Minutes Missions", Family: "30 Minutes",
		Tagline: "机甲", Color: "#0891b2", Scraper: "bandai-hobby", ScraperArg: "30mm", Type: "kit", SortOrder: 3},
	{Code: "30MF", Slug: "30mf", Name: "30 Minutes Fantasy", Family: "30 Minutes",
		Tagline: "奇幻", Color: "#16a34a", Scraper: "bandai-hobby", ScraperArg: "30mf", Type: "kit", SortOrder: 4},

	{Code: "METAL BUILD", Slug: "metalbuild", Name: "METAL BUILD", Family: "Tamashii Nations",
		Tagline: "高级合金成品", Color: "#b8860b", Scraper: "tamashii", ScraperArg: "metal_build", Type: "finished", SortOrder: 10},
}

// SeedCollections inserts any DefaultCollections whose code is absent, and keeps
// the structural `type` field in sync for built-in collections (it has no UI, so
// syncing never clobbers user data). Idempotent.
func (s *Store) SeedCollections(ctx context.Context) (added int, err error) {
	for i := range DefaultCollections {
		dc := &DefaultCollections[i]
		ok, e := s.InsertCollectionIfAbsent(ctx, dc)
		if e != nil {
			return added, e
		}
		if ok {
			added++
		}
		if _, e := s.DB.ExecContext(ctx, `UPDATE collections SET type=? WHERE code=?`, dc.Type, dc.Code); e != nil {
			return added, e
		}
	}
	return added, nil
}
