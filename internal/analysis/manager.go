package analysis

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"novelagent/internal/llm"
	"novelagent/internal/novel"
	"novelagent/internal/store"
)

type Manager struct {
	store  *store.Store
	log    *slog.Logger
	notify chan struct{}

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewManager(st *store.Store, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		store:  st,
		log:    logger,
		notify: make(chan struct{}, 1),
	}
}

func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.wg.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.wg.Done()
		m.loop(ctx)
	}()
	m.Notify()
}

func (m *Manager) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	m.wg.Wait()
}

func (m *Manager) Notify() {
	select {
	case m.notify <- struct{}{}:
	default:
	}
}

func (m *Manager) Retry() {
	project, err := m.store.LoadProject()
	switch {
	case err != nil:
		m.log.Error("load project for analysis retry", "err", err)
	case len(project.Chapters) > 0 && (project.Analysis.Status == novel.AnalysisError || project.Analysis.Status == novel.AnalysisIdle):
		if _, err := m.store.SetAnalysisPending(); err != nil {
			m.log.Error("set analysis pending", "err", err)
		}
	}
	m.Notify()
}

func (m *Manager) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.notify:
		}
		for {
			if !m.step(ctx) {
				break
			}
			if ctx.Err() != nil {
				return
			}
		}
	}
}

func (m *Manager) step(ctx context.Context) bool {
	project, err := m.store.LoadProject()
	if err != nil {
		m.log.Error("load project for analysis", "err", err)
		return false
	}
	if len(project.Chapters) == 0 {
		return false
	}
	status := project.Analysis.Status
	if status != novel.AnalysisPending && status != novel.AnalysisRunning {
		return false
	}

	cfg, err := m.store.LoadConfig()
	if err != nil {
		m.log.Error("load config for analysis", "err", err)
		return false
	}
	if cfg.BaseURL == "" || cfg.Model == "" {
		return m.markFailed(project.Revision, "请先完成模型设置")
	}
	key, _ := cfg.EffectiveAPIKey()
	client := llm.NewClient(cfg.BaseURL, key, cfg.Model)

	knowledge, err := m.store.LoadKnowledge()
	if err != nil {
		m.log.Error("load knowledge", "err", err)
		return false
	}

	revision := project.Revision
	if status == novel.AnalysisPending {
		updated, ok, err := m.store.CommitAnalysis(revision, project.Analysis.AnalyzedCount, knowledge)
		if err != nil {
			m.log.Error("mark analysis running", "err", err)
			return false
		}
		if !ok {
			return true
		}
		project = updated
	}

	for project.Analysis.AnalyzedCount < len(project.Chapters) {
		if ctx.Err() != nil {
			return false
		}
		meta := project.Chapters[project.Analysis.AnalyzedCount]
		chapter, err := m.store.LoadChapter(meta.ID)
		if err != nil {
			m.log.Error("load chapter for analysis", "chapter", meta.Index, "err", err)
			return m.markFailed(project.Revision, "读取章节失败，请重试")
		}

		if strings.TrimSpace(chapter.Content) != "" {
			messages := buildAnalystMessages(project.Title, chapter, knowledge)
			text, err := completeWithTimeout(ctx, client, messages, 0.2)
			if err != nil {
				if ctx.Err() != nil {
					return false
				}
				m.log.Error("analyze chapter", "chapter", chapter.Index, "err", err, "model", cfg.Model)
				return m.markFailed(project.Revision, "分析中断，请检查模型设置后重试")
			}
			var result chapterResult
			if err := parseJSONObject(text, &result); err != nil {
				m.log.Error("decode chapter analysis", "chapter", chapter.Index, "err", err, "head", headForLog(text, 300))
				return m.markFailed(project.Revision, "分析结果格式异常，请重试")
			}
			applyChapter(&knowledge, chapter, result)
		}

		updated, ok, err := m.store.CommitAnalysis(project.Revision, chapter.Index, knowledge)
		if err != nil {
			m.log.Error("commit chapter analysis", "chapter", chapter.Index, "err", err)
			return false
		}
		if !ok {
			return true
		}
		project = updated
	}

	if len(knowledge.Summaries) > 0 {
		messages := buildSynthesisMessages(project.Title, knowledge)
		text, err := completeWithTimeout(ctx, client, messages, 0.3)
		if err != nil {
			if ctx.Err() != nil {
				return false
			}
			m.log.Error("synthesize knowledge", "err", err, "model", cfg.Model)
			return m.markFailed(project.Revision, "总览更新失败，请重试")
		}
		var result synthesisResult
		if err := parseJSONObject(text, &result); err != nil {
			m.log.Error("decode synthesis", "err", err, "head", headForLog(text, 300))
			return m.markFailed(project.Revision, "总览格式异常，请重试")
		}
		applySynthesis(&knowledge, result)
	}

	_, ok, err := m.store.FinishAnalysis(project.Revision, knowledge)
	if err != nil {
		m.log.Error("finish analysis", "err", err)
		return false
	}
	if !ok {
		return true
	}
	m.log.Info("knowledge base updated",
		"chapters", len(project.Chapters),
		"characters", len(knowledge.Characters),
		"world", len(knowledge.World),
	)
	return false
}

func (m *Manager) markFailed(revision int64, message string) bool {
	if _, _, err := m.store.FailAnalysis(revision, message); err != nil {
		m.log.Error("record analysis failure", "err", err)
		return false
	}
	return true
}

const analysisCallTimeout = 5 * time.Minute

func completeWithTimeout(ctx context.Context, client *llm.Client, messages []llm.Message, temperature float64) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, analysisCallTimeout)
	defer cancel()
	return client.CompleteChat(callCtx, messages, temperature)
}

func headForLog(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}
