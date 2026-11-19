package app

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// handleGlobalCost 汇总所有项目的 token 成本；tokens 为聚合数据，不返回正文或词条内容。
func (r *Router) handleGlobalCost(w http.ResponseWriter, req *http.Request) {
	query := req.URL.Query()
	since := time.Time{}
	if value := strings.TrimSpace(query.Get("since")); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "since 需要 RFC3339 时间")
			return
		}
		since = parsed
	} else if days, err := strconv.Atoi(query.Get("days")); err == nil && days > 0 {
		since = time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	}
	summary, err := r.manager.GlobalCostSummary(since)
	if err != nil {
		r.log.Error("global cost summary", "err", err)
		writeError(w, http.StatusInternalServerError, "读取全局成本看板失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, summary)
}
