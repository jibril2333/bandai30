CREATE TABLE IF NOT EXISTS items (
    id           TEXT PRIMARY KEY,
    series       TEXT NOT NULL,
    category     TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL,
    name_zh      TEXT NOT NULL DEFAULT '',
    release_date TEXT NOT NULL DEFAULT '',
    price        TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'none',
    notes        TEXT NOT NULL DEFAULT '',
    photo_url    TEXT NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_items_series       ON items(series);
CREATE INDEX IF NOT EXISTS idx_items_status       ON items(status);
CREATE INDEX IF NOT EXISTS idx_items_release_date ON items(release_date);

CREATE TABLE IF NOT EXISTS collections (
    code        TEXT PRIMARY KEY,   -- matches items.series (e.g. "30MS", "METAL BUILD")
    slug        TEXT NOT NULL,      -- URL route segment (e.g. "30ms", "metalbuild")
    name        TEXT NOT NULL,      -- display name
    family      TEXT NOT NULL DEFAULT '', -- grouping on landing/nav (e.g. "30 Minutes")
    tagline     TEXT NOT NULL DEFAULT '',
    color       TEXT NOT NULL DEFAULT '#e8467a',
    scraper     TEXT NOT NULL DEFAULT '',  -- "bandai-hobby" | "tamashii" | "" (manual)
    scraper_arg TEXT NOT NULL DEFAULT '',  -- brand slug for the scraper
    type        TEXT NOT NULL DEFAULT 'kit', -- "kit" (assemble) | "finished" (pre-built toy)
    sort_order  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_collections_slug ON collections(slug);

CREATE TABLE IF NOT EXISTS users (
    username      TEXT PRIMARY KEY,
    password_hash TEXT NOT NULL,
    created_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    token      TEXT PRIMARY KEY,
    username   TEXT NOT NULL,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    FOREIGN KEY(username) REFERENCES users(username) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);

-- Gallery shots from an item's detail page. items.photo_url stays the cover
-- image, so everything that only needs one picture is unaffected.
CREATE TABLE IF NOT EXISTS item_photos (
    item_id TEXT NOT NULL,
    idx     INTEGER NOT NULL,   -- display order within the gallery
    url     TEXT NOT NULL,      -- local path, e.g. /photos/01_5027_2.jpg
    src     TEXT NOT NULL DEFAULT '', -- remote URL it came from, minus signature
    PRIMARY KEY (item_id, idx),
    FOREIGN KEY (item_id) REFERENCES items(id) ON DELETE CASCADE
);

-- Small key/value bag for scheduler bookkeeping that must outlive the process
-- (e.g. last_scrape_at — see the weekly refresh in cmd/bandai30).
CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_log (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    ts        INTEGER NOT NULL,
    username  TEXT,
    action    TEXT NOT NULL,
    item_id   TEXT,
    detail    TEXT
);
CREATE INDEX IF NOT EXISTS idx_audit_item ON audit_log(item_id);
CREATE INDEX IF NOT EXISTS idx_audit_ts   ON audit_log(ts);
