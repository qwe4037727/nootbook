package analysis

import (
	"testing"

	"novelagent/internal/novel"
)

func TestApplyChapterSkipsLockedFields(t *testing.T) {
	knowledge := novel.NewKnowledge()
	knowledge.Characters = []novel.Character{{
		ID: "c_locked01", Name: "林砚", Role: "捕快", State: "左肩受伤", LastSeen: 1,
	}}
	knowledge.World = []novel.WorldEntry{{
		ID: "w_locked01", Title: "灵气复苏", Category: "设定", Content: "三年前开始。", LastSeen: 1,
	}}
	locks := novel.FieldLockSet{
		"c_locked01": {"role": {}, "state": {}},
		"w_locked01": {"content": {}},
	}

	result := chapterResult{
		Characters: []novel.Character{{
			Name: "林砚", Role: "典史", State: "痊愈", Notes: "开始追查旧案", LastSeen: 2,
		}},
		World: []novel.WorldEntry{{
			Title: "灵气复苏", Category: "世界观", Content: "五年前开始。",
		}},
	}
	chapter := novel.Chapter{ID: "ch_merge001", Index: 2, Title: "第二章", Content: "第二章正文"}
	applyChapterWithLocks(&knowledge, chapter, result, locks)

	character := knowledge.Characters[0]
	if character.Role != "捕快" {
		t.Errorf("locked role overwritten: %q", character.Role)
	}
	if character.State != "左肩受伤" {
		t.Errorf("locked state overwritten: %q", character.State)
	}
	if character.Notes != "开始追查旧案" {
		t.Errorf("unlocked notes not updated: %q", character.Notes)
	}
	world := knowledge.World[0]
	if world.Content != "三年前开始。" {
		t.Errorf("locked content overwritten: %q", world.Content)
	}
	if world.Category != "世界观" {
		t.Errorf("unlocked category not updated: %q", world.Category)
	}
}
