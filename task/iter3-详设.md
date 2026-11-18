# 迭代 3 详设：记忆检索（Retriever）接入续写上下文（V1.1 增补稿）

> 上游依据：[详设.md](../详设.md) §6（记忆检索与上下文组装）、§6.2（冲突优先级）、§9（与现有功能整合）、§12（验收清单）；[memory_model_spec.md](../docs/memory_model_spec.md) V1.2 冻结版；[iter2-plan.md](iter2-plan.md) / [iter2-详设.md](iter2-详设.md)（深度提取 + ChangeSet）；[reference-repos-review.md](reference-repos-review.md)（R1–R9 借鉴评估）。
> 本期定位：迭代 1/2 解决了「记忆怎么存、怎么审、怎么安全提交」，但**续写链路至今一行未改**——`internal/agent/prompt.go` 无任何 memory 引用，`handleContinue` 只传 `knowledge`（[server.go:479](../internal/server/server.go)）。本期把已入库的五类记忆**接进续写上下文**，让前两期的投入产生可观测的续写质量收益。
> 状态：**V1.1（2026-10-23）**。I3-1…I3-7 已落地（I3-01…13、16/17 自动化全绿，见 [iter3-acceptance.md](iter3-acceptance.md)）；I3-6/I3-7（R1/R2）于 2026-10-23 实现，I3-15 改写（R3）与 §13 实施级条款仍待模型通道。I3-14/15 仍受上游通道阻塞。本文档不修改 `详设.md` 与 spec 冻结条款，只做实施级展开与衔接说明。

---

## 1. 范围与边界

### 1.1 本期交付

| 编号 | 交付 | 对应依据 |
|---|---|---|
| I3-1 | 记忆检索层（种子实体解析 → 五类召回 → 红线过滤 → 确定性排序） | 详设 §6.1 P0/P1 行 |
| I3-2 | 统一上下文预算（`ContextChars` 从「正文窗口」升级为「总预算」，记忆段硬上限） | 详设 §6.1 预算段、§12「记忆不会挤占正文上下文」 |
| I3-3 | `agent.Build` 注入记忆段 + systemPrompt 冲突优先级增补 | 详设 §6.2、§9 整合表 |
| I3-4 | 续写链路接线（降级矩阵 + SSE meta 扩展 + 只读预览接口） | 详设 §10 |
| I3-5 | 前端「记忆注入」开关与召回来源展示 | 详设 §9 |
| I3-6 | 截断可见性（R1，借鉴 Pagekin）：`finish_reason` 解析、分析失败原因分类、续写 `done.truncated` | §13.1 |
| I3-7 | 全量预算计量（R2，借鉴 Pagekin）：`TotalChars` + 分段 `Sections` + 固定收缩顺位 + 守卫用例 | §13.2、§4.1 |

### 1.2 本期明确不做（防范围蔓延）

- StoryPlan 生成 / 激活 / 传播 / 取消（U22/U23、F20–F23）：`plans.json` 本期只有**读取侧**（`status==active` 过滤），无产出方时该段自然为空。
- 正文修改后的记忆修复分级（详设 §7）、正文 + 记忆联合确认提交（iter2-详设 §1.2 同样排除）。
- 向量/语义检索、跨章证据回查、较远历史章节按需检索（详设 §6.1 P2 末行）：本期取「关键词提及 + 实体 ID 联结」的确定性检索，不引入嵌入。
- `reinitialize`、记忆 Retriever 的缓存层与并发预取。
- Cognitive（认知）提取提示词：`cognitions.json` 无产出方，本期交付读取与渲染，实测无数据则该段省略。
- **R4 长篇召回**（BM25 + CJK n-gram）：S6 才做；本期检索仍是「关键词提及 + 实体 ID 联结」，不得提前引入索引结构。
- **R6 双时间线 / R7 章节约束卡**：属于 S4/StoryPlan 字段扩展，本期不新增 `StoryEvent`/`StoryPlan` 字段、不改计划渲染。
- **R8 去 AI 味 lint / R9 生成后一致性审稿**：S6 可选项，本期不碰。这些远期项与本期唯一的关系是：`§4.1` 的预算框架必须预留分段位，避免以后二次改动。

### 1.3 与迭代 2 的衔接（前置依赖）

- **联结键是 knowledge 实体 ID**（`c_`/`w_`）。记忆与知识库能对上，靠的是迭代 1 回填映射（`TestInitBackfillMapping` 断言 `subjectId=="c_a1"`）与迭代 2 提取侧 `MatchCharacter`（歧义不自动合并）。
- 因此 iter2 验收偏差 **D5（register 预写未走 MatchCharacter 查重）是本期命中率的直接前置风险**：重复实体会把同一角色的记忆劈成两个 `c_`，检索只命中一半。开工前 D5 必须修复，或在工作区数据上先人工核对无重复实体。
- iter2 的 B1（测试缺失）不阻塞本期；本期测试只覆盖自己的检索/预算/组装层，不复用 ChangeSet 用例。

---

## 2. 数据契约：`novel.ContinuationMemory`

依赖方向决策（冻结）：`internal/agent` 只依赖 `internal/novel`（现状 [prompt.go:9](../internal/agent/prompt.go)），**不得 import `internal/store`**；检索结果 DTO 定义在 `novel` 包，由 store 填充、agent 渲染。

```go
// novel/memory_context.go（新）
type MemorySeed struct {
    EntityID   string  `json:"entityId"`   // c_/w_
    EntityName string  `json:"entityName"`
    Score      float64 `json:"score"`      // 复用 selectCharacters/selectWorld 打分，不新造打分函数
    Reason     string  `json:"reason"`     // recent_mention|overview_mention|last_seen|adjacent
}

type MemoryRecord struct {
    ID       string `json:"id"`       // evt_/state_/thread_/cog_/plan_（仅预览与日志用，不入 prompt 文本）
    Kind     string `json:"kind"`     // state|event|thread|cognition|plan
    Subject  string `json:"subject"`  // 角色名/设定名（已由 knowledge 反查；空则省略主语）
    Text     string `json:"text"`     // 渲染就绪的一句话
    Order    int    `json:"order"`    // 事件用 StoryOrder，其余用 0
    Priority string `json:"priority"` // P0|P1
}

type ContinuationMemory struct {
    Seeds            []MemorySeed   `json:"seeds"`
    Records          []MemoryRecord `json:"records"`
    BudgetChars      int            `json:"budgetChars"`      // 本期分配给记忆段的 rune 上限
    UsedChars        int            `json:"usedChars"`
    DroppedByBudget  int            `json:"droppedByBudget"`
    SkippedReview    int            `json:"skippedReview"`    // needsReview 命中但按红线排除
    SkippedAmbiguous int            `json:"skippedAmbiguous"` // 别名歧义放弃的种子
    DisabledReason   string         `json:"disabledReason,omitempty"` // safe_state|read_error|memory_absent
}
```

规则：`Records` 已按 §3.5 确定性排序并完成 §4 预算裁剪；**agent 不再做任何过滤**，只做渲染。

---

## 3. 检索算法（I3-1，`internal/store/memory_retrieve.go` 新）

入口：`func (s *Store) RetrieveContinuationMemory(knowledge novel.Knowledge, recentText string, startIndex int) (novel.ContinuationMemory, error)`

- 锁：只取 `memoryMu`；调用方（server）**不得持 `store.mu` 调用**。全仓库唯一持序仍是 `store.mu → memoryMu`（spec §4.1.4），本期不新增持序分支。
- 锁内禁止 LLM/网络/读章节文件（沿用 iter2-详设 §4.3）。`knowledge` 与 `recentText` 由调用方在 `store.mu` 域读好后传入——与 `handleContinue` 现有的 `LoadKnowledge`/`LoadChapters` 时序天然吻合。

### 3.1 种子解析（Step 1）

1. 角色种子：遍历 `knowledge.Characters`，用 agent 现有提及判定语义（`compact` + 姓名/别名命中最近正文窗口，剔除「他/她/它/我/你/他们/她们」这类代词）打分。**打分实现抽为 `novel` 包共享函数**（`ScoreCharacterMention` / `ScoreWorldMention`），`agent.selectCharacters` 与 store 检索共用同一份，禁止两处各写一套排序直觉。
2. 设定种子：同法遍历 `knowledge.World` → `w_`。
3. 种子规模：角色 ≤ 8、设定 ≤ 6，按 `(score 降序, LastSeen 降序, ID 升序)` 截断。
4. 别名冲突（同一提及串命中 ≥2 个实体）→ **该串放弃**，计入 `SkippedAmbiguous`；宁缺毋滥（spec §6.2「歧义不自动合并」的检索侧延伸）。

### 3.2 状态召回（P0）

`states.json` 过滤：`Status==StateActive` **且** `!NeedsReview` **且** `SubjectID ∈ 种子`（`c_` 走角色种子，`w_` 走设定种子）。
渲染：`{角色}｜{attribute}：{value}`，`category=="world"` 的条目并入「世界规则」子段而非人物子段（详设 §6.1 P0 第 4 行）。
同三元组多条 active（U18 跨 category 共存）全部保留，按 `recordVersion` 降序。

### 3.3 事件召回（P1）

`events.json` 过滤：`Status==EventConfirmed` **且** `!NeedsReview` **且** `Participants ∩ 角色种子 ≠ ∅`。
排序：`StoryOrder` 降序（近因优先），同序按 `ID` 升序；上限 6 条。
`EventProposed` / `EventConflicted` / `EventInvalidated` / `needsReview=chapter_deleted` **一律不注入**（R1：proposed 不入事实集）。

### 3.4 伏笔与认知召回（P1 / P0）

- `threads.json`：`Status ∈ {open, developing}` 且（`InvolvedCharacters ∩ 角色种子 ≠ ∅` **或** title/question 命中最近正文串）；上限 5 条，按 `updatedAt` 降序。渲染带首条 `ResolutionCriteria`（若有），提示「尚未兑现的条件」。
- `cognitions.json`：`Status ∈ {known, wrong}` 且 `SubjectID ∈ 角色种子`；上限 4 条。`wrong` 渲染后缀「（该认知为错误认知，人物尚未纠正）」——这是叙事信息，不注入会让模型写出「人物已知」的情节。`forgotten` / `uncertain` 不注入。

### 3.5 计划召回（P1，本期只做读取）

`plans.json`：`Status==PlanActive`；`near_term` 取 1 条（`InvolvedCharacters` 命中优先），`volume` 取 1 条。无 active 计划 → 整段省略，不占预算。

### 3.6 确定性要求（冻结）

同一 `(knowledge, recentText, memory 文件集)` 输入，两次 `RetrieveContinuationMemory` 输出必须**逐字节相同**。所有排序末尾追加 `ID` 升序兜底；禁止依赖 map 遍历顺序、禁止用 `time.Now()` 参与排序。

---

## 4. 上下文预算（I3-2，冻结公式）

现状：`cfg.ContextChars` 整体当作正文尾窗（`manuscriptTail(chapters, contextChars)`，[prompt.go:64](../internal/agent/prompt.go)），默认 12000，实测配置 30000。

本期语义变更（**破坏性，须记入 issues 并在 UI 文案说明**）：`ContextChars` = 上下文总预算，正文窗口 = 总预算 − 记忆预算。

```
memoryBudget     = clamp(ContextChars*1/4, 800, 3000)   // 记忆段硬上限 3000 字
manuscriptWindow = ContextChars - memoryBudget           // 12000→9000，30000→27000
```

不变式（验收断言，详设 §12「记忆不会无限扩张」）：

1. `UsedChars ≤ memoryBudget`；`DroppedByBudget` 记录被裁条数。
2. 正文窗口 ≥ `3/4 × ContextChars`，即记忆最多占用 25%。
3. `ContextChars ≤ 1600` 时 `memoryBudget=0` → 退化为迭代 2 行为（零注入），保护小窗口配置。
4. 记忆段是**追加预算**还是**扣减预算**：取扣减（决策冻结，理由：详设 §6.1 明确要求统一受 `ContextChars` 约束，不允许各模块分别无上限拼接）。

> I3-7（§4.1）落地后，第 2 条从「验收断言」降级为**分配目标**，绝对下限改由 §4.1 第 4 条约束。

段内子预算与裁切顺序（超限先丢 P2/P1 低分，**P0 不被裁**；P0 仍超则整条丢弃、不截断句子中段）：

| 子段 | 归属 | 上限 | 裁切顺位 |
|---|---|---|---|
| 世界规则（`category=world` 的 active states） | P0 | 600 | 最后裁 |
| 人物当前状态 | P0 | 1000 | 倒数第二 |
| 人物认知（known/wrong） | P0 | 400 | 倒数第三 |
| 未解决伏笔 | P1 | 600 | 先裁 |
| 关键事件（因果） | P1 | 400 | 最先裁 |

### 4.1 全量预算计量（I3-7 / R2，增补，已实现 2026-10-23）

**现状缺口**：上面的预算只约束「记忆 vs 正文」，知识库（角色/设定/摘要）与计划段按「条数」取、不计 rune；`Prompt.ContextChars` 实际只等于正文尾窗长度（[prompt.go:171](../internal/agent/prompt.go)），现有用例还断言了这个错量（[prompt_test.go:164](../internal/agent/prompt_test.go)）。配置 30000 时真实 user prompt 约 32–35K（正文窗口 27000 + 知识/记忆/计划段 + 5–8K 固定开销）。

**目标契约**（增补字段，不改既有字段语义）：

```go
type Prompt struct {
    // …既有字段保持不变
    TotalChars int            // system + user 的 rune 总数
    Sections   map[string]int // overview|characters|world|memories|plans|summaries|manuscript
    OverBudget bool           // 收缩后仍超预算，已记日志并在 meta 暴露
}
```

**不变式（验收断言）**：

1. `sum(Sections) + 段落标题长度 + len(system) = TotalChars`（标题与 system 都用常量断言，不允许估算）。
2. `user` 部分 ≤ `ContextChars`；超限时必须按下方顺位收缩，**不得静默超限**。
3. 收缩只减少条目/截断字段，不改变记录顺序与效力优先级（不得为了塞进去而把 P0 排到 P1 后面）。
4. 记忆段仍受 §4 的 25% / 硬顶 3000 约束；`§4` 的「正文窗口 ≥ 3/4」在知识库参与计量后降级为**分配目标**，绝对下限改为「正文尾窗 ≥ 1/2 × ContextChars」。

**收缩顺位（冻结）**：

1. 知识库条目的非核心字段：先 `Notes`/`Appearance`，再 `Personality`/`Relationships`；仍超则按打分从低到高减少条目。
2. 摘要段：从最早章开始减少。
3. 记忆段 P1 子段：先事件、后伏笔（P0 不动，见 §4 表）。
4. 正文尾窗：收缩到不低于 `1/2 × ContextChars`。
5. 仍超 → `OverBudget=true` + 日志 + SSE meta 暴露，**不阻断续写**。

**守卫用例**：`ContextChars ∈ {1600, 4000, 12000, 30000}` 四档，各造「知识库 50 角色 + 50 设定 + 30 摘要 + 记忆塞满」的输入，断言不变式 1–4 全部成立；1600 档同时验证记忆退化为 0（§4 不变式 3）。

---

## 5. Prompt 组装与冲突优先级（I3-3）

`agent.Input` 增字段 `Memory *novel.ContinuationMemory`（nil 或 `Records` 空 → 输出与迭代 2 基线**逐字节一致**，golden 用例锁死）。

新增 user 段插在现有【相关设定】之后、【前情提要】之前：

```
【已确认剧情记忆（来自正文抽取，效力低于下方最近正文）】
世界规则：
- …
人物当前状态：
- 林渊｜伤势：左臂贯穿伤，未处理
人物认知：
- 苏黎｜认为林渊已死（该认知为错误认知，人物尚未纠正）
未解决伏笔：
- 铜牌来历｜已知线索：…；解决条件：…
关键事件（近因优先）：
- 第 41 章：林渊当众折断铜牌
近期计划（仅约束走向，不得覆盖已确认事实）：
- …
```

渲染红线（冻结）：

- **不注入内部 ID**（`state_0007`/`evt_` 等），只注入自然语言；防止模型复述 ID 污染正文。
- 不注入 `SourceRef` 原文（已有【最近正文】承载原文证据；重复 quote 会挤占预算并诱导模型复述）。
- 段落标题固定措辞，便于 diff 与人评。

`systemPrompt` 增补一条（插在现有规则 3 之后，原编号顺延）：

> 3.x 【已确认剧情记忆】用于维持长程一致性（伤势、目标、关系、未兑现伏笔）。它与【最近正文】冲突时**一律以最近正文为准，不要试图调和或解释差异**；计划只能影响走向建议，不能改写已确认事实。

---

## 6. 降级矩阵（I3-4）

| 场景 | 行为 | 续写可用性 |
|---|---|---|
| 记忆文件不存在 / 五类全空 | `DisabledReason=memory_absent`，零注入 | 与迭代 2 完全一致 |
| `memorySafe==true` | 不读五类文件，`DisabledReason=safe_state`，meta 带原因 | 可用（绝不基于可疑链注入） |
| 五类文件读取/解码失败 | 记日志 + `DisabledReason=read_error`，零注入 | **不阻断续写**（正文优先，详设 §10） |
| 记忆子系统未初始化但正文正常 | 正常续写，零注入 | 可用 |
| 种子为空（最近正文未提及任何已登记实体） | 零注入，meta `seeds:0` | 可用 |
| 别名歧义 | 放弃该串 + 计数 | 可用 |
| 预算耗尽 | 按 §4 顺位丢弃，`DroppedByBudget` 计数 | 可用 |
| 前端关闭「记忆注入」 | `useMemory=false` → 完全不检索 | 可用 |

红线：任何降级路径**不得回退到「用上一次缓存的记忆」或「用 knowledge 冒充记忆」**。

---

## 7. API（风格对齐 iter2-详设 §6：`decodeStrictJSON`、4096B、未知字段 400、尾随 JSON 400、超限 413）

| 方法 | 路由 | 变更 |
|---|---|---|
| POST | `/api/continue` | 请求体新增可选 `useMemory bool`（默认 true）；SSE `meta` 事件新增 `memoryEnabled / memoryRevision / memoryBudgetChars / memoryUsedChars / seeds / recalledByKind{states,events,threads,cognitions,plans} / droppedByBudget / skippedReview / skippedAmbiguous / memoryDisabledReason` |
| POST | `/api/continue/preview` | **新增只读预览**：体同 `/api/continue`（不含生成）。200 返回 `{memorySection, seeds, records[], budget, used, counts, manuscriptChars, memoryEnabled}`，`memorySection` 为渲染后文本。不建 LLM 连接、不落盘、`memoryRevision` 不变 |
| GET | `/api/memory` | 契约不变（不新增字段） |

- `/api/continue/preview` 是本期验收主力：不调模型即可复现「注入什么、占多少预算、被裁掉什么」，绕开上游网关封禁（F-UPSTREAM-01）。
- `preview` 请求体上限同样 4096B；空体/`{}` 合法（表示默认参数）。
- **增补（I3-6/I3-7，已实现 2026-10-23）**：SSE `done` 事件新增 `truncated bool`（上游 `finish_reason=="length"` 或本地判定输出未完整）；`meta` 事件追加 `promptTotalChars / promptOverBudget / promptSections`；`preview` 响应追加 `promptTotalChars / sections / promptOverBudget`。均为**新增字段**，不改既有字段含义，旧前端可忽略。

---

## 8. 前端（I3-5）

在现有续写面板（非迭代 2 的记忆审阅区块）内追加，原生 JS，不引框架：

1. 「记忆注入」复选框（默认勾选）+ 一句话说明「开启后从已确认剧情记忆补充人物状态/伏笔，最多占用 25% 上下文」。
2. 续写前 `POST /api/continue/preview` 渲染「本次将注入的记忆」折叠区：按子段分组、每条带 `ID` 与「待复核」角标、底部显示 `used/budget` 与被裁条数。
3. 生成中的 SSE `meta` 到达后，把召回计数并入现有状态行（现有 `characters/worldEntries/summaries` 同一位置）。
4. `memoryDisabledReason` 非空时以现有 toast/提示样式说明原因（`safe_state` 额外提示「记忆链校验异常，本次未注入」）。

---

## 9. 任务拆分与允许改动范围

### I3-1 检索层

- 范围：`internal/store/memory_retrieve.go`（新）、`internal/novel/memory_context.go`（新，DTO）、`internal/novel/mention.go`（从 `agent/prompt.go` 抽出共享打分/提及判定）、`internal/store/memory_retrieve_test.go`（新）。
- 禁止：改 `commitMemoryLocked`、`LoadMemoryRecords` 语义（可新增只读辅助）、五类文件布局。
- 自测：`go test ./internal/novel ./internal/store -run "Retrieve|Mention" -count=1`。

### I3-2 预算 + I3-3 组装

- 范围：`internal/agent/prompt.go`（改：`Input.Memory`、新增段渲染、预算扣减、systemPrompt 增补一条）、`internal/agent/memory_render_test.go`（新）。
- 红线：`Memory` 为 nil/空时输出与基线逐字节一致 → 先录 golden 文件再改代码。
- 自测：`go test ./internal/agent -count=1` + 预算不变式用例。

### I3-4 接线与接口

- 范围：`internal/server/server.go`（`handleContinue` 取 `memoryRevision`/`memorySafe`、调检索、传参、扩 meta）、`internal/server/memory_preview.go`（新，`/api/continue/preview` 路由与 `useMemory` 解析）、`internal/server/continue_test.go`（新）。
- 禁止：改 `handleContinue` 现有的正文非空/配置校验/SSE 写出顺序。
- 自测：`go test ./internal/server -count=1` + 空库冒烟（预览接口 200 且 `memoryEnabled=false`）。

### I3-5 前端

- 范围：`web/app.js`、`web/index.html`、`web/style.css`（追加）。
- 自测：起服务走通「导入→分析→初始化记忆→（迭代 2）提取并采纳→预览→续写」，记录两次对照正文。

### I3-6 / I3-7（V1.1 增补，已实现 2026-10-23）

- I3-6 范围：`internal/llm/client.go`（`Result`/`FinishReason`/`IsTruncated`）、四个调用点、`internal/analysis/manager.go`（失败文案分流）、`internal/server/server.go`（`done.truncated`）、前端状态行提示。
- I3-7 范围：`internal/agent/prompt.go`（`TotalChars/Sections/OverBudget` + 收缩 + 字段级裁剪）、`internal/agent/prompt_test.go`（修错量断言 + 四档守卫）、`internal/server`（meta 透出）。
- 红线：I3-6 不改重试语义；I3-7 不改 `ContextChars` 字段名/默认值。详见 §13。

### 全线通用约束

- 不得修改：`memory_model_spec.md` 冻结条款、`详设.md`、`internal/llm/client.go` 重试语义、四个基础记忆 API 与迭代 2 ChangeSet API 契约。
- 全程 `go build ./... && go vet ./... && go test ./... -count=1` 绿；收尾补 `go test -tags acceptance_review ./... -count=2`。
- 不自动 commit；每任务完成更新任务 MD（+ `-changes.md` / `-issues.md`）后等人工审阅——**本期严格补齐，不再出现 iter2 的记录缺失**。

---

## 10. 验收用例

| # | 用例 | 判定 |
|---|---|---|
| I3-01 | 零记忆基线一致 | 五类文件缺失 → `agent.Build` 输出与迭代 2 golden 逐字节相同；`/api/continue` meta `memoryEnabled=false` |
| I3-02 | P0 状态注入 | active 且非 needsReview 的角色状态出现在【已确认剧情记忆】，措辞含 `attribute：value` |
| I3-03 | needsReview 红线 | `needsReview=true`（含 `evidence_stale`/`chapter_deleted`/`quote_mismatch`）的记录不出现，`skippedReview` 计数正确 |
| I3-04 | proposed/失效不入事实集 | `EventProposed`、`StateSuperseded`、`ThreadResolved/Abandoned`、`EventInvalidated` 均不注入（R1/U20 语义延伸） |
| I3-05 | 事件按 StoryOrder 近因 | 6 条 confirmed 事件按 `StoryOrder` 降序，截断后保留最近的 |
| I3-06 | 歧义别名不注入 | 同一提及串命中两个 `c_` → 两个主体的记忆都不注入，`skippedAmbiguous=1` |
| I3-07 | 预算不变式 | `ContextChars=12000` → `memoryBudget=3000`、正文窗口 9000；塞 50 条记忆 → `used≤3000`、`droppedByBudget>0`、正文窗口不被再压缩 |
| I3-08 | 小窗口退化 | `ContextChars≤1600` → `memoryBudget=0`，行为同 I3-01 |
| I3-09 | 确定性 | 同输入两次调用输出逐字节相同（含 map 无序字段渲染） |
| I3-10 | safeState 降级 | 断链进安全态 → 零注入 + `disabledReason=safe_state`，续写仍 200 |
| I3-11 | 五类文件损坏 | 注入 `threads.json` 垃圾字节 → 零注入 + `read_error`，**续写不被阻断**，日志有记录 |
| I3-12 | 预览接口契约 | 尾随 JSON/未知字段/非法类型 400；>4096B 413；空体合法；预览前后 `memoryRevision` 与全部数据文件哈希不变 |
| I3-13 | 关闭开关 | `useMemory=false` → 不读五类文件（探针：临时置损坏文件后仍正常续写） |
| I3-14 | systemPrompt 优先级 | 规则含「与最近正文冲突一律以正文为准」；构造记忆与正文矛盾样本，产出正文不复述被否决的记忆条目（人评） |
| I3-15 | 质量对照（**R3 协议**） | 3 个冻结停点，每停点开/关记忆各生成 1 篇；按 `landed / summarized / missing` 三态逐条核对「应记事实」，另计未授权事实；两位评审**交换顺序**盲评（先 A 后 B、先 B 后 A 各一轮），≥2/3 停点开记忆胜出或打平且未授权事实不增加 |
| I3-16 | 截断可见性（I3-6 验收） | 上游 `finish_reason=="length"` → `done.truncated=true`；分析阶段截断 → 失败文案为「输出被截断」而非「格式异常」；正常结束 `truncated=false` |
| I3-17 | 全量预算守卫（I3-7 验收） | `ctx ∈ {1600,4000,12000,30000}` 四档：`sum(Sections)+标题+system = TotalChars`、user ≤ `ContextChars`、收缩后 P0 仍先于 P1、`OverBudget` 仅在收缩到底后置真 |

I3-15 是本期的**收益判定**，但不再用「一次开/关人评」这种不可复现的口径：

- **为什么改**：单次人评受顺序、疲劳、自恋偏差影响，且只看「写得好不好」会漏掉「该记住的没记住」。参考 oh-story 的 judge/metrics 做法，改为「覆盖判据 + 交换顺序盲评」。
- **三态覆盖**：每个冻结停点先人工列出「本次续写应当使用的事实」，逐条标注生成结果的表现——`landed`（正确使用）、`summarized`（提及但压缩失真）、`missing`（完全没用）。
- **未授权事实**：统计生成正文里出现、但记忆与正文都没有依据的新设定（幻觉增量）。
- **判定**：若对照无可见收益，检索层不进下一期扩展，转为排查提取质量（回 S1）；若开记忆胜出但未授权事实增加，同样判负。
- 证据路径：`docs/acceptance-evidence/iter3/`（停点原文、两份生成、覆盖表、盲评表）。

---

## 11. 风险

1. **垃圾进垃圾出**：迭代 2 的深度提取尚未验收（B1 测试未落地），若抽取事实不准，注入记忆会**放大**错误而不是修正它。缓解：只注入 confirmed/active 且非 needsReview；I3-15 对照留证；预览接口先于生成暴露内容。
2. **`ContextChars` 语义破坏性变更**：同一配置下正文窗口变小（12000→9000），可能改变已有续写观感。缓解：§4 的 25% 上限 + I3-07/I3-08 断言 + UI 文案说明 + 记入 issues 台账。
3. **D5 未修导致实体劈叉**（§1.3）：开工前核对工作区无重复实体，或先修 D5。
4. **打分函数抽取引入回归**：`selectCharacters/selectWorld` 迁到 `novel` 包共享，行为必须等价。缓解：抽取前先写现有行为的 golden 用例。
5. 上游网关（F-UPSTREAM-01）：本期运行路径不调 LLM 提取，只影响 I3-14/15 人评采样，需走 Clash 7897 或加白。
6. **全量计量会暴露既有超预算**（I3-7）：配置 30000 时真实 user prompt 约 32–35K，落地后可能必须调低 `ContextChars` 或减少知识库条目。这是既有问题被暴露，不是 I3-7 引入；须在 issues 台账记录一次基线。
7. **截断分类依赖上游字段**（I3-6）：部分网关不返回 `finish_reason`。此时只能判空、不做推断；禁止用「JSON 解析失败」反推截断作为唯一依据。

---

## 12. 待确认事项（不擅自决定）

| # | 事项 | 初步建议 |
|---|---|---|
| Q1 | `knowledge.World` 与 `category=world` 的 active states 同时命中同一设定 → 去重还是并集注入 | 本期：记忆优先，同 `w_` 主体的 knowledge 设定条目从渲染中剔除；实测有歧义再调 |
| Q2 | `memoryBudget` 比例（1/4、上限 3000）是否入 `config.json` | 本期固定派生值，不加配置字段（避免同时改 config 契约与前端表单） |
| Q3 | `cognitions` 无产出方，是否本期补提取提示词 | 不补（守 §1.2 边界），先靠 `wrong` 读取路径的空结果确认无数据 |
| Q4 | 章节标题/序号在事件行中的呈现（`第 41 章：`）是否用 `chapterOrder` 而非 `Index` | 用 `StoryOrder` 反查的章节标题；被删章节的事件已被 needsReview 红线排除 |
| Q5 | I3-7 后是否把 `ContextChars` 改名为「prompt 总预算」并迁移配置 | 不改字段名（避免迁移与前端表单改动），只改文档/UI 文案，新增 `OverBudget` 观测 |
| Q6 | R3 的 3 个冻结停点从哪本书选 | 用 S1 的《无边悔恨》前 30 章内选「伏笔未回收 / 人物伤势 / 关系变化」各 1 个，S1 验收时冻结；只存摘录与哈希，不进大文件 |

---

## 13. 实施级增补（I3-6 / I3-7 / I3-15，V1.1；I3-6/I3-7 已实现，I3-15 未实现）

> 来源：[reference-repos-review.md](reference-repos-review.md) R1/R2/R3。三项都是**现有实现的缺口修复**，不是新功能：截断被误报成格式错误、预算只量正文、评测口径不可复现。优先级：I3-6 先于 S1 执行（否则 S1 的失败无法归因）；I3-7 随 S2；I3-15 在模型通道恢复后执行。

### 13.1 I3-6 截断可见性（R1）

- **改动面**：
  - `internal/llm/client.go`：新增 `type Result struct { Content string; FinishReason string }`；`StreamChat` 改为 `(Result, error)`（流式取最后一个非空 `finish_reason`，非流式回退路径取 `completionResponse.choices[0].finish_reason`），`CompleteChat` 改为 `(Result, error)`；导出 `IsTruncated(reason string) bool`（`length|max_tokens`）。**重试与超时语义不变。**
  - 调用点同步改：`internal/server/server.go:535`、`internal/analysis/manager.go:236`、`internal/plan/generate.go:98`、`cmd/probe/main.go:44`；`client_test.go` 既有用例改为解构 `Result`。
  - `internal/analysis/manager.go`：`parseJSONObject` 失败时，若 `FinishReason` 为截断 → 文案「模型输出被截断，请缩短章节或调大输出上限」；否则保持「格式异常」。**截断不自动重试**（避免放大成本），由用户决定。
  - `internal/server/server.go`：SSE `done` 事件带 `truncated`；前端状态行提示「输出可能未完成」。
- **不做**：不改重试策略、不自动续写补全、不改分析提示词（S1 的提示词迭代另算）。
- **验收**：I3-16。

### 13.2 I3-7 全量预算计量（R2）

- **改动面**：
  - `internal/agent/prompt.go`：新增 `TotalChars/Sections/OverBudget`；`Build` 末尾统一计量并按 §4.1 顺位收缩；`formatCharacter/formatWorld` 增加字段级裁剪（先删 `Notes`/`Appearance` 等低价值字段，再按句读截断，不截半句）。
  - `internal/agent/prompt_test.go`：修正现有错量断言（[prompt_test.go:164](../internal/agent/prompt_test.go)），新增 `ctx ∈ {1600,4000,12000,30000}` 守卫用例。
  - `internal/server`：`meta` 增加 `promptTotalChars/promptOverBudget/promptSections`。
- **不做**：不改 `ContextChars` 字段名与默认值；不引入 token 计费（rune 计量足够，token 换算随模型而变）。
- **验收**：I3-17。

### 13.3 I3-15 评测协议（R3）

- **资产**（只存清单与哈希，大文件不进库）：
  - `docs/acceptance-evidence/iter3/cases/`：停点原文摘录 + 「应记事实」清单。
  - `docs/acceptance-evidence/iter3/results/`：开/关生成正文、覆盖表、盲评表。
- **流程**：
  1. S1 冻结 3 个停点（伏笔未回收 / 人物伤势 / 关系变化各 1），记录章节 ID、最近正文窗口与 direction。
  2. 每停点同 direction、同 temperature，开/关记忆各生成 1 篇。
  3. 覆盖表：每条应记事实标 `landed / summarized / missing`；未授权事实单列计数。
  4. 盲评：开/关结果随机标 A/B，两位评审各看两种顺序（A→B、B→A），记录偏好与理由。
- **判定**：见 §10 I3-15。任一停点出现「开记忆导致未授权事实增加」即判负，回 S1 查提取质量，而不是继续调检索。
