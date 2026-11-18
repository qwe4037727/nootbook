// Package recall 提供零依赖的长篇历史正文召回（R4）。
// 中文按 2-gram + 3-gram、ASCII 按整词切分，词频按 BM25 饱和；
// 每个块的得分只取贡献最高的若干词（ScoreTermLimit），避免「与最近正文表层词重叠面广」
// 压过「唯一的关键线索」；注入前置闸是块内至少命中一条罕见且可追溯的线索
// （df ≤ RareDocumentRatio×块数，且与查询共享 ≥ MinClueRunes 个连续汉字或全书唯一），
// 没有可追溯线索时整段不注入，把预算还给正文尾窗。
// 续写方向（Intent）里的词按 IDF 加权放大。
package recall

import (
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	defaultChunkRunes   = 3200
	defaultOverlapRunes = 400
	defaultSnippetRunes = 800
	defaultTopK         = 3
	defaultScoreTerms   = 3
	defaultRareRatio    = 0.05
	defaultMinClueRunes = 3
	bm25K1              = 1.5
	bm25B               = 0.75
	defaultIntentWeight = 3.0
)

// Document 是一章可供召回的历史正文。
type Document struct {
	ChapterID string
	Index     int
	Title     string
	Content   string
}

// Options 为召回参数；零值使用默认值。
type Options struct {
	TopK         int
	ChunkRunes   int
	OverlapRunes int
	SnippetRunes int
	// Intent 是本次续写的方向/意图。query 通常来自最近正文，词量大而信号弱；
	// Intent 分出的词权重放大，保证远处埋着的关键名词不被最近文本的 n-gram 淹没。
	Intent       string
	IntentWeight float64
	// ScoreTermLimit 是每个块参与求和的词贡献条数（默认 3）。取前 N 大而不是全部相加，
	// 是为了让「命中一条决定性线索」能赢过「与最近正文表层词重叠面最广」。
	ScoreTermLimit int
	// RareDocumentRatio 是「罕见词」闸的章节频占比上限（默认 0.05，即最多 5% 的章节提到过）。
	// 带 Intent 时要求块内至少命中一个「方向里的罕见词」，没有方向时退回到「块内至少命中一个
	// 罕见词」；不满足就整条召回作废，把预算还给正文尾窗。
	RareDocumentRatio float64
	// MinClueRunes 是一条线索至少要有多少个连续汉字才够资格当注入闸（默认 3）。方向里的
	// 2-gram 大多是「现有/的情节」这类虚词残片，全书罕见却是巧合而非线索，靠它决定的注入
	// 只会白占正文尾窗；两字词只有在全书（块级）唯一时才放行，因为人名与物名这类短线索
	// 真的只出现一次。ASCII 整词本身成词，不受此长度约束。
	MinClueRunes int
}

// Hit 是一次召回命中，StartRune/EndRune 相对该章正文（不含标题）。
type Hit struct {
	ChapterID    string
	ChapterIndex int
	ChapterTitle string
	Score        float64
	Snippet      string
	StartRune    int
	EndRune      int
}

type chunkInfo struct {
	doc    int
	start  int
	end    int
	length int
	hits   []int
	tfs    []int
}

type window struct {
	start int
	end   int
}

// Search 在 docs 中按 query 做召回，返回最多 TopK 个不同章节。
// 空查询、空文档或无命中时返回 nil；相同输入结果确定。
func Search(docs []Document, query string, opts Options) []Hit {
	opts = opts.withDefaults()
	termSet := tokenize(query)
	intentSet := tokenize(opts.Intent)
	for term, count := range intentSet {
		if count > termSet[term] {
			termSet[term] = count
		}
	}
	if len(termSet) == 0 || len(docs) == 0 {
		return nil
	}
	queryTerms := make([]string, 0, len(termSet))
	for term := range termSet {
		queryTerms = append(queryTerms, term)
	}
	sort.Strings(queryTerms)
	queryIndex := make(map[string]int, len(queryTerms))
	for i, term := range queryTerms {
		queryIndex[term] = i
	}

	type matchedTerm struct {
		index int
		tf    int
	}
	chunks := make([]chunkInfo, 0, len(docs))
	totalChunks := 0
	totalDocs := 0
	totalLength := 0
	for di := range docs {
		content := strings.TrimSpace(docs[di].Content)
		if content == "" {
			continue
		}
		totalDocs++
		runes := []rune(content)
		titleTokens := tokenize(docs[di].Title)
		for _, w := range chunkWindows(len(runes), opts.ChunkRunes, opts.OverlapRunes) {
			totalChunks++
			tokens := tokenCounts(runes[w.start:w.end])
			for term := range titleTokens {
				if _, ok := tokens[term]; !ok {
					tokens[term] = 1
				}
			}
			length := 0
			for _, count := range tokens {
				length += count
			}
			totalLength += length
			matched := make([]matchedTerm, 0, 16)
			for term, count := range tokens {
				if index, ok := queryIndex[term]; ok {
					matched = append(matched, matchedTerm{index: index, tf: count})
				}
			}
			if len(matched) == 0 {
				continue
			}
			sort.Slice(matched, func(i, j int) bool { return matched[i].index < matched[j].index })
			hits := make([]int, len(matched))
			tfs := make([]int, len(matched))
			for i, m := range matched {
				hits[i], tfs[i] = m.index, m.tf
			}
			chunks = append(chunks, chunkInfo{
				doc: di, start: w.start, end: w.end, length: length, hits: hits, tfs: tfs,
			})
		}
	}
	if len(chunks) == 0 || totalChunks == 0 {
		return nil
	}

	df := make([]int, len(queryTerms))
	for _, chunk := range chunks {
		for _, term := range chunk.hits {
			df[term]++
		}
	}
	// 罕见闸按「多少章提到」判定，不按块频：400 rune 的重叠窗口会把长章节里的同一个词
	// 数两遍，用块频当稀有度会误杀埋在长章节里的线索。
	chapterDF := make([]int, len(queryTerms))
	{
		seen := make(map[int]struct{}, 64)
		lastDoc := -1
		for _, chunk := range chunks {
			if chunk.doc != lastDoc {
				lastDoc = chunk.doc
				seen = make(map[int]struct{}, 64)
			}
			for _, term := range chunk.hits {
				if _, ok := seen[term]; ok {
					continue
				}
				seen[term] = struct{}{}
				chapterDF[term]++
			}
		}
	}
	avgdl := float64(totalLength) / float64(totalChunks)
	idf := make([]float64, len(queryTerms))
	for i, count := range df {
		idf[i] = math.Log(1 + (float64(totalChunks)-float64(count)+0.5)/(float64(count)+0.5))
	}
	intentIndex := make(map[int]struct{}, len(intentSet))
	for term := range intentSet {
		if index, ok := queryIndex[term]; ok {
			intentIndex[index] = struct{}{}
		}
	}
	weights := make([]float64, len(queryTerms))
	for i := range idf {
		weights[i] = idf[i]
		if _, ok := intentIndex[i]; ok {
			weights[i] = idf[i] * opts.IntentWeight
		}
	}
	// 罕见词闸：df 上限按章节数比例向上取整，小语料至少允许 df=1（全书只出现一次的线索）。
	rareLimit := int(math.Ceil(float64(totalDocs) * opts.RareDocumentRatio))
	if rareLimit < 1 {
		rareLimit = 1
	}
	// 长度闸：词要么承载一段真正的短语（连续 MinClueRunes 个汉字），要么全书（块级）唯一；
	// ASCII 整词本身成词，不受长度约束。「现有 / 的情节」这类两字虚词残片在全书罕见，
	// 但只会以巧合打开闸门，所以必须靠长度或唯一性把「线索」和「巧合」分开。
	clueOK := make([]bool, len(queryTerms))
	for i, term := range queryTerms {
		runes := []rune(term)
		clueOK[i] = !unicode.Is(unicode.Han, runes[0]) || len(runes) >= opts.MinClueRunes
	}

	type docBest struct {
		chunk int
		score float64
	}
	best := make(map[int]docBest, len(chunks))
	for ci, chunk := range chunks {
		norm := bm25K1 * (1 - bm25B + bm25B*float64(chunk.length)/avgdl)
		rareMatched := false
		intentRareMatched := false
		top := make([]float64, 0, opts.ScoreTermLimit)
		for i, term := range chunk.hits {
			if chapterDF[term] <= rareLimit && (clueOK[term] || chapterDF[term] == 1) {
				rareMatched = true
				if _, ok := intentIndex[term]; ok {
					intentRareMatched = true
				}
			}
			tf := float64(chunk.tfs[i])
			contribution := weights[term] * (tf * (bm25K1 + 1)) / (tf + norm)
			if len(top) == opts.ScoreTermLimit && contribution <= top[len(top)-1] {
				continue
			}
			pos := len(top)
			for pos > 0 && top[pos-1] < contribution {
				pos--
			}
			top = append(top, 0)
			copy(top[pos+1:], top[pos:])
			top[pos] = contribution
			if len(top) > opts.ScoreTermLimit {
				top = top[:opts.ScoreTermLimit]
			}
		}
		// 注入闸：有方向时要求「方向里的罕见词」真的出现在块内——否则召回到的是与最近正文
		// 共用几个半常用词的同质章节，白占 800 字正文尾窗；没有方向时退回「块内至少一个罕见词」。
		if len(intentIndex) > 0 {
			if !intentRareMatched {
				continue
			}
		} else if !rareMatched {
			continue
		}
		score := 0.0
		for _, value := range top {
			score += value
		}
		if score <= 0 {
			continue
		}
		current, ok := best[chunk.doc]
		if !ok || score > current.score ||
			(score == current.score && chunk.start < chunks[current.chunk].start) {
			best[chunk.doc] = docBest{chunk: ci, score: score}
		}
	}
	if len(best) == 0 {
		return nil
	}

	hits := make([]Hit, 0, len(best))
	for di, candidate := range best {
		chunk := chunks[candidate.chunk]
		doc := docs[di]
		snippet, start, end := buildSnippet(doc.Content, chunk.start, chunk.end, queryIndex, weights, opts.SnippetRunes)
		hits = append(hits, Hit{
			ChapterID:    doc.ChapterID,
			ChapterIndex: doc.Index,
			ChapterTitle: doc.Title,
			Score:        candidate.score,
			Snippet:      snippet,
			StartRune:    start,
			EndRune:      end,
		})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		if hits[i].ChapterIndex != hits[j].ChapterIndex {
			return hits[i].ChapterIndex > hits[j].ChapterIndex
		}
		return hits[i].ChapterID < hits[j].ChapterID
	})
	if len(hits) > opts.TopK {
		hits = hits[:opts.TopK]
	}
	return hits
}

func (o Options) withDefaults() Options {
	if o.TopK <= 0 {
		o.TopK = defaultTopK
	}
	if o.ChunkRunes <= 0 {
		o.ChunkRunes = defaultChunkRunes
	}
	if o.OverlapRunes < 0 {
		o.OverlapRunes = 0
	}
	if o.OverlapRunes >= o.ChunkRunes {
		o.OverlapRunes = o.ChunkRunes / 8
	}
	if o.SnippetRunes <= 0 {
		o.SnippetRunes = defaultSnippetRunes
	}
	if o.IntentWeight <= 0 {
		o.IntentWeight = defaultIntentWeight
	}
	if o.ScoreTermLimit <= 0 {
		o.ScoreTermLimit = defaultScoreTerms
	}
	if o.RareDocumentRatio <= 0 || o.RareDocumentRatio > 1 {
		o.RareDocumentRatio = defaultRareRatio
	}
	if o.MinClueRunes <= 0 {
		o.MinClueRunes = defaultMinClueRunes
	}
	return o
}

func chunkWindows(length, chunkRunes, overlap int) []window {
	if length <= 0 {
		return nil
	}
	if length <= chunkRunes {
		return []window{{start: 0, end: length}}
	}
	step := chunkRunes - overlap
	if step <= 0 {
		step = chunkRunes
	}
	windows := make([]window, 0, length/step+1)
	for start := 0; start < length; start += step {
		end := start + chunkRunes
		if end > length {
			end = length
		}
		windows = append(windows, window{start: start, end: end})
		if end == length {
			break
		}
	}
	return windows
}

func tokenize(text string) map[string]int {
	return tokenCounts([]rune(text))
}

// tokenCounts 切出 2/3-gram 与 ASCII 整词，并保留每个词的出现次数：
// 反复使用的线索必须比「只出现一次」更值钱，否则召回退化成存在性打分。
func tokenCounts(runes []rune) map[string]int {
	tokens := make(map[string]int, len(runes)/2)
	for i := 0; i < len(runes); {
		switch {
		case unicode.Is(unicode.Han, runes[i]):
			j := i + 1
			for j < len(runes) && unicode.Is(unicode.Han, runes[j]) {
				j++
			}
			run := runes[i:j]
			if len(run) == 1 {
				tokens[string(run)]++
			} else {
				for k := 0; k+1 < len(run); k++ {
					tokens[string(run[k:k+2])]++
				}
				for k := 0; k+2 < len(run); k++ {
					tokens[string(run[k:k+3])]++
				}
			}
			i = j
		case isASCIIAlnum(runes[i]):
			j := i + 1
			for j < len(runes) && isASCIIAlnum(runes[j]) {
				j++
			}
			if j-i >= 2 {
				tokens[strings.ToLower(string(runes[i:j]))]++
			}
			i = j
		default:
			i++
		}
	}
	return tokens
}

func isASCIIAlnum(r rune) bool {
	return r < 128 && (r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
}

// buildSnippet 按词权重（IDF × 意图加权）选择信号最强的行，锚定该行权重最高的命中取窗口，
// 避免高频噪声词把片段锚到无关段落。返回片段（含省略号）不超过 limit rune。
func buildSnippet(content string, chunkStart, chunkEnd int, queryIndex map[string]int, weights []float64, limit int) (string, int, int) {
	runes := []rune(content)
	if chunkStart < 0 {
		chunkStart = 0
	}
	if chunkEnd > len(runes) {
		chunkEnd = len(runes)
	}
	if chunkStart >= chunkEnd {
		return "", chunkStart, chunkStart
	}
	chunk := runes[chunkStart:chunkEnd]
	starts, lines := splitLines(chunk)

	bestLine := 0
	bestWeight := -1.0
	for i, line := range lines {
		weight := 0.0
		for term := range tokenize(line) {
			if index, ok := queryIndex[term]; ok {
				weight += weights[index]
			}
		}
		if weight > bestWeight {
			bestWeight = weight
			bestLine = i
		}
	}
	hitPos := 0
	if bestWeight > 0 && bestLine < len(lines) {
		line := lines[bestLine]
		lower := strings.ToLower(line)
		bestByte := -1
		bestTermWeight := -1.0
		for term := range tokenize(line) {
			index, ok := queryIndex[term]
			if !ok {
				continue
			}
			byteIndex := strings.Index(lower, term)
			if byteIndex < 0 {
				continue
			}
			if weights[index] > bestTermWeight ||
				(weights[index] == bestTermWeight && (bestByte < 0 || byteIndex < bestByte)) {
				bestTermWeight = weights[index]
				bestByte = byteIndex
			}
		}
		if bestByte >= 0 {
			hitPos = starts[bestLine] + utf8.RuneCountInString(line[:bestByte])
		} else {
			hitPos = starts[bestLine]
		}
	}

	// 前导只留少量上下文：调用方可能把片段再裁剪到更短预算，
	// 前导过长会把真正的命中点挤出可见范围。
	lead := 60
	if lead > limit/4 {
		lead = limit / 4
	}
	start := hitPos - lead
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > len(chunk) {
		// 不为凑满长度而把窗口整体回填，否则靠近章尾的命中会被推离可见区。
		end = len(chunk)
	}
	// 尽量落在行边界，避免片段从半句开始或结束。
	if start > 0 {
		for offset := 1; offset <= 80 && start+offset < len(chunk); offset++ {
			if start+offset > hitPos {
				break
			}
			if chunk[start+offset-1] == '\n' {
				start += offset
				break
			}
		}
	}
	if end < len(chunk) {
		for offset := 0; offset < 120 && end-offset > start; offset++ {
			if chunk[end-offset-1] == '\n' {
				end -= offset
				break
			}
		}
	}

	prefix, suffix := "", ""
	if start > 0 {
		prefix = "…"
	}
	if end < len(chunk) {
		suffix = "…"
	}
	bodyLimit := limit - utf8.RuneCountInString(prefix) - utf8.RuneCountInString(suffix)
	if bodyLimit < 1 {
		bodyLimit = 1
	}
	body := chunk[start:end]
	if len(body) > bodyLimit {
		body = body[:bodyLimit]
		suffix = "…"
	}
	snippetRunes := []rune(strings.TrimSpace(prefix + string(body) + suffix))
	if len(snippetRunes) > limit {
		snippetRunes = snippetRunes[:limit]
	}
	return string(snippetRunes), chunkStart + start, chunkStart + start + len(body)
}

func splitLines(runes []rune) ([]int, []string) {
	starts := make([]int, 0, 8)
	lines := make([]string, 0, 8)
	start := 0
	for i, r := range runes {
		if r != '\n' {
			continue
		}
		starts = append(starts, start)
		lines = append(lines, strings.TrimRight(string(runes[start:i]), "\r"))
		start = i + 1
	}
	if start < len(runes) {
		starts = append(starts, start)
		lines = append(lines, strings.TrimRight(string(runes[start:]), "\r"))
	}
	if len(lines) == 0 {
		starts = append(starts, 0)
		lines = append(lines, string(runes))
	}
	return starts, lines
}
