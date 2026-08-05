package scrape

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/rei/bandai30/internal/store"
)

// Tamashii (tamashiiweb.com) exposes a clean JSON search API used by its item
// search page:
//   GET /api/site-item/search_item.php?brandCode[]=<brand>&sort=2&per_page=100&current_page=N&area=japan
// Response: { data: [ {tamashiiWebId, title, priceTaxStr, releaseDateData, thumbnailImg, brandCode, mainBrandName} ], pagination: {total,lastPage} }

const tamashiiBase = "https://tamashiiweb.com"

type tamashiiResp struct {
	Data []struct {
		TamashiiWebID  string `json:"tamashiiWebId"`
		Title          string `json:"title"`
		PriceTaxStr    string `json:"priceTaxStr"`
		PriceText      string `json:"priceText"`
		ReleaseDate    string `json:"releaseDateData"` // "YYYY-MM" or "YYYY-MM-DD"
		ThumbnailImg   string `json:"thumbnailImg"`
		BrandCode      string `json:"brandCode"`
		MainBrandName  string `json:"mainBrandName"`
	} `json:"data"`
	Pagination struct {
		Total       int `json:"total"`
		LastPage    int `json:"lastPage"`
		CurrentPage int `json:"currentPage"`
	} `json:"pagination"`
}

// tamashiiRow is one raw API record.
type tamashiiRow struct {
	tid   int    // numeric tamashiiWebId
	id    string // "tw-<tid>"
	title string
	price string
	date  string
	img   string
}

func (c *Client) scrapeTamashii(ctx context.Context, brandCode, series string) (*Report, error) {
	rep := &Report{Brand: brandCode}
	if err := os.MkdirAll(c.PhotosDir, 0o755); err != nil {
		return nil, err
	}

	// 1. Collect every raw record across all pages.
	var rows []tamashiiRow
	page := 1
	for {
		resp, err := c.fetchTamashiiPage(ctx, brandCode, page)
		if err != nil {
			return nil, fmt.Errorf("tamashii page %d: %w", page, err)
		}
		for _, d := range resp.Data {
			if d.BrandCode != brandCode {
				continue // API can leak cross-brand rows; keep only this brand
			}
			rep.ItemsFound++
			price := parsePriceDigits(d.PriceTaxStr) // tax-included price → digits only
			if price == "" {
				price = parsePriceDigits(d.PriceText)
			}
			rows = append(rows, tamashiiRow{
				tid:   atoi(d.TamashiiWebID),
				id:    "tw-" + d.TamashiiWebID,
				title: d.Title,
				price: price,
				date:  normalizeTamashiiDate(d.ReleaseDate),
				img:   d.ThumbnailImg,
			})
		}
		if page >= resp.Pagination.LastPage || resp.Pagination.LastPage == 0 {
			break
		}
		page++
	}

	// Every listing is its own item. Re-releases, lottery rounds and shop
	// exclusives ship as distinct products with their own tamashiiWebId, so
	// collapsing them by name hid rows the owner may well have bought.
	for i := range rows {
		r := &rows[i]
		it := store.Item{
			ID:          r.id,
			Series:      series,
			Category:    CategorizeTamashii(r.title, ""),
			Name:        r.title,
			ReleaseDate: r.date,
			Price:       r.price,
			Status:      "none",
		}
		existing, _ := c.Store.GetItem(ctx, r.id)
		if existing != nil {
			it.PhotoURL = existing.PhotoURL
		}

		if it.PhotoURL == "" {
			// Reuse an already-downloaded file if present (any extension).
			if existing := findExistingPhoto(c.PhotosDir, r.id); existing != "" {
				it.PhotoURL = "/photos/" + existing
			} else if r.img != "" {
				ext := filepath.Ext(r.img)
				if ext == "" {
					ext = ".webp"
				}
				fname := r.id + ext
				photoPath := filepath.Join(c.PhotosDir, fname)
				imgURL := r.img
				if strings.HasPrefix(imgURL, "/") {
					imgURL = tamashiiBase + imgURL
				}
				if err := c.downloadImage(ctx, imgURL, photoPath); err != nil {
					rep.Failures = append(rep.Failures, fmt.Sprintf("%s photo: %v", r.id, err))
				} else {
					it.PhotoURL = "/photos/" + fname
					rep.NewPhotos++
				}
			}
		}

		if err := c.Store.UpsertCatalog(ctx, &it); err != nil {
			rep.Failures = append(rep.Failures, fmt.Sprintf("%s upsert: %v", r.id, err))
			continue
		}
		rep.Upserted++
		if d := diffItem(existing, &it); d != "" {
			rep.Changes = append(rep.Changes, d)
		}
		if existing == nil {
			rep.NewItems = append(rep.NewItems, r.title)
		}
	}
	return rep, nil
}

// findExistingPhoto returns the filename of an already-downloaded photo for id
// (matching id + any common image extension), or "" if none is on disk.
func findExistingPhoto(dir, id string) string {
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp", ".gif"} {
		if _, err := os.Stat(filepath.Join(dir, id+ext)); err == nil {
			return id + ext
		}
	}
	return ""
}

func (c *Client) fetchTamashiiPage(ctx context.Context, brandCode string, page int) (*tamashiiResp, error) {
	q := url.Values{}
	q.Set("sort", "2") // newest first
	q.Set("per_page", "100")
	q.Set("current_page", fmt.Sprintf("%d", page))
	q.Set("area", "japan")
	// brandCode is an array param: brandCode[]=metal_build
	endpoint := tamashiiBase + "/api/site-item/search_item.php?brandCode%5B%5D=" + url.QueryEscape(brandCode) + "&" + q.Encode()

	body, err := c.fetch(ctx, endpoint, brandCode)
	if err != nil {
		return nil, err
	}
	var r tamashiiResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &r, nil
}

// normalizeTamashiiDate keeps "YYYY-MM" / "YYYY-MM-DD" as-is; blanks anything else.
func normalizeTamashiiDate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 7 && s[4] == '-' {
		return s
	}
	return ""
}

// CategorizeTamashii classifies a Tamashii (finished-toy) item by name.
func CategorizeTamashii(title, mainBrand string) string {
	u := strings.ToUpper(title)
	switch {
	case strings.Contains(u, "OPTION") || strings.Contains(u, "オプション") || strings.Contains(u, "EXPANSION"):
		return "Option Parts"
	case strings.Contains(u, "EFFECT") || strings.Contains(u, "エフェクト"):
		return "Effect"
	default:
		return "Figure"
	}
}
