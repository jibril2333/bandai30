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

		// Decal sheets name themselves before the grade they are for. A kit
		// whose title merely mentions decals states its grade first.
		{"ガンダムデカールNo.33 MG 1/100 クロスボーンガンダムX1（Ver.Ka）用", "デカール"},
		{"ガンダムデカールNo.118 MG 1/100 ジム・スナイパーII/ジム・コマンド用", "デカール"},
		{"HG 1/144 ガンダムデカール", "HG"},

		// HG's series sub-brands are HG kits, not grades of their own. Filing
		// them separately split HG across a dozen chips, eight under ten items.
		{"HGUC 1/144 νガンダム", "HG"},
		{"HGBF 1/144 ビルドストライクガンダム フルパッケージ", "HG"},
		{"HGBD:R 1/144 コアガンダムII(ティターンズカラー)", "HG"},
		{"HGPG 1/144 プチッガイ", "HG"},
		{"HGCE 1/144 フリーダムガンダム", "HG"},
		{"SDBF S×D×Gガンダム", "SD"},
		{"ENTRY GRADE 1/144 RX-78-2 ガンダム", "EG"},

		// The grade is not reliably the first token: store-limited and lottery
		// markers push it along, and some titles lead with the scale.
		{"【抽選販売】ＨＧ 1/144 ユニコーンガンダム（デストロイモード） Ver.NIKE SB", "HG"},
		{"GUNDAM NEXT FUTURE限定 HG 1/144 ガンダムエアリアル", "HG"},
		{"1/144 HG デスティニーガンダム", "HG"},

		// U+00A0 NO-BREAK SPACE as the separator. One live title uses it, and a
		// prefix test for "RG " does not match it.
		{"RG\u00a01/144\u00a0νガンダム[クリアカラー] 初回生産限定パッケージ", "RG"},

		// Genuinely gradeless: 6 of 2066 in the live catalogue.
		{"GUNPLA 40th メモリアルセット", "その他"},
		{"1/144 RX-79改 Ez8(08小隊版)", "その他"},
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

// Titles taken verbatim from the five キャラクタープラモデル listings.
func TestCharaLine(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"Figure-rise Standard Amplified マグナガルルモン", "Amplified"},
		{"Figure-rise Standard Amplified -三幻神降臨- オシリスの天空竜", "Amplified"},
		{"Figure-rise Standard ウルトラマン(ＢタイプVer.)", "Figure-rise"},
		// Written without the space; comparing with spaces and hyphens removed
		// is what covers it.
		{"Figure-riseStandard 仮面ライダークウガ マイティフォーム", "Figure-rise"},

		{"ＲＧ 汎用ヒト型決戦兵器 人造人間エヴァンゲリオン初号機", "RG"},
		{"RG エヴァンゲリオン Mark.06", "RG"},
		{"ENTRY GRADE 1/144 RX-78-2 ガンダム", "ENTRY GRADE"},
		// HG's sub-brands are HG here too.
		{"HGAB 1/144 ガンダムAGE-1", "HG"},
		{"HG 1/144 ウルトラマン", "HG"},

		// Full Metal Panic's 1/60 kits carry no line marker at all: 3 of 394.
		{"1/60 アーバレスト Ver.IV", "その他"},
	} {
		if got := charaLine(tc.name); got != tc.want {
			t.Errorf("charaLine(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
	// The Gunpla and 30-series rules are untouched by the new branch.
	if got := Categorize("HGUC 1/144 νガンダム", "GUNPLA"); got != "HG" {
		t.Errorf("GUNPLA regressed: %q", got)
	}
	if got := Categorize("Figure-rise Standard Amplified アグモン", "キャラプラ"); got != "Amplified" {
		t.Errorf("Categorize did not reach charaLine: %q", got)
	}
}
