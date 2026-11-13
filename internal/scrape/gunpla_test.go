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
		{"MGEX 1/100 ユニコーンガンダム Ver.Ka", "MGEX"},

		// No grade in the title: 1 of 161 sampled. Must not guess.
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
	if got := foldWidth("ＭＧ　1/100　ＲＸ－７８"); got != "MG 1/100 RX－78" {
		t.Errorf("foldWidth = %q", got)
	}
}
