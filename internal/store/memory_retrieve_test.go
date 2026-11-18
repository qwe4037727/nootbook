package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"novelagent/internal/novel"
)

func writeMemoryRetrieveFile(t *testing.T, dir, name string, value any) {
	t.Helper()
	memoryDir := filepath.Join(dir, memoryDirName)
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	if err := writeJSONFile(filepath.Join(memoryDir, name), value, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func retrievalKnowledge() novel.Knowledge {
	return novel.Knowledge{
		Characters: []novel.Character{
			{ID: "c_lin", Name: "林渊", Aliases: []string{"阿渊"}, Confidence: 0.9, LastSeen: 2},
			{ID: "c_su", Name: "苏黎", Confidence: 0.3},
		},
		World: []novel.WorldEntry{
			{ID: "w_bronze", Title: "铜牌", Confidence: 0.8},
		},
	}
}

const retrievalRecent = "林渊按住左臂的伤，铜牌贴在胸口。"

func recordTexts(records []novel.MemoryRecord) []string {
	texts := make([]string, 0, len(records))
	for _, record := range records {
		texts = append(texts, record.Text)
	}
	return texts
}

func hasRecordText(records []novel.MemoryRecord, want string) bool {
	for _, record := range records {
		if strings.Contains(record.Text, want) {
			return true
		}
	}
	return false
}

func TestRetrieveP0StatesWithRedLines(t *testing.T) {
	st, dir := newTestStore(t)
	writeMemoryRetrieveFile(t, dir, "states.json", []novel.StoryState{
		{ID: "state_1", Status: novel.StateActive, SubjectID: "c_lin", Category: "状态", Attribute: "伤势", Value: "左臂贯穿伤，未处理", MemoryMeta: novel.MemoryMeta{RecordVersion: 2}},
		{ID: "state_2", Status: novel.StateActive, SubjectID: "c_lin", Category: "状态", Attribute: "旧状态", Value: "不应注入", MemoryMeta: novel.MemoryMeta{RecordVersion: 1, NeedsReview: true}},
		{ID: "state_3", Status: novel.StateSuperseded, SubjectID: "c_lin", Category: "状态", Attribute: "过时", Value: "不应注入", MemoryMeta: novel.MemoryMeta{RecordVersion: 3}},
		{ID: "state_4", Status: novel.StateActive, SubjectID: "c_ghost", Category: "状态", Attribute: "未知主体", Value: "不应注入"},
		{ID: "state_5", Status: novel.StateActive, SubjectID: "w_bronze", Category: "world", Attribute: "纹样", Value: "旧朝双螭纹", MemoryMeta: novel.MemoryMeta{RecordVersion: 2}},
	})

	result, err := st.RetrieveContinuationMemory(retrievalKnowledge(), retrievalRecent, 2, 3000)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if !hasRecordText(result.Records, "林渊｜伤势：左臂贯穿伤，未处理") {
		t.Errorf("character state not injected: %v", recordTexts(result.Records))
	}
	if !hasRecordText(result.Records, "铜牌｜纹样：旧朝双螭纹") {
		t.Errorf("world rule not injected: %v", recordTexts(result.Records))
	}
	for _, forbidden := range []string{"旧状态", "过时", "未知主体"} {
		if hasRecordText(result.Records, forbidden) {
			t.Errorf("red-line record %q injected: %v", forbidden, recordTexts(result.Records))
		}
	}
	if result.SkippedReview != 1 {
		t.Errorf("SkippedReview = %d, want 1", result.SkippedReview)
	}
	if len(result.Seeds) == 0 || result.Seeds[0].EntityID != "c_lin" {
		t.Errorf("seeds = %+v, want c_lin first", result.Seeds)
	}
	for _, record := range result.Records {
		if record.Kind == novel.MemoryKindWorldState && record.Subject != "铜牌" {
			t.Errorf("world state kind assigned to %+v", record)
		}
	}
	if result.UsedChars == 0 || result.UsedChars != novel.MemorySectionChars(&result) {
		t.Errorf("UsedChars = %d, section chars = %d", result.UsedChars, novel.MemorySectionChars(&result))
	}
}

func TestRetrieveEventsRecentFirstWithCap(t *testing.T) {
	st, dir := newTestStore(t)
	events := []novel.StoryEvent{
		{ID: "evt_1", Status: novel.EventConfirmed, Summary: "事件一", ChapterID: "ch_1", StoryOrder: 1, Participants: []string{"c_lin"}},
		{ID: "evt_2", Status: novel.EventConfirmed, Summary: "事件二", StoryOrder: 2, Participants: []string{"c_lin"}},
		{ID: "evt_3", Status: novel.EventConfirmed, Summary: "事件三", StoryOrder: 3, Participants: []string{"c_lin"}},
		{ID: "evt_4", Status: novel.EventConfirmed, Summary: "事件四", StoryOrder: 4, Participants: []string{"c_lin"}},
		{ID: "evt_5", Status: novel.EventConfirmed, Summary: "事件五", StoryOrder: 5, Participants: []string{"c_lin"}},
		{ID: "evt_6", Status: novel.EventConfirmed, Summary: "事件六", StoryOrder: 6, Participants: []string{"c_lin"}},
		{ID: "evt_7", Status: novel.EventConfirmed, Summary: "事件七", StoryOrder: 7, Participants: []string{"c_lin"}},
		{ID: "evt_8", Status: novel.EventProposed, Summary: "提案事件", StoryOrder: 8, Participants: []string{"c_lin"}},
		{ID: "evt_9", Status: novel.EventConfirmed, Summary: "待复核事件", StoryOrder: 9, Participants: []string{"c_lin"}, MemoryMeta: novel.MemoryMeta{NeedsReview: true}},
		{ID: "evt_10", Status: novel.EventInvalidated, Summary: "失效事件", StoryOrder: 10, Participants: []string{"c_lin"}},
	}
	writeMemoryRetrieveFile(t, dir, "events.json", events)

	result, err := st.RetrieveContinuationMemory(retrievalKnowledge(), retrievalRecent, 2, 3000)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	got := recordTexts(result.Records)
	if len(got) != 6 {
		t.Fatalf("event count = %d, want 6: %v", len(got), got)
	}
	if !strings.Contains(got[0], "第7章：事件七") {
		t.Errorf("events not sorted by storyOrder desc: %v", got)
	}
	for _, forbidden := range []string{"提案事件", "待复核事件", "失效事件", "事件一"} {
		if hasRecordText(result.Records, forbidden) {
			t.Errorf("event %q should not be injected: %v", forbidden, got)
		}
	}
	if result.SkippedReview != 1 {
		t.Errorf("SkippedReview = %d, want 1", result.SkippedReview)
	}
}

func TestRetrieveAmbiguousAliasSkipsBothSeeds(t *testing.T) {
	st, dir := newTestStore(t)
	writeMemoryRetrieveFile(t, dir, "states.json", []novel.StoryState{
		{ID: "state_1", Status: novel.StateActive, SubjectID: "c_a", Category: "状态", Attribute: "伤势", Value: "不应注入"},
	})
	knowledge := novel.Knowledge{
		Characters: []novel.Character{
			{ID: "c_a", Name: "林渊", Aliases: []string{"阿渊"}, Confidence: 0.2},
			{ID: "c_b", Name: "苏黎", Aliases: []string{"阿渊"}, Confidence: 0.2},
		},
	}
	result, err := st.RetrieveContinuationMemory(knowledge, "阿渊推门进来。", 1, 3000)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if result.SkippedAmbiguous != 1 {
		t.Errorf("SkippedAmbiguous = %d, want 1", result.SkippedAmbiguous)
	}
	if len(result.Seeds) != 0 || len(result.Records) != 0 {
		t.Errorf("ambiguous alias must not seed: seeds=%v records=%v", result.Seeds, result.Records)
	}
}

func TestRetrieveThreadsCognitionsAndPlans(t *testing.T) {
	st, dir := newTestStore(t)
	writeMemoryRetrieveFile(t, dir, "threads.json", []novel.StoryThread{
		{ID: "thread_1", Status: novel.ThreadOpen, Title: "铜牌来历", KnownClues: []string{"旧朝纹样"}, ResolutionCriteria: []string{"找到铸牌人"}, InvolvedCharacters: []string{"c_lin"}},
		{ID: "thread_2", Status: novel.ThreadResolved, Title: "已解决线索", ResolutionEvent: "evt_1"},
		{ID: "thread_3", Status: novel.ThreadDeveloping, Title: "雨夜伏击", KnownClues: []string{"黑衣人"}, InvolvedCharacters: []string{"c_lin"}, MemoryMeta: novel.MemoryMeta{NeedsReview: true}},
		{ID: "thread_4", Status: novel.ThreadAbandoned, Title: "废弃线索", AbandonReason: "方向放弃", InvolvedCharacters: []string{"c_lin"}},
	})
	writeMemoryRetrieveFile(t, dir, "cognitions.json", []novel.Cognition{
		{ID: "cog_1", Status: novel.CogWrong, SubjectID: "c_su", Content: "认为林渊已死"},
		{ID: "cog_2", Status: novel.CogForgotten, SubjectID: "c_lin", Content: "不应注入"},
	})
	writeMemoryRetrieveFile(t, dir, "plans.json", []novel.StoryPlan{
		{ID: "plan_1", Status: novel.PlanActive, Level: "near_term", Title: "渡口重逢", Objective: "让两人当面确认身份"},
		{ID: "plan_2", Status: novel.PlanActive, Level: "volume", Title: "南下卷", Objective: "抵达旧都"},
	})

	knowledge := retrievalKnowledge()
	knowledge.Characters[1].Confidence = 0.9
	result, err := st.RetrieveContinuationMemory(knowledge, "苏黎在渡口等了一夜。铜牌来历仍未解开。", 2, 3000)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if !hasRecordText(result.Records, "铜牌来历｜已知线索：旧朝纹样；解决条件：找到铸牌人") {
		t.Errorf("thread not rendered: %v", recordTexts(result.Records))
	}
	if !hasRecordText(result.Records, "苏黎｜认为林渊已死（该认知为错误认知，人物尚未纠正）") {
		t.Errorf("wrong cognition suffix missing: %v", recordTexts(result.Records))
	}
	if hasRecordText(result.Records, "不应注入") || hasRecordText(result.Records, "已解决线索") || hasRecordText(result.Records, "废弃线索") {
		t.Errorf("forbidden records injected: %v", recordTexts(result.Records))
	}
	if !hasRecordText(result.Records, "渡口重逢：让两人当面确认身份") || !hasRecordText(result.Records, "南下卷：抵达旧都") {
		t.Errorf("plans not injected: %v", recordTexts(result.Records))
	}
	if result.SkippedReview != 1 {
		t.Errorf("SkippedReview = %d, want 1", result.SkippedReview)
	}
}

func TestRetrieveBudgetTrimAndDeterminism(t *testing.T) {
	st, dir := newTestStore(t)
	states := make([]novel.StoryState, 0, 50)
	for i := 0; i < 50; i++ {
		states = append(states, novel.StoryState{
			ID: novel.NewID("state"), Status: novel.StateActive, SubjectID: "c_lin",
			Category: "状态", Attribute: "属性", Value: strings.Repeat("很长的状态值", 20),
		})
	}
	writeMemoryRetrieveFile(t, dir, "states.json", states)

	knowledge := retrievalKnowledge()
	first, err := st.RetrieveContinuationMemory(knowledge, retrievalRecent, 2, 3000)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if first.UsedChars > 3000 {
		t.Errorf("UsedChars = %d, want <= 3000", first.UsedChars)
	}
	if first.DroppedByBudget == 0 {
		t.Error("DroppedByBudget = 0, want > 0")
	}
	second, err := st.RetrieveContinuationMemory(knowledge, retrievalRecent, 2, 3000)
	if err != nil {
		t.Fatalf("retrieve again: %v", err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Error("retrieval is not deterministic")
	}
	if !reflect.DeepEqual(first.Records, second.Records) {
		t.Error("records differ between identical calls")
	}
}

func TestRetrieveDegradations(t *testing.T) {
	t.Run("memory_absent", func(t *testing.T) {
		st, dir := newTestStore(t)
		writeMemoryRetrieveFile(t, dir, "meta.json", memoryMetaFile{SchemaVersion: 1, MemoryRevision: 7})
		result, err := st.RetrieveContinuationMemory(retrievalKnowledge(), retrievalRecent, 2, 3000)
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if result.DisabledReason != novel.MemoryDisabledAbsent || len(result.Records) != 0 {
			t.Errorf("result = %+v, want memory_absent", result)
		}
		if result.MemoryRevision != 7 {
			t.Errorf("MemoryRevision = %d, want 7", result.MemoryRevision)
		}
	})

	t.Run("read_error_does_not_block", func(t *testing.T) {
		st, dir := newTestStore(t)
		writeMemoryRetrieveFile(t, dir, "states.json", []novel.StoryState{{ID: "state_1", Status: novel.StateActive, SubjectID: "c_lin", Category: "状态", Attribute: "伤势", Value: "有效"}})
		memoryDir := filepath.Join(dir, memoryDirName)
		if err := os.WriteFile(filepath.Join(memoryDir, "threads.json"), []byte("{not json"), 0o644); err != nil {
			t.Fatalf("write corrupt file: %v", err)
		}
		result, err := st.RetrieveContinuationMemory(retrievalKnowledge(), retrievalRecent, 2, 3000)
		if err == nil {
			t.Fatal("expected read error for logging")
		}
		if result.DisabledReason != novel.MemoryDisabledReadError || len(result.Records) != 0 {
			t.Errorf("result = %+v, want read_error with zero injection", result)
		}
	})

	t.Run("budget_zero_skips_everything", func(t *testing.T) {
		st, dir := newTestStore(t)
		memoryDir := filepath.Join(dir, memoryDirName)
		if err := os.MkdirAll(memoryDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(memoryDir, "states.json"), []byte("{not json"), 0o644); err != nil {
			t.Fatalf("write corrupt file: %v", err)
		}
		result, err := st.RetrieveContinuationMemory(retrievalKnowledge(), retrievalRecent, 2, 0)
		if err != nil {
			t.Fatalf("budget zero must not read files: %v", err)
		}
		if result.BudgetChars != 0 || len(result.Records) != 0 || result.DisabledReason != "" {
			t.Errorf("result = %+v", result)
		}
	})
}
