package scrape

import (
	"context"
	"fmt"

	"github.com/rei/bandai30/internal/store"
)

// ScrapeCollection dispatches to the scraper named by the collection. The series
// code stored on items is always the collection's Code.
func (c *Client) ScrapeCollection(ctx context.Context, col *store.Collection) (*Report, error) {
	switch col.Scraper {
	case "bandai-hobby":
		return c.scrapeBandaiHobby(ctx, col.ScraperArg, col.Code)
	case "tamashii":
		return c.scrapeTamashii(ctx, col.ScraperArg, col.Code)
	case "":
		return nil, fmt.Errorf("collection %q has no scraper (manual entry only)", col.Code)
	default:
		return nil, fmt.Errorf("unknown scraper %q for collection %q", col.Scraper, col.Code)
	}
}
