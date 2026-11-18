package recall

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTokenizeCJKAndASCII(t *testing.T) {
	tokens := tokenize("青玉符，青玉符 Alpha-9 x 7")
	for _, want := range []string{"青玉", "玉符", "青玉符", "alpha"} {
		if _, ok := tokens[want]; !ok {
			t.Errorf("token %q missing from %v", want, tokens)
		}
	}
	for _, unwanted := range []string{"9", "x", "7", "青玉符，"} {
		if _, ok := tokens[unwanted]; ok {
			t.Errorf("unexpected token %q", unwanted)
		}
	}
}

func TestSearchFindsPlantedChapter(t *testing.T) {
	docs := make([]Document, 0, 25)
	for i := 1; i <= 25; i++ {
		content := strings.Repeat("雨夜的长街空无一人。", 120)
		if i == 3 {
			content += "\n林砚从旧宅梁下摸出半枚青玉符，符上刻着旧朝年号。"
		}
		docs = append(docs, Document{
			ChapterID: fmt.Sprintf("ch_%d", i),
			Index:     i,
			Title:     fmt.Sprintf("第%d章 雨夜", i),
			Content:   content,
		})
	}

	hits := Search(docs, "继续追查青玉符的来历", Options{TopK: 3})
	if len(hits) == 0 {
		t.Fatal("Search returned no hits")
	}
	top := hits[0]
	if top.ChapterIndex != 3 {
		t.Fatalf("top hit chapter = %d, want 3 (hits=%+v)", top.ChapterIndex, hits)
	}
	if !strings.Contains(top.Snippet, "青玉符") {
		t.Errorf("snippet missing planted fact: %q", top.Snippet)
	}
	if top.StartRune < 0 || top.EndRune <= top.StartRune {
		t.Errorf("invalid range [%d,%d)", top.StartRune, top.EndRune)
	}
	if top.Score <= 0 {
		t.Errorf("score = %f, want > 0", top.Score)
	}
	if got := utf8.RuneCountInString(top.Snippet); got > defaultSnippetRunes {
		t.Errorf("snippet runes = %d, want <= %d", got, defaultSnippetRunes)
	}
}

// TestSearchIntentWeightingOutranksRecentNoise 覆盖方向加权：查询里的最近正文词
// 数量多而信号弱，埋在旧章的关键名词必须靠 Intent 权重胜出。
// 有方向时闸门只放行「方向里的罕见词真的出现在块内」的块，因此没有可追溯线索的
// 方向不会注入，最近正文的表层重叠也不会被召回——权重对比只能在同一闸门内做。
func TestSearchIntentWeightingOutranksRecentNoise(t *testing.T) {
	recent := "长街雨夜"
	docs := []Document{
		{ChapterID: "ch_1", Index: 1, Content: recent + "，众人沉默地看着。"},
		{ChapterID: "ch_2", Index: 2, Content: "他摸出半枚青玉符。\n符上刻着旧朝年号。"},
		{ChapterID: "ch_3", Index: 3, Content: "码头的风把缆绳吹得笔直。"},
	}

	weighted := Search(docs, recent, Options{Intent: "继续追查青玉符的来历"})
	if len(weighted) == 0 {
		t.Fatal("intent-weighted search returned no hits")
	}
	if weighted[0].ChapterIndex != 2 {
		t.Fatalf("intent-weighted top hit = chapter %d, want 2 (hits=%+v)", weighted[0].ChapterIndex, weighted)
	}
	if !strings.Contains(weighted[0].Snippet, "青玉符") {
		t.Errorf("intent-weighted snippet lost the planted fact: %q", weighted[0].Snippet)
	}

	// 没有方向时只有最近正文词参与打分：表层重叠的噪声章胜出，关键章根本进不来。
	flat := Search(docs, recent, Options{})
	if len(flat) == 0 {
		t.Fatal("unweighted search returned no hits")
	}
	if flat[0].ChapterIndex != 1 {
		t.Fatalf("without an intent top hit = chapter %d, want the recent-noise chapter 1 (hits=%+v)", flat[0].ChapterIndex, flat)
	}
	for _, hit := range flat {
		if hit.ChapterIndex == 2 {
			t.Fatalf("without an intent the planted chapter should not be recalled, got %+v", flat)
		}
	}

	// 同一闸门下，IntentWeight=1 与默认权重的差值只能来自方向加权本身。
	unweightedIntent := Search(docs, recent, Options{Intent: "继续追查青玉符的来历", IntentWeight: 1})
	var flatScore, weightedScore float64
	for _, hit := range unweightedIntent {
		if hit.ChapterIndex == 2 {
			flatScore = hit.Score
		}
	}
	for _, hit := range weighted {
		if hit.ChapterIndex == 2 {
			weightedScore = hit.Score
		}
	}
	if flatScore == 0 {
		t.Fatalf("intent-weight-1 search did not recall the planted chapter: %+v", unweightedIntent)
	}
	if weightedScore <= flatScore {
		t.Errorf("intent weighting did not raise the planted chapter score: unweighted=%f weighted=%f", flatScore, weightedScore)
	}
}

// TestSearchDropsHitsWithoutRareClue 覆盖罕见词闸：查询词几乎每章都有时，
// 注入只是把最近正文的表层重叠搬到 prompt 里，应当整段不注入。
func TestSearchDropsHitsWithoutRareClue(t *testing.T) {
	filler := strings.Repeat("雨夜的长街空无一人。", 40)
	docs := make([]Document, 0, 11)
	for i := 1; i <= 10; i++ {
		docs = append(docs, Document{
			ChapterID: fmt.Sprintf("ch_%d", i), Index: i, Content: filler,
		})
	}
	query := "雨夜的长街空无一人"
	if hits := Search(docs, query, Options{}); len(hits) != 0 {
		t.Fatalf("common-only query produced %d hits, want 0: %+v", len(hits), hits)
	}
	docs = append(docs, Document{
		ChapterID: "ch_11", Index: 11, Content: filler + "\n半枚青玉符在袖中发烫。",
	})
	hits := Search(docs, query, Options{Intent: "青玉符在袖中发烫"})
	if len(hits) == 0 {
		t.Fatal("search with a rare intent clue produced no hits")
	}
	if hits[0].ChapterIndex != 11 {
		t.Fatalf("top hit = chapter %d, want 11 (the only chapter with a rare clue)", hits[0].ChapterIndex)
	}
	if gated := Search(docs, query, Options{}); len(gated) != 0 {
		t.Fatalf("without an intent clue the gate should drop all hits, got %+v", gated)
	}
	// 有方向时闸门收紧到「方向里的罕见词」：方向本身不含可追溯线索就不该注入。
	vague := Search(docs, query, Options{Intent: "让情节自然推进下去"})
	if len(vague) != 0 {
		t.Fatalf("intent without a traceable rare clue produced %d hits, want 0: %+v", len(vague), vague)
	}
}

// TestSearchRequiresCluePhraseLength 覆盖长度—唯一性阶梯：方向与旧章只共享「现有」这类
// 两字虚词残片时，即使它在全书罕见也不构成长程线索，必须整段不注入；但只出现一次的两字
// 专名仍是有效线索，不能一并误杀。
func TestSearchRequiresCluePhraseLength(t *testing.T) {
	filler := strings.Repeat("雨夜的长街空无一人。", 40)
	docs := make([]Document, 0, 22)
	for i := 1; i <= 19; i++ {
		docs = append(docs, Document{ChapterID: fmt.Sprintf("ch_%d", i), Index: i, Content: filler})
	}
	fragmented := filler + "\n他环顾现有布置，没有多说什么。"
	docs = append(docs,
		Document{ChapterID: "ch_20", Index: 20, Content: fragmented},
		Document{ChapterID: "ch_21", Index: 21, Content: fragmented})
	query := "雨夜的长街空无一人"
	intent := "继续写下去，保持现有的叙事节奏。"

	if hits := Search(docs, query, Options{Intent: intent}); len(hits) != 0 {
		t.Fatalf("a two-rune fragment shared by several blocks must not open the gate, got %+v", hits)
	}
	loosened := Search(docs, query, Options{Intent: intent, MinClueRunes: 2})
	if len(loosened) == 0 {
		t.Fatalf("with the length gate loosened to 2 the fragment chapters should be recalled, got %+v", loosened)
	}

	docs = append(docs, Document{ChapterID: "ch_22", Index: 22, Content: filler + "\n阿槿把门闩落下。"})
	unique := Search(docs, query, Options{Intent: "写写阿槿的过去"})
	if len(unique) == 0 || unique[0].ChapterIndex != 22 {
		t.Fatalf("a two-rune name unique in the book is still a clue, got %+v", unique)
	}
}

// TestSearchPlantedClueRateOnSyntheticCorpus 是长篇埋点召回率的回归闸：合成一本 60 章、
// 每章约六百字、共约三万五千字的书（每章有跨章复现的通用句、中频母题句，末五章是只属于自己的独特正文，
// 即生产里的「最近正文尾窗」），把 20 条全书唯一的三字线索埋进前段分散章节，再按生产语义
// 逐条探针——查询=末五章正文，方向=引用该线索的续写意图。断言三件事：
// 目标章召回率 ≥ 0.90、埋点原句仍在片段里、每个方向只有目标章一道闸门通过（len(hits)==1，
// 罕见线索闸一旦被放宽，末章噪声就会挤进 TopK，这条先红）；另加同输入排序逐次一致，
// 以及「只与两章共享两字虚词残片」的模糊方向与「没有任何共享线索」的方向都必须零注入。
// 阈值来自 2026-10-24 真实语料复测（修复前 5/26，修复后 25/26，见
// docs/acceptance-evidence/20261024/r4-recall-quality/）。反向验证过：把 MinClueRunes 降到 1
// 时命中率仍是 1.0（目标章排序没塌），但每条探针的 hits 变成 2——所以闸门看的是 hits 数量，
// 不只看命中率。tf 饱和、贡献封顶、片段长度等单项语义由本文件其余用例分别守。
func TestSearchPlantedClueRateOnSyntheticCorpus(t *testing.T) {
	positional := []rune("甲乙丙丁戊己庚辛壬癸")
	digits := []rune("一二三四五六七八九十")
	nouns := []rune("砖钥鼎幡匣铃砚瓢")
	tags := []rune("春夏秋冬风雨霜雪江河湖海山石草木花鸟虫鱼")
	common := []string{
		"雨夜的长街空无一人。",
		"码头的风把缆绳吹得笔直。",
		"他吹熄油灯，听远处更鼓响过两遍。",
		"船家收好鱼篓，蹲在船头默不作声。",
		"院里的井绳换了新的，青石板上还剩水痕。",
		"她把账册合上，指尖停在封口的绳结处。",
		"天色未明，马厩里传来蹄铁磕地的声响。",
		"巷口的老人收起摊子，慢慢往河堤方向走去。",
	}
	motifs := []string{
		"井台的青苔被夜雨冲得发亮。",
		"他蹲在门槛上擦拭那把短刀。",
		"檐角的铁马被风吹得叮当作响。",
		"她把药渣倒进后院的泥沟里。",
		"河面的薄冰在正午裂开道缝。",
		"老周把骡子拴在磨坊的木桩上。",
		"柜上的伙计拨着算盘打到深夜。",
		"雪化了半日，台阶还是湿的。",
		"他掀开锅盖，蒸汽糊住了眉眼。",
		"祠堂的门环锈成暗红色。",
		"井绳磨断的那日他没吭声。",
		"雁群掠过屋顶，瓦片响了阵。",
		"灶膛的火光照着她手背的疤。",
		"他把信件缝进棉袄的夹层。",
		"桥洞底下睡着个哑巴车夫。",
		"她数完那批货，吹灭了烛。",
		"马蹄铁松动，走路时发出钝响。",
		"柴房换了新锁，旧的那把扔进河湾。",
		"雾大，渡船在半途停下等号子。",
		"她把银镯悄悄塞进孩子袖口。",
		"屋檐下挂着的辣椒干了大半。",
		"老兵坐在城根下磨他的刀。",
		"河水退去，河床留着腥气的泥。",
		"他记着那句没说完的嘱咐。",
	}
	const chapterCount = 60
	const tailFirst = chapterCount - 4

	clues := make([]string, 0, 20)
	clueChapter := make(map[string]int, 20)
	chapterClue := make(map[int]string, 20)
	for i := 0; i < 20; i++ {
		clue := string([]rune{positional[i%len(positional)], digits[(i*3)%len(digits)], nouns[i%len(nouns)]})
		if _, ok := clueChapter[clue]; ok {
			t.Fatalf("clue %q generated twice, the fixture cannot measure uniqueness", clue)
		}
		chapter := (i*2)%(tailFirst-1) + 1
		if _, ok := chapterClue[chapter]; ok {
			t.Fatalf("clues planted twice in chapter %d", chapter)
		}
		clues = append(clues, clue)
		clueChapter[clue] = chapter
		chapterClue[chapter] = clue
	}
	if len(chapterClue) != 20 {
		t.Fatalf("planted chapters = %d, want 20 distinct", len(chapterClue))
	}

	contents := make([]string, chapterCount+1)
	for i := 1; i <= chapterCount; i++ {
		var body strings.Builder
		for round := 0; round < 4; round++ {
			for _, s := range common {
				body.WriteString(s)
			}
		}
		for k := 0; k < 4; k++ {
			body.WriteString(motifs[(i*7+k*5)%len(motifs)])
		}
		switch i {
		case 16:
			// 与方向共享「现有」这类两字残片，但后续字不同，三字组互不重合。
			body.WriteString("现有绳索都换过了。")
		case 17:
			body.WriteString("现有灯油只够两夜。")
		}
		if i >= tailFirst {
			// 末段正文：每章六条只在本章出现的独特句子，模拟最近窗口的表层词密度。
			for s := 0; s < 6; s++ {
				pair := string([]rune{tags[(i*6+s)%len(tags)], tags[(i*6+s*3+5)%len(tags)]})
				body.WriteString(pair + "那年，后来再没人提起过这盏灯。")
			}
		}
		if clue, ok := chapterClue[i]; ok {
			body.WriteString("他半夜把" + clue + "埋进旧墙的缝隙里。")
		}
		contents[i] = body.String()
	}

	docs := make([]Document, 0, chapterCount)
	for i := 1; i <= chapterCount; i++ {
		docs = append(docs, Document{
			ChapterID: fmt.Sprintf("ch_%d", i), Index: i,
			Title: fmt.Sprintf("第%d章", i), Content: contents[i],
		})
	}
	var tail strings.Builder
	for i := tailFirst; i <= chapterCount; i++ {
		tail.WriteString(contents[i])
	}
	query := tail.String()

	// 自检：模糊方向与正文只允许共享「现有」这一条两字残片（章节频 2，罕见但按长度阶梯不可追溯）。
	// 语料以后被改动时这条检查先红，免得下面的断言悄悄失去前提。
	vagueIntent := "保持现有叙事节奏，凑不够现有的线索先放一放。"
	rareLimit := int(math.Ceil(float64(chapterCount) * defaultRareRatio))
	shared := map[string]int{}
	for token := range tokenize(vagueIntent) {
		df := 0
		for i := 1; i <= chapterCount; i++ {
			if strings.Contains(contents[i], token) {
				df++
			}
		}
		if df > 0 && df <= rareLimit {
			shared[token] = df
		}
	}
	if len(shared) != 1 || shared["现有"] != 2 {
		t.Fatalf("fixture premise broken: the vague direction shares rare tokens %v with the corpus, want only 现有 in 2 chapters", shared)
	}

	var runs [][]string
	for run := 0; run < 2; run++ {
		rankings := make([]string, 0, len(clues))
		hitChapters := 0
		for _, clue := range clues {
			hits := Search(docs, query, Options{Intent: "回到" + clue + "那条旧线索，接着写下去。"})
			target := clueChapter[clue]
			if len(hits) != 1 {
				t.Errorf("clue %q got %d gated hits, want exactly the planted chapter (hits=%+v)", clue, len(hits), hits)
			}
			if len(hits) == 0 {
				rankings = append(rankings, clue+":none")
				continue
			}
			rankings = append(rankings, fmt.Sprintf("%s:%d", clue, hits[0].ChapterIndex))
			if hits[0].ChapterIndex != target {
				t.Errorf("clue %q top hit = chapter %d, want planted chapter %d (hits=%+v)", clue, hits[0].ChapterIndex, target, hits)
				continue
			}
			if !strings.Contains(hits[0].Snippet, clue) {
				t.Errorf("clue %q recalled chapter %d but the snippet lost the line: %q", clue, hits[0].ChapterIndex, hits[0].Snippet)
				continue
			}
			hitChapters++
		}
		rate := float64(hitChapters) / float64(len(clues))
		if rate < 0.90 {
			t.Fatalf("planted three-rune clue recall = %.2f (%d/%d), want >= 0.90", rate, hitChapters, len(clues))
		}
		runs = append(runs, rankings)
	}
	for i := range runs[0] {
		if runs[0][i] != runs[1][i] {
			t.Fatalf("Search is not deterministic: run 1 = %s, run 2 = %s", runs[0][i], runs[1][i])
		}
	}

	// 模糊方向：只与两章共享「现有」这个两字残片（章节频 2，够罕见但按长度阶梯不可追溯），零注入。
	if vague := Search(docs, query, Options{Intent: vagueIntent}); len(vague) != 0 {
		t.Fatalf("a direction whose only shared fragment is a two-rune boilerplate produced %d hits, want 0: %+v", len(vague), vague)
	}
	// 与正文没有任何共享线索的方向同样不注入。
	absent := Search(docs, query, Options{Intent: "把这段往前推，不必回看。"})
	if len(absent) != 0 {
		t.Fatalf("a direction with no shared clue produced %d hits, want 0: %+v", len(absent), absent)
	}
}

// TestSearchRepeatedClueOutranksSingleMention 覆盖真实词频：同一线索在一章里反复出现，
// 必须比在另一章里只出现一次更值钱。tf≡1 的存在性打分在这里只会按「越新越前」误排。
func TestSearchRepeatedClueOutranksSingleMention(t *testing.T) {
	filler := strings.Repeat("码头的风把缆绳吹得笔直。", 40)
	docs := make([]Document, 0, 42)
	for i := 1; i <= 40; i++ {
		docs = append(docs, Document{
			ChapterID: fmt.Sprintf("ch_%d", i), Index: i, Content: filler,
		})
	}
	docs = append(docs,
		Document{ChapterID: "ch_41", Index: 41, Content: filler + strings.Repeat("\n他反复摩挲青玉符。", 8)},
		Document{ChapterID: "ch_42", Index: 42, Content: filler + "\n他摸出青玉符。"})

	hits := Search(docs, filler, Options{Intent: "青玉符的来历"})
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	var repeated, single float64
	var repeatedSeen, singleSeen bool
	for _, hit := range hits {
		switch hit.ChapterIndex {
		case 41:
			repeated, repeatedSeen = hit.Score, true
		case 42:
			single, singleSeen = hit.Score, true
		}
	}
	if !repeatedSeen || !singleSeen {
		t.Fatalf("both clue chapters should be recalled, got %+v", hits)
	}
	if repeated <= single {
		t.Errorf("repeated clue score %f did not outrank single mention %f", repeated, single)
	}
}

func TestSearchTopKDistinctChaptersAndDeterministicTie(t *testing.T) {
	docs := []Document{
		{ChapterID: "ch_1", Index: 1, Content: strings.Repeat("无关内容。", 100)},
		{ChapterID: "ch_2", Index: 2, Content: "青玉符藏在旧宅。"},
		{ChapterID: "ch_3", Index: 3, Content: "青玉符藏在旧宅。"},
		{ChapterID: "ch_4", Index: 4, Content: "青玉符藏在旧宅。"},
		{ChapterID: "ch_5", Index: 5, Content: "青玉符藏在旧宅。"},
	}
	// 本例只考 TopK 去重、并列序与确定性，语料里几乎每章都有线索词，
	// 因此显式关掉罕见词闸（RareDocumentRatio=1 即任何命中都算罕见）。
	first := Search(docs, "青玉符", Options{TopK: 3, RareDocumentRatio: 1})
	second := Search(docs, "青玉符", Options{TopK: 3, RareDocumentRatio: 1})
	if len(first) != 3 {
		t.Fatalf("hits = %d, want 3", len(first))
	}
	seen := map[string]struct{}{}
	for _, hit := range first {
		if _, ok := seen[hit.ChapterID]; ok {
			t.Errorf("duplicate chapter in hits: %s", hit.ChapterID)
		}
		seen[hit.ChapterID] = struct{}{}
	}
	if first[0].ChapterIndex != 5 || first[1].ChapterIndex != 4 || first[2].ChapterIndex != 3 {
		t.Errorf("tie order = [%d,%d,%d], want recency order [5,4,3]",
			first[0].ChapterIndex, first[1].ChapterIndex, first[2].ChapterIndex)
	}
	for i := range first {
		if first[i].ChapterID != second[i].ChapterID || first[i].Snippet != second[i].Snippet {
			t.Errorf("Search is not deterministic at %d", i)
		}
	}
}

func TestSearchSnippetBounded(t *testing.T) {
	content := strings.Repeat("这是没有命中的铺垫。", 400) +
		"\n" + strings.Repeat("命中词之前的背景。", 100) +
		"青玉符" + strings.Repeat("命中词之后的收束。", 100)
	docs := []Document{{ChapterID: "ch_1", Index: 1, Content: content}}
	hits := Search(docs, "青玉符", Options{SnippetRunes: 300})
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	hit := hits[0]
	if !strings.Contains(hit.Snippet, "青玉符") {
		t.Errorf("snippet missing hit: %q", hit.Snippet)
	}
	if got := utf8.RuneCountInString(hit.Snippet); got > 300 {
		t.Errorf("snippet runes = %d, want <= 300", got)
	}
	if hit.EndRune > utf8.RuneCountInString(content) {
		t.Errorf("EndRune = %d beyond content", hit.EndRune)
	}
}

func TestSearchEmptyInputs(t *testing.T) {
	if hits := Search(nil, "青玉符", Options{}); hits != nil {
		t.Errorf("nil docs returned %v", hits)
	}
	docs := []Document{{ChapterID: "ch_1", Index: 1, Content: "青玉符"}}
	if hits := Search(docs, "   ", Options{}); hits != nil {
		t.Errorf("empty query returned %v", hits)
	}
	if hits := Search(docs, "完全无关", Options{}); hits != nil {
		t.Errorf("no-match query returned %v", hits)
	}
}

func corpusDocs(chapters, repeat int) []Document {
	docs := make([]Document, 0, chapters)
	for i := 1; i <= chapters; i++ {
		content := strings.Repeat("雨夜长街，人物对话与旧事反复出现。", repeat)
		if i == 3 {
			content += "青玉符藏在旧宅梁下。"
		}
		docs = append(docs, Document{
			ChapterID: fmt.Sprintf("ch_%d", i), Index: i,
			Title: fmt.Sprintf("第%d章", i), Content: content,
		})
	}
	return docs
}

func benchSearch(b *testing.B, docs []Document) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if hits := Search(docs, "继续追查青玉符的来历", Options{}); len(hits) == 0 {
			b.Fatal("no hits")
		}
	}
}

func BenchmarkSearch200Chapters(b *testing.B) {
	benchSearch(b, corpusDocs(200, 200))
}

// BenchmarkSearch5MChapterCorpus 度量 500 章、约 500 万 rune 全集检索成本，
// 对应 longform-5m 规模下每次续写启用 R4 的最坏时延与内存。
func BenchmarkSearch5MChapterCorpus(b *testing.B) {
	benchSearch(b, corpusDocs(500, 625))
}
