package agents

import "novelagent/internal/agentruntime"

// Descriptor 是 Agent 中心的静态插件清单（与运行时注册保持一致）。
type Descriptor struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Triggers []string `json:"triggers"`
}

// Descriptors 返回内置 Agent 的静态描述，供配置接口与前端渲染。
func Descriptors() []Descriptor {
	return []Descriptor{
		{Key: "continuity", Name: (&ContinuityAgent{}).Name(), Triggers: (&ContinuityAgent{}).Triggers()},
		{Key: "fuse", Name: (&FuseAgent{}).Name(), Triggers: (&FuseAgent{}).Triggers()},
		{Key: "memory", Name: (&MemoryAgent{}).Name(), Triggers: (&MemoryAgent{}).Triggers()},
		{Key: "plot", Name: (&PlotAgent{}).Name(), Triggers: (&PlotAgent{}).Triggers()},
		{Key: "style", Name: (&StyleAgent{}).Name(), Triggers: (&StyleAgent{}).Triggers()},
		{Key: "relation", Name: (&RelationAgent{}).Name(), Triggers: (&RelationAgent{}).Triggers()},
		{Key: "proofread", Name: (&ProofreadAgent{}).Name(), Triggers: (&ProofreadAgent{}).Triggers()},
		{Key: "task", Name: (&TaskAgent{}).Name(), Triggers: (&TaskAgent{}).Triggers()},
	}
}

// Keys 返回内置 Agent 的 key 集合，用于配置校验。
func Keys() map[string]struct{} {
	out := make(map[string]struct{}, len(Descriptors()))
	for _, descriptor := range Descriptors() {
		out[descriptor.Key] = struct{}{}
	}
	return out
}

var _ agentruntime.Agent = (*ContinuityAgent)(nil)
var _ agentruntime.Agent = (*FuseAgent)(nil)
var _ agentruntime.Agent = (*MemoryAgent)(nil)
var _ agentruntime.Agent = (*PlotAgent)(nil)
var _ agentruntime.Agent = (*StyleAgent)(nil)
var _ agentruntime.Agent = (*RelationAgent)(nil)
var _ agentruntime.Agent = (*ProofreadAgent)(nil)
var _ agentruntime.Agent = (*TaskAgent)(nil)
