package scrape

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// A listing card carries one thumbnail. The detail page carries the whole
// shoot — usually three to six shots for a kit, more for a Tamashii figure.
// Fetching those is what separates a full refresh from an incremental one:
// it costs one request per item, so ~700 for the whole catalogue.

var (
	// The hobby site's gallery is a swiper whose main track sits between these
	// two markers; the thumbnail track below repeats the same pictures, and the
	// rest of the page carries banners we don't want.
	hobbyGalleryRE = regexp.MustCompile(`(?s)pg-products__sliderMain(.*?)pg-products__sliderThumbnail`)
	hobbyImgSrcRE  = regexp.MustCompile(`<img[^>]+src="([^"]+)"`)

	// Tamashii names every asset after the item, so filtering by id keeps
	// related-product shots out.
	twAssetRE = regexp.MustCompile(`/storage/images/products/[^"' >]+\.(?:jpg|jpeg|png|webp)`)
)

// galleryURLs returns the source URLs of an item's gallery, in page order.
func (c *Client) galleryURLs(ctx context.Context, itemID string) ([]string, error) {
	switch {
	case strings.HasPrefix(itemID, "tw-"):
		return c.tamashiiGallery(ctx, strings.TrimPrefix(itemID, "tw-"))
	case strings.HasPrefix(itemID, "pb-"):
		// Premium Bandai items live on p-bandai.jp, which has no equivalent
		// page we can read; the listing thumbnail is all there is.
		return nil, nil
	default:
		return c.hobbyGallery(ctx, itemID)
	}
}

func (c *Client) hobbyGallery(ctx context.Context, itemID string) ([]string, error) {
	body, err := c.fetch(ctx, "https://bandai-hobby.net/item/"+itemID+"/", "30ms")
	if err != nil {
		return nil, err
	}
	m := hobbyGalleryRE.FindSubmatch(body)
	if m == nil {
		return nil, nil
	}
	var out []string
	for _, im := range hobbyImgSrcRE.FindAllSubmatch(m[1], -1) {
		out = appendUnique(out, string(im[1]))
	}
	return out, nil
}

func (c *Client) tamashiiGallery(ctx context.Context, tid string) ([]string, error) {
	body, err := c.fetch(ctx, tamashiiBase+"/item/"+tid, "metal_build")
	if err != nil {
		return nil, err
	}
	// Assets are named item_<10-digit id>_<hash>_<nn>.jpg.
	want := fmt.Sprintf("item_%010s_", tid)
	var out []string
	for _, m := range twAssetRE.FindAll(body, -1) {
		u := string(m)
		if strings.Contains(u, want) {
			out = appendUnique(out, tamashiiBase+u)
		}
	}
	return out, nil
}

// appendUnique adds u unless an identical URL (ignoring any signature query)
// is already present — galleries repeat the same file at several sizes.
func appendUnique(list []string, u string) []string {
	if u == "" {
		return list
	}
	key := strings.SplitN(u, "?", 2)[0]
	for _, e := range list {
		if strings.SplitN(e, "?", 2)[0] == key {
			return list
		}
	}
	return append(list, u)
}

// FetchGallery downloads an item's gallery and records it. Returns how many
// new files were written.
//
// Photos land as "<id>_<n><ext>" alongside the cover, so the whole gallery is
// still just files in the photos dir — no extra storage to back up.
func (c *Client) FetchGallery(ctx context.Context, itemID string) (int, error) {
	srcs, err := c.galleryURLs(ctx, itemID)
	if err != nil {
		return 0, err
	}
	if len(srcs) == 0 {
		return 0, nil
	}
	if err := os.MkdirAll(c.PhotosDir, 0o755); err != nil {
		return 0, err
	}

	var local []string
	downloaded := 0
	for i, src := range srcs {
		ext := filepath.Ext(strings.SplitN(src, "?", 2)[0])
		if ext == "" {
			ext = ".jpg"
		}
		name := fmt.Sprintf("%s_%d%s", itemID, i+1, ext)
		path := filepath.Join(c.PhotosDir, name)
		if _, err := os.Stat(path); err != nil {
			if err := c.downloadImage(ctx, src, path); err != nil {
				// One bad shot shouldn't cost us the rest of the gallery.
				continue
			}
			downloaded++
		}
		local = append(local, "/photos/"+name)
	}
	if err := c.Store.SetItemPhotos(ctx, itemID, local); err != nil {
		return downloaded, err
	}
	return downloaded, nil
}
