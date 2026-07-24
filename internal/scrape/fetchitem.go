package scrape

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rei/bandai30/internal/store"
)

// The brand listing only shows in-production items, so discontinued regular
// items silently drop off it even though their detail page still exists.
// FetchItem pulls a single item straight from its global.bandai-hobby.net
// detail page so such gaps can be filled in by item id.

var (
	titleTagRE = regexp.MustCompile(`(?s)<title>([^<]*)</title>`)
)

func detailField(body []byte, label string) string {
	re := regexp.MustCompile(`(?s)labelInner">` + regexp.QuoteMeta(label) + `<.*?labelTxt">\s*([^<]+?)\s*<`)
	m := re.FindSubmatch(body)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(string(m[1]))
}

// FetchItem scrapes one item's detail page and upserts it under series.
func (c *Client) FetchItem(ctx context.Context, itemID, series string) (*Report, error) {
	rep := &Report{Brand: "item:" + itemID}
	body, err := c.fetch(ctx, "https://global.bandai-hobby.net/en-us/item/"+itemID+"/", "30ms")
	if err != nil {
		return rep, err
	}
	rawTitle := ""
	if m := titleTagRE.FindSubmatch(body); m != nil {
		rawTitle = string(m[1])
	}
	// title is "<name>｜BANDAI HOBBY SITE"
	name := strings.TrimSpace(strings.Split(rawTitle, "｜")[0])
	if name == "" {
		return rep, fmt.Errorf("no product name on detail page for %s", itemID)
	}
	rep.ItemsFound++

	if err := os.MkdirAll(c.PhotosDir, 0o755); err != nil {
		return rep, err
	}
	it := store.Item{
		ID:          itemID,
		Series:      series,
		Category:    Categorize(name, series),
		Name:        name,
		Price:       parsePriceDigits(detailField(body, "Price")),
		ReleaseDate: parseDate(detailField(body, "Launch date")),
		Status:      "none",
	}
	if ex, _ := c.Store.GetItem(ctx, itemID); ex != nil {
		it.NameZh = ex.NameZh
		it.Notes = ex.Notes
		it.Status = ex.Status
		it.PhotoURL = ex.PhotoURL
		it.CreatedAt = ex.CreatedAt
	}

	if it.PhotoURL == "" {
		photoPath := filepath.Join(c.PhotosDir, itemID+".jpg")
		if _, err := os.Stat(photoPath); err == nil {
			// already on disk (e.g. fetched separately via browser)
			it.PhotoURL = "/photos/" + itemID + ".jpg"
		} else if m := akamaiRE.Find(body); m != nil {
			if err := c.downloadImage(ctx, string(m), photoPath); err != nil {
				rep.Failures = append(rep.Failures, fmt.Sprintf("%s photo: %v", itemID, err))
			} else {
				it.PhotoURL = "/photos/" + itemID + ".jpg"
				rep.NewPhotos++
			}
		}
	}

	if err := c.Store.UpsertItem(ctx, &it); err != nil {
		return rep, err
	}
	rep.Upserted++
	return rep, nil
}
