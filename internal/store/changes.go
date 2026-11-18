package store

import "log"

// ChapterChangeKind 标识章节集合或正文发生变化的事件类型。
type ChapterChangeKind string

const (
	ChapterSaved     ChapterChangeKind = "chapter.saved"
	ChapterCreated   ChapterChangeKind = "chapter.created"
	ChapterDeleted   ChapterChangeKind = "chapter.deleted"
	ChaptersImported ChapterChangeKind = "chapters.imported"
)

// ChapterChange 是章节变更钩子的最小载荷；不携带正文，消费方按需读取真源。
type ChapterChange struct {
	Kind         ChapterChangeKind
	ChapterID    string
	Title        string
	Index        int
	CharCount    int
	ChapterCount int
}

// ChapterChangeHook 在章节写操作完成且 store.mu 释放后调用。
// 失败或 panic 仅记日志，不得阻断、不得回滚章节操作。
type ChapterChangeHook func(ChapterChange)

// SetChapterChangeHook 为当前 Store 注册实例级章节变更钩子；传 nil 取消。
// 钩子不跨 Store 共享，项目之间互不串扰。
func (s *Store) SetChapterChangeHook(fn ChapterChangeHook) {
	s.changeHookMu.Lock()
	s.changeHook = fn
	s.changeHookMu.Unlock()
}

func (s *Store) notifyChapterChange(change ChapterChange) {
	s.changeHookMu.RLock()
	fn := s.changeHook
	s.changeHookMu.RUnlock()
	if fn == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("store: chapter change hook failed (ignored): %v", r)
		}
	}()
	fn(change)
}
