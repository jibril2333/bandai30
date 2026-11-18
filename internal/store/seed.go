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

	// One collection across five brand listings. Splitting them into five
	// collections would put the grade in the collection name and leave each
	// collection's category filter with a single value; this way the grade IS
	// the category, and the dashboard shows one card instead of five wildly
	// unequal ones (HG alone is larger than everything else here combined).
	{Code: "GUNPLA", Slug: "gunpla", Name: "ガンプラ", Family: "ガンプラ",
		Tagline: "高达拼装", Color: "#dc2626", Scraper: "bandai-hobby",
		ScraperArg: "hg,mg,rg,pg,mgsd,mgka,mgex", Type: "kit", SortOrder: 5},

	// Bandai's own キャラクタープラモデル section, which is five brand listings
	// organised by line rather than by franchise. Taking the lines rather than
	// /series/evangelion/ and /series/digimon/ matters: those are franchise
	// views that cut across product lines, and items.series holds one value —
	// a franchise collection would take the RG Evangelion and 30MP kits away
	// from the collections that already hold them.
	{Code: "キャラプラ", Slug: "charaplastic", Name: "キャラクタープラモデル",
		Family: "キャラクタープラモデル", Tagline: "角色模型", Color: "#c026d3",
		Scraper:    "bandai-hobby",
		ScraperArg: "figurerise-standard,figurerise-standard-amp,hg-c,rg-c,entry_grade",
		Type:       "kit", SortOrder: 6},

	{Code: "METAL BUILD", Slug: "metalbuild", Name: "METAL BUILD", Family: "Tamashii Nations",
		Tagline: "高级合金成品", Color: "#b8860b", Scraper: "tamashii", ScraperArg: "metal_build", Type: "finished", SortOrder: 10},
	{Code: "解体匠機", Slug: "kaitaishouki", Name: "METAL STRUCTURE 解体匠機", Family: "Tamashii Nations",
		Tagline: "内部结构再现", Color: "#8b5cf6", Scraper: "tamashii", ScraperArg: "kaitai_shou_ki", Type: "finished", SortOrder: 11},
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
		// Insert-if-absent leaves an existing row alone, so scraper settings
		// have to be written back every boot: adding a brand slug to a
		// collection that already exists would otherwise change nothing
		// anywhere it is already deployed. Presentation (name, colour,
		// tagline, order) is left as stored.
		if _, e := s.DB.ExecContext(ctx,
			`UPDATE collections SET type=?, scraper=?, scraper_arg=? WHERE code=?`,
			dc.Type, dc.Scraper, dc.ScraperArg, dc.Code); e != nil {
			return added, e
		}
	}
	return added, nil
}
