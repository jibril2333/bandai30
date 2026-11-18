package scrape

import (
	"context"
	"fmt"
	"log"
)

// Franchises are a second axis over the catalogue. A collection says which
// product line a kit belongs to — items.series holds one value — but "RG 初号機"
// is a Gunpla-grade kit AND an Evangelion one, and browsing by show is the
// natural way to find the models you own.
//
// The mapping is Bandai's, read from /series/<slug>/, not guessed from titles.
// Titles name a character as often as a work, so parsing them would be wrong
// in both directions.
//
// Taken from the rendered navigation of /characterplastic/, which is the only
// place the site lists them; there is no brand or series index page.
var Franchises = []struct{ Slug, Name string }{
	{"evangelion", "エヴァンゲリオン"},
	{"digimon", "デジモンシリーズ"},
	{"dragonball", "ドラゴンボールシリーズ"},
	{"rider", "仮面ライダーシリーズ"},
	{"ultraman", "ウルトラマン"},
	{"pokemon", "ポケットモンスター"},
	{"onepiece", "ワンピース"},
	{"naruto", "NARUTO -ナルト-"},
	{"kimetsu", "鬼滅の刃"},
	{"yugioh", "遊☆戯☆王デュエルモンスターズ"},
	{"macross", "マクロスシリーズ"},
	{"patlabor", "機動警察パトレイバーシリーズ"},
	{"votoms", "装甲騎兵ボトムズ"},
	{"yamato", "宇宙戦艦ヤマトシリーズ"},
	{"armoredcore", "ARMORED CORE VI"},
	{"dxm", "デモンエクスマキナ"},
	{"geass", "コードギアスシリーズ"},
	{"idolmaster", "アイドルマスターシリーズ"},
	{"madoka-magica", "魔法少女まどか☆マギカ"},
	{"fate", "Fate/Grand Order"},
	{"umamusume", "ウマ娘 プリティーダービー"},
	{"bocchi-rocks", "ぼっち・ざ・ろっく！"},
	{"bluearchive", "ブルーアーカイブ"},
	{"mahoyo", "魔法使いの夜"},
	{"kaiju-no8", "怪獣8号"},
	{"godzilla", "ゴジラ"},
	{"starwars", "STAR WARS"},
	{"theghostintheshell", "攻殻機動隊"},
	{"monsterhunterwilds", "モンスターハンターワイルズ"},
	{"sandland", "SAND LAND"},
	{"synduality", "SYNDUALITY"},
	{"kyoukaisenki", "境界戦機"},
	{"gaogaigar", "勇者王ガオガイガー"},
	{"dunbine", "聖戦士ダンバイン"},
	{"lgaim", "重戦機エルガイム"},
	{"dragonar", "機甲戦記ドラグナー"},
	{"vifam", "銀河漂流バイファム"},
	{"samurai-trooper", "鎧伝サムライトルーパー"},
	{"wataru", "ワタルシリーズ"},
	{"lbx", "ダンボール戦機"},
	{"keroro", "ケロロ軍曹"},
	{"superrobot_og", "スーパーロボット大戦OG"},
	{"plannosaurus", "プラノサウルス"},
	{"vertexforce", "バーテックスフォース"},
	{"unkosuldays", "ウンコスルデイズ"},

	// The Gundam shows, listed on /gunpla/ rather than /characterplastic/.
	// Without them the largest collection would carry no franchise at all.
	{"gundam", "機動戦士ガンダム"},
	{"z-gundam", "機動戦士Zガンダム"},
	{"gundam-zz", "機動戦士ガンダムZZ"},
	{"gundam-cca", "逆襲のシャア"},
	{"g-0080", "0080 ポケットの中の戦争"},
	{"g-0083", "0083 STARDUST MEMORY"},
	{"sentinel", "ガンダム・センチネル"},
	{"gundamf91", "機動戦士ガンダムF91"},
	{"v-gundam", "機動戦士Vガンダム"},
	{"ggundam", "機動武闘伝Gガンダム"},
	{"endlesswaltz", "新機動戦記ガンダムW"},
	{"x", "機動新世紀ガンダムX"},
	{"turn-a", "∀ガンダム"},
	{"seed", "機動戦士ガンダムSEED"},
	{"seed-d", "SEED DESTINY"},
	{"seed-freedom", "SEED FREEDOM"},
	{"seed-freedom-zero", "SEED FREEDOM ZERO"},
	{"g-00", "機動戦士ガンダム00"},
	{"unicorn", "機動戦士ガンダムUC"},
	{"age", "機動戦士ガンダムAGE"},
	{"origin", "THE ORIGIN"},
	{"g-reco", "Gのレコンギスタ"},
	{"thunderbolt", "サンダーボルト"},
	{"tekketsu", "鉄血のオルフェンズ"},
	{"narrative", "機動戦士ガンダムNT"},
	{"hathaway", "閃光のハサウェイ"},
	{"g-witch", "水星の魔女"},
	{"g-doan", "ククルス・ドアンの島"},
	{"requiem", "復讐のレクイエム"},
	{"gquuuuuux", "Gundam GQuuuuuuX"},
	{"buildfighters", "ガンダムビルドファイターズ"},
	{"builddivers", "ガンダムビルドダイバーズ"},
	{"builddivers-rerise", "ビルドダイバーズ Re:RISE"},
	{"gundambuildmetaverse", "ガンダムビルドメタバース"},
	{"gundam_breaker_battlogue", "GUNDAM BREAKER BATTLOGUE"},
	{"sdgundamworld_heroes", "SDガンダムワールド ヒーローズ"},
	{"soketsuden", "三国創傑伝"},
	{"gunplakun", "ガンプラくん"},
	{"rogueorbit", "GUNDAM ROGUE ORBIT"},
	{"xarx-zero", "ガンダムRG XARX-ZERO"},
}

// TagFranchises walks every series listing and records the work each item
// belongs to. Runs after the product-line pass, because it only tags rows that
// already exist: a series page also lists kits from lines this app does not
// collect, and inserting those would add items with a franchise and no
// collection.
func (c *Client) TagFranchises(ctx context.Context) (tagged int, err error) {
	for _, fr := range Franchises {
		ids, err := c.seriesItemIDs(ctx, fr.Slug)
		if err != nil {
			// One unreachable show should not cost the other forty-four.
			log.Printf("franchise %s: %v", fr.Slug, err)
			continue
		}
		n, err := c.Store.SetFranchise(ctx, ids, fr.Name)
		if err != nil {
			return tagged, fmt.Errorf("tag %s: %w", fr.Slug, err)
		}
		tagged += n
	}
	return tagged, nil
}

// seriesItemIDs reads every page of one /series/ listing. Same card markup as
// the brand listings, so the same regexp finds the ids.
func (c *Client) seriesItemIDs(ctx context.Context, slug string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for page := 1; page <= 40; page++ {
		body, err := c.fetch(ctx, c.seriesURL(slug, page), slug)
		if err != nil {
			if page == 1 {
				return nil, err
			}
			break // a later page failing just ends the walk
		}
		found := 0
		for _, m := range cardBlockRE.FindAllStringSubmatch(string(body), -1) {
			id := m[1]
			if id == "" && m[2] != "" {
				id = "pb-" + m[2]
			}
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
			found++
		}
		if found == 0 {
			break
		}
	}
	return out, nil
}
