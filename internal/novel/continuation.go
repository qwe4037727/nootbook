package novel

import "time"

// 续写候选（M4）：一次请求生成 1~3 个候选版本，并排对比、独立持久化。

const (
	ContinuationStartFull          = "full"
	ContinuationStartChapterOffset = "chapter_offset"
)

type ContinuationGroup struct {
	ID             string    `json:"id"`
	StartMode      string    `json:"startMode"`
	ChapterID      string    `json:"chapterId,omitempty"`
	StartOffset    int       `json:"startOffset,omitempty"`
	StartIndex     int       `json:"startIndex"`
	Direction      string    `json:"direction,omitempty"`
	TargetChars    int       `json:"targetChars"`
	CandidateCount int       `json:"candidateCount"`
	Temperature    float64   `json:"temperature"`
	Model          string    `json:"model,omitempty"`
	PromptChars    int       `json:"promptChars"`
	ContextChars   int       `json:"contextChars"`
	MemoryChars    int       `json:"memoryChars"`
	CreatedAt      time.Time `json:"createdAt"`
}

type ContinuationCandidate struct {
	ID               string    `json:"id"`
	GroupID          string    `json:"groupId"`
	Index            int       `json:"index"`
	Content          string    `json:"content"`
	CharCount        int       `json:"charCount"`
	Temperature      float64   `json:"temperature"`
	PromptTokens     int       `json:"promptTokens"`
	CompletionTokens int       `json:"completionTokens"`
	TotalTokens      int       `json:"totalTokens"`
	Status           string    `json:"status"`
	Error            string    `json:"error,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
}

// NormalizeCandidateCount 缺省 1，硬上限 3（验收：1~3 个候选）。
func NormalizeCandidateCount(n int) int {
	if n <= 0 {
		return 1
	}
	if n > 3 {
		return 3
	}
	return n
}
