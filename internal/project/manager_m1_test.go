package project

import "testing"

func TestRuntimeRegistersMilestoneAgents(t *testing.T) {
	manager, err := NewManager(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(manager.Stop)
	info, err := manager.Create("装配检查")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rt, err := manager.Runtime(info.ID)
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	keys := map[string]bool{}
	for _, agent := range rt.Agents.Agents() {
		keys[agent.Key()] = true
	}
	for _, want := range []string{"echo", "continuity", "fuse", "memory"} {
		if !keys[want] {
			t.Errorf("runtime missing agent %q; have %v", want, keys)
		}
	}
}
