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

// Some 30-series characters are Premium Bandai (p-bandai.jp) exclusives that
// never appear on the regular global brand listing. The Japanese "lineup" page
// for a series DOES list them (linking to p-bandai), so we scrape that page to
// pick up the exclusives and fold them into the same series.
//
// Maps a brand slug → its lineup page. Only series that have such a page need an
// entry; the rest are skipped.
var lineupPBURL = map[string]string{
	"30ms": "https://bandai-hobby.net/site/30minutes_sisters/lineup/",
}

var (
	lineupItemRE = regexp.MustCompile(`(?s)<li class="lineupItem"><a href="([^"]+)".*?<p class="spec[^"]*">([^<]*)</p>\s*<h3 class="name[^"]*">(.*?)</h3>.*?<img src="([^"]+)"`)
	tagRE        = regexp.MustCompile(`<[^>]+>`)
	digitsRE     = regexp.MustCompile(`\d{5,}`)
)

// scrapeLineupPB scans the series lineup page and upserts the Premium-Bandai
// exclusive entries (the ones whose detail link points at p-bandai.jp).
func (c *Client) scrapeLineupPB(ctx context.Context, lineupURL, series string, rep *Report) {
	body, err := c.fetch(ctx, lineupURL, "30ms")
	if err != nil {
		rep.Failures = append(rep.Failures, fmt.Sprintf("lineup fetch: %v", err))
		return
	}
	for _, m := range lineupItemRE.FindAllSubmatch(body, -1) {
		href := string(m[1])
		if !strings.Contains(href, "p-bandai") {
			continue // regular item, already covered by the brand listing
		}
		idnum := digitsRE.FindString(href)
		if idnum == "" {
			continue
		}
		id := "pb-" + idnum
		spec := strings.TrimSpace(string(m[2]))       // "30MS SIS-Ac65n"
		name := cleanLineupName(string(m[3]))         // "パワラリー=パリトン(グラーヴェフォーム)"
		img := string(m[4])
		fullName := strings.TrimSpace(spec + " " + name)

		rep.ItemsFound++
		existing, _ := c.Store.GetItem(ctx, id)
		it := store.Item{
			ID:       id,
			Series:   series,
			Category: "Sisters",
			Name:     fullName,
			Status:   "none",
		}
		if existing != nil {
			it.NameZh = existing.NameZh
			it.Notes = existing.Notes
			it.Status = existing.Status
			it.PhotoURL = existing.PhotoURL
			it.Price = existing.Price
			it.ReleaseDate = existing.ReleaseDate
			it.CreatedAt = existing.CreatedAt
		}

		if it.PhotoURL == "" && img != "" {
			ext := filepath.Ext(img)
			if ext == "" {
				ext = ".png"
			}
			fname := id + ext
			photoPath := filepath.Join(c.PhotosDir, fname)
			if _, err := os.Stat(photoPath); err == nil {
				it.PhotoURL = "/photos/" + fname
			} else if err := c.downloadImage(ctx, img, photoPath); err != nil {
				rep.Failures = append(rep.Failures, fmt.Sprintf("%s photo: %v", id, err))
			} else {
				it.PhotoURL = "/photos/" + fname
				rep.NewPhotos++
			}
		}

		if err := c.Store.UpsertItem(ctx, &it); err != nil {
			rep.Failures = append(rep.Failures, fmt.Sprintf("%s upsert: %v", id, err))
			continue
		}
		rep.Upserted++
		if existing == nil {
			rep.NewItems = append(rep.NewItems, fullName)
		}
	}
}

func cleanLineupName(raw string) string {
	s := tagRE.ReplaceAllString(raw, " ") // drop <br> etc.
	return strings.TrimSpace(wsRE.ReplaceAllString(s, " "))
}
