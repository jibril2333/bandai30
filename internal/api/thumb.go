package api

import (
	"errors"
	"image"
	"image/jpeg"
	_ "image/png" // uploads are often PNG; decode them too
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Grid covers are the catalogue's own photographs, which run to 1500x1500 and
// 450 KB. A phone draws them into cells about 170 px wide, and a decoded
// 1500x1500 frame costs 9 MB of memory whatever it is displayed at — so
// scrolling a 2,000-card collection decodes gigabytes, pins the main thread
// and leaves the page scrolling (that runs on the compositor) while taps go
// unanswered. Reported from a phone on an external connection, where the
// slower CPU makes it unmissable.
//
// The originals stay untouched: the detail view wants them. The grid gets a
// derived copy, generated on first request and cached next to the photos, so
// there is no migration pass over 18,000 existing files.
const (
	thumbMax     = 420 // longest side, enough for a 2x phone grid cell
	thumbQuality = 82
	thumbDir     = ".thumb"
)

// One generation at a time per file: a cold grid asks for a hundred thumbs at
// once, and without this each would decode the same original in parallel.
var thumbLocks sync.Map

func (s *Server) serveThumb(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		http.NotFound(w, r)
		return
	}
	src := filepath.Join(s.PhotosDir, name)
	si, err := os.Stat(src)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	dst := filepath.Join(s.PhotosDir, thumbDir, name+".jpg")

	if di, err := os.Stat(dst); err != nil || di.ModTime().Before(si.ModTime()) {
		if err := s.makeThumb(src, dst); err != nil {
			// A cover we cannot shrink is still a cover. Serving the original
			// is slower than a thumbnail and better than a broken image.
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, src)
			return
		}
	}
	// Same reasoning as servePhoto: a placeholder cover is replaced in place,
	// so the URL outlives its contents and must be revalidated.
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, dst)
}

func (s *Server) makeThumb(src, dst string) error {
	mu, _ := thumbLocks.LoadOrStore(src, &sync.Mutex{})
	lock := mu.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	defer thumbLocks.Delete(src)

	// Another request may have finished it while this one waited.
	if si, err := os.Stat(src); err == nil {
		if di, err := os.Stat(dst); err == nil && !di.ModTime().Before(si.ModTime()) {
			return nil
		}
	}

	f, err := os.Open(src)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return err
	}
	small := downscale(img, thumbMax)
	if small == nil {
		return errors.New("nothing to downscale")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	// Write to a temp name and rename: a reader must never see a half-written
	// JPEG, and two generations racing must not interleave their bytes.
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return err
	}
	if err := jpeg.Encode(tmp, small, &jpeg.Options{Quality: thumbQuality}); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	return os.Rename(tmp.Name(), dst)
}

// downscale box-filters img so its longest side is at most max. A box filter
// is written out here rather than pulling in golang.org/x/image: averaging
// every source pixel that falls in a destination cell is the right filter for
// shrinking, and it is the only resampling this app does.
//
// Returns nil when the image is already small enough, so the caller can keep
// serving the original rather than re-encoding it for nothing.
func downscale(img image.Image, max int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= max && h <= max {
		return nil
	}
	nw, nh := w, h
	if w >= h {
		nw, nh = max, h*max/w
	} else {
		nw, nh = w*max/h, max
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	out := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		y0, y1 := b.Min.Y+y*h/nh, b.Min.Y+(y+1)*h/nh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < nw; x++ {
			x0, x1 := b.Min.X+x*w/nw, b.Min.X+(x+1)*w/nw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var sr, sg, sb, sa, n uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					r, g, bl, a := img.At(xx, yy).RGBA()
					sr, sg, sb, sa, n = sr+uint64(r), sg+uint64(g), sb+uint64(bl), sa+uint64(a), n+1
				}
			}
			i := out.PixOffset(x, y)
			out.Pix[i+0] = uint8(sr / n >> 8)
			out.Pix[i+1] = uint8(sg / n >> 8)
			out.Pix[i+2] = uint8(sb / n >> 8)
			out.Pix[i+3] = uint8(sa / n >> 8)
		}
	}
	return out
}
