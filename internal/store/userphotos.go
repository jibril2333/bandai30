package store

import (
	"context"
	"time"
)

// A scrape replaces an item's entire gallery (SetItemPhotos deletes then
// re-inserts), so the owner's own photos cannot live there — the next refresh
// would erase them. They are the one kind of picture in this app that cannot
// be re-downloaded, so they get their own table.

// UserPhoto is a picture the owner added to an item.
type UserPhoto struct {
	ID      int64  `json:"id"`
	URL     string `json:"url"`
	Caption string `json:"caption"`
	AddedAt int64  `json:"addedAt"`
}

// AddUserPhoto appends a photo to an item and returns it.
func (s *Store) AddUserPhoto(ctx context.Context, itemID, url, caption string) (*UserPhoto, error) {
	now := time.Now().Unix()
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO user_photos(item_id, url, caption, added_at) VALUES(?,?,?,?)`,
		itemID, url, caption, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &UserPhoto{ID: id, URL: url, Caption: caption, AddedAt: now}, nil
}

// UserPhotos lists an item's own photos, oldest first.
func (s *Store) UserPhotos(ctx context.Context, itemID string) ([]UserPhoto, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, url, caption, added_at FROM user_photos WHERE item_id=? ORDER BY id`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserPhoto
	for rows.Next() {
		var p UserPhoto
		if err := rows.Scan(&p.ID, &p.URL, &p.Caption, &p.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeleteUserPhoto removes one, scoped to its item so a stray id can't reach
// another item's pictures. Reports whether a row actually went away, so the
// caller doesn't log a deletion that never happened.
func (s *Store) DeleteUserPhoto(ctx context.Context, itemID string, id int64) (bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM user_photos WHERE id=? AND item_id=?`, id, itemID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// UserPhotoCounts returns how many own photos each item has, for the list view.
func (s *Store) UserPhotoCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT item_id, COUNT(*) FROM user_photos GROUP BY item_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
