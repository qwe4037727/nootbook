# 迭代 2 详设：深度提取 + Draft / MemoryChangeSet（V1.0 冻结稿）

> 上游依据：[详设.md](../详设.md)（总详设）、[memory_model_spec.md](../docs/memory_model_spec.md) V1.2 冻结版、[iter2-plan.md](iter2-plan.md)（起步范围由 yj 指定）。
> 执行方：workbuddy。本文档是实施级详设——数据模型、状态机、API、异常与验收用例均为冻结结论，实施中发现的规格空白按「先记录、文末待确认、不擅自扩范围」处理。
> 状态：待评审（yj 确认后开工）。

---

## 1. 范围与边界

### 1.1 本期交付（对应 iter2-plan I2-1 ~ I2-5）

| 编号 | 交付 | 对应 U 用例 |
|---|---|---|
| I2-2 | MemoryChangeSet 模型 + 状态机 + 存储 | U16、U33（写入口） |
| I2-1 | 正文深度提取管线（异步任务） | U19、U24（产出方） |
| I2-3 | 记忆条目草稿态（ChangeSet 内 staged 记录） | U20 语义延伸 |
| I2-4 | ChangeSet 审核与正式提交（accept/reject/rebase） | U16、U18、U29、U31、U33 |
| I2-5 | 证据失效复核（evidence_stale / quote_mismatch 完整路径） | U9、U10（完整版） |
| I2-6 | 实体候选确认入口（register / merge / dismiss） | U24（确认侧） |
| I2-7 | 最小前端审阅面板 | — |

### 1.2 本期明确不做（防范围蔓延）

- 续写章节草稿（详设 §4 的 `Draft{content, target_chapter_id}` 正文草稿 + 正文与记忆联合确认提交）：`draft_` 命名已在 spec §2 冻结，本期仅交付**记忆侧** ChangeSet；正文草稿复用本机制，后续迭代接入。
- 记忆 Retriever 与续写上下文组装（详设 §6，`storyBudgetChars`）：iter2-plan §4 已排除，`agent/prompt.go` 零改动。
- StoryPlan 生成 / 激活 / 传播（U22/U23 业务入口）、认知（Cognition）提取提示词、历史依赖修复、全量重初始化。
- ChangeSet 操作类型的模型层定义覆盖五类记录（保证机制完整），但提取管线本期只产出 events / states / threads / 实体候选；cognitions、plans 的操作类型随机制交付、无产出方。

### 1.3 与 spec V1.2 的衔接说明（不修改冻结表，仅澄清解释）

1. spec §8.3「深度提取批次写入成功 +1/批次」与「变更提案 → 否」的调和：本期提取结果**先入 ChangeSet 草稿区（不递增）**，用户确认后 accept 提交正式记忆（type=formal，+1/ChangeSet，即「批次」）。不存在绕过审核的提取直写路径。
2. spec §9.2 状态机中的 `deep_extracting`：本期提取是**用户按章节范围触发的独立任务**，不复用 init_state.json，单独落 `extract_state.json`（§3.1）；init_state 语义不变。
3. spec §6.3「候选确认可作为 ChangeSet 操作类型；本期仅文件级确认」：本期取**文件级确认**路径（独立 endpoint + type=entity_confirm 提交），ChangeSet op 枚举不含实体操作。

---

## 2. 核心数据模型

### 2.1 ChangeSet（变更集，`mcs_`）

存储：`data/memory/changesets.json`（`[]ChangeSet`，整体覆盖写——与冻结决策 11 一致）。**草稿区数据，进出均不产生 memoryRevision 变化。**

```go
type ChangeSetStatus string

const (
    ChangeSetProposed ChangeSetStatus = "proposed"  // 已暂存，待审核
    ChangeSetApplied  ChangeSetStatus = "applied"   // 已正式提交
    ChangeSetRejected ChangeSetStatus = "rejected"  // 已驳回（保留记录供审计）
)

type ChangeSet struct {
    ID               string          `json:"id"`               // mcs_
    WorkID           string          `json:"workId"`           // 恒 work_001
    Status           ChangeSetStatus `json:"status"`
    Proposer         string          `json:"proposer"`         // 本期恒 "deep_extract"；预留 manual 等
    Reason           string          `json:"reason"`           // 人类可读批次说明（如 "chapter ch_xx 深度提取"）
    BasedOnRevision  int64           `json:"basedOnMemoryRevision"` // 暂存时的 memoryRevision 快照
    Ops              []ChangeOp      `json:"ops"`
    Conflicts        []ConflictItem  `json:"conflicts,omitempty"`
    CommitID         string          `json:"commitId,omitempty"`    // apply 成功后回填
    CreatedAt        time.Time       `json:"createdAt"`
    AppliedAt        time.Time       `json:"appliedAt,omitempty"`
    RejectedAt       time.Time       `json:"rejectedAt,omitempty"`
    RejectReason     string          `json:"rejectReason,omitempty"`
}
```

### 2.2 ChangeOp（单条变更操作）

```go
type ChangeOpType string

const (
    OpAddEvent        ChangeOpType = "add_event"
    OpUpdateEvent     ChangeOpType = "update_event"
    OpAddState        ChangeOpType = "add_state"
    OpUpdateState     ChangeOpType = "update_state"    // 语义 = 旧 active 置 superseded + 新记录 active
    OpAddCognition    ChangeOpType = "add_cognition"
    OpUpdateCognition ChangeOpType = "update_cognition"
    OpAddThread       ChangeOpType = "add_thread"
    OpUpdateThread    ChangeOpType = "update_thread"
    OpAddPlan         ChangeOpType = "add_plan"
    OpUpdatePlan      ChangeOpType = "update_plan"
    // 无 remove_*：正式记录不物理删除；"失效"用 update_* 把 Status 置 invalidated
)

type ChangeOp struct {
    Op                    ChangeOpType    `json:"op"`
    TargetID              string          `json:"targetId,omitempty"`              // update_* 必填，前缀须与 op 类型匹配
    ExpectedRecordVersion int64           `json:"expectedRecordVersion,omitempty"` // update_* 必填，乐观校验
    Record                json.RawMessage `json:"record"`                          // add_*：完整新记录；update_*：变更后完整记录
    EvidenceNote          string          `json:"evidenceNote,omitempty"`          // 提取侧说明（不入正式记录）
}
```

规则（冻结）：

1. `Record` 内嵌完整的五类模型 JSON（含 MemoryMeta 骨架），持久化前必须过该类型 L-N + L-S（复用 `novel.ValidateStoryEvent` 等现有校验器）。
2. add_* 的 Record.ID 为空 → 提交时由系统 `NewID(prefix)` 分配；`recordVersion` 恒 1。
3. update_* 的 `TargetID` 前缀与 op 对应：`update_event`→`evt_`、`update_state`→`state_`、`update_cognition`→`cog_`、`update_thread`→`thread_`、`update_plan`→`plan_`，不符 → L-S 拒绝整个 ChangeSet（spec §4.7 持久化边界 2/3）。
4. **update_state 语义特例**：三元组（subjectId, category, attribute）命中既有 active 记录时，提取不直接改旧记录，而是提交「旧记录 Status=superseded（recordVersion+1、lastSource=agent_extraction）+ 新记录 active（recordVersion=1）」两个落盘动作，合并为一个 op 表达。同三元组同值 → 不产生 op（U29 no-op 语义的写入口）。
5. 提取产出的 StoryState 命中三元组且既有记录 `lastSource=manual_edit` → 不产生 op，转 ConflictItem（spec §3.4 人工修订保护）。

### 2.3 ConflictItem（冲突提案项）

```go
type ConflictItem struct {
    Kind                  string          `json:"kind"`     // manual_edit_conflict | evidence_unlocated | resolved_without_proof | ambiguous_participant
    TargetID              string          `json:"targetId,omitempty"`
    ExpectedRecordVersion int64           `json:"expectedRecordVersion,omitempty"`
    Proposed              json.RawMessage `json:"proposed"` // 提议的新值（完整记录）
    Old                   json.RawMessage `json:"old,omitempty"` // 冲突时的现值快照
    Detail                string          `json:"detail"`   // 人类可读说明
}
```

- 冲突项**永不自动提交**。accept 默认只提交非冲突 ops；冲突项由用户人工处理（本期：前端展示，用户在记忆记录上手动修正或忽略；不提供 per-conflict resolve 接口）。
- ChangeSet applied 后 Conflicts 仍保留在记录中供审计。

### 2.4 状态语义对照（草稿区 vs 正式区）

| 层 | 载体 | 状态 | 是否入正式事实集 |
|---|---|---|---|
| 暂存 | changesets.json 内 Ops[].Record | 记录骨架 status=proposed / needsReview=true | 否（不在五类正式文件中，检索不可见） |
| 提交后 | events.json 等五类文件 | 有定位证据且过 L-S/L-V → confirmed/active；否则 proposed + needsReview | confirmed 入事实集；proposed 不入（R1 语义） |

「有正文证据 → confirmed」的判定发生在 **accept 提交时的服务端校验**（spec §9.1 第二级：有正文证据且过校验 → confirmed，否则 proposed），不由模型自报。

---

## 3. 深度提取管线（I2-1）

### 3.1 任务模型与状态机

存储：`data/memory/extract_state.json`：

```go
type ExtractState struct {
    Status     string       `json:"status"` // idle | extracting | done | failed | partial
    RunID      int64        `json:"runId"`
    StartedAt  time.Time    `json:"startedAt,omitempty"`
    FinishedAt time.Time    `json:"finishedAt,omitempty"`
    LastError  string       `json:"lastError,omitempty"`
    Scope      []string     `json:"scope"` // 本次章节 ID 列表（ch_）
    Stats      ExtractStats `json:"stats"`
}

type ExtractStats struct {
    ChaptersDone      int `json:"chaptersDone"`
    ChangeSetsCreated int `json:"changeSetsCreated"`
    CandidatesCreated int `json:"candidatesCreated"`
    OpsTotal          int `json:"opsTotal"`
    SkippedUpToDate   int `json:"skippedUpToDate"`
    FailedChapters    int `json:"failedChapters"`
}
```

状态机：`idle / done / failed / partial --POST extract--> extracting --> done（全部章节处理完）| partial（部分章节失败）| failed（启动即败/配置缺失）`。

**两级凭据复用**（spec §9.2 R12 模式推广，冻结）：

- 记忆代次 `memoryGeneration`：导入新书递增，旧书快照作废——提取任务与初始化任务共用同一代次计数。
- 运行号 `extractCurrentRun`（进程内 + extract_state.runId）：每次授权新任务递增。
- 校验点三处：章节快照读取后、ChangeSet 暂存前、终态回写前。失效任务不写 changesets、不写候选、不回写 extract_state。
- 重启恢复：`New()` 内检测孤儿 `extracting` → 置 failed + lastError=interrupted_by_restart（与 init 孤儿处理同构）。ChangeSet 是**整章原子暂存**（一章一个 ChangeSet 一次写盘），不存在半章暂存，无需逐章回滚。
- 导入重置 `resetMemoryLocked` 同步清空 changesets.json、extract_state.json、entity_candidates.json（候选已是重置范围，本期补齐前两者）。

### 3.2 触发范围与去重

请求：`POST /api/memory/extract`，体 `{"chapterIds": ["ch_..."], "force": false}`（均可选；空 chapterIds = 全部正式章节）。

- 逐章幂等键 = `(chapterId, chapterRevision)`：已存在**同键**的 pending ChangeSet 或已 applied ChangeSet（溯源见 §3.5）→ 跳过并计 skippedUpToDate。章节内容变化导致 Revision 递增后键自然失效，无需主动失效机制。
- `force=true`：忽略 pending 去重（已 applied 同键仍跳过——同内容重复提取无意义），为某章强制再提一版 ChangeSet。
- `force=true`：忽略 pending 去重（已 applied 同键仍跳过——同内容重复提取无意义），为某章强制再提一版 ChangeSet。

### 3.3 单章提取流程

```
对每章（顺序执行，与 analysis 同款 8 分钟超时 + #22 重试）：
  1. 组装输入：
     - 本章正文（截断 30000 rune，与 analysis 一致）
     - 知识摘要裁剪（复用 buildAnalystMessages 的知识摘要逻辑，≤6000 字）
     - 相关正式记忆快照：本章提及/命中角色的 active states、open/developing threads、
       本章已有 confirmed events 摘要（拼接上限 4000 字，超出按 recordVersion 降序裁）
  2. LLM 调用（temperature=0.2），要求只输出 JSON（schema 见 §3.4）
  3. 解析（复用 parseJSONObject 容错）→ L-N Normalize
  4. 逐条 L-S 结构校验；失败条目丢弃并计入 stats（提取侧允许部分成功，与回填一致）
  5. 实体解析：事件 participants / state subjectId 走 MatchCharacter / MatchWorldEntry
     - 唯一命中 → 解析为 c_/w_ ID
     - ambiguous → 写 EntityCandidate（status=pending，reason=ambiguous_alias）；
       对应记录剔除该参与者 + needsReview=true + reviewReason=ambiguous_participant（U24）
     - 未命中 → 写 EntityCandidate（reason=unknown_entity，possibleIds 空）；
       事件参与者按未识别处理：participantsKnown=false
     - 模型明确声明无参与者 → participantsKnown=true + 空列表（U19）
  6. 证据定位（服务端，不信模型给的偏移）：
     - 模型只提供 quote；服务端在正文中定位 quote → 计算 rune 偏移
       （复用 SliceByRune；多处命中取第一处并记 evidenceNote）
     - 定位成功 → SourceRef{chapterId, chapterRevision=当前章节Revision, start, end, quote}
     - 定位失败 → 该条不留 sourceRefs，记录降级 proposed + needsReview + reviewReason=evidence_unlocated
  7. 组装 ChangeSet（proposed，BasedOnRevision=当前 memoryRevision 快照）暂存：
     - states：三元组查重 → add_state / update_state / 同值跳过 / manual_edit→ConflictItem
     - threads：按 title 匹配既有 thread（唯一命中→update_thread 提案；未命中→add_thread）；
       resolved 提案必须带已定位的解决证据，否则降级 developing + ConflictItem(resolved_without_proof)
     - events：一律 add_event（事件不可变；同一事件重复提取的去重见 §3.5）
  8. 整章一个 ChangeSet 原子写 changesets.json（memoryMu 域，非提交协议——草稿区单文件原子写即可）
```

### 3.4 模型输出 schema（提示词约束的机械落实）

```json
{
  "events": [{"summary": "...", "eventType": "character_action|...", "participants": ["名字"], "participantNamesProvided": true, "quote": "原文片段", "preconditions": [], "consequences": []}],
  "states": [{"subject": "名字", "category": "physical|location|possession|relation|goal|world", "attribute": "...", "value": "...", "quote": "..."}],
  "threads": [{"title": "...", "action": "new|update|resolve", "clue": "...", "quote": "..."}],
  "entityHints": [{"name": "...", "aliases": ["..."]}]
}
```

提示词规则（写入 prompt.go，与详设 §11 对齐）：

- 只提取本章正文**明确发生**的事实；推测、隐含信息不得作为事件/状态提交。
- quote 必须是正文原文连续片段，不得改写；无法给出原文的条目不要提交。
- 只允许输出上述 schema；status/confirmed 等字段由系统决定，模型不得输出。
- 伏笔 resolve 必须同时给出解决性原文证据。
- 不知道/不确定 → 不提交该条，而非猜测填值。

### 3.5 事件去重（防重复确认产生重复事件）

- 同章重提取被 §3.2 幂等键挡住，常态不会产生重复。
- 跨章/force 场景的兜底：暂存前按 `(chapterId, 归一化 summary)` 对既有 pending ChangeSet 与正式 events.json 做查重，命中 → 跳过该 op 并记 evidenceNote=dedup_suspected。此为启发式兜底，不追求语义级判重（语义判重属 L-V/后续 reconcile）。

---

## 4. ChangeSet 审核与提交（I2-3 / I2-4）

### 4.1 审核操作

| 操作 | 语义 | memoryRevision |
|---|---|---|
| 查询 / 列表 | 只读 | 不变 |
| accept | 校验 + 正式提交（下节） | +1（有实际落盘 ops 时） |
| reject | Status→rejected + rejectReason，零记忆写入 | 不变 |
| rebase | 基于当前记忆重算校验与 expectedRecordVersion（§4.4） | 不变 |

- accept 一个**零有效 ops**（全部冲突/被去重）的 ChangeSet：直接置 applied，不产生 CommitRecord、不递增（空提交无意义）。
- applied / rejected 为终态，不可逆；重复 accept 同 ID → 幂等返回原结果（commitId 已回填），不重复提交（T07 语义）。

### 4.2 accept 提交流程（锁内，复用既有提交协议）

入口 `ApplyChangeSet(id)`：

1. `store.mu` 前置：读 ChangeSet，校验 status=proposed；校验分析任务未在运行（spec §10.4：分析进行中 409 `analysis_running`）。
2. 进入 `memoryMu`（全仓库唯一持序 store.mu → memoryMu 不变）。
3. **基线校验**：`changeSet.BasedOnRevision == meta.memoryRevision`，不符 → `revision_conflict`（提示先 rebase），零落盘。
4. **逐 op 重校验**（不信任暂存时点结果）：
   - Record 过 L-N + L-S；任一失败 → **整个 accept 失败，零写入**（spec §4.7-3，U16）。
   - update_*：`expectedRecordVersion == 目标当前 recordVersion`，不符 → accept 失败返回冲突详情（U33 乐观校验入口）。
   - update_state 三元组目标、update_thread 目标必须仍存在且状态可转（如对 superseded 记录再 update → 失败）。
   - evidence 复核：confirmed 候选的 SourceRef 过 `CheckSourceRefAgainstContent`（章节正文在进 memoryMu 前快照读入，锁内只做内存比对）；revision 不一致 → 降级 proposed + needsReview=evidence_stale，不判失败（L-V 语义）。
   - 人工修订保护复核：目标 lastSource 在暂存后变为 manual_edit → 该 op 移入 Conflicts，不提交（其余 ops 继续）。
5. **状态授予**：服务端按校验结果定终态——events 有定位证据 → confirmed，否则 proposed+needsReview；states → active（无证据 → active + needsReview=true，spec §5.1「State 无证据须 needsReview」）；threads 按提案 + 证据校验结果。
6. 组织 payload（五类文件整体覆盖写内容）→ 调 `commitMemoryLocked(type="formal", basedOn, payload)`——崩溃恢复、revertPayload、安全态等全部复用既有协议，**本期不在提交协议上新增分支**。
7. 成功后：ChangeSet.Status=applied、回填 CommitID、写 changesets.json（提交解锁后单独原子写；失败仅记日志——CommitRecord 已含全部事实，changeset 标注可重建）。

### 4.3 并发与锁约束（冻结）

- 锁内禁止 LLM / 网络 / 读章节文件（§4.2-4 的章节正文在锁外预读）。
- accept 与分析任务的互斥：步骤 1 前置检查 + 锁内二次校验（analysis status 读 project.json 缓存字段，不触发 IO）。
- 多 ChangeSet accept 串行：memoryMu 天然串行化；后者基线校验决定成败。

### 4.4 rebase（基线过期恢复路径）

`POST /api/memory/changesets/{id}/rebase`：

1. 仅限 status=proposed。
2. 对 ChangeSet 内全部 ops 重跑 §4.2-4 校验（不写盘）：刷新 expectedRecordVersion、重检 manual_edit、重算 evidence（章节 Revision 变化 → 更新降级标记）。
3. `BasedOnRevision` 更新为当前 memoryRevision，写 changesets.json。
4. 返回刷新后的 ops/conflicts 摘要，用户确认后再 accept。

### 4.5 实体候选确认（I2-6，spec §6.3 文件级路径）

`POST /api/memory/candidates/{id}/resolve`，体 `{"action": "register|merge|dismiss", "resolution": "c_xxx（merge 时必填）"}`：

- register：经 knowledge 既有路径注册新实体（分配 c_/w_，走归并导出函数 MatchCharacter/MatchWorldEntry 判定后追加）；候选 Status=register。
- merge：resolution 目标实体存在性校验 → 候选名/别名并入目标实体 aliases；候选 Status=merge。
- dismiss：候选 Status=dismissed。
- 三种动作均为一次 `commitMemoryLocked(type="entity_confirm", ...)`：payload 覆盖 entity_candidates.json；register/merge 还需写 knowledge.json——**注意：knowledge.json 不在既有 memory 提交协议的五类文件内**。本期冻结结论：knowledge 写入经 store 现有知识保存路径在提交前完成（锁外预写，失败则不进入提交），候选状态翻转走 entity_confirm 提交；「knowledge 纳入统一提交日志」记入待确认事项，不擅自扩展 payload 范围。
- 并发保护：resolve 校验候选当前 Status（非 pending → 409）。

---

## 5. 证据失效复核（I2-5，U9/U10 完整路径）

### 5.1 触发点

| 触发 | 行为 |
|---|---|
| SaveChapter 正文/标题变化（Revision 递增） | 异步复核该章全部 SourceRef：`chapterRevision != 当前` → needsReview=true + reviewReason=evidence_stale（status 不变，spec §5.1 红线）；quote 与现内容不符 → reviewReason=quote_mismatch |
| DeleteChapter | 已有行为（needsReview=chapter_deleted）不变；复核跳过已删章节 |
| 手动触发 `POST /api/memory/reconcile` | 全量复核所有 SourceRef |

### 5.2 执行语义

- 复核 = reconcile 类型提交（`commitMemoryLocked(type="reconcile")`，+1/次操作，spec §8.3）；**无实际标记变化 → 不提交、不递增**（沿用 StoryOrder no-op 语义）。
- 标记写入遵守 R8：needsReview/reviewReason 变化 → 目标 recordVersion+1（复核在锁内基于刚读快照，版本即真值）。
- 同值不递增（已 needsReview 且同 reason → 跳过）。
- 章节正文读取在锁外完成（与 §4.2-4 同一快照模式）。
- 联动位置：SaveChapter 保存路径收尾（与 chapterOrderHook 同模式：锁外、error 仅记日志、不阻断章节保存）。

---

## 6. API 设计（风格对齐 §12：MaxBytesReader、DisallowUnknownFields、{code,error}）

| 方法 | 路由 | 说明 |
|---|---|---|
| POST | `/api/memory/extract` | 启动提取。体 ≤4096B；`{chapterIds?:[], force?:bool}`。分析/初始化/提取任务进行中 → 409 `already_running` / `analysis_running`；前置（迁移/知识）同 §12.3 → 409；成功 202 `{extractStatus:"extracting"}` |
| GET | `/api/memory/extract/status` | 200 ExtractState |
| GET | `/api/memory/changesets?status=proposed` | 200 列表（摘要字段，不含 Record 全文） |
| GET | `/api/memory/changesets/{id}` | 200 完整 ChangeSet（含 ops/conflicts）；404 |
| POST | `/api/memory/changesets/{id}/accept` | 空体/`{}`；200 `{status:"applied", commitId, memoryRevision}`；幂等重放 200 原结果；409 `revision_conflict` / `analysis_running` / 终态；422 `invalid_changeset`（L-S 失败明细） |
| POST | `/api/memory/changesets/{id}/reject` | 体 `{"reason":"..."}`（可选，≤200 字）；200；终态 409 |
| POST | `/api/memory/changesets/{id}/rebase` | 空体；200 刷新后摘要；终态 409 |
| GET | `/api/memory/candidates?status=pending` | 200 列表 |
| POST | `/api/memory/candidates/{id}/resolve` | §4.5；200 `{status:"..."}`；非 pending 409；merge 缺/错 resolution 400 |
| POST | `/api/memory/reconcile` | 空体；手动全量证据复核；200 `{marked, memoryRevision}` |

- ID 参数过 `novel.ValidID` + 前缀校验（mcs_ / cand_），不符 400。
- 所有写接口体上限 4096B、未知字段 400、尾随 JSON 400、超限 413——与 §12.3 契约同构。
- 所有写接口体上限 4096B、未知字段 400、尾随 JSON 400、超限 413——与 §12.3 契约同构。

---

## 7. 前端最小审阅面板（I2-7）

在现有 web/ 单页内新增「记忆审阅」区块（不引入新框架，沿用现有原生 JS + fetch 风格）：

1. 待审 ChangeSet 列表：来源章节、ops 数量、冲突数、创建时间。
2. 详情视图：逐 op 展示类型、目标、新旧值对照、证据 quote；冲突项单独分组标红。
3. 操作：采纳 / 驳回（填原因）/ 重算基线（revision_conflict 时提示并一键 rebase）。
4. 实体候选列表：名称、可能命中、操作（注册为新实体 / 并入所选实体 / 忽略）。
5. 提取任务入口：按钮 + 状态轮询（复用现有分析进度轮询模式）。

---

## 8. 异常与恢复矩阵

| 场景 | 行为 |
|---|---|
| 提取中进程崩溃 | 重启置 failed + interrupted_by_restart；已整章暂存的 ChangeSet 保留有效 |
| 提取 LLM 持续失败 | 单章记 failedChapters 继续后续章节；全部失败 → failed/partial（不阻塞既有记忆） |
| accept 提交中途崩溃 | 既有六崩溃点恢复矩阵全覆盖（type=formal 走同一协议） |
| accept 时基线过期 | 409 revision_conflict → rebase → 重 accept |
| accept 时记录被人工改版本 | expectedRecordVersion 不符 → 零写入，返回冲突详情 |
| 提取快照后导入新书 | memoryGeneration 失效 → 任务零写入零回写 |
| changesets.json 损坏 | 解码失败 → 提取/accept/reject/rebase 拒绝并报告错误（同 commits.json 安全态语义）；不影响五类正式文件读取 |
| 重复 accept | 幂等返回原 commitId，memoryRevision 不再 +1 |

---

## 9. 任务拆分与允许改动范围

按 iter2-plan §6 顺序执行。每步遵循 task 工作流（任务 MD + -changes.md + -issues.md）。

### I2-2 ChangeSet 模型（先行）

- 范围：`internal/novel/memory_changeset.go`（新，ChangeSet/ChangeOp/ConflictItem/校验）、`internal/store/memory_changeset.go`（新，暂存读写/查重）、对应单测。
- 冻结：op 类型枚举、L-S 分层复用现有 Validate*、changesets.json 布局。
- 自测：`go test ./internal/novel ./internal/store -run ChangeSet -count=1`。

### I2-1 提取管线

- 范围：`internal/analysis/extract.go`（新，任务循环/凭据/单章流程）、`internal/analysis/extract_prompt.go`（新）、`internal/store/memory_extract.go`（新，extract_state 持久化/恢复/重置接入）、`internal/server/memory.go`（extract 两路由追加）。
- 禁止改动：`analysis/manager.go` 现有分析状态机逻辑、`agent/`。
- 自测：`go test ./internal/analysis ./internal/store -count=1` + 构造假 LLM 的管线单测。

### I2-3 + I2-4 审核提交

- 范围：`internal/store/memory_changeset.go`（accept/reject/rebase/apply 逻辑）、`internal/server/memory.go`（changesets 五路由）、store/server 测试。
- 衔接：只调 `commitMemoryLocked`，不改 `memory.go` 提交协议实现。
- 自测：U16/U18/U29/U31/U33 写入口用例 + 幂等/崩溃恢复复测（`-tags acceptance_review`）。

### I2-5 证据复核

- 范围：`internal/store/memory_reconcile.go`（新）、`internal/store/store.go`（SaveChapter 收尾挂点，模式同 chapterOrderHook）、`internal/server/memory.go`（reconcile 路由）。
- 自测：U9/U10 完整版用例 + no-op 不递增回归。

### I2-6 候选确认

- 范围：`internal/store/memory_candidates.go`（新）、`internal/server/memory.go`（两路由）、knowledge 注册/归并衔接。
- 自测：U24 全链路（ambiguous→pending→register/merge/dismiss + entity_confirm 提交）。

### I2-7 前端面板

- 范围：`web/index.html`、`web/app.js`、`web/style.css`（追加，不改既有续写/分析区块逻辑）。
- 自测：手动冒烟（启动服务走通 提取→审阅→采纳/驳回→候选确认）。

### 全线通用约束

- 不得修改：`memory_model_spec.md` 冻结条款、`internal/llm/client.go` 重试语义、提交协议实现、四个既有记忆 API 契约。
- 全程 `go build ./... && go vet ./... && go test ./... -count=1` 绿；收尾补 `go test -tags acceptance_review ./... -count=2`。
- 不自动 commit；每任务完成更新任务 MD 状态行并等人工审阅。

---

## 10. 验收用例（本期新增/补全）

| # | 用例 | 判定 |
|---|---|---|
| U9（完整） | 章节 Revision 递增后复核 | needsReview=true、evidence_stale、status 不变、recordVersion+1、reconcile 提交 +1 |
| U10（完整） | quote 与现正文不符 | quote_mismatch，同上语义 |
| U16（写入口） | accept 载荷含 L-S 失败 op | 整个 accept 拒绝、零写入、memoryRevision 不变、ChangeSet 保持 proposed |
| U18（写入口） | update_state 跨 category 同 attribute | 共存互不替代 |
| U19 | 提取确认无参与者 | participantsKnown=true + 空 participants |
| U24（全链路） | 提取遇 ambiguous | candidate pending 落盘、knowledge 不变、记录带 ambiguous_participant、resolve 三动作各正确 |
| U29（写入口） | 提取同值 state | 不产生 op、recordVersion 不变 |
| U31（写入口） | 过期 BasedOnRevision accept | revision_conflict 零落盘；rebase 后 accept 成功 |
| U33（写入口） | accept 路径全语义字段变更 | recordVersion 按 R8 表递增、expectedRecordVersion 校验生效、同值不递增 |
| I2-A | 幂等 accept | 二次 accept 返回原 commitId、revision 不再 +1、事件不重复 |
| I2-B | manual_edit 冲突 | 暂存/提交双时点命中 manual_edit → ConflictItem、不覆盖 |
| I2-C | 证据定位 | quote 定位 → rune 偏移正确（含 emoji）；定位失败 → 降级 proposed 并标记 evidence_unlocated |
| I2-D | resolved 无证据 | 提案降级 developing + resolved_without_proof 冲突项 |
| I2-E | 提取任务失效 | 快照后导入新书 → 零写入零回写；孤儿 extracting 重启置 failed |
| I2-F | 空 ops accept | 直接 applied、无 CommitRecord、不递增 |
| I2-G | 分析运行中 accept | 409 analysis_running |
| I2-H | changesets.json 损坏 | 草稿区写操作拒绝报错，正式区读正常 |

---

## 11. 待确认事项（实施中如遇空白追加于此，不擅自决定）

| # | 事项 | 初步建议 |
|---|---|---|
| Q1 | entity_confirm 是否把 knowledge.json 纳入统一提交日志 payload | 本期按 §4.5 冻结结论（锁外预写 + 候选提交分离）执行；后续评估统一 |
| Q2 | 提取输入中「相关正式记忆快照」的 4000 字上限与裁剪策略 | 按 §3.3-1 执行，实测不足再调 |
| Q3 | force 重提产生的同章多版 pending ChangeSet 是否需互斥提示 | 列表 UI 标注同章即可，不强制互斥 |
| Q4 | 章节正文在 accept 锁外快照与锁内校验之间被编辑的极端窗口 | evidence 复核本身会捕获 revision 变化并降级，语义安全；不引入章节读锁 |
