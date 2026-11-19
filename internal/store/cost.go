package store

import (
	"fmt"
	"time"
)

// CostBucket 是成本看板中的一个来源分组（Agent 运行 / 改编 / 续写候选）。
type CostBucket struct {
	Source           string `json:"source"`
	Calls            int64  `json:"calls"`
	Failed           int64  `json:"failed"`
	PromptTokens     int64  `json:"promptTokens"`
	CompletionTokens int64  `json:"completionTokens"`
	TotalTokens      int64  `json:"totalTokens"`
}

// CostSummary 是项目级成本看板：token 汇总 + 任务队列状态计数。
type CostSummary struct {
	Since      time.Time        `json:"since,omitempty"`
	Buckets    []CostBucket     `json:"buckets"`
	Total      CostBucket       `json:"total"`
	TaskCounts map[string]int64 `json:"taskCounts"`
}

// CostSummary 汇总 since 之后 agent_run / adapt_run / continue_candidate 的 token 成本。
func (s *Store) CostSummary(since time.Time) (CostSummary, error) {
	db, err := s.agentDatabase()
	if err != nil {
		return CostSummary{}, err
	}
	out := CostSummary{
		Since:      since,
		Buckets:    []CostBucket{},
		TaskCounts: map[string]int64{},
	}
	add := func(bucket CostBucket) {
		out.Buckets = append(out.Buckets, bucket)
		out.Total.Calls += bucket.Calls
		out.Total.Failed += bucket.Failed
		out.Total.PromptTokens += bucket.PromptTokens
		out.Total.CompletionTokens += bucket.CompletionTokens
		out.Total.TotalTokens += bucket.TotalTokens
	}

	type querySpec struct {
		source  string
		timeCol string
		sql     string
		args    []any
	}
	specs := []querySpec{
		{
			source:  "agent_run",
			timeCol: "started_at",
			sql: `SELECT COUNT(*), COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END),0),
				COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0), COALESCE(SUM(total_tokens),0)
				FROM agent_run`,
		},
		{
			source:  "adapt_run",
			timeCol: "created_at",
			sql: `SELECT COUNT(*), COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END),0),
				COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0), COALESCE(SUM(total_tokens),0)
				FROM adapt_run`,
		},
		{
			source:  "continue_candidate",
			timeCol: "created_at",
			sql: `SELECT COUNT(*), COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END),0),
				COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0), COALESCE(SUM(total_tokens),0)
				FROM continue_candidate`,
		},
	}
	for i := range specs {
		query := specs[i].sql
		args := append([]any{}, specs[i].args...)
		if !since.IsZero() {
			query += ` WHERE ` + specs[i].timeCol + ` >= ?`
			args = append(args, unixMillis(since))
		}
		var bucket CostBucket
		bucket.Source = specs[i].source
		if err := db.QueryRow(query, args...).Scan(&bucket.Calls, &bucket.Failed, &bucket.PromptTokens,
			&bucket.CompletionTokens, &bucket.TotalTokens); err != nil {
			return CostSummary{}, fmt.Errorf("store: cost summary %s: %w", bucket.Source, err)
		}
		add(bucket)
	}

	rows, err := db.Query(`SELECT status, COUNT(*) FROM task GROUP BY status`)
	if err != nil {
		return CostSummary{}, fmt.Errorf("store: task counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return CostSummary{}, fmt.Errorf("store: scan task counts: %w", err)
		}
		out.TaskCounts[status] = count
	}
	if err := rows.Err(); err != nil {
		return CostSummary{}, err
	}
	return out, nil
}
