package scrape

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/rei/bandai30/internal/store"
)

// A refresh takes minutes and writes to every collection, so exactly one may
// be in flight at a time — process-wide, whoever asked for it. The weekly
// scheduler, the first-run import and the UI's refresh button all go through
// Run, so a button press during the weekly job is rejected instead of running
// a second pass that would double the load on Bandai and interleave writes.

// ErrBusy is returned by Run when a refresh is already in progress.
var ErrBusy = errors.New("a refresh is already running")

// Mode selects how much of the catalogue a refresh re-reads.
type Mode string

const (
	// ModeIncremental reads the brand listings only. Official fields on every
	// listed item are refreshed, new items get their cover and gallery, and
	// items still missing a gallery have it filled in. A couple of minutes.
	ModeIncremental Mode = "incremental"
	// ModeFull additionally opens EVERY item's detail page to re-read its
	// gallery. One request per item — hundreds of them — so it is the manual,
	// occasional option.
	ModeFull Mode = "full"
)

func (m Mode) valid() bool { return m == ModeIncremental || m == ModeFull }

// RunState is a snapshot of the current (or last finished) refresh, shaped for
// the UI's progress display.
type RunState struct {
	Running   bool   `json:"running"`
	Trigger   string `json:"trigger"`   // "manual" | "weekly" | "first-run"
	Mode      Mode   `json:"mode"`      // "incremental" | "full"
	Scope     string `json:"scope"`     // collection code, or "" for all
	Current   string `json:"current"`   // collection being scraped right now
	Completed int    `json:"completed"` // collections finished
	Total     int    `json:"total"`     // collections in this run
	Photos    int    `json:"photos"`    // gallery shots downloaded

	// Progress within the current collection. The gallery pass is the slow
	// part of a full run — minutes per collection — so it needs its own
	// counter or the UI looks frozen.
	Phase     string `json:"phase"`     // "listing" | "gallery" | ""
	ItemsDone int    `json:"itemsDone"` // items processed in this phase
	ItemsAll  int    `json:"itemsAll"`  // items to process in this phase

	NewItems   []string `json:"newItems"`   // "<code> · <name>"
	Failures   []string `json:"failures"`   // per-collection errors, run continues
	StartedAt  int64    `json:"startedAt"`  // unix seconds
	FinishedAt int64    `json:"finishedAt"` // unix seconds, 0 while running
	Err        string   `json:"err"`        // fatal error, empty on success
}

// State returns a copy of the current run state, safe to serialise.
func (c *Client) State() RunState {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.state
	s.NewItems = append([]string(nil), c.state.NewItems...)
	s.Failures = append([]string(nil), c.state.Failures...)
	return s
}

// Run refreshes collections and reports the brand-new items found.
//
// scope selects a single collection by Code; empty means every collection that
// has a scraper configured. It blocks for the duration of the run (minutes) —
// callers serving an HTTP request should run it in a goroutine and poll State.
// A collection that fails is recorded and skipped so one dead source can't
// abort the rest.
func (c *Client) Run(ctx context.Context, scope, trigger string, mode Mode) ([]string, error) {
	if err := c.begin(scope, trigger, mode); err != nil {
		return nil, err
	}
	return c.finish(ctx, scope, trigger, mode)
}

// Start is Run for callers that must not block, such as an HTTP handler. The
// slot is reserved synchronously, so a caller that gets nil back can report
// "running" immediately and a second caller gets ErrBusy rather than racing.
func (c *Client) Start(ctx context.Context, scope, trigger string, mode Mode, done func([]string, error)) error {
	if err := c.begin(scope, trigger, mode); err != nil {
		return err
	}
	go func() {
		fresh, err := c.finish(ctx, scope, trigger, mode)
		if done != nil {
			done(fresh, err)
		}
	}()
	return nil
}

func (c *Client) begin(scope, trigger string, mode Mode) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state.Running {
		return ErrBusy
	}
	if !mode.valid() {
		mode = ModeIncremental
	}
	c.state = RunState{Running: true, Trigger: trigger, Mode: mode, Scope: scope, StartedAt: time.Now().Unix()}
	return nil
}

func (c *Client) finish(ctx context.Context, scope, trigger string, mode Mode) ([]string, error) {
	fresh, err := c.run(ctx, scope, trigger, mode)

	c.mu.Lock()
	c.state.Running = false
	c.state.Current = ""
	c.state.Phase, c.state.ItemsDone, c.state.ItemsAll = "", 0, 0
	c.state.FinishedAt = time.Now().Unix()
	if err != nil {
		c.state.Err = err.Error()
	}
	c.mu.Unlock()
	return fresh, err
}

func (c *Client) run(ctx context.Context, scope, trigger string, mode Mode) ([]string, error) {
	cols, err := c.Store.ListCollections(ctx)
	if err != nil {
		return nil, err
	}
	var todo []store.Collection
	for _, col := range cols {
		if col.Scraper == "" {
			continue // manual-entry collection
		}
		if scope != "" && col.Code != scope {
			continue
		}
		todo = append(todo, col)
	}
	if len(todo) == 0 {
		return nil, errors.New("no scrapable collection matched")
	}

	c.mu.Lock()
	c.state.Total = len(todo)
	c.mu.Unlock()

	var fresh []string
	for i := range todo {
		col := todo[i]
		c.mu.Lock()
		// Code ("30MP"), not Name ("30 Minutes Preference"): this lands in a
		// button label that would otherwise resize on every collection.
		c.state.Current = col.Code
		c.mu.Unlock()

		c.setPhase("listing", 0, 0)
		rep, err := c.ScrapeCollection(ctx, &col)
		if err != nil {
			log.Printf("%s scrape %s: %v", trigger, col.Code, err)
			c.mu.Lock()
			c.state.Failures = append(c.state.Failures, col.Code+": "+err.Error())
			c.state.Completed++
			c.mu.Unlock()
			continue
		}
		var got []string
		for _, name := range rep.NewItems {
			got = append(got, col.Code+" · "+name)
		}
		fresh = append(fresh, got...)

		// rep.Failures holds per-item problems — a photo that wouldn't
		// download, an upsert that failed. They must reach the UI: without
		// this the run reports "no failures" while items silently end up
		// with no picture.
		var failed []string
		for _, f := range rep.Failures {
			failed = append(failed, col.Code+": "+f)
		}
		if len(failed) > 0 {
			log.Printf("%s %s: %d item-level failure(s), first: %s", trigger, col.Code, len(failed), failed[0])
		}

		c.mu.Lock()
		c.state.NewItems = append(c.state.NewItems, got...)
		c.state.Failures = append(c.state.Failures, failed...)
		c.state.Completed++
		c.mu.Unlock()

		if err := c.galleries(ctx, col.Code, mode, trigger); err != nil {
			return fresh, err // only ctx cancellation gets here
		}

		if i < len(todo)-1 {
			// Be polite between sites.
			select {
			case <-ctx.Done():
				return fresh, ctx.Err()
			case <-time.After(3 * time.Second):
			}
		}
	}
	return fresh, nil
}

// galleries fetches detail-page photos for one collection.
//
// Incremental only visits items that have no gallery yet, so a steady state
// costs nothing; full revisits everything, which is the point of it. Either
// way this is one request per item, paced so a full run doesn't hammer the
// site — it is expected to take minutes.
func (c *Client) galleries(ctx context.Context, code string, mode Mode, trigger string) error {
	var ids []string
	var err error
	if mode == ModeFull {
		items, e := c.Store.ListItems(ctx, store.ItemFilter{Series: code})
		err = e
		for _, it := range items {
			ids = append(ids, it.ID)
		}
	} else {
		ids, err = c.Store.ItemsWithoutGallery(ctx, code)
	}
	if err != nil {
		c.mu.Lock()
		c.state.Failures = append(c.state.Failures, code+": list for gallery: "+err.Error())
		c.mu.Unlock()
		return nil
	}
	if len(ids) == 0 {
		return nil
	}
	log.Printf("%s %s: gallery pass over %d item(s) (%s)", trigger, code, len(ids), mode)
	c.setPhase("gallery", 0, len(ids))

	for n, id := range ids {
		c.setPhase("gallery", n+1, len(ids))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(gallerySpacing):
		}
		n, err := c.FetchGallery(ctx, id)
		if err != nil {
			c.mu.Lock()
			c.state.Failures = append(c.state.Failures, code+": "+id+" gallery: "+err.Error())
			c.mu.Unlock()
			continue
		}
		if n > 0 {
			c.mu.Lock()
			c.state.Photos += n
			c.mu.Unlock()
		}
	}
	return nil
}

// setPhase records where inside a collection the run currently is.
func (c *Client) setPhase(phase string, done, all int) {
	c.mu.Lock()
	c.state.Phase, c.state.ItemsDone, c.state.ItemsAll = phase, done, all
	c.mu.Unlock()
}

// gallerySpacing paces detail-page requests. A full pass is ~700 of them; at
// this rate that is a few minutes, which is a fair trade for not hammering a
// site that owes us nothing.
const gallerySpacing = 400 * time.Millisecond
