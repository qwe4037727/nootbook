package novel

import "testing"

// TestMatchCharacter 覆盖 spec §6.2：唯一命中 / 多实体歧义 / 无命中 / 别名相交。
func TestMatchCharacter(t *testing.T) {
	characters := []Character{
		{ID: "c_1", Name: "林越", Aliases: []string{"小越"}},
		{ID: "c_2", Name: "沈青", Aliases: []string{"阿青", "青儿"}},
	}

	// 主名唯一命中
	index, ambiguous := MatchCharacter(characters, "沈青", nil)
	if index != 1 || ambiguous {
		t.Errorf("exact match = (%d,%v), want (1,false)", index, ambiguous)
	}
	// 别名相交命中
	index, ambiguous = MatchCharacter(characters, "青儿", nil)
	if index != 1 || ambiguous {
		t.Errorf("alias match = (%d,%v), want (1,false)", index, ambiguous)
	}
	// 新名字+别名同时命中两个实体 → 歧义（"林越"命中 c_1、"阿青"命中 c_2）
	index, ambiguous = MatchCharacter(characters, "林越", []string{"阿青"})
	if !ambiguous {
		t.Errorf("incoming name+alias hitting two entities should be ambiguous, got (%d,%v)", index, ambiguous)
	}
	// 歧义：新别名同时命中两个实体
	index, ambiguous = MatchCharacter(characters, "双重身份", []string{"小越", "阿青"})
	if !ambiguous {
		t.Errorf("multi-hit should be ambiguous, got (%d,%v)", index, ambiguous)
	}
	if index != 0 {
		t.Errorf("ambiguous first hit = %d, want 0", index)
	}
	// 无命中
	index, ambiguous = MatchCharacter(characters, "路人甲", nil)
	if index != -1 || ambiguous {
		t.Errorf("no match = (%d,%v), want (-1,false)", index, ambiguous)
	}
	// key 归一化：多空白与大小写差异不影响命中
	index, _ = MatchCharacter(characters, "  林越  ", nil)
	if index != 0 {
		t.Errorf("normalized key match = %d, want 0", index)
	}
}

// TestMatchWorldEntry 覆盖 world 侧同名逻辑。
func TestMatchWorldEntry(t *testing.T) {
	entries := []WorldEntry{
		{ID: "w_1", Title: "青云宗", Aliases: []string{"青云门"}},
		{ID: "w_2", Title: "北漠", Aliases: []string{"大漠"}},
	}
	index, ambiguous := MatchWorldEntry(entries, "青云门", nil)
	if index != 0 || ambiguous {
		t.Errorf("alias match = (%d,%v), want (0,false)", index, ambiguous)
	}
	index, ambiguous = MatchWorldEntry(entries, "沙海", []string{"大漠", "青云门"})
	if !ambiguous {
		t.Errorf("multi-hit should be ambiguous, got (%d,%v)", index, ambiguous)
	}
	index, _ = MatchWorldEntry(entries, "东海", nil)
	if index != -1 {
		t.Errorf("no match = %d, want -1", index)
	}
}
