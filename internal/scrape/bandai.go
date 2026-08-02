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
	"sync"
	"time"

	"github.com/rei/bandai30/internal/store"
)

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

var (
	// Two kinds of card sit in the same listing: regular items link to the
	// hobby site, Premium Bandai exclusives link out to the PB store. Both
	// carry the same title/price/date/image markup, so capture either and
	// derive the id from whichever group matched.
	cardBlockRE = regexp.MustCompile(`(?s)<a href="https://(?:bandai-hobby\.net/item/(01_\d+)|p-bandai\.jp/item/item-(\d+))/"[^>]*>(.*?)</a>`)
	titleRE     = regexp.MustCompile(`<div class="p-card__tit">([^<]*)</div>`)
	imgRE       = regexp.MustCompile(`<img src="([^"]+)"[^>]*alt="([^"]*)"`)
	priceRE     = regexp.MustCompile(`<div class="p-card__price">([^<]*)</div>`)
	dateRE      = regexp.MustCompile(`<div class="p-card_date">([^<]*)</div>`)
	// Match the numbered pagination anchors only: the page also carries
	// unrelated "?p=" links (news teasers) with much larger numbers.
	paginRE     = regexp.MustCompile(`c-archives__pagination-list-item-link"[^>]*>\s*(\d+)\s*<`)
	akamaiRE    = regexp.MustCompile(`https://bandai-a\.akamaihd\.net/bc/img/model/xl/[\w]+_1\.jpg`)
	// Signed URL: the ?Expires/Signature query is part of it — dropping the
	// query yields a 403. The signature is short-lived (~3 min), so a stale
	// listing page is a real failure mode, not a parse bug.
	cfBackupRE  = regexp.MustCompile(`https://d[a-z0-9]+\.cloudfront\.net/hobby/[^"']+\.jpg[^"']*`)
)

type Client struct {
	HTTP      *http.Client
	PhotosDir string
	Store     *store.Store

	// mu guards state, the single in-flight refresh shared by the weekly
	// scheduler and the UI's refresh button. See run.go.
	mu    sync.Mutex
	state RunState
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
	// Computed once per collection: hashing the photo dir per item would be
	// wasteful, and a placeholder that appears mid-run can wait for the next.
	shared := c.sharedCovers()
	for p := 1; p <= maxPage; p++ {
		if err := c.scrapePage(ctx, brand, series, p, rep, shared); err != nil {
			rep.Failures = append(rep.Failures, fmt.Sprintf("page %d: %v", p, err))
		}
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

func (c *Client) scrapePage(ctx context.Context, brand, series string, page int, rep *Report, shared map[string]bool) error {
	body, err := c.fetch(ctx, c.brandURL(brand, page), brand)
	if err != nil {
		return err
	}

	for _, blk := range cardBlockRE.FindAllSubmatch(body, -1) {
		itemID := string(blk[1])
		if itemID == "" {
			itemID = "pb-" + string(blk[2]) // Premium Bandai exclusive
		}
		inner := blk[3]
		title := firstSubmatch(titleRE, inner)
		_, imgURL := imgFields(inner)
		price := parsePriceDigits(firstSubmatch(priceRE, inner))
		date := parseDate(firstSubmatch(dateRE, inner))

		rep.ItemsFound++

		// existing tells us whether this is a brand-new item and whether a
		// photo is already on file. User fields need no copying — see
		// store.UpsertCatalog, which never writes them.
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
			it.PhotoURL = existing.PhotoURL
		}
		// A cover shared with other items is a brand-logo placeholder, not a
		// photo of this product: forget it so the code below fetches again,
		// which picks up the real shot as soon as Bandai publishes one.
		if it.PhotoURL != "" && c.coverIsPlaceholder(shared, itemID) {
			it.PhotoURL = ""
		}

		// Download photo if we don't already have one.
		if it.PhotoURL == "" {
			photoPath := filepath.Join(c.PhotosDir, itemID+".jpg")
			// Reuse a file already on disk — unless it is the placeholder we
			// just rejected, in which case it must be overwritten.
			if _, err := os.Stat(photoPath); err == nil && !c.coverIsPlaceholder(shared, itemID) {
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

		if err := c.Store.UpsertCatalog(ctx, &it); err != nil {
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

// priceLeadRE grabs the FIRST run of digits (commas allowed), which is the
// amount itself.
var priceLeadRE = regexp.MustCompile(`[\d,]*\d`)

// parsePriceDigits extracts the amount from a catalogue price string:
// "2,200円(税10%込)" → "2200", "2,000Yen" → "2000", "オープン価格" → "".
//
// It must not simply keep every digit: the JP catalogue spells the tax rate
// inside the same string, so "880円(税10%込)" would come out as "88010".
func parsePriceDigits(s string) string {
	m := priceLeadRE.FindString(s)
	return strings.ReplaceAll(m, ",", "")
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

// brandURL carries a cache-busting parameter on purpose.
//
// The listing HTML is CDN-cached, but the card images are signed CloudFront
// URLs valid for only a few minutes. A cached page therefore hands out
// signatures that expired hours ago and every image fetch returns 403. Asking
// for an uncached render costs ~60 requests a week and gets both fresh
// signatures and newly listed items.
func (c *Client) brandURL(brand string, page int) string {
	bust := time.Now().UnixNano()
	if page <= 1 {
		return fmt.Sprintf("https://bandai-hobby.net/brand/%s/?_=%d", brand, bust)
	}
	return fmt.Sprintf("https://bandai-hobby.net/brand/%s/?p=%d&_=%d", brand, page, bust)
}

func (c *Client) fetch(ctx context.Context, url, brand string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "ja,en;q=0.8")
	req.Header.Set("Referer", "https://bandai-hobby.net/brand/"+brand+"/")
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
	req.Header.Set("Referer", "https://bandai-hobby.net/")
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
	// Premium Bandai items have no hobby-site detail page; the listing card is
	// the only source for their image.
	if strings.HasPrefix(itemID, "pb-") {
		return "", fmt.Errorf("no hobby-site detail page for %s", itemID)
	}
	body, err := c.fetch(ctx, "https://bandai-hobby.net/item/"+itemID+"/", "30ms")
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
// Categorize buckets an item by its Japanese product name. The catalogue is
// scraped from bandai-hobby.net, so every name is Japanese — including the
// Premium Bandai entries from the lineup pages.
//
// The rules were derived from the full JP catalogue and checked against the
// 423 items whose category was already known from the English names: all 423
// land in the same bucket.
func Categorize(name, series string) string {
	u := strings.ToUpper(name)
	switch {
	case strings.Contains(name, "オプションボディパーツ"):
		return "Option Body"
	// "オプションヘアスタイル&フェイスパーツセット" is filed under hair, so this
	// must come before the face check. The loose prefix also absorbs a typo in
	// the catalogue ("パーﾂ" with a half-width ﾂ) that a stricter match misses.
	case strings.Contains(name, "オプションヘアスタイル"), strings.Contains(name, "ヘアスタイルパーツ"):
		return "Option Hair"
	case strings.Contains(name, "フェイスパーツ"), strings.Contains(name, "表情"):
		return "Option Face"
	case strings.Contains(name, "ハンドパーツ"):
		return "Option Hand"
	case strings.Contains(name, "デカール"):
		return "Decals"
	case strings.Contains(name, "オプションパーツセット"):
		return "Option Parts Set"
	case strings.Contains(u, "SIS-"):
		return "Sisters"
	case series == "30MP":
		return "Figure"
	case series == "30MM":
		// Accessory sets first, so they don't fall into the mecha bucket.
		// "エグザビー" covers both エグザビークル (vehicle) and エグザビースト (beast).
		switch {
		case strings.Contains(name, "カスタマイズ"), strings.Contains(name, "エグザビー"),
			strings.Contains(name, "オプション"):
			return "Option Parts"
		}
		return "Mecha" // eEXM/EXM originals + licensed (Armored Core, Daemon X Machina)
	case series == "30MF":
		switch {
		case strings.Contains(name, "カスタマイズ"), strings.Contains(name, "クラスアップアーマー"),
			strings.Contains(name, "オプション"), strings.Contains(name, "アイテムショップ"):
			return "Option Parts"
		}
		return "Fantasy"
	}
	return "Coordination"
}

var monthIdx = map[string]int{
	"Jan": 1, "Feb": 2, "Mar": 3, "Apr": 4, "May": 5, "Jun": 6,
	"Jul": 7, "Aug": 8, "Sep": 9, "Oct": 10, "Nov": 11, "Dec": 12,
}

var (
	dateFullRE  = regexp.MustCompile(`^(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+(\d{1,2}),\s+(\d{4})`)
	dateMonthRE = regexp.MustCompile(`^(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+(\d{4})`)
	// JP catalogue: "2023年12月09日 (土)" and the month-only "2026年12月".
	dateJPFullRE  = regexp.MustCompile(`(\d{4})年(\d{1,2})月(\d{1,2})日`)
	dateJPMonthRE = regexp.MustCompile(`(\d{4})年(\d{1,2})月`)
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
	if m := dateJPFullRE.FindStringSubmatch(raw); m != nil {
		return fmt.Sprintf("%s-%02d-%02d", m[1], atoi(m[2]), atoi(m[3]))
	}
	if m := dateJPMonthRE.FindStringSubmatch(raw); m != nil {
		return fmt.Sprintf("%s-%02d", m[1], atoi(m[2]))
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
