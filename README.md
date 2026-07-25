# bandai30

Personal tracker for Bandai collectibles. Collections are **data-driven** — each
product line is a row in the `collections` table grouped into a *family*:

| Family | Collections | Source |
|--------|-------------|--------|
| 30 Minutes | 30MS, 30MP, 30MM, 30MF | bandai-hobby.net |
| Tamashii Nations | METAL BUILD | tamashiiweb.com |

Adding a new line (e.g. 超合金, S.H.Figuarts) = inserting a collection row with
a scraper type + brand slug; no code change. You mark each item as owned, wanted,
building, or finished.

Single Go binary + embedded SPA + SQLite + photo store. Built for NAS deployment
behind Cloudflare Tunnel.

## Scrapers

- **bandai-hobby**: paginates `global.bandai-hobby.net/en-us/brand/<slug>/`,
  parses the server-rendered product cards.
- **tamashii**: hits the JSON search API
  `tamashiiweb.com/api/site-item/search_item.php?brandCode[]=<slug>&per_page=100&current_page=N&area=japan`
  (paginated). The full list of brand slugs is at `…/search_attr.php`.

To add another Tamashii line, add a `store.Collection` to `DefaultCollections`
in `internal/store/seed.go` with `Scraper: "tamashii"` and the brand slug.

## First run: there is no database to install

**You don't need a seed database, and none ships with this repo.** The catalog
is scraped from Bandai's own sites, so a fresh install builds its own.

On startup the app creates `data/bandai30.db`, applies the schema, and seeds the
five product lines. If the items table is then still empty, it kicks off a
**first-run scrape in the background**: the site is up and browsable
immediately, and items stream in over roughly **5 minutes** (~600 items and
their photos, ~40 MB). Reload the page to watch it fill in.

This only happens when the catalog is empty — restarting an existing
deployment never re-triggers it. You can also refresh one line at a time from
the UI.

## Weekly refresh

With `BANDAI30_SCRAPE_INTERVAL=168h` (the compose default) every collection is
re-scraped once a week, and any brand-new items are pushed to your phone via
ntfy. New releases are announced weeks ahead of shipping, so a weekly check is
plenty; drop it to `24h` if you want to see announcements the day they land.

The countdown is measured from the **last run recorded in the database**, not
from process start. That matters at weekly cadence: a plain timer restarts on
every launch, and since this app redeploys on `git push` and the host sleeps
and reboots, a week-long timer could keep getting reset and never fire. It also
means a refresh missed while the machine was off runs shortly after it comes
back, rather than being skipped.

The run time is recorded *before* scraping, so a week of failing sources
retries next week instead of hammering Bandai in a hot loop.

Two known gaps the automatic scrape can't cover, because Bandai's brand
listings omit them — fill them in by hand if you want them:

- **Premium Bandai exclusives** — a separate lineup-page scraper.
- **Discontinued items** whose detail page still exists, e.g.
  `./bandai30 -data ./data -fetch-item "30MM:01_5078"`.

Your own collection state (owned / wanted / building / finished, notes, Chinese
names, uploaded photos) is yours alone and is never scraped — it lives only in
your `data/` dir.

## Run locally

```sh
go build -o bandai30 ./cmd/bandai30
BANDAI30_ADMIN_USER=rei BANDAI30_ADMIN_PASS=changeme ./bandai30 -addr 127.0.0.1:3010 -data ./data
# http://127.0.0.1:3010
```

## Run with Docker

```sh
git clone <this repo> && cd bandai30
docker compose up -d --build
# http://localhost:3010 — empty at first, populated within ~5 min
```

`data/` on the host is bind-mounted to `/data` in the container, so the DB and
photos survive image rebuilds and `docker rm`. Never bake them into the image.

Config via environment (see `.env.example`):

| Variable | Meaning |
|---|---|
| `BANDAI30_NO_AUTH` | `1` disables app login — only safe behind a trusted network layer |
| `BANDAI30_ADMIN_USER` / `_PASS` | first-run admin, created when the users table is empty |
| `BANDAI30_SCRAPE_INTERVAL` | e.g. `168h` (weekly); **unset means no automatic refresh ever** |
| `BANDAI30_NTFY_TOPIC` | ntfy.sh topic for "new item" push notifications |
| `BANDAI30_DATA_DIR` | host path to bind-mount; must be absolute when compose runs from elsewhere |

### Exposing it publicly (Cloudflare Tunnel)

Run `cloudflared` on the host (not as a compose service — that way redeploying
the app never drops the tunnel) and point the public hostname at
`http://localhost:3010`. The tunnel egresses, so no inbound port forwarding is
needed and Cloudflare terminates HTTPS at the edge. Put Cloudflare Access in
front of it if you run with `BANDAI30_NO_AUTH=1`.

### One-time legacy import

If you're migrating from the old `~/30ms/data.json`:

```sh
./bandai30 -data ./data -seed-from /path/to/old/data.json -import-only
```

It's idempotent (skips IDs that already exist).

## API

All endpoints require a session cookie except `/api/auth/login` and the SPA
itself.

| Endpoint                          | Notes                          |
|-----------------------------------|--------------------------------|
| `POST /api/auth/login`            | `{username, password}`         |
| `POST /api/auth/logout`           |                                |
| `GET  /api/auth/me`               |                                |
| `GET  /api/items?series=&q=&...`  | filter list                    |
| `POST /api/items`                 | create                         |
| `GET  /api/items/{id}`            |                                |
| `PUT  /api/items/{id}`            | update                         |
| `DELETE /api/items/{id}`          |                                |
| `POST /api/upload`                | multipart file → photo URL     |
| `POST /api/scrape/{30ms\|30mp\|30mm\|30mf}` | refresh from Bandai  |
| `GET  /api/stats`                 | per-series status counts       |
| `GET  /api/categories?series=`    | distinct categories            |
| `GET  /api/export`                | downloads full JSON            |
| `POST /api/import`                | restore JSON                   |
| `GET  /photos/{name}`             | gated; served from disk        |

## Data layout

```
data/
├── bandai30.db        # SQLite (WAL mode); back this up
└── photos/            # all images (scraped + user uploads)
```

The two together are the entire state. Backing up = copying the `data/` dir.

The catalog half of that is reproducible (delete it and the first-run scrape
rebuilds it), but your collection state is not — that's what backups are for.
