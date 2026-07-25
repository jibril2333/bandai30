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

// RunState is a snapshot of the current (or last finished) refresh, shaped for
// the UI's progress display.
type RunState struct {
	Running   bool   `json:"running"`
	Trigger   string `json:"trigger"`   // "manual" | "weekly" | "first-run"
	Scope     string `json:"scope"`     // collection code, or "" for all
	Current   string `json:"current"`   // collection being scraped right now
	Completed int    `json:"completed"` // collections finished
	Total     int    `json:"total"`     // collections in this run

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
func (c *Client) Run(ctx context.Context, scope, trigger string) ([]string, error) {
	if err := c.begin(scope, trigger); err != nil {
		return nil, err
	}
	return c.finish(ctx, scope, trigger)
}

// Start is Run for callers that must not block, such as an HTTP handler. The
// slot is reserved synchronously, so a caller that gets nil back can report
// "running" immediately and a second caller gets ErrBusy rather than racing.
func (c *Client) Start(ctx context.Context, scope, trigger string, done func([]string, error)) error {
	if err := c.begin(scope, trigger); err != nil {
		return err
	}
	go func() {
		fresh, err := c.finish(ctx, scope, trigger)
		if done != nil {
			done(fresh, err)
		}
	}()
	return nil
}

func (c *Client) begin(scope, trigger string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state.Running {
		return ErrBusy
	}
	c.state = RunState{Running: true, Trigger: trigger, Scope: scope, StartedAt: time.Now().Unix()}
	return nil
}

func (c *Client) finish(ctx context.Context, scope, trigger string) ([]string, error) {
	fresh, err := c.run(ctx, scope, trigger)

	c.mu.Lock()
	c.state.Running = false
	c.state.Current = ""
	c.state.FinishedAt = time.Now().Unix()
	if err != nil {
		c.state.Err = err.Error()
	}
	c.mu.Unlock()
	return fresh, err
}

func (c *Client) run(ctx context.Context, scope, trigger string) ([]string, error) {
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

		c.mu.Lock()
		c.state.NewItems = append(c.state.NewItems, got...)
		c.state.Completed++
		c.mu.Unlock()

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
