package scrape

import "testing"

// Misclassifying a cover as a gallery shot would exclude it from the shared-
// hash count and let placeholders survive; misclassifying the other way would
// count an item's own gallery against it and make every item look like a
// placeholder, re-downloading the whole catalogue every run.
func TestIsGalleryShot(t *testing.T) {
	covers := []string{
		"01_5027.jpg",           // hobby item id contains an underscore itself
		"01_2238.jpg",
		"tw-14624.jpg",          // tamashii
		"pb-1000185584.jpg",     // premium bandai
		"tw-15862.webp",
	}
	shots := []string{
		"01_5027_1.jpg",
		"01_5027_12.jpg",
		"tw-14624_3.jpg",
		"pb-1000185584_2.jpg",
		"tw-15862_4.webp",
	}
	for _, n := range covers {
		if isGalleryShot(n) {
			t.Errorf("%s: classified as a gallery shot, want cover", n)
		}
	}
	for _, n := range shots {
		if !isGalleryShot(n) {
			t.Errorf("%s: classified as a cover, want gallery shot", n)
		}
	}
}
