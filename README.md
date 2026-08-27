# bandai30

Personal tracker for Bandai collectibles. Collections are **data-driven** — each
product line is a row in the `collections` table grouped into a *family*:

| Family | Collections | Source |
|--------|-------------|--------|
| 30 Minutes | 30MS, 30MP, 30MM, 30MF | bandai-hobby.net |
| Tamashii Nations | METAL BUILD | tamashiiweb.com |

Adding a new line (e.g. 超合金, S.H.Figuarts) = inserting a collection row with
a scraper type + brand slug; no code change.

You mark each item with one status: 未拥有 → 想要 → **未到货** (paid for, not
delivered) → 未拆 → 在做 → 已完成. 已拥有 counts only what's in hand, so
未到货 is tracked and surfaced separately rather than folded into it.

Single Go binary + embedded SPA + SQLite + photo store. Built for NAS deployment
behind Cloudflare Tunnel.

## Scrapers

- **bandai-hobby**: paginates `bandai-hobby.net/brand/<slug>/`, parses the
  server-rendered product cards. The **Japanese** site, not the `en-us` one:
  its prices are tax-inclusive (the English site quotes them ex-tax, e.g. 800
  vs 880 for the same part) and it lists items the English site omits. Product
  names are therefore Japanese, matching Tamashii and the JP storefronts.
  Card images are short-lived signed CloudFront URLs — a listing page held for
  more than a few minutes yields 403s on the image fetch.
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

### What gets pushed

An automatic run sends one ntfy message covering both new arrivals **and edits
to items already in the catalogue** — Bandai revises prices and slips release
months after announcing a product, and for something on the wishlist or already
ordered that is the news. Price, release date and name are compared; category
is derived from the name so it would only echo a rename, and photos change far
too often to be worth a ping.

A blank incoming value is never reported as a change: the catalogue
occasionally serves an empty field, and `UpsertCatalog` keeps the old value in
that case, so "¥2200 → nothing" would describe something that did not happen.

Manual refreshes stay silent — the result is already on screen.

Topic, server and token are configured on the **settings page**, not in the
environment, and live in the database like the schedules do. On the Mac they
came from repo secrets that the deploy workflow injected; once this moved to a
NAS that CI never touches, an environment variable would have meant editing
YAML on the box to change a topic. A token is only needed for a reserved or
self-hosted topic.

The settings page has a **发送测试** button, because the alternative way to
learn that a token is wrong is to not receive the one notification you wanted,
with the error sitting in a container log. Publishing uses ntfy's JSON API
rather than its header form: titles here contain Japanese and Chinese, and HTTP
headers are latin-1 only.

### Full vs incremental

A refresh comes in two sizes, because their costs differ by two orders of
magnitude:

| | reads | cost |
|---|---|---|
| **incremental** (default) | brand listings only; galleries filled in for items that lack one | a handful of pages, ~1 min |
| **full** | the above, plus EVERY item's detail page, re-reading its gallery | ~700 requests, paced 400ms apart, several minutes |

Existing items are updated either way: name, price, release date and category
come off the listing on every run, so a price change or a renamed product is
picked up without a full pass. Full mode exists for the gallery, which only the
detail page carries.

Each mode has its OWN schedule, set on the settings page (⚙ in the header):
weekly listings and monthly detail pages is a sensible pairing. Whichever falls
due first runs; a full pass covers the incremental ground too, so it resets
both clocks. Both intervals live in the database — the container is recreated
on every deploy, so anything kept only in the environment would revert.
`BANDAI30_SCRAPE_INTERVAL` / `BANDAI30_FULL_INTERVAL` only seed the defaults
for a fresh install.

The cover is the gallery's first shot, not the listing thumbnail. The two are
not always the same picture — 30MF リーベルフォートレス shows loose runner
parts on its card and the assembled model on its detail page — and the detail
page is also the larger image now that listings serve 450px. Items with no
readable detail page (Premium Bandai) keep their listing thumbnail, and a photo
the owner uploaded is never replaced.

The full pass also repairs **placeholder covers**. Bandai serves a brand logo
for products announced before their photos are shot; since a cover is only
fetched when an item has none, that logo used to stick forever. A cover that is
byte-identical across several items cannot be a photo of any one of them, so it
is treated as missing and re-fetched until the real shot appears.

### Refreshing by hand

The **检查更新** button on the dashboard refreshes every collection; the same
button on a collection page refreshes just that one. **全量** next to it runs
the same scope in full mode. A refresh runs for
minutes, far longer than an HTTP request should live, so the button starts it
in the background and polls `GET /api/scrape/status` for progress — reloading
the page mid-run picks the progress back up.

Exactly one refresh exists process-wide, shared with the weekly job: pressing
the button while that is running returns 409 and the button simply follows the
run already in flight.

Two known gaps the automatic scrape can't cover, because Bandai's brand
listings omit them — fill them in by hand if you want them:

- **Premium Bandai exclusives** — a separate lineup-page scraper.
- **Discontinued items** whose detail page still exists, e.g.
  `./bandai30 -data ./data -fetch-item "30MM:01_5078"`.

Your own collection state (owned / wanted / building / finished, notes, Chinese
names, uploaded photos) is yours alone and is never scraped — it lives only in
your `data/` dir.

## Your own photos

Every item has a **我的照片** section holding pictures you took — the built
model, the box on the shelf, a paint job. They lead the detail-page gallery,
ahead of Bandai's press shots, and the grid marks the items you've
photographed.

They live in their own table, not in the scraped gallery, because a refresh
replaces an item's gallery wholesale (`SetItemPhotos` deletes then re-inserts).
Anything of yours kept there would disappear at the next scrape, and unlike a
press shot it cannot be re-downloaded.

Only paths this server stored are accepted — the upload endpoint's own
`/photos/...`. A remote URL would leave the picture on someone else's host,
free to vanish or change, which is the opposite of what "my photo" means.
Deleting a photo drops the row but leaves the file: your cover may still point
at it, and an orphaned image costs a few hundred KB where a broken cover is a
visible bug.

## Backups

The catalogue is reproducible; the collection is not. On 2026-07-31 the
database silently lost 18 of its 64 pages — header included — and the marks
were only recovered by parsing surviving pages and replaying the audit log.

So the app snapshots itself. `VACUUM INTO` writes a fully-checkpointed copy
from inside the process: no lock the app doesn't already hold, no outside
process touching the bind-mounted file (the layer that most likely dropped
those pages). Each snapshot is then reopened and checked — `integrity_check`
plus a row count — and **deleted if it fails**, because an unverified backup is
discovered to be useless only when it is needed.

Daily by default, keeping 14 (~300 KB each), both configurable on the settings
page along with a "立即备份" button and a per-snapshot download link. Take that
download occasionally: the snapshots sit on the same disk as the database, so
they survive file corruption but not a dead drive.

Only the database is snapshotted. `photos/` is ~700 MB and every image can be
re-fetched from Bandai.

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
| `BANDAI30_DATA_DIR` | host path to bind-mount; must be absolute when compose runs from elsewhere |

## Deploying to the NAS

Production runs on the TrueNAS SCALE box, not on the Mac. `git push origin
main` builds a `linux/amd64` image on GitHub's runners, publishes it to GHCR,
then asks watchtower on the NAS — over Tailscale — to pull it.

**Nothing in this repo executes on the NAS.** It pulls a finished image. That
is deliberate: a self-hosted runner executes whatever a workflow says, and the
blast radius there would be the machine holding the household's storage. What
CI can do is ask watchtower to pull labelled images. Not a shell, not SSH; the
Docker socket stays inside watchtower's container.

The image is multi-arch. The NAS is x86_64, so amd64 is the half that gets
deployed; arm64 is built because the image is public and an Apple Silicon
machine should be able to `docker run` it without compiling Go from source.
Each architecture builds on a runner of that architecture rather than through
QEMU, and GitHub's arm64 runners are free for public repositories.

The deploy is verified, not assumed: it waits until `/api/health` on the NAS
reports the **commit this build came from**, then checks the catalogue is
non-empty. Gating on "something answers 200" is satisfied instantly by the
container that was already running, and an empty catalogue is the shape of the
mount-the-wrong-directory failure this app hit once on the Mac.

Runtime configuration (ntfy credentials, the data path) now lives in the app's
YAML on the NAS rather than in GitHub secrets, because CI no longer touches
that machine. See `docker-compose.nas.yml`.

### Why it moved off the Mac

There used to be a self-hosted runner on the Mac that built and recreated the
container in place. It is gone, along with the runner registration: a runner
executes whatever a workflow says, and this repo is public.


On the Mac, `data/` sat on a Docker Desktop bind mount — a VirtioFS forwarding
layer — and the database was silently corrupted twice, on 2026-07-31 and
2026-08-06, losing whole pages with the file header among them. A local ZFS
dataset under native Linux Docker removes that layer. The data directory must
be a **local** dataset for the same family of reasons: SQLite coordinates
writers with POSIX locks, and network filesystems do not implement them
faithfully.

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
| `GET  /api/items/{id}/photos`     | the owner's own photos         |
| `POST /api/items/{id}/photos`     | attach one (uploaded path only)|
| `DELETE /api/items/{id}/photos/{photoID}` | detach one             |
| `POST /api/scrape`                | start a refresh of everything  |
| `POST /api/scrape/{slug}`         | start a refresh of one line    |
| `GET  /api/scrape/status`         | progress of the current run    |
| `GET  /api/stats`                 | per-series status counts       |
| `GET  /api/categories?series=`    | distinct categories            |
| `GET  /api/export`                | downloads full JSON            |
| `POST /api/import`                | restore JSON                   |
| `GET  /photos/{name}`             | gated; served from disk        |
| `GET  /api/health`                | **no session required** — `{ok, version}` |
| `GET  /api/settings`              | schedules + ntfy; never returns the token |
| `PUT  /api/settings`              | omit `ntfyToken` to keep the stored one   |
| `POST /api/settings/ntfy-test`    | send one notification with stored config  |

## Data layout

```
data/
├── bandai30.db        # SQLite (WAL mode); back this up
└── photos/            # all images (scraped + user uploads)
```

The two together are the entire state. Backing up = copying the `data/` dir.

The catalog half of that is reproducible (delete it and the first-run scrape
rebuilds it), but your collection state is not — that's what backups are for.
