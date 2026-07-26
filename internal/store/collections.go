package store

import (
	"context"
	"database/sql"
	"errors"
)

// Collection is one product line (e.g. 30MS, METAL BUILD). Its Code matches
// Item.Series, so items join to collections via series == code.
type Collection struct {
	Code       string `json:"code"`
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Family     string `json:"family"`
	Tagline    string `json:"tagline"`
	Color      string `json:"color"`
	Scraper    string `json:"scraper"`
	ScraperArg string `json:"scraperArg"`
	Type       string `json:"type"` // "kit" | "finished"
	SortOrder  int    `json:"sortOrder"`
}

// InsertCollectionIfAbsent adds a collection only if its code isn't present, so
// startup seeding never overwrites user edits.
func (s *Store) InsertCollectionIfAbsent(ctx context.Context, c *Collection) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `
		INSERT OR IGNORE INTO collections(code, slug, name, family, tagline, color, scraper, scraper_arg, type, sort_order)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		c.Code, c.Slug, c.Name, c.Family, c.Tagline, c.Color, c.Scraper, c.ScraperArg, c.Type, c.SortOrder)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) ListCollections(ctx context.Context) ([]Collection, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT code, slug, name, family, tagline, color, scraper, scraper_arg, type, sort_order
		FROM collections ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Collection
	for rows.Next() {
		var c Collection
		if err := rows.Scan(&c.Code, &c.Slug, &c.Name, &c.Family, &c.Tagline, &c.Color, &c.Scraper, &c.ScraperArg, &c.Type, &c.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetCollectionBySlug(ctx context.Context, slug string) (*Collection, error) {
	return s.scanCollection(s.DB.QueryRowContext(ctx, `
		SELECT code, slug, name, family, tagline, color, scraper, scraper_arg, type, sort_order
		FROM collections WHERE slug=?`, slug))
}

func (s *Store) scanCollection(row *sql.Row) (*Collection, error) {
	var c Collection
	err := row.Scan(&c.Code, &c.Slug, &c.Name, &c.Family, &c.Tagline, &c.Color, &c.Scraper, &c.ScraperArg, &c.Type, &c.SortOrder)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}
