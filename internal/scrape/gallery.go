package scrape

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	// Tamashii pages carry dozens of product images — recommendations, banners,
	// other items in the same brand — so the gallery has to be located
	// structurally. productImgWrapper is the block holding just this item's
	// shots, on both the old and the current page template.
	twGalleryRE = regexp.MustCompile(`(?s)productImgWrapper(.*?)productInfo"`)
	twAssetRE   = regexp.MustCompile(`/storage/images/products/[^"' >]+\.(?:jpg|jpeg|png|webp)`)
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
	// Older items name their assets item_<10-digit id>_<hash>_<nn>.jpg, which
	// made filtering by id enough. Newer ones (初音ミク and everything since)
	// use bare UUIDs, so that filter silently returned nothing and those items
	// ended up with no gallery at all. Scope by the container instead.
	m := twGalleryRE.FindSubmatch(body)
	if m == nil {
		return nil, nil
	}
	var out []string
	for _, a := range twAssetRE.FindAll(m[1], -1) {
		out = appendUnique(out, tamashiiBase+string(a))
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

// --- placeholder covers ---
//
// Bandai serves a brand logo ("30 MINUTES PREFERENCE" and friends) for products
// announced before their photos are shot. We download that like any other
// picture, and because a cover is only fetched when the item has none, it then
// sticks forever — 30MP アリス（メイド）kept a logo long after its real photos
// went up.
//
// Rather than hard-coding hashes (there is one per brand, and they change),
// spot them by their defining property: a placeholder is byte-identical across
// SEVERAL items, whereas a real product shot belongs to exactly one. Any cover
// whose content is shared is treated as "no photo yet" and re-fetched on the
// next run, until the real thing replaces it.

func fileHash(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// sharedCovers returns the hashes of cover images used by more than one item.
func (c *Client) sharedCovers() map[string]bool {
	entries, err := os.ReadDir(c.PhotosDir)
	if err != nil {
		return nil
	}
	count := map[string]int{}
	for _, e := range entries {
		name := e.Name()
		// Covers only. Gallery shots legitimately repeat their own cover, and
		// counting them would make every item look shared.
		if isGalleryShot(name) {
			continue
		}
		h, err := fileHash(filepath.Join(c.PhotosDir, name))
		if err != nil {
			continue
		}
		count[h]++
	}
	out := map[string]bool{}
	for h, n := range count {
		if n > 1 {
			out[h] = true
		}
	}
	return out
}

// isGalleryShot reports whether a filename is "<id>_<n>.<ext>" rather than a
// cover. Item ids themselves contain underscores ("01_5027"), so only a
// trailing "_<digits>" beyond the id counts.
func isGalleryShot(name string) bool {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	i := strings.LastIndex(base, "_")
	if i < 0 {
		return false
	}
	tail := base[i+1:]
	if tail == "" {
		return false
	}
	for _, r := range tail {
		if r < '0' || r > '9' {
			return false
		}
	}
	// "01_5027" is a cover: the part before the underscore is the "01" prefix.
	return base[:i] != "01"
}

// coverIsPlaceholder reports whether the stored cover for id is one of the
// shared images, i.e. not a real photo of this product.
func (c *Client) coverIsPlaceholder(shared map[string]bool, id string) bool {
	if len(shared) == 0 {
		return false
	}
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
		h, err := fileHash(filepath.Join(c.PhotosDir, id+ext))
		if err == nil {
			return shared[h]
		}
	}
	return false
}
