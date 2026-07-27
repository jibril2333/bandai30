package scrape

import "testing"

func TestParsePriceDigits(t *testing.T) {
	cases := map[string]string{
		"880円(税10%込)":   "880",
		"2,200円(税10%込)": "2200",
		"46,200円(税10%込)": "46200",
		"2,000Yen":       "2000",
		"¥17,600（税込）":   "17600",
		"オープン価格":         "",
		"":               "",
	}
	for in, want := range cases {
		if got := parsePriceDigits(in); got != want {
			t.Errorf("parsePriceDigits(%q) = %q, want %q", in, got, want)
		}
	}
}
