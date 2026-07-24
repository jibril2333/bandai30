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

## Run locally

```sh
go build -o bandai30 ./cmd/bandai30
BANDAI30_ADMIN_USER=rei BANDAI30_ADMIN_PASS=changeme ./bandai30 -addr 127.0.0.1:3010 -data ./data
# http://127.0.0.1:3010
```

## Run on a NAS (Docker + Cloudflare Tunnel)

1. Create a tunnel: <https://one.dash.cloudflare.com> → Networks → Tunnels → Create
2. Copy the token, then on the NAS:

   ```sh
   git clone <this repo> && cd bandai30
   cp .env.example .env
   $EDITOR .env   # paste CF_TUNNEL_TOKEN, set BANDAI30_ADMIN_USER/PASS
   docker compose up -d --build
   ```

3. In the Cloudflare dashboard, point the public hostname (e.g.
   `bandai30.example.com`) to the **service URL** `http://app:8080`.

That's it — the tunnel egresses, no inbound port forwarding needed. Cloudflare
handles HTTPS at the edge.

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
