// Package scrape pulls 30-series product data from bandai-hobby.net.
package scrape

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rei/bandai30/internal/store"
)

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

// Brand identifiers used by bandai-hobby.net URLs.
var Brands = []string{"30ms", "30mp", "30mm", "30mf"}

// SeriesOf maps a brand slug to the canonical series tag stored in items.series.
func SeriesOf(brand string) string {
	return strings.ToUpper(brand)
}

var (
	cardBlockRE = regexp.MustCompile(`(?s)<a href="https://global\.bandai-hobby\.net/en-us/item/(01_\d+)/"[^>]*>(.*?)</a>`)
	titleRE     = regexp.MustCompile(`<div class="p-card__tit">([^<]*)</div>`)
	imgRE       = regexp.MustCompile(`<img src="([^"]+)"[^>]*alt="([^"]*)"`)
	priceRE     = regexp.MustCompile(`<div class="p-card__price">([^<]*)</div>`)
	dateRE      = regexp.MustCompile(`<div class="p-card_date">([^<]*)</div>`)
	paginRE     = regexp.MustCompile(`<li class="p-pagination__list[^"]*"[^>]*>\s*<a[^>]*>(\d+)</a>`)
	akamaiRE    = regexp.MustCompile(`https://bandai-a\.akamaihd\.net/bc/img/model/xl/[\w]+_1\.jpg`)
	cfBackupRE  = regexp.MustCompile(`https://d[a-z0-9]+\.cloudfront\.net/hobby/[^"]+\.jpg`)
)

type Client struct {
	HTTP      *http.Client
	PhotosDir string
	Store     *store.Store
}

func New(st *store.Store, photosDir string) *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		PhotosDir: photosDir,
		Store:     st,
	}
}

// Report summarises the result of a scrape run.
type Report struct {
	Brand      string   `json:"brand"`
	ItemsFound int      `json:"itemsFound"`
	Duplicates int      `json:"duplicates"` // re-sale/batch rows collapsed into a canonical item
	Upserted   int      `json:"upserted"`
	NewPhotos  int      `json:"newPhotos"`
	NewItems   []string `json:"newItems,omitempty"` // titles of items not previously in the DB
	Failures   []string `json:"failures,omitempty"`
}

// scrapeBandaiHobby fetches every page of a bandai-hobby.net brand listing,
// upserts items under the given series code, and downloads missing images.
func (c *Client) scrapeBandaiHobby(ctx context.Context, brand, series string) (*Report, error) {
	rep := &Report{Brand: brand}
	maxPage, err := c.discoverPages(ctx, brand)
	if err != nil {
		return nil, fmt.Errorf("discover pages for %s: %w", brand, err)
	}
	if err := os.MkdirAll(c.PhotosDir, 0o755); err != nil {
		return nil, err
	}
	for p := 1; p <= maxPage; p++ {
		if err := c.scrapePage(ctx, brand, series, p, rep); err != nil {
			rep.Failures = append(rep.Failures, fmt.Sprintf("page %d: %v", p, err))
		}
	}
	// Premium Bandai exclusives aren't on the brand listing — pick them up from
	// the series lineup page, if one is known.
	if u := lineupPBURL[brand]; u != "" {
		c.scrapeLineupPB(ctx, u, series, rep)
	}
	return rep, nil
}

func (c *Client) discoverPages(ctx context.Context, brand string) (int, error) {
	body, err := c.fetch(ctx, c.brandURL(brand, 1), brand)
	if err != nil {
		return 0, err
	}
	max := 1
	for _, m := range paginRE.FindAllSubmatch(body, -1) {
		var n int
		fmt.Sscanf(string(m[1]), "%d", &n)
		if n > max {
			max = n
		}
	}
	return max, nil
}

func (c *Client) scrapePage(ctx context.Context, brand, series string, page int, rep *Report) error {
	body, err := c.fetch(ctx, c.brandURL(brand, page), brand)
	if err != nil {
		return err
	}

	for _, blk := range cardBlockRE.FindAllSubmatch(body, -1) {
		itemID := string(blk[1])
		inner := blk[2]
		title := firstSubmatch(titleRE, inner)
		_, imgURL := imgFields(inner)
		price := parsePriceDigits(firstSubmatch(priceRE, inner))
		date := parseDate(firstSubmatch(dateRE, inner))

		rep.ItemsFound++

		// Preserve user edits: nameZh/notes/status untouched on update.
		existing, _ := c.Store.GetItem(ctx, itemID)
		it := store.Item{
			ID:          itemID,
			Series:      series,
			Category:    Categorize(title, series),
			Name:        title,
			ReleaseDate: date,
			Price:       price,
			Status:      "none",
		}
		if existing != nil {
			it.NameZh = existing.NameZh
			it.Notes = existing.Notes
			it.Status = existing.Status
			it.PhotoURL = existing.PhotoURL
			it.CreatedAt = existing.CreatedAt
		}

		// Download photo if we don't already have one.
		if it.PhotoURL == "" {
			photoPath := filepath.Join(c.PhotosDir, itemID+".jpg")
			if _, err := os.Stat(photoPath); err == nil {
				it.PhotoURL = "/photos/" + itemID + ".jpg"
			} else if imgURL != "" {
				if err := c.downloadImage(ctx, imgURL, photoPath); err != nil {
					// Fall back to the unsigned akamai URL on the detail page.
					if alt, derr := c.fallbackImage(ctx, itemID); derr == nil && alt != "" {
						if err2 := c.downloadImage(ctx, alt, photoPath); err2 == nil {
							it.PhotoURL = "/photos/" + itemID + ".jpg"
							rep.NewPhotos++
						} else {
							rep.Failures = append(rep.Failures, fmt.Sprintf("%s photo: %v / %v", itemID, err, err2))
						}
					} else {
						rep.Failures = append(rep.Failures, fmt.Sprintf("%s photo: %v", itemID, err))
					}
				} else {
					it.PhotoURL = "/photos/" + itemID + ".jpg"
					rep.NewPhotos++
				}
			}
		}

		if err := c.Store.UpsertItem(ctx, &it); err != nil {
			rep.Failures = append(rep.Failures, fmt.Sprintf("%s upsert: %v", itemID, err))
			continue
		}
		rep.Upserted++
		if existing == nil {
			rep.NewItems = append(rep.NewItems, title)
		}
	}
	return nil
}

// parsePriceDigits strips everything but digits, e.g. "2,000Yen" → "2000",
// "¥17,600（税込）" → "17600". Returns "" if there's no number.
func parsePriceDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func imgFields(inner []byte) (alt, src string) {
	m := imgRE.FindSubmatch(inner)
	if m == nil {
		return "", ""
	}
	return string(m[2]), string(m[1])
}

func firstSubmatch(re *regexp.Regexp, b []byte) string {
	m := re.FindSubmatch(b)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(string(m[1]))
}

func (c *Client) brandURL(brand string, page int) string {
	if page <= 1 {
		return "https://global.bandai-hobby.net/en-us/brand/" + brand + "/"
	}
	return fmt.Sprintf("https://global.bandai-hobby.net/en-us/brand/%s/?p=%d", brand, page)
}

func (c *Client) fetch(ctx context.Context, url, brand string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", "https://global.bandai-hobby.net/en-us/brand/"+brand+"/")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) downloadImage(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "image/*")
	req.Header.Set("Referer", "https://global.bandai-hobby.net/")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if n < 1000 {
		os.Remove(tmp)
		return fmt.Errorf("image too small (%d bytes)", n)
	}
	return os.Rename(tmp, path)
}

func (c *Client) fallbackImage(ctx context.Context, itemID string) (string, error) {
	body, err := c.fetch(ctx, "https://global.bandai-hobby.net/en-us/item/"+itemID+"/", "30ms")
	if err != nil {
		return "", err
	}
	if m := akamaiRE.Find(body); m != nil {
		return string(m), nil
	}
	if m := cfBackupRE.Find(body); m != nil {
		return string(m), nil
	}
	return "", fmt.Errorf("no image url on detail page")
}

// Categorize maps a product name to a category. Heuristic; 30MP defaults to Figure.
func Categorize(name, series string) string {
	u := strings.ToUpper(name)
	switch {
	case strings.Contains(u, "OPTION BODY PARTS"):
		return "Option Body"
	case strings.Contains(u, "OPTION HAIR"):
		return "Option Hair"
	case strings.Contains(u, "FACE PARTS"), strings.Contains(u, "FACIAL EXPRESSION"):
		return "Option Face"
	case strings.Contains(u, "HAND PARTS"):
		return "Option Hand"
	case strings.Contains(u, "WATER DECAL"), strings.Contains(u, "DECAL"):
		return "Decals"
	// match both spellings: "OPTION PARTS SET" and "OPTIONAL PARTS SET (Speed Armor)"
	case strings.Contains(u, "OPTION PARTS SET"), strings.Contains(u, "OPTIONAL PARTS SET"):
		return "Option Parts Set"
	case strings.Contains(u, "30MS SIS-"):
		return "Sisters"
	case series == "30MP":
		return "Figure"
	case series == "30MM":
		// accessory sets first, so they don't fall into the mecha bucket
		switch {
		case strings.Contains(u, "CUSTOMIZE"), strings.Contains(u, "EXTENDED ARMAMENT"),
			strings.Contains(u, "VEHICLE"), strings.Contains(u, "OPTION"):
			return "Option Parts"
		}
		return "Mecha" // eEXM/EXM originals + licensed (Armored Core, Daemon X Machina)
	case series == "30MF":
		switch {
		case strings.Contains(u, "CUSTOMIZE"), strings.Contains(u, "CLASS UP ARMOR"),
			strings.Contains(u, "OPTION"):
			return "Option Parts"
		}
		return "Fantasy"
	case strings.Contains(u, "30MS "):
		return "Coordination"
	}
	return "Other"
}

var monthIdx = map[string]int{
	"Jan": 1, "Feb": 2, "Mar": 3, "Apr": 4, "May": 5, "Jun": 6,
	"Jul": 7, "Aug": 8, "Sep": 9, "Oct": 10, "Nov": 11, "Dec": 12,
}

var (
	dateFullRE  = regexp.MustCompile(`^(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+(\d{1,2}),\s+(\d{4})`)
	dateMonthRE = regexp.MustCompile(`^(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+(\d{4})`)
)

func parseDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if m := dateFullRE.FindStringSubmatch(raw); m != nil {
		return fmt.Sprintf("%s-%02d-%02d", m[3], monthIdx[m[1]], atoi(m[2]))
	}
	if m := dateMonthRE.FindStringSubmatch(raw); m != nil {
		return fmt.Sprintf("%s-%02d", m[2], monthIdx[m[1]])
	}
	return raw
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}
