package scrape

import "testing"

// Titles taken verbatim from bandai-hobby.net listings. The full-width ones are
// not hypothetical: 27% of a 161-title sample was written that way, and the
// pairs below ("MG 1/100 グフ" / "ＭＧ 1/100 ガンダムアストレア") sat on the same page.
func TestGunplaGrade(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"HG 1/144 ガンダムレオパルド", "HG"},
		{"HG 1/144 Ξガンダム[クリアカラー]劇場公開記念パッケージ", "HG"},
		{"MG 1/100 グフ Ver.2.0 [クリアカラー]", "MG"},
		{"ＭＧ 1/100 ガンダムアストレア＋プロトＧＮツインブロードブレイド", "MG"},
		{"ＭＧ 1/100 量産型リコ専用ザク", "MG"},
		{"RG 1/144 ユニコーンガンダム２号機 バンシィ・ノルン (最終決戦仕様)", "RG"},
		{"ＲＧ 1/144 ブラストインパルスガンダムＳｐｅｃＩＩ", "RG"},
		{"PG 1/60 ガンダムエクシア (LIGHTING MODEL)", "PG"},
		{"ＰＧ 1/60 RX-0 ユニコーンガンダム（最終決戦Ｖｅｒ．）", "PG"},

		// MGSD and MGEX must not be swallowed by the MG prefix.
		{"MGSD フリーダムガンダム", "MGSD"},
		{"ＭＧＳＤ ストライクフリーダムガンダム", "MGSD"},
		// MG Ver.Ka is its own listing, so it is its own category. The second
		// one writes Ver.Ka with a FULL-WIDTH FULL STOP, which folding only
		// letters and digits would leave as "Ver．Ka" and miss.
		{"MG 1/100 フルアーマーZZガンダム Ver.Ka", "MG Ver.Ka"},
		{"ＭＧ 1/100 ナラティブガンダム Ｂ装備 Ｖｅｒ．Ｋａ", "MG Ver.Ka"},
		{"MG 1/100 シナンジュ・スタイン (ナラティブ Ver.) Ver.Ka", "MG Ver.Ka"},
		{"MG 1/100 サザビー Ver.Ka [メカニカルクリア]", "MG Ver.Ka"},
		// A plain MG stays MG.
		{"MG 1/100 グフ Ver.2.0 [クリアカラー]", "MG"},

		// MGEX wins over the Ver.Ka marker: these are MGEX kits that also say
		// Ver.Ka in the title.
		{"MGEX 1/100 ユニコーンガンダム Ver.Ka", "MGEX"},
		{"MGEX 1/100 ユニコーンガンダム Ver．Ka ［プレミアム “ユニコーンモード” ボックス］", "MGEX"},
		{"MGEX 1/100 マイティーストライクフリーダムガンダム", "MGEX"},

		// Decal sheets name themselves before the grade they are for, so they
		// never match a prefix. Seen live in the mgka listing.
		{"ガンダムデカールNo.33 MG 1/100 クロスボーンガンダムX1（Ver.Ka）用", "デカール"},
		// A kit whose title merely mentions decals still reads as its grade.
		{"HG 1/144 ガンダムデカール", "HG"},

		// No grade and no decal: 1 of 161 sampled. Must not guess.
		{"『機動戦士ガンダム』特別企画", "その他"},
	} {
		if got := gunplaGrade(tc.name); got != tc.want {
			t.Errorf("gunplaGrade(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Categorize must reach the Gunpla branch before the decal/hand-part branches,
// which are not gated on series.
func TestCategorizeGunplaBeatsPartBranches(t *testing.T) {
	if got := Categorize("HG 1/144 ガンダムデカール", "GUNPLA"); got != "HG" {
		t.Errorf("Gunpla decal set categorised as %q, want HG", got)
	}
	// And the 30-series behaviour is untouched.
	if got := Categorize("30MS オプションボディパーツ", "30MS"); got != "Option Body" {
		t.Errorf("30MS categorisation regressed: %q", got)
	}
}

func TestFoldWidth(t *testing.T) {
	if got := foldWidth("ＭＧ　1/100　ＲＸ－７８"); got != "MG 1/100 RX-78" {
		t.Errorf("foldWidth = %q", got)
	}
}
