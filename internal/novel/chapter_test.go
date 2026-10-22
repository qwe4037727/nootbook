package novel

import (
	"strings"
	"testing"
)

func TestSplitChaptersChineseHeadings(t *testing.T) {
	text := `书名：无边悔恨
作者：作手
状态：完结

第1章 初始

　　天空下着雨。

第2章 重瞳

　　他睁开眼，看见了两道影子。

第3章 女帝（本章字数：1200）

　　她来了。

本章未完，点击下一页继续阅读
`
	chapters := SplitChapters(text, "")
	if len(chapters) != 3 {
		t.Fatalf("len(chapters) = %d, want 3", len(chapters))
	}
	wantTitles := []string{"第1章 初始", "第2章 重瞳", "第3章 女帝"}
	for i, want := range wantTitles {
		if chapters[i].Title != want {
			t.Errorf("chapters[%d].Title = %q, want %q", i, chapters[i].Title, want)
		}
		if chapters[i].Index != i+1 {
			t.Errorf("chapters[%d].Index = %d, want %d", i, chapters[i].Index, i+1)
		}
		if !ValidID(chapters[i].ID) {
			t.Errorf("chapters[%d].ID = %q is invalid", i, chapters[i].ID)
		}
	}
	if strings.Contains(chapters[2].Content, "本章字数") {
		t.Errorf("chapter content kept heading metadata: %q", chapters[2].Content)
	}
	if strings.Contains(chapters[2].Content, "点击下一页") {
		t.Errorf("chapter content kept export junk: %q", chapters[2].Content)
	}
	if chapters[0].CharCount != CountChars(chapters[0].Content) {
		t.Errorf("CharCount = %d, want %d", chapters[0].CharCount, CountChars(chapters[0].Content))
	}
}

func TestSplitChaptersMarkdownHeadings(t *testing.T) {
	text := `# 第一章 落雨

雨声渐密。

## 第二章 旧信

信纸已经泛黄。
`
	chapters := SplitChapters(text, "")
	if len(chapters) != 2 {
		t.Fatalf("len(chapters) = %d, want 2", len(chapters))
	}
	if chapters[0].Title != "第一章 落雨" || chapters[1].Title != "第二章 旧信" {
		t.Errorf("titles = %q / %q", chapters[0].Title, chapters[1].Title)
	}
	if chapters[0].Content != "雨声渐密。" {
		t.Errorf("content = %q", chapters[0].Content)
	}
}

func TestSplitChaptersIgnoresBodySentences(t *testing.T) {
	text := `第一章的内容十分精彩。

他合上书，继续赶路。`
	chapters := SplitChapters(text, "残卷")
	if len(chapters) != 1 {
		t.Fatalf("len(chapters) = %d, want 1", len(chapters))
	}
	if chapters[0].Title != "残卷" {
		t.Errorf("Title = %q, want fallback title", chapters[0].Title)
	}
	if !strings.Contains(chapters[0].Content, "第一章的内容十分精彩。") {
		t.Errorf("body sentence was dropped: %q", chapters[0].Content)
	}
}

func TestSplitChaptersWithoutHeadings(t *testing.T) {
	chapters := SplitChapters("只有一段正文。", "")
	if len(chapters) != 1 {
		t.Fatalf("len(chapters) = %d, want 1", len(chapters))
	}
	if chapters[0].Title != "正文" {
		t.Errorf("Title = %q, want 正文", chapters[0].Title)
	}
	if chapters[0].Index != 1 {
		t.Errorf("Index = %d, want 1", chapters[0].Index)
	}
}

func TestSplitChaptersKeepsSubstantialPreface(t *testing.T) {
	preface := strings.Repeat("这是序章里的一段旧事，交代了故事的来处。", 20)
	text := preface + "\n\n第一章 起风\n\n正文开始。"
	chapters := SplitChapters(text, "")
	if len(chapters) != 2 {
		t.Fatalf("len(chapters) = %d, want 2", len(chapters))
	}
	if chapters[0].Title != "前言" {
		t.Errorf("preface title = %q", chapters[0].Title)
	}
	if chapters[1].Title != "第一章 起风" || chapters[1].Index != 2 {
		t.Errorf("chapter = %q index %d", chapters[1].Title, chapters[1].Index)
	}
}

func TestInferTitle(t *testing.T) {
	if got := InferTitle("\ufeff书名：山河旧事\n作者：某某"); got != "山河旧事" {
		t.Errorf("InferTitle = %q", got)
	}
	if got := InferTitle("随便一段开头\n作品名: 长夜"); got != "长夜" {
		t.Errorf("InferTitle = %q", got)
	}
	if got := InferTitle("没有书名信息"); got != "" {
		t.Errorf("InferTitle = %q, want empty", got)
	}
}
