package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Item is one product in the catalog.
type Item struct {
	ID          string `json:"id"`
	Series      string `json:"series"`
	Category    string `json:"category"`
	Name        string `json:"name"`
	NameZh      string `json:"nameZh"`
	ReleaseDate string `json:"releaseDate"`
	Price       string `json:"price"`
	Status      string `json:"status"`
	Notes       string `json:"notes"`
	PhotoURL    string `json:"photoUrl"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// ItemFilter narrows ListItems results.
type ItemFilter struct {
	Series   string
	Category string
	Status   string
	Marked   bool   // only items the user has marked (status != 'none')
	Search   string // matches name / nameZh / notes (case-insensitive)
	Limit    int
	Offset   int
}

type Store struct {
	DB *sql.DB
}

// Open opens (or creates) the SQLite database at path and applies the schema.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite serialises writes; WAL handles concurrent readers via separate handle if needed
	if _, err := db.ExecContext(context.Background(), schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	s := &Store{DB: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// migrate applies idempotent schema tweaks for databases created by older versions.
func (s *Store) migrate(ctx context.Context) error {
	// item_photos.src records where each shot came from; without it a
	// reordered source list leaves stale files in place.
	if !s.columnExists(ctx, "item_photos", "src") {
		if _, err := s.DB.ExecContext(ctx, `ALTER TABLE item_photos ADD COLUMN src TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	// collections.type was added after the initial release.
	if !s.columnExists(ctx, "collections", "type") {
		if _, err := s.DB.ExecContext(ctx, `ALTER TABLE collections ADD COLUMN type TEXT NOT NULL DEFAULT 'kit'`); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) columnExists(ctx context.Context, table, col string) bool {
	rows, err := s.DB.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil && name == col {
			return true
		}
	}
	return false
}

func (s *Store) Close() error { return s.DB.Close() }

// --- Items ---

// UpsertItem writes every column, user fields included. This is the path for
// a deliberate edit: the edit form, a JSON import, a restore. Scrapers must
// use UpsertCatalog instead.
func (s *Store) UpsertItem(ctx context.Context, it *Item) error {
	now := time.Now().Unix()
	if it.CreatedAt == 0 {
		it.CreatedAt = now
	}
	it.UpdatedAt = now
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO items(id, series, category, name, name_zh, release_date, price, status, notes, photo_url, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			series=excluded.series, category=excluded.category, name=excluded.name, name_zh=excluded.name_zh,
			release_date=excluded.release_date, price=excluded.price, status=excluded.status,
			notes=excluded.notes, photo_url=excluded.photo_url, updated_at=excluded.updated_at`,
		it.ID, it.Series, it.Category, it.Name, it.NameZh, it.ReleaseDate, it.Price, it.Status, it.Notes,
		it.PhotoURL, it.CreatedAt, it.UpdatedAt)
	return err
}

// UpsertCatalog writes the fields that belong to the official catalogue and
// leaves everything the user owns alone. Every scraper goes through it.
//
// The guarantee lives here, in SQL, rather than in each scraper: name_zh,
// notes and status are simply absent from the UPDATE list, so no caller can
// blank them by forgetting to copy the old row first. Losing them would be
// unrecoverable — they exist nowhere but this database.
//
// Official fields are only overwritten by a NON-EMPTY value. A listing that
// momentarily omits a price (a rendering change, an "オープン価格" placeholder)
// would otherwise wipe a good one. photo_url likewise sticks once set: it may
// point at a photo the user uploaded.
func (s *Store) UpsertCatalog(ctx context.Context, it *Item) error {
	now := time.Now().Unix()
	if it.CreatedAt == 0 {
		it.CreatedAt = now
	}
	it.UpdatedAt = now
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO items(id, series, category, name, name_zh, release_date, price, status, notes, photo_url, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			series=excluded.series,
			category=excluded.category,
			name=CASE WHEN excluded.name != '' THEN excluded.name ELSE items.name END,
			release_date=CASE WHEN excluded.release_date != '' THEN excluded.release_date ELSE items.release_date END,
			price=CASE WHEN excluded.price != '' THEN excluded.price ELSE items.price END,
			photo_url=CASE WHEN items.photo_url != '' THEN items.photo_url ELSE excluded.photo_url END,
			updated_at=excluded.updated_at`,
		it.ID, it.Series, it.Category, it.Name, it.NameZh, it.ReleaseDate, it.Price, it.Status, it.Notes,
		it.PhotoURL, it.CreatedAt, it.UpdatedAt)
	return err
}

// --- Gallery photos ---

// SetItemPhotos replaces an item's gallery in one transaction. Passing an
// empty slice is a no-op rather than a wipe: a detail page that failed to
// parse should leave the pictures we already have alone.
func (s *Store) SetItemPhotos(ctx context.Context, itemID string, urls, srcs []string) error {
	if len(urls) == 0 {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM item_photos WHERE item_id=?`, itemID); err != nil {
		return err
	}
	for i, u := range urls {
		src := ""
		if i < len(srcs) {
			src = srcs[i]
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO item_photos(item_id, idx, url, src) VALUES(?,?,?,?)`, itemID, i, u, src); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ItemPhotos returns an item's gallery in display order.
func (s *Store) ItemPhotos(ctx context.Context, itemID string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT url FROM item_photos WHERE item_id=? ORDER BY idx`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ItemPhotoSources returns the remote URL each gallery slot was fetched from,
// in slot order. Rows written before sources were recorded yield "".
func (s *Store) ItemPhotoSources(ctx context.Context, itemID string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT src FROM item_photos WHERE item_id=? ORDER BY idx`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ItemsWithoutGallery lists ids that have no gallery yet, so an incremental
// run can fill the gaps without visiting every detail page.
func (s *Store) ItemsWithoutGallery(ctx context.Context, series string) ([]string, error) {
	q := `SELECT i.id FROM items i
	      WHERE NOT EXISTS (SELECT 1 FROM item_photos p WHERE p.item_id = i.id)`
	args := []any{}
	if series != "" {
		q += ` AND i.series = ?`
		args = append(args, series)
	}
	q += ` ORDER BY i.id`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) GetItem(ctx context.Context, id string) (*Item, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT id, series, category, name, name_zh, release_date, price, status, notes, photo_url, created_at, updated_at FROM items WHERE id=?`, id)
	var it Item
	err := row.Scan(&it.ID, &it.Series, &it.Category, &it.Name, &it.NameZh, &it.ReleaseDate, &it.Price, &it.Status, &it.Notes, &it.PhotoURL, &it.CreatedAt, &it.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &it, nil
}

func (s *Store) DeleteItem(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM items WHERE id=?`, id)
	return err
}

// SetPhotoURL overwrites the cover URL, which UpsertCatalog deliberately will
// not do (it keeps whatever is already there, so a scrape can never drop a
// photo the user uploaded). Replacing a placeholder is the one case that must
// override that, and it needs a NEW url — the file is rewritten in place, so a
// browser holding the old bytes would otherwise keep showing them.
func (s *Store) SetPhotoURL(ctx context.Context, id, url string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE items SET photo_url=?, updated_at=? WHERE id=?`,
		url, time.Now().Unix(), id)
	return err
}

// SetCategory updates only an item's category (used by the recategorize pass).
func (s *Store) SetCategory(ctx context.Context, id, category string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE items SET category=? WHERE id=?`, category, id)
	return err
}

// SetPrice updates only an item's price (used by the price-normalize pass).
func (s *Store) SetPrice(ctx context.Context, id, price string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE items SET price=? WHERE id=?`, price, id)
	return err
}

func (s *Store) ListItems(ctx context.Context, f ItemFilter) ([]Item, error) {
	var conds []string
	var args []any
	if f.Series != "" {
		conds = append(conds, "series=?")
		args = append(args, f.Series)
	}
	if f.Category != "" {
		conds = append(conds, "category=?")
		args = append(args, f.Category)
	}
	if f.Status != "" {
		conds = append(conds, "status=?")
		args = append(args, f.Status)
	}
	if f.Marked {
		conds = append(conds, "status != 'none'")
	}
	if f.Search != "" {
		conds = append(conds, "(LOWER(name) LIKE ? OR LOWER(name_zh) LIKE ? OR LOWER(notes) LIKE ?)")
		q := "%" + strings.ToLower(f.Search) + "%"
		args = append(args, q, q, q)
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	// Order: items with release_date first (newest first), then undated by id desc.
	sqlStr := `SELECT id, series, category, name, name_zh, release_date, price, status, notes, photo_url, created_at, updated_at FROM items` + where +
		` ORDER BY (release_date != '') DESC, release_date DESC, id DESC`
	if f.Limit > 0 {
		sqlStr += fmt.Sprintf(" LIMIT %d OFFSET %d", f.Limit, f.Offset)
	}
	rows, err := s.DB.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Series, &it.Category, &it.Name, &it.NameZh, &it.ReleaseDate, &it.Price, &it.Status, &it.Notes, &it.PhotoURL, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// SeriesCounts returns a per-series breakdown { series -> { status -> count } }.
type StatusCounts struct {
	Total    int `json:"total"`
	None     int `json:"none"`
	Wishlist int `json:"wishlist"`
	Ordered  int `json:"ordered"` // bought, not delivered yet
	Sealed   int `json:"sealed"`
	WIP      int `json:"wip"`
	Done     int `json:"done"`
}

func (s *Store) SeriesCounts(ctx context.Context) (map[string]*StatusCounts, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT series, status, COUNT(*) FROM items GROUP BY series, status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*StatusCounts{}
	for rows.Next() {
		var series, status string
		var n int
		if err := rows.Scan(&series, &status, &n); err != nil {
			return nil, err
		}
		c, ok := out[series]
		if !ok {
			c = &StatusCounts{}
			out[series] = c
		}
		c.Total += n
		switch status {
		case "none":
			c.None += n
		case "wishlist":
			c.Wishlist += n
		case "ordered":
			c.Ordered += n
		case "sealed":
			c.Sealed += n
		case "wip":
			c.WIP += n
		case "done":
			c.Done += n
		}
	}
	return out, rows.Err()
}

// CategoriesUsed returns distinct categories that appear in at least one item, optionally scoped to a series.
func (s *Store) CategoriesUsed(ctx context.Context, series string) ([]string, error) {
	var rows *sql.Rows
	var err error
	if series == "" {
		rows, err = s.DB.QueryContext(ctx, `SELECT DISTINCT category FROM items WHERE category != '' ORDER BY category`)
	} else {
		rows, err = s.DB.QueryContext(ctx, `SELECT DISTINCT category FROM items WHERE series=? AND category != '' ORDER BY category`, series)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- Users ---

type User struct {
	Username     string
	PasswordHash string
	CreatedAt    int64
}

func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO users(username, password_hash, created_at) VALUES(?,?,?)`, username, passwordHash, time.Now().Unix())
	return err
}

func (s *Store) GetUser(ctx context.Context, username string) (*User, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT username, password_hash, created_at FROM users WHERE username=?`, username)
	var u User
	err := row.Scan(&u.Username, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// --- Meta (small persistent key/value bag) ---

// GetMeta returns the stored value, or "" when the key was never set.
func (s *Store) GetMeta(ctx context.Context, key string) (string, error) {
	var v string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO meta(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}

// GetMetaTime reads a key written by SetMetaTime. Zero time = never set.
func (s *Store) GetMetaTime(ctx context.Context, key string) (time.Time, error) {
	v, err := s.GetMeta(ctx, key)
	if err != nil || v == "" {
		return time.Time{}, err
	}
	sec, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		// A corrupt value shouldn't wedge the scheduler forever; treat it as
		// "never" so the caller falls back to running now.
		return time.Time{}, nil
	}
	return time.Unix(sec, 0), nil
}

func (s *Store) SetMetaTime(ctx context.Context, key string, t time.Time) error {
	return s.SetMeta(ctx, key, strconv.FormatInt(t.Unix(), 10))
}

// ItemCount reports how many items the catalog holds. Zero means the DB was
// just created and has never been populated — see the first-run scrape in cmd.
func (s *Store) ItemCount(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM items`).Scan(&n)
	return n, err
}

func (s *Store) UserCount(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// --- Sessions ---

func (s *Store) CreateSession(ctx context.Context, token, username string, expiresAt int64) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO sessions(token, username, expires_at, created_at) VALUES(?,?,?,?)`, token, username, expiresAt, time.Now().Unix())
	return err
}

func (s *Store) LookupSession(ctx context.Context, token string) (username string, ok bool, err error) {
	row := s.DB.QueryRowContext(ctx, `SELECT username, expires_at FROM sessions WHERE token=?`, token)
	var u string
	var exp int64
	if err = row.Scan(&u, &exp); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	if exp < time.Now().Unix() {
		return "", false, nil
	}
	return u, true, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token=?`, token)
	return err
}

func (s *Store) ReapExpiredSessions(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, time.Now().Unix())
	return err
}

// --- Audit ---

func (s *Store) Audit(ctx context.Context, username, action, itemID, detail string) {
	// Best-effort: errors are swallowed because audit failure should never break a user action.
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO audit_log(ts, username, action, item_id, detail) VALUES(?,?,?,?,?)`,
		time.Now().Unix(), username, action, itemID, detail)
}
