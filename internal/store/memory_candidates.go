package store

import (
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"novelagent/internal/novel"
)

// 实体候选确认（I2-6，spec §6.3 文件级路径，iter2-详设 §4.5）。
// entity_candidates.json 与 changesets.json / extract_state.json 同属草稿区（data/memory/），
// 由 resetMemoryLocked 一同清空（memory_init.go）。本期候选确认走"文件级确认"而非 ChangeSet op。

const entityCandidatesFile = "entity_candidates.json"

var (
	ErrCandidateResolutionInvalid = errors.New("store: invalid candidate resolution")
)

// loadEntityCandidatesLocked 读取 entity_candidates.json（memoryMu 域内）。
func (s *Store) loadEntityCandidatesLocked() ([]novel.EntityCandidate, error) {
	var cands []novel.EntityCandidate
	err := s.readMemoryFileLocked(entityCandidatesFile, &cands)
	if errors.Is(err, os.ErrNotExist) {
		return []novel.EntityCandidate{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: load entity candidates: %w", err)
	}
	if cands == nil {
		cands = []novel.EntityCandidate{}
	}
	return cands, nil
}

func (s *Store) writeEntityCandidatesLocked(cands []novel.EntityCandidate) error {
	return s.writeMemoryFileLocked(entityCandidatesFile, cands)
}

func (s *Store) getCandidateLocked(id string) (novel.EntityCandidate, error) {
	cands, err := s.loadEntityCandidatesLocked()
	if err != nil {
		return novel.EntityCandidate{}, err
	}
	for _, c := range cands {
		if c.ID == id {
			return c, nil
		}
	}
	return novel.EntityCandidate{}, fmt.Errorf("%w: %s", ErrCandidateNotFound, id)
}

// mergeCandidates 合并提取产出候选入既有候选列表（memoryMu 域内，由 saveChangeSetLocked 调用）：
// 同键（归一化 name+aliases）去重——保留既有、并入新别名与 SourceRefs；新增的追加。
// 返回合并后列表与新增数（供提取统计 CandidatesCreated）。
func mergeCandidates(existing, incoming []novel.EntityCandidate) ([]novel.EntityCandidate, int) {
	merged := make([]novel.EntityCandidate, 0, len(existing)+len(incoming))
	seen := make(map[string]int) // 归一键 -> 在 merged 中的下标
	now := time.Now().UTC()
	added := 0

	addOrMerge := func(c novel.EntityCandidate) {
		c.Normalize(now)
		key := entityCandidateKey(c.Name, c.Aliases)
		if idx, ok := seen[key]; ok {
			// 并入既有：追加别名/可能 ID/证据引用并去重。
			base := merged[idx]
			base.Aliases = dedupeStrings(append(base.Aliases, c.Aliases...))
			base.PossibleIDs = dedupeStrings(append(base.PossibleIDs, c.PossibleIDs...))
			base.SourceRefs = mergeSourceRefs(base.SourceRefs, c.SourceRefs)
			if base.Reason == "" {
				base.Reason = c.Reason
			}
			merged[idx] = base
			return
		}
		seen[key] = len(merged)
		merged = append(merged, c)
		added++
	}

	for i := range existing {
		addOrMerge(existing[i])
	}
	for i := range incoming {
		addOrMerge(incoming[i])
	}
	return merged, added
}

// LoadCandidates 列表（可按 status 过滤；空返回全部），按创建时间倒序。
func (s *Store) LoadCandidates(statusFilter string) ([]novel.EntityCandidate, error) {
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()
	cands, err := s.loadEntityCandidatesLocked()
	if err != nil {
		return nil, err
	}
	if statusFilter != "" {
		filtered := cands[:0]
		for _, c := range cands {
			if c.Status == statusFilter {
				filtered = append(filtered, c)
			}
		}
		cands = filtered
	}
	sort.Slice(cands, func(i, j int) bool {
		return cands[i].CreatedAt.After(cands[j].CreatedAt)
	})
	return cands, nil
}

// ResolveCandidate 确认一个候选（§4.5）：register（注册新实体）/ merge（并入目标实体）/ dismiss（忽略）。
// 并发保护：非 pending → 409。register/merge 需在提交前经 store 既有知识保存路径预写 knowledge.json
// （锁外预写：持 store.mu；失败则不进入 entity_confirm 提交）。候选状态翻转走 entity_confirm 提交
// （payload 仅覆盖 entity_candidates.json，不擅自扩展协议 payload 范围，见 §11 Q1）。
func (s *Store) ResolveCandidate(id, action, resolution string) (novel.EntityCandidate, error) {
	// 1. memoryMu：读取并校验状态（首次）。
	s.memoryMu.Lock()
	cand, err := s.getCandidateLocked(id)
	if err != nil {
		s.memoryMu.Unlock()
		return novel.EntityCandidate{}, err
	}
	if cand.Status != string(novel.CandidatePending) {
		s.memoryMu.Unlock()
		return novel.EntityCandidate{}, fmt.Errorf("%w: candidate status %s", ErrCandidateNotPending, cand.Status)
	}
	s.memoryMu.Unlock()

	// 2. 锁外预写 knowledge（register/merge），失败即终止（不进入提交）。
	targetID := ""
	if action == "register" || action == "merge" {
		var err error
		targetID, err = s.prewriteCandidateKnowledge(cand, action, resolution)
		if err != nil {
			return novel.EntityCandidate{}, err
		}
	}

	// 3. memoryMu：重读二次校验（防窗口期被改）、翻转状态、entity_confirm 提交。
	s.memoryMu.Lock()
	defer s.memoryMu.Unlock()

	cand, err = s.getCandidateLocked(id)
	if err != nil {
		return novel.EntityCandidate{}, err
	}
	if cand.Status != string(novel.CandidatePending) {
		return novel.EntityCandidate{}, fmt.Errorf("%w: candidate status %s", ErrCandidateNotPending, cand.Status)
	}

	switch action {
	case "register":
		cand.Status = string(novel.CandidateRegister)
		cand.Resolution = targetID // D6：记录最终实体 ID（新建或查重命中）
	case "merge":
		if resolution == "" {
			return novel.EntityCandidate{}, fmt.Errorf("%w: merge requires resolution target id", ErrCandidateResolutionInvalid)
		}
		cand.Status = string(novel.CandidateMerge)
		cand.Resolution = resolution
	case "dismiss":
		cand.Status = string(novel.CandidateDismissed)
		cand.Resolution = "dismissed"
	default:
		return novel.EntityCandidate{}, fmt.Errorf("%w: action %q", ErrCandidateResolutionInvalid, action)
	}
	cand.ResolvedAt = time.Now().UTC()

	cands, err := s.loadEntityCandidatesLocked()
	if err != nil {
		return novel.EntityCandidate{}, err
	}
	for i := range cands {
		if cands[i].ID == id {
			cands[i] = cand
			break
		}
	}
	meta, err := s.ensureMemoryMetaLocked()
	if err != nil {
		return novel.EntityCandidate{}, err
	}
	record, err := s.commitMemoryLocked(CommitEntityCheck, meta.MemoryRevision, map[string]any{
		entityCandidatesFile: cands,
	})
	if err != nil {
		return novel.EntityCandidate{}, err
	}
	_ = record
	// 提交后单独原子写候选终态（提交解锁后；失败仅记日志，§4.5 同构）。
	if err := s.writeEntityCandidatesLocked(cands); err != nil {
		log.Printf("store: candidate %s resolved (%s) but failed to persist: %v", id, action, err)
	}
	return cand, nil
}

// prewriteCandidateKnowledge 在 store.mu 域内经既有知识保存路径预写 knowledge.json（锁外预写，
// 提交前完成；失败返回错误，阻止后续 entity_confirm 提交）。返回最终实体 ID：
//   - register：先按 MatchCharacter/MatchWorldEntry 查重（D5），唯一命中则并入别名并复用该实体；
//     两处同时命中或任一歧义则拒绝（要求显式 merge），未命中才新建角色（c_）；
//   - merge：将候选名/别名并入 resolution 指定的既有实体（c_/w_）。
func (s *Store) prewriteCandidateKnowledge(cand novel.EntityCandidate, action, resolution string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	knowledge, err := s.loadKnowledgeLocked()
	if err != nil {
		return "", err
	}
	targetID := ""
	switch action {
	case "register":
		name := strings.TrimSpace(cand.Name)
		aliases := cleanAliasList(cand.Aliases)
		cIdx, cAmbiguous := novel.MatchCharacter(knowledge.Characters, name, aliases)
		wIdx, wAmbiguous := novel.MatchWorldEntry(knowledge.World, name, aliases)
		if cAmbiguous || wAmbiguous || (cIdx >= 0 && wIdx >= 0) {
			return "", fmt.Errorf("%w: %q ambiguously matches existing entities; use merge with explicit target id", ErrCandidateResolutionInvalid, name)
		}
		merged := append([]string{name}, aliases...)
		switch {
		case cIdx >= 0:
			targetID = knowledge.Characters[cIdx].ID
			knowledge.Characters[cIdx].Aliases = dedupeStrings(append(knowledge.Characters[cIdx].Aliases, merged...))
		case wIdx >= 0:
			targetID = knowledge.World[wIdx].ID
			knowledge.World[wIdx].Aliases = dedupeStrings(append(knowledge.World[wIdx].Aliases, merged...))
		default:
			targetID = novel.NewID("c")
			knowledge.Characters = append(knowledge.Characters, novel.Character{
				ID:      targetID,
				Name:    name,
				Aliases: aliases,
			})
		}
	case "merge":
		if !strings.HasPrefix(resolution, "c_") && !strings.HasPrefix(resolution, "w_") {
			return "", fmt.Errorf("%w: resolution %q must be c_/w_ id", ErrCandidateResolutionInvalid, resolution)
		}
		merged := append([]string{strings.TrimSpace(cand.Name)}, cand.Aliases...)
		found := false
		if strings.HasPrefix(resolution, "c_") {
			for i := range knowledge.Characters {
				if knowledge.Characters[i].ID == resolution {
					knowledge.Characters[i].Aliases = dedupeStrings(append(knowledge.Characters[i].Aliases, merged...))
					found = true
					break
				}
			}
		} else {
			for i := range knowledge.World {
				if knowledge.World[i].ID == resolution {
					knowledge.World[i].Aliases = dedupeStrings(append(knowledge.World[i].Aliases, merged...))
					found = true
					break
				}
			}
		}
		if !found {
			return "", fmt.Errorf("%w: resolution entity %q not found", ErrCandidateResolutionInvalid, resolution)
		}
		targetID = resolution
	}
	knowledge.Normalize()
	knowledge.UpdatedAt = time.Now().UTC()
	if err := s.writeKnowledgeLocked(knowledge); err != nil {
		return "", err
	}
	return targetID, nil
}

// ---- 候选去重辅助 ----

func entityCandidateKey(name string, aliases []string) string {
	parts := append([]string{strings.TrimSpace(name)}, aliases...)
	cleaned := dedupeStrings(parts)
	for i := range cleaned {
		cleaned[i] = normalizeEntityKey(cleaned[i])
	}
	sort.Strings(cleaned)
	return strings.Join(cleaned, "|")
}

func normalizeEntityKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func dedupeStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		key := normalizeEntityKey(v)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, v)
	}
	return result
}

func cleanAliasList(aliases []string) []string {
	return dedupeStrings(aliases)
}

func mergeSourceRefs(base, incoming []novel.SourceRef) []novel.SourceRef {
	seen := make(map[string]struct{}, len(base))
	out := make([]novel.SourceRef, 0, len(base)+len(incoming))
	add := func(ref novel.SourceRef) {
		key := ref.ChapterID + ":" + ref.Quote
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	for _, ref := range base {
		add(ref)
	}
	for _, ref := range incoming {
		add(ref)
	}
	return out
}
