package novel

import (
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	chapterHeadingPattern  = regexp.MustCompile(`^(?:#{1,6}\s*)?第\s*[0-9０-９零〇一二三四五六七八九十百千万两]+\s*[章回节](.*)$`)
	metadataLinePattern    = regexp.MustCompile(`^(?:本章字数|更新时间|发布时间|本章|字数|阅读人数|推荐票)\s*[:：]`)
	headingMetadataPattern = regexp.MustCompile(`\s*[（(【\[]?\s*(?:本章字数|更新时间|发布时间)\s*[:：].*$`)
	trailingExportPattern  = regexp.MustCompile(`^(?:本章未完|点击下一页|请收藏|最新网址|章节报错|百度搜索|加入书签|投推荐票)`)
	prefaceMetadataPattern = regexp.MustCompile(`^(?:书名|作者|状态|评分|字数|章节|分类|标签|在读|简介|版权|来源|book_id|封面)\s*[:：=]`)
)

func SplitChapters(text, fallbackTitle string) []Chapter {
	text = normalizeText(text)
	if strings.TrimSpace(text) == "" {
		return nil
	}

	lines := strings.Split(text, "\n")
	type heading struct {
		title string
		line  int
	}
	headings := make([]heading, 0, 64)
	for i, line := range lines {
		if title, ok := parseChapterHeading(line); ok {
			headings = append(headings, heading{title: title, line: i})
		}
	}

	if len(headings) == 0 {
		content := cleanChapterBody(text)
		if content == "" {
			return nil
		}
		title := strings.TrimSpace(fallbackTitle)
		if title == "" {
			title = "正文"
		}
		chapter := newChapter(title, content)
		chapter.Index = 1
		return []Chapter{chapter}
	}

	chapters := make([]Chapter, 0, len(headings)+1)
	preface := strings.TrimSpace(strings.Join(lines[:headings[0].line], "\n"))
	if usablePreface(preface) {
		chapters = append(chapters, newChapter("前言", cleanChapterBody(preface)))
	}
	for i, h := range headings {
		end := len(lines)
		if i+1 < len(headings) {
			end = headings[i+1].line
		}
		content := cleanChapterBody(strings.Join(lines[h.line+1:end], "\n"))
		if content == "" {
			continue
		}
		chapters = append(chapters, newChapter(h.title, content))
	}
	if len(chapters) == 0 {
		return nil
	}
	for i := range chapters {
		chapters[i].Index = i + 1
	}
	return chapters
}

func parseChapterHeading(line string) (string, bool) {
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "\ufeff"))
	if line == "" || utf8.RuneCountInString(line) > 64 {
		return "", false
	}
	line = strings.TrimPrefix(line, "**")
	line = strings.TrimSuffix(line, "**")
	line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
	match := chapterHeadingPattern.FindStringSubmatch(line)
	if match == nil {
		return "", false
	}

	rest := match[1]
	trimmed := strings.TrimSpace(rest)
	if trimmed != "" {
		first, _ := utf8.DecodeRuneInString(rest)
		separated := unicode.IsSpace(first) || strings.ContainsRune(":：、.．-—·", first)
		if !separated {
			for _, prefix := range []string{"的", "是", "中", "里", "内", "节", "章", "回", "讲", "说", "写", "描"} {
				if strings.HasPrefix(trimmed, prefix) {
					return "", false
				}
			}
			if strings.HasSuffix(trimmed, "。") || strings.HasSuffix(trimmed, "，") {
				return "", false
			}
		}
	}

	title := strings.TrimSpace(headingMetadataPattern.ReplaceAllString(line, ""))
	title = strings.TrimSpace(strings.Trim(title, "*"))
	if title == "" {
		return "", false
	}
	return title, true
}

func normalizeText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.ReplaceAll(text, "\u200b", "")
	text = strings.ReplaceAll(text, "\u200c", "")
	text = strings.ReplaceAll(text, "\u200d", "")
	text = strings.TrimPrefix(text, "\ufeff")
	return text
}

func cleanChapterBody(body string) string {
	body = normalizeText(body)
	lines := strings.Split(body, "\n")

	for len(lines) > 0 {
		line := strings.TrimSpace(lines[0])
		if line == "" || metadataLinePattern.MatchString(line) {
			lines = lines[1:]
			continue
		}
		break
	}
	for len(lines) > 0 {
		line := strings.TrimSpace(lines[len(lines)-1])
		if line == "" || metadataLinePattern.MatchString(line) || trailingExportPattern.MatchString(line) {
			lines = lines[:len(lines)-1]
			continue
		}
		break
	}

	cleaned := make([]string, 0, len(lines))
	blank := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t\u3000")
		if strings.TrimSpace(line) == "" {
			blank++
			if blank > 2 {
				continue
			}
			cleaned = append(cleaned, "")
			continue
		}
		blank = 0
		cleaned = append(cleaned, line)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func usablePreface(text string) bool {
	if CountChars(text) < 200 {
		return false
	}
	trimmed := strings.TrimSpace(text)
	if strings.Contains(trimmed, "book_id") || strings.HasPrefix(trimmed, "书名：") || strings.HasPrefix(trimmed, "书名:") {
		return false
	}
	lines := strings.Split(trimmed, "\n")
	total, metadata := 0, 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "=") {
			continue
		}
		total++
		if prefaceMetadataPattern.MatchString(line) {
			metadata++
		}
	}
	return total > 0 && metadata*2 < total
}

func newChapter(title, content string) Chapter {
	title = strings.TrimSpace(headingMetadataPattern.ReplaceAllString(title, ""))
	content = strings.TrimSpace(content)
	return Chapter{
		ID:        NewID("ch"),
		Title:     title,
		Content:   content,
		CharCount: CountChars(content),
		UpdatedAt: time.Now().UTC(),
	}
}
