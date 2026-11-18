package agentruntime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"novelagent/internal/store"
)

// EchoAgent 是 M0 的贯穿线 Agent：只观察章节事件并返回一份报告。
// 权限固定为 observe，不修改正文、知识库或记忆。
type EchoAgent struct {
	st  *store.Store
	log *slog.Logger
}

func (a *EchoAgent) Name() string { return "echo" }

func (a *EchoAgent) CanRun(ev Event) bool {
	switch ev.Type {
	case string(store.ChapterSaved), string(store.ChapterCreated),
		string(store.ChapterDeleted), string(store.ChaptersImported):
		return true
	default:
		return false
	}
}

func (a *EchoAgent) Run(ctx context.Context, ev Event) (Report, error) {
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	project, err := a.st.LoadProject()
	if err != nil {
		return Report{}, fmt.Errorf("read project: %w", err)
	}
	knowledge, kerr := a.st.LoadKnowledge()

	var summary string
	switch ev.Type {
	case string(store.ChapterSaved), string(store.ChapterCreated):
		title := strings.TrimSpace(ev.Title)
		chars := ev.CharCount
		if ev.ChapterID != "" {
			if chapter, err := a.st.LoadChapter(ev.ChapterID); err == nil {
				if title == "" {
					title = chapter.Title
				}
				chars = chapter.CharCount
			}
		}
		label := fmt.Sprintf("第 %d 章", ev.Index)
		if title != "" {
			label += "「" + title + "」"
		}
		action := "保存"
		if ev.Type == string(store.ChapterCreated) {
			action = "新建"
		}
		summary = fmt.Sprintf("已记录%s%s（%d 字）；全书共 %d 章。", label, action, chars, len(project.Chapters))
	case string(store.ChapterDeleted):
		summary = fmt.Sprintf("已记录章节删除；全书剩余 %d 章。", len(project.Chapters))
	case string(store.ChaptersImported):
		summary = fmt.Sprintf("已记录整书导入；全书共 %d 章。", len(project.Chapters))
	default:
		summary = "已记录章节事件。"
	}

	details := []string{
		"权限：observe（只读，不写入正文或知识库）",
		"事件：" + ev.Type,
	}
	if kerr == nil {
		details = append(details, fmt.Sprintf("知识库：%d 个角色，%d 条世界设定", len(knowledge.Characters), len(knowledge.World)))
	}
	if a.log != nil {
		a.log.Debug("echo agent observed chapter event", "type", ev.Type, "chapter", ev.ChapterID)
	}
	return Report{Summary: summary, Details: details}, nil
}
