// Command bandai30 is the entry point for the catalog server.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	bandai30 "github.com/rei/bandai30"
	"github.com/rei/bandai30/internal/api"
	"github.com/rei/bandai30/internal/auth"
	"github.com/rei/bandai30/internal/notify"
	"github.com/rei/bandai30/internal/scrape"
	"github.com/rei/bandai30/internal/store"
)

func main() {
	addr := flag.String("addr", envOrDefault("BANDAI30_ADDR", "0.0.0.0:3010"), "listen address")
	dataDir := flag.String("data", envOrDefault("BANDAI30_DATA", "./data"), "data directory (holds db + photos)")
	seedFrom := flag.String("seed-from", "", "path to a legacy data.json to import on startup (only runs if items table is empty)")
	importOnly := flag.Bool("import-only", false, "exit after seed import instead of serving")
	recategorize := flag.Bool("recategorize", false, "recompute every item's category with the current rules, then exit")
	fixPrices := flag.Bool("fix-prices", false, "normalize every item's price to a bare number, then exit")
	fetchItem := flag.String("fetch-item", "", "fetch specific items missed by the listing: 'SERIES:itemID[,SERIES:itemID...]', then exit")
	noAuth := flag.Bool("no-auth", envBool("BANDAI30_NO_AUTH"), "disable application-level login (network layer must enforce trust)")
	flag.Parse()

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}
	photosDir := filepath.Join(*dataDir, "photos")
	dbPath := filepath.Join(*dataDir, "bandai30.db")

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if added, err := st.SeedCollections(ctx); err != nil {
		log.Fatalf("seed collections: %v", err)
	} else if added > 0 {
		log.Printf("seeded %d collection(s)", added)
	}

	if *recategorize {
		// map each series code → its scraper so we pick the right categorizer
		cols, err := st.ListCollections(ctx)
		if err != nil {
			log.Fatalf("recategorize: %v", err)
		}
		scraperOf := map[string]string{}
		for _, c := range cols {
			scraperOf[c.Code] = c.Scraper
		}
		items, err := st.ListItems(ctx, store.ItemFilter{})
		if err != nil {
			log.Fatalf("recategorize: %v", err)
		}
		n := 0
		for _, it := range items {
			var cat string
			if scraperOf[it.Series] == "tamashii" {
				cat = scrape.CategorizeTamashii(it.Name, "")
			} else {
				cat = scrape.Categorize(it.Name, it.Series)
			}
			if cat != it.Category {
				if err := st.SetCategory(ctx, it.ID, cat); err != nil {
					log.Fatalf("recategorize %s: %v", it.ID, err)
				}
				n++
			}
		}
		log.Printf("recategorized %d of %d items", n, len(items))
		return
	}

	if *fetchItem != "" {
		sc := scrape.New(st, photosDir)
		for _, pair := range strings.Split(*fetchItem, ",") {
			parts := strings.SplitN(strings.TrimSpace(pair), ":", 2)
			if len(parts) != 2 {
				log.Printf("skip %q (want SERIES:itemID)", pair)
				continue
			}
			rep, err := sc.FetchItem(ctx, parts[1], parts[0])
			if err != nil {
				log.Printf("fetch %s: %v", pair, err)
				continue
			}
			log.Printf("fetched %s → upserted=%d newPhoto=%d failures=%d", pair, rep.Upserted, rep.NewPhotos, len(rep.Failures))
		}
		return
	}

	if *fixPrices {
		digits := func(s string) string {
			var b strings.Builder
			for _, r := range s {
				if r >= '0' && r <= '9' {
					b.WriteRune(r)
				}
			}
			return b.String()
		}
		items, err := st.ListItems(ctx, store.ItemFilter{})
		if err != nil {
			log.Fatalf("fix-prices: %v", err)
		}
		n := 0
		for _, it := range items {
			if d := digits(it.Price); d != it.Price {
				if err := st.SetPrice(ctx, it.ID, d); err != nil {
					log.Fatalf("fix-prices %s: %v", it.ID, err)
				}
				n++
			}
		}
		log.Printf("normalized %d of %d prices", n, len(items))
		return
	}

	if *seedFrom != "" {
		n, err := seedFromJSON(ctx, st, *seedFrom)
		if err != nil {
			log.Fatalf("seed-from: %v", err)
		}
		log.Printf("seeded %d items from %s", n, *seedFrom)
		if *importOnly {
			return
		}
	}

	if !*noAuth {
		created, err := auth.EnsureSeedUser(ctx, st,
			os.Getenv("BANDAI30_ADMIN_USER"),
			os.Getenv("BANDAI30_ADMIN_PASS"))
		if err != nil {
			log.Fatalf("seed user: %v", err)
		}
		if created != "" {
			log.Printf("created admin user: %s", created)
		}
	} else {
		log.Print("running with --no-auth: all endpoints open, current user = \"anon\"")
	}

	go reapSessions(ctx, st)

	webFS, err := fs.Sub(bandai30.WebFS, "web")
	if err != nil {
		log.Fatalf("sub fs: %v", err)
	}

	scraper := scrape.New(st, photosDir)
	notifier := notify.New(os.Getenv("BANDAI30_NTFY_SERVER"), os.Getenv("BANDAI30_NTFY_TOPIC"))
	// The environment only seeds the defaults; once the owner saves anything on
	// the settings page, the database wins. That matters here because the
	// container is recreated on every deploy.
	defInterval := os.Getenv("BANDAI30_SCRAPE_INTERVAL")
	defFull := os.Getenv("BANDAI30_FULL_INTERVAL")
	if cfg, err := st.GetSettings(ctx, defInterval, defFull); err == nil {
		log.Printf("auto-scrape: incremental=%v full=%v (ntfy: %v)",
			orOff(cfg.Interval()), orOff(cfg.Full()), notifier.Enabled())
	}
	// One goroutine for both, in sequence: the server starts listening
	// immediately (a fresh install would otherwise block it for minutes), and
	// the first-run import can't race the scheduled one — running two scrapes
	// against the same sources at once would double the load and interleave
	// writes. The first-run pass is independent of the interval: a new user
	// shouldn't have to configure a schedule to get any data at all.
	go func() {
		firstRunScrape(ctx, st, scraper)
		scheduledScrape(ctx, st, scraper, notifier, defInterval, defFull)
	}()
	// Backups get their own goroutine on purpose: a scrape that hangs on a
	// slow site must not be able to stop them, and they are the one thing
	// standing between a corrupt file and losing the collection.
	go scheduledBackup(ctx, st, filepath.Join(*dataDir, "backups"), notifier)

	srv := &api.Server{
		Store:     st,
		Scraper:   scraper,
		PhotosDir: photosDir,
		WebFS:     webFS,
		NoAuth:    *noAuth,
		BaseCtx:   ctx,
	}

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("bandai30 listening on http://%s (data=%s)", *addr, *dataDir)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	fmt.Println("bye")
}

// firstRunScrape populates a brand-new database in the background.
//
// Without this, a fresh install (empty data dir → empty DB) serves an empty
// catalog until the first scheduled tick fires — a full day away on the
// default 24h interval, or never if no interval is configured. It runs only
// when the items table is empty, so an existing deployment restarting never
// triggers an extra scrape.
//
// No ntfy push here on purpose: the first run turns up the ENTIRE catalog
// (~600 items), and a notification listing all of them is noise, not news.
func firstRunScrape(ctx context.Context, st *store.Store, scraper *scrape.Client) {
	n, err := st.ItemCount(ctx)
	if err != nil {
		log.Printf("first-run scrape: count items: %v", err)
		return
	}
	if n > 0 {
		return
	}
	log.Print("empty catalog: running first-time scrape (this takes a few minutes)")
	if err := st.SetMetaTime(ctx, lastScrapeKey, time.Now()); err != nil {
		log.Printf("first-run scrape: record run time: %v", err)
	}
	fresh, err := scraper.Run(ctx, "", "first-run", scrape.ModeIncremental)
	if err != nil {
		log.Printf("first-time scrape: %v", err)
		return
	}
	log.Printf("first-time scrape done: %d item(s) imported", len(fresh))
}

// lastScrapeKey stores when the last refresh was ATTEMPTED, so the schedule
// survives restarts.
const lastScrapeKey = "last_scrape_at"

// lastFullKey tracks the detail-page pass separately from lastScrapeKey.
const lastFullKey = "last_full_scrape_at"

// lastBackupKey tracks snapshots, which run on their own goroutine.
const lastBackupKey = "last_backup_at"

// settleDelay holds off an overdue scrape briefly after startup, so a
// redeploy doesn't fire a scrape while the container is still settling.
const settleDelay = 2 * time.Minute

// scheduledScrape refreshes every collection on a persistent schedule and
// pushes a phone notification (via ntfy) summarising any brand-new items.
//
// The interval is measured from the last attempt RECORDED IN THE DATABASE,
// not from process start. A plain time.Ticker restarts its countdown on every
// launch, which is fine at 24h but breaks at weekly cadence: this app is
// redeployed by git push and the Mac sleeps/reboots, so a week-long timer
// would keep getting reset and might never fire at all. Persisting the
// timestamp also means a scrape missed while the machine was off runs shortly
// after it comes back, instead of being silently skipped.
// scheduledScrape refreshes on a persistent schedule and pushes a phone
// notification (via ntfy) summarising any brand-new items.
//
// The interval and mode are re-read from the database every cycle, so a change
// made on the settings page takes effect at the next tick instead of needing a
// restart. defInterval/defMode seed the very first read from the environment.
//
// The interval is measured from the last attempt RECORDED IN THE DATABASE, not
// from process start. A plain time.Ticker restarts its countdown on every
// launch, which is fine at 24h but breaks at weekly cadence: this app is
// redeployed by git push and the Mac sleeps/reboots, so a week-long timer would
// keep getting reset and might never fire at all. Persisting the timestamp also
// means a scrape missed while the machine was off runs shortly after it comes
// back, instead of being silently skipped.
// scheduledScrape drives two independent schedules and pushes a phone
// notification (via ntfy) summarising any brand-new items.
//
// Incremental and full are separate because their costs differ by two orders
// of magnitude: reading the listings is cheap enough to do weekly, opening
// every item's detail page is not. Each keeps its own last-run timestamp; the
// loop waits for whichever falls due first. A full run also does everything an
// incremental one does, so it resets both clocks.
//
// Both intervals are re-read from the database every cycle, so a change on the
// settings page takes effect at the next tick rather than needing a restart.
// The timestamps live in the database for the same reason the settings do: a
// plain in-process timer restarts its countdown on every launch, and with this
// app redeployed by git push and the host sleeping, a week-long timer could
// keep getting reset and never fire. Persisting also means a run missed while
// the machine was off happens shortly after it returns.
func scheduledScrape(ctx context.Context, st *store.Store, scraper *scrape.Client, notifier *notify.Ntfy, defInterval, defFull string) {
	for {
		cfg, err := st.GetSettings(ctx, defInterval, defFull)
		if err != nil {
			log.Printf("auto-scrape: read settings: %v", err)
		}

		// Pick whichever mode is due soonest. due() is time.Time zero when the
		// mode is switched off.
		due := func(key string, every time.Duration) (time.Time, bool) {
			if every == 0 {
				return time.Time{}, false
			}
			last, err := st.GetMetaTime(ctx, key)
			if err != nil {
				log.Printf("auto-scrape: read %s: %v", key, err)
				return time.Time{}, false
			}
			if last.IsZero() {
				return time.Now(), true // never run: do it at the next opportunity
			}
			return last.Add(every), true
		}
		incAt, incOn := due(lastScrapeKey, cfg.Interval())
		fullAt, fullOn := due(lastFullKey, cfg.Full())

		if !incOn && !fullOn {
			// Both disabled. Idle politely and look again, so switching one
			// back on from the settings page doesn't need a restart either.
			select {
			case <-ctx.Done():
				return
			case <-time.After(settleDelay):
			}
			continue
		}

		mode, at := scrape.ModeIncremental, incAt
		if !incOn || (fullOn && fullAt.Before(incAt)) {
			mode, at = scrape.ModeFull, fullAt
		}
		wait := time.Until(at)
		if wait < settleDelay {
			// Never fire immediately on boot: a redeploy restarts this loop,
			// and an overdue job would then run on every deploy.
			wait = settleDelay
		}
		log.Printf("auto-scrape: next run %s in %s", mode, wait.Round(time.Minute))

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		// Record the attempt BEFORE checking results: if every source is down
		// we must still wait a full interval, or a failing scrape would retry
		// in a hot loop and hammer Bandai's servers.
		now := time.Now()
		if err := st.SetMetaTime(ctx, lastScrapeKey, now); err != nil {
			log.Printf("auto-scrape: record run time: %v", err)
		}
		if mode == scrape.ModeFull {
			// A full pass covers the incremental ground too, so both clocks reset.
			if err := st.SetMetaTime(ctx, lastFullKey, now); err != nil {
				log.Printf("auto-scrape: record full run time: %v", err)
			}
		}

		fresh, err := scraper.Run(ctx, "", "auto", mode)
		if err != nil {
			// Busy means a manual refresh is in flight; it covers the same
			// ground, so skip this tick rather than queueing behind it.
			log.Printf("auto-scrape: %v", err)
			continue
		}
		if len(fresh) > 0 {
			log.Printf("auto-scrape: %d new item(s)", len(fresh))
			body := strings.Join(fresh, "\n")
			if len(fresh) > 20 {
				body = strings.Join(fresh[:20], "\n") + "\n…"
			}
			title := fmt.Sprintf("Bandai30: %d new item(s)", len(fresh))
			if err := notifier.Send(ctx, title, body, "new,robot"); err != nil {
				log.Printf("auto-scrape: ntfy send: %v", err)
			}
		} else {
			log.Print("auto-scrape: no new items")
		}
	}
}

// scheduledBackup takes a verified snapshot on the configured cadence and
// prunes old ones.
//
// Like the scrape schedule, the clock is the timestamp in the database rather
// than a process-lifetime timer: the container is recreated on every deploy,
// and an in-process ticker would restart its countdown each time — a daily
// backup could then never fire on a week with daily deploys.
func scheduledBackup(ctx context.Context, st *store.Store, dir string, notifier *notify.Ntfy) {
	for {
		cfg, err := st.GetSettings(ctx, "", "")
		if err != nil {
			log.Printf("backup: read settings: %v", err)
		}
		every := cfg.BackupEvery()
		if every == 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(settleDelay):
			}
			continue
		}

		wait := settleDelay
		if last, err := st.GetMetaTime(ctx, lastBackupKey); err == nil && !last.IsZero() {
			if due := time.Until(last.Add(every)); due > wait {
				wait = due
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		// Record the attempt first, so a repeatedly failing backup retries on
		// the normal cadence instead of spinning.
		if err := st.SetMetaTime(ctx, lastBackupKey, time.Now()); err != nil {
			log.Printf("backup: record run time: %v", err)
		}
		path, err := st.Backup(ctx, dir)
		if err != nil {
			// Worth interrupting the owner for: this is the safety net itself
			// failing, and it failed silently once already.
			log.Printf("backup FAILED: %v", err)
			_ = notifier.Send(ctx, "Bandai30: 备份失败", err.Error(), "warning,floppy_disk")
			continue
		}
		n, _ := store.PruneBackups(dir, cfg.BackupKeep)
		log.Printf("backup: wrote %s (pruned %d old)", filepath.Base(path), n)
	}
}

func reapSessions(ctx context.Context, st *store.Store) {
	t := time.NewTicker(1 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = st.ReapExpiredSessions(ctx)
		}
	}
}

// orOff renders a duration for the startup log, or "off" when zero.
func orOff(d time.Duration) string {
	if d == 0 {
		return "off"
	}
	return d.String()
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string) bool {
	switch os.Getenv(key) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// seedFromJSON imports a legacy ~/30ms-style data.json. Skips items whose IDs
// are already in the DB so re-running is idempotent.
func seedFromJSON(ctx context.Context, st *store.Store, path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var legacy struct {
		Models []store.Item `json:"models"`
	}
	if err := json.Unmarshal(b, &legacy); err != nil {
		return 0, err
	}
	n := 0
	for i := range legacy.Models {
		it := legacy.Models[i]
		if it.ID == "" {
			continue
		}
		if it.Status == "" {
			it.Status = "none"
		}
		if it.Series == "" {
			it.Series = "30MS"
		}
		existing, _ := st.GetItem(ctx, it.ID)
		if existing != nil {
			continue // never clobber existing rows on re-seed
		}
		if err := st.UpsertItem(ctx, &it); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
