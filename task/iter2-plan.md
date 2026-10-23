# 迭代 2 规划：深度提取 + Draft / MemoryChangeSet

> 基于 V1.0 记忆基础设施（提交日志/崩溃恢复/低成本回填/四个基础 API/导入隔离/在途任务失效，commit `0c521b5`）。
> 起步范围由 yj 指定：**从「深度提取」与「Draft / MemoryChangeSet」起步**。

## 1. 定位

V1.0 解决了"记忆怎么存、怎么安全提交、怎么从手动编辑/一次性回填进入系统"。
迭代 2 补齐**从正文到记忆的内容生产闭环**：让系统能自动从正文深度抽取结构化事实进入记忆，并以**可审核、可回滚的草稿态（ChangeSet）**提交，而非仅靠手动编辑或一次性全量回填。

## 2. 起步范围（用户指定）

1. 正文深度提取（Deep Extraction）
2. Draft / MemoryChangeSet 可审核提交

## 3. 任务拆分

### I2-1 深度提取管线
- 基于 `internal/llm` 客户端（含 #22 指数退避重试），对每章/全书做结构化抽取：角色属性、人物关系、世界设定条目、伏笔/线索。
- 复用已实现 `MatchCharacter` / `MatchWorldEntry` 的 `ambiguous` 判定，产出 `EntityCandidate`；歧义候选进入确认队列（迭代 1 已实现"匹配不自动合并"，对接 U24 候选确认机制）。
- 抽取结果写入内存**草稿区**，不直接改 Active 记忆。

### I2-2 MemoryChangeSet 模型（`novel/memory.go`）
- 定义 `ChangeSet`：操作集（`add` / `update` / `remove`）、`target` 类型与 ID、`proposer`、`reason`、`evidence` 引用、`expectedRecordVersion`（乐观校验）。
- 状态机：`proposed` → `accepted` → `applied` / `rejected`。
- 与现有 `memory_revision` 提交日志对齐：`apply` 时携带 `expectedRecordVersion`，冲突即拒（对接 U16/U33 乐观校验入口，迭代 1 已留 `Revision` 字段）。

### I2-3 草稿态（Draft）
- 记忆条目支持 `Draft` / `Proposed` / `Active`（部分类型已有 `manual_edit` 保护语义，扩展为显式状态）。
- 深度提取结果默认入 `Draft`；UI 可逐条审阅 / 采纳 / 驳回。

### I2-4 联合确认提交
- `ChangeSet` 经确认后 `apply` 到记忆（active 态），触发 `memoryRevision+1` 与对应提交日志；提供正文 + 记忆联合确认入口（U33 全语义变更）。

### I2-5 证据失效复核（部分，对应 U9 `evidence_stale`）
- 事实被覆盖 / 删除时标记关联 `evidence` 为 stale，供后续重抽。

## 4. 不在迭代 2（留待后续）

完整认知 / 伏笔业务联动、记忆 Retriever、StoryPlan 生成 / 激活 / 传播 / 回灌、历史依赖修复、全量重初始化（reinitialize 本期显式返回 `400 reinit_not_supported`，见 D02）。

## 5. 验收与风险

- 复用现有 `memory` 测试框架 + `acceptance_review` 探针；重点覆盖 I2-2 乐观校验冲突、I2-1 歧义候选不自动合并。
- **前置风险**：F-UPSTREAM-01 网关 Cloudflare 按出口 IP 封禁，深度提取需走 Clash 7897 代理或网关加白（代码侧重试已具备，#22）。
- **锁序**：`ChangeSet.apply` 沿用 `memoryMu` 域提交协议，避免新增锁序分支（spec §10/§4.1.4 唯一持序点 `store.mu → memoryMu`）。

## 6. 建议执行顺序

`I2-2`（模型 / 状态机）→ `I2-1`（抽取管线，产出 Candidate）→ `I2-3`（Draft 态）→ `I2-4`（确认提交 + 乐观校验）→ `I2-5`（证据失效，部分）。
