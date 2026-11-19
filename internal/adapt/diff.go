package adapt

import "strings"

// DiffPart 是 diff 的片段：equal / delete / insert。
type DiffPart struct {
	Op   string `json:"op"`
	Text string `json:"text"`
}

// maxDiffCells 限制 LCS 规模，超限降级为整段替换，避免长选区耗尽内存。
const maxDiffCells = 2_000_000

// Diff 计算旧选区与新文本的差异，返回可读 diff 文本与结构化片段。
func Diff(oldText, newText string) (string, []DiffPart) {
	a := []rune(oldText)
	b := []rune(newText)
	parts := diffParts(a, b)
	return renderDiff(parts), parts
}

func diffParts(a, b []rune) []DiffPart {
	if string(a) == string(b) {
		if len(a) == 0 {
			return []DiffPart{}
		}
		return []DiffPart{{Op: "equal", Text: string(a)}}
	}
	if len(a)*len(b) > maxDiffCells {
		return []DiffPart{
			{Op: "delete", Text: string(a)},
			{Op: "insert", Text: string(b)},
		}
	}

	// LCS 长度表：lcs[i][j] = a[i:] 与 b[j:] 的公共子序列长度。
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	parts := []DiffPart{}
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			appendPart(&parts, "equal", string(a[i]))
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			appendPart(&parts, "delete", string(a[i]))
			i++
		default:
			appendPart(&parts, "insert", string(b[j]))
			j++
		}
	}
	for ; i < len(a); i++ {
		appendPart(&parts, "delete", string(a[i]))
	}
	for ; j < len(b); j++ {
		appendPart(&parts, "insert", string(b[j]))
	}
	return parts
}

func appendPart(parts *[]DiffPart, op, text string) {
	if n := len(*parts); n > 0 && (*parts)[n-1].Op == op {
		(*parts)[n-1].Text += text
		return
	}
	*parts = append(*parts, DiffPart{Op: op, Text: text})
}

func renderDiff(parts []DiffPart) string {
	var b strings.Builder
	for _, part := range parts {
		switch part.Op {
		case "equal":
			b.WriteString(part.Text)
		case "delete":
			b.WriteString("\n【删】")
			b.WriteString(part.Text)
			b.WriteString("\n")
		case "insert":
			b.WriteString("\n【增】")
			b.WriteString(part.Text)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}
