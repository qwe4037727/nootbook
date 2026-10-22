# novelagent 记忆系统 · 数据模型与接口基线（V1.2 冻结版 · 含复审补齐 R7–R9）

> 任务：#2 数据模型与接口基线文档 ｜ 日期：2026-10-22
> 版本：V1.2（按 yj 对 V1.1 的整体审查意见定点修订 R1–R6；V1.2 复审结论"修改后通过"，按复审意见补齐 R7–R9 后**正式冻结**；V1.1 有效内容原样保留，未重开任何已冻结架构决策；修改记录见 §17）
> 范围：仅设计冻结。不实现 API、不改业务代码、不开始 #3–#9。
> 上游：`详设.md`(V1.0) → `docs/详设_review.md`(P1–P18) → `docs/详设_V1.1_核实与决策基线.md`(Q1–Q8) → 本文档
> 事实来源：本会话对 `internal/novel/project.go`、`internal/novel/chapter.go`、`internal/store/store.go`、`internal/analysis/merge.go`、`internal/analysis/manager.go`、`internal/server/server.go` 的完整通读，及 `data/`、`.tmp_e2e/data/` 盘点。无法确认之处标注【待源码确认】。

---

## 1. 现状盘点（源码事实，规格的出发点）

### 1.1 现有模型与文件

| 事实 | 出处 |
|---|---|
| `novel.Chapter{ID,Index,Title,Content,CharCount,UpdatedAt}`，**无 Revision 字段** | project.go L14–21 |
| `novel.Knowledge{Overview,Style,Characters[],World[],Summaries[],UpdatedAt}` | project.go L172–179 |
| `Character.ID` 由 `NewID("c")` 分配；`WorldEntry.ID` 由 `NewID("w")` 分配；两者在 `Normalize()` 内按 `normalizeKey(名称)` 去重合并 | project.go L189–292 |
| 章节文件 `chapters/<ch_xxx>.json` 存完整 Chapter；`project.json` 的 `ChapterMeta` 为列表缓存 | store.go L552–610 |
| `store.Store` 单一 `mu sync.Mutex`，公共方法各自全程持锁；`loadProjectLocked` 等 **xxxLocked 内部方法模式已存在** | store.go L56–59 及各方法 |
| `writeJSONFile` = temp → write → Sync → Close → Rename，单文件原子；跨文件无事务 | store.go L627–658 |
| 分析器：锁外调 LLM → 锁内 `CommitAnalysis(revision,…)`，revision 不匹配返回 `ok=false`，Manager 重入自愈 | manager.go L103–219 |
| `CommitAnalysis`/`FinishAnalysis` 在 revision 匹配时无条件写 knowledge.json + project.json | store.go L375–435 |
| 归并逻辑两套：`analysis.matchCharacter/matchWorldEntry`（名字+别名 key 相交）与 `novel.Normalize` 的 `mergeCharacter/mergeWorldEntry`（仅主名精确 key），**行为不一致** | merge.go L180–200；project.go L210–219, L294–328 |
| `data/` 现状：仅 `config.json` + **legacy v1** `project.json`；`migrateLegacy` 未执行，无 `chapters/`、无 `knowledge.json` | 盘点 2026-10-22 |
| `.tmp_e2e/data/` 含完整新格式产物（chapters/、knowledge.json、project.json），可作新格式样例 | 盘点 |

### 1.2 相关现有行为约束

- `SaveChapter`（store.go L226–272）当前**每次保存无条件** `project.Revision++` 并回退 Analysis 至该章前置 `pending`——引入 Revision 后按 §7 修改。
- `LoadChapter`（store.go L178–202）当前以 meta 覆盖 Index/Title/CharCount——按 §7.4 真源规则微调。
- `server.go` 路由为 Go 1.22 风格，单作品、无 workId 段。

---

## 2. ID 体系规范（四类 ID 严格区分）

| 类别 | 前缀 | 生成方式 | 示例 | 说明 |
|---|---|---|---|---|
| **实体 ID** | `c_` / `w_` | 复用 `novel.NewID`，由 knowledge 归并路径分配 | `c_a1b2…` | 人物 / World 实体。**记忆系统禁止另建 `char_xxx`、`world_xxx`** |
| **章节记录 ID** | `ch_` | 现有 `NewID("ch")` | `ch_…` | 章节文件与 meta 引用 |
| **记忆记录 ID** | `evt_` `state_` `cog_` `thread_` `plan_` | `NewID(prefix)` | `evt_00ab…` | 与实体 ID 无推导关系 |
| **流程 ID** | `draft_` `mcs_` `commit_` `cand_` | `NewID(prefix)` | `commit_…` | 草稿/变更集/提交/**实体候选提案**（draft、mcs 本期仅冻结命名） |

1. 所有 ID 过 `novel.ValidID`（`[A-Za-z0-9_-]`，≤64）。
2. 引用字段按前缀做类型约束（§6.1）；前缀不符属**结构校验失败**（§4.7），非静默修复项。
3. `work_id` 恒为服务端常量 `internalWorkID = "work_001"`，持久化字段保留，URL 不引入 workId（冻结决策 9）。

---

## 3. 通用记忆字段（MemoryMeta）与专用状态类型

### 3.1 MemoryMeta（仅真正共用的元数据；**不含 Status**——状态由各模型独立声明，见 §3.2）

```go
type MemoryMeta struct {
    WorkID        string      `json:"workId"`                  // 恒 internalWorkID
    RecordVersion int64       `json:"recordVersion"`           // 初始 1，递增定义见 3.3
    CreatedAt     time.Time   `json:"createdAt"`               // UTC
    UpdatedAt     time.Time   `json:"updatedAt"`               // UTC
    SourceRefs    []SourceRef `json:"sourceRefs,omitempty"`    // 证据，§5
    Confidence    float64     `json:"confidence,omitempty"`    // 0–1，0=未标注
    NeedsReview   bool        `json:"needsReview"`             // 默认 false
    ReviewReason  string      `json:"reviewReason,omitempty"`
    Origin        Origin      `json:"origin"`
    LastSource    string      `json:"lastSource"`              // §3.4
}

type OriginType string

const (
    OriginBackfill     OriginType = "backfill"
    OriginDeepExtract  OriginType = "deep_extract"
    OriginManual       OriginType = "manual"
    OriginCommit       OriginType = "commit"
    OriginReconcile    OriginType = "reconcile"
)

type Origin struct {
    Type    OriginType `json:"type"`
    Chapter string     `json:"chapterId,omitempty"`
    Kind    string     `json:"kind,omitempty"` // backfill 细分：summary | character_state
}
```

| 字段 | 必填 | 默认 | 规则 |
|---|---|---|---|
| workId | 是 | `"work_001"` | 服务端填充 |
| recordVersion | 是 | 1 | **一次记录级逻辑变更递增一次；同值更新不递增**。逻辑变更=影响语义内容或自动覆盖保护的可持久化字段变化；递增表为**穷尽定义**（§3.3，R8）。更新操作须携带 `expectedRecordVersion` 乐观校验 |
| sourceRefs | 否 | 空 | **StoryEvent 的 confirmed 状态必须至少一条**（结构校验，§4.7）；回填骨架/候选状态可空但需 needsReview=true |
| confidence | 否 | 0 | 0–1；**越界属结构校验失败（拒绝），不静默钳制** |
| needsReview / reviewReason | 否 | false/空 | 语义：记录存在但未确认为事实；检索降权并标注 |
| origin / lastSource | 是 | — | lastSource ∈ `agent_extraction` / `manual_edit` / `backfill` / `reconcile`（写入侧校验） |

### 3.2 专用状态类型（各模型独立；MemoryMeta 不含 Status）

```go
type StoryEventStatus  string // proposed | confirmed | conflicted | invalidated   (V1.2: +proposed)
type StoryStateStatus  string // active | superseded | invalidated
type CognitionStatus   string // known | wrong | forgotten | uncertain
type StoryThreadStatus string // open | developing | resolved | deferred | abandoned | conflicted
type StoryPlanStatus   string // proposed | active | paused | completed | superseded | cancelled

func (s StoryEventStatus) Valid() bool { … } // 每类型各一个，校验枚举封闭性
```

五类模型的结构体均包含 `Status <各自类型>`。状态枚举**不可跨模型混用**——专用类型在编译期阻断。

### 3.3 record_version 递增判定（R8 穷尽化）

**原则（R8 冻结）**：递增表为**穷尽定义**——凡可持久化字段（除 `recordVersion`、`updatedAt` 本身外）的逻辑变化都必须落入下表；若实现中出现未列出字段的写入路径，视为规格违规，须先补表再实现。判定标准：**该变化是否影响语义内容或自动覆盖保护**——影响则 +1，并受 `expectedRecordVersion` 乐观锁校验。

| 变更 | recordVersion |
|---|---|
| 语义字段（summary/value/content/约束等）、EventType、Status、SourceRefs 集合、Participants 集合、RelatedEntities 集合、Preconditions/Consequences、Confidence 变化 | +1 |
| **needsReview / reviewReason 变化**（R8：人工修订标记，直接影响检索降权与保护判定） | +1 |
| **lastSource 变化**（R8：manual_edit 等来源标记，直接影响 §3.4 保护协议判定） | +1 |
| 同值更新（内容逐字段相等，含重复写入相同 needsReview/lastSource） | 不递增 |
| 纯 Normalize 格式结果（trim/时间标准化） | 不递增 |

- 混合变更（同一次更新中多项落入 +1 的变化）仍**整体只 +1**（一次记录级逻辑变更递增一次，§3.1）；
- needsReview/reviewReason、lastSource 的写入与语义字段同受 `expectedRecordVersion` 乐观校验（reconcile 批量标注同理）。

### 3.4 lastSource 与人工修订保护

- lastSource ∈ `agent_extraction` / `manual_edit` / `backfill` / `reconcile`（写入侧校验合法值）。
- **人工修订冲突协议**：任何自动流程（回填重放、深度提取、reconcile）遇到 `lastSource=manual_edit` 的记录，**一律不直接覆盖**：
  - 回填：跳过；
  - 深度提取/提取器：产出**冲突提案项**（目标记录 ID + expectedRecordVersion + 新值 + 旧值 + 新证据），进入提交的 `conflicts` 清单，由用户裁决。本期（迭代 1）无提案 UI，规则先冻结，行为在迭代 2 ChangeSet 落地。

---

## 4. 五类记忆模型

统一约定：时间字段一律 UTC。"必填"的执行方式见 §4.7 分层校验——Normalize 不丢弃记录。

### 4.1 StoryEvent（事件）

```go
type StoryEvent struct {
    ID                 string            `json:"id"`                    // evt_
    MemoryMeta
    Status             StoryEventStatus  `json:"status"`                // proposed | confirmed | conflicted | invalidated
    EventType          string            `json:"eventType"`
    Summary            string            `json:"summary"`               // ≤200 字
    ChapterID          string            `json:"chapterId,omitempty"`   // 主来源章节快捷字段，§5.2
    StoryOrder         int               `json:"storyOrder"`            // 可重建的派生排序字段，见 4.1.4
    ParticipantsKnown  bool              `json:"participantsKnown"`     // 区分"未识别/确认无"，见 4.1.5
    Participants       []string          `json:"participants,omitempty"`    // c_
    RelatedEntities    []string          `json:"relatedEntities,omitempty"` // w_
    Preconditions      []EventLink       `json:"preconditions,omitempty"`
    Consequences       []EventLink       `json:"consequences,omitempty"`
}

type EventLink struct {
    EventID string `json:"eventId,omitempty"`
    Text    string `json:"text,omitempty"`
}
```

| 字段 | 类型/JSON | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| ID | string/`id` | 是 | `NewID("evt")` | |
| Status | StoryEventStatus/`status` | 是 | **由来源决定**（R1）：有正文证据且通过校验→`confirmed`；否则→`proposed` | proposed / confirmed / conflicted / invalidated |
| EventType | string/`eventType` | 是 | — | character_action / relation_change / info_reveal / conflict / location_change / item_transfer / world_change / other |
| Summary | string/`summary` | 是 | — | Normalize 截断 ≤200 rune |
| ChapterID | string/`chapterId` | 否 | 空 | 主来源快捷字段（§5.2） |
| StoryOrder | int/`storyOrder` | 是 | 来源章节 Index | 派生字段，见 4.1.4 |
| ParticipantsKnown | bool/`participantsKnown` | 是 | false | false=未识别；true=已识别（空列表=确认无参与者） |
| Participants | []string | 否 | 空 | 每项须 `c_` 前缀 |
| RelatedEntities | []string | 否 | 空 | 每项须 `w_` 前缀 |
| Preconditions/Consequences | []EventLink | 否 | 空 | 每项 EventID/Text 至少其一非空 |

**4.1.4 StoryOrder 派生性**：
- 本期规则 `storyOrder = 来源章节 Index`（chapterId 非空时）。
- **StoryOrder 是可重建的派生排序字段，不是不可变事实标识**。
- 章节插入/删除/重排导致 Index 变化时，按 `chapterId → 当前 Index` 映射**批量重算**受影响事件并留痕 CommitRecord。
- **实现边界（V1.2 修订，R6/R9/R10）**：#3（Chapter.Revision）**不新增空壳 API、不定义 Store 方法形式的可替换扩展点**。采取**包级回调变量**方案：
  - `store` 包定义 `var chapterOrderHook func(oldIndexes, newIndexes map[string]int) error`（默认 `nil`，无记忆模块时零开销）；**签名携带 error 返回（R10）**；
  - #3 仅在**章节重排/删除保存路径的收尾处保留调用点**：调用位于 `store.mu` 释放之后，传入锁内计算的 `oldIndexes/newIndexes` 快照；
  - 注册方 = 记忆模块（#7 初始化完成后经 `RegisterChapterOrderHook` 一次性赋值注册）；本期不提供反注册；**注册与读取经 `sync.RWMutex` 互斥保护（R11），杜绝运行期注册与章节写操作的数据竞争**；
  - **失败处理（R9/R10 冻结）**：回调**返回 error 或 panic** 均仅记录日志，**不得阻断、不得回滚章节操作**——章节保存结果与记忆侧解耦；记忆侧依赖 StoryOrder 可重建性（决策 16）与提交日志，在下次重算时自愈；
  - 回调内部**不得调用 Store 的公共加锁方法**（死锁防护）；记忆模块使用自身锁，与 `store.mu` 无嵌套。
  - **不要求 #3 单独完成尚不存在的记忆写入**。

**4.1.5 参与者语义**：回填骨架恒 `participantsKnown=false, participants=[]`；深度提取确认后置 `true`；确认无参与者也是 `true` + 空列表。

**4.1.6 status 语义与转换（R1 冻结）**：
- `proposed`：**待确认候选**。无正文证据（或证据未过校验）的事件的合法持久化状态——回填摘要骨架的**固定初始状态**。
- `confirmed`：已确认事实。**机械前置（L-S）：sourceRefs 非空**；且证据须通过 L-V（版本一致、quote 一致、章节存在）。
- `conflicted`：检出冲突待核对；`invalidated`：事实不成立。
- 转换表：

| 转换 | 机械前置条件 |
|---|---|
| proposed → confirmed | sourceRefs 非空 且 全部证据通过 L-V |
| proposed → conflicted / invalidated | Validator/reconcile 置入；invalidated 需记录依据（reviewReason 留痕） |
| proposed → proposed（内容更新） | 允许，recordVersion+1 |
| confirmed → conflicted | L-V 检出冲突 |
| conflicted → confirmed、invalidated → confirmed | **必须伴随证据集合变化**（§5.3） |
| confirmed/invalidated → proposed | **禁止**（降级走 conflicted/invalidated） |

- **检索层区分（R1 冻结）**：`confirmed` 计入"已确认事实"（上下文 P0/P1 区）；`proposed` 仅以**待确认候选**身份低权重出现，续写 prompt 中**不得作为事实陈述**。

### 4.2 StoryState（当前状态）

```go
type StoryState struct {
    ID                   string           `json:"id"`          // state_
    MemoryMeta
    Status               StoryStateStatus `json:"status"`      // active | superseded | invalidated
    SubjectID            string           `json:"subjectId"`   // c_ 或 w_
    Category             string           `json:"category"`
    Attribute            string           `json:"attribute"`   // ≤40 字
    Value                string           `json:"value"`       // ≤200 字
    ValidFromEvent       string           `json:"validFromEvent,omitempty"`
    LastConfirmedChapter string           `json:"lastConfirmedChapter,omitempty"`
}
```

**唯一键（三元组）**：同 `(subjectId, category, attribute)` 最多一个 `active`。**同值重复写入为 no-op（R6 冻结）**：新 Value 与旧 active 的 Value 相同 → 不新建记录、不替代、不递增版本。
新 active 写入（Value 不同）触发同三元组旧 active → `superseded`（同一提交内原子完成，旧记录保留）。
State 的**证据要求**（R1 联动修订）：active 状态**不强制** sourceRefs（生命周期由状态表达）；**无 sourceRefs 的 active 记录必须 `needsReview=true`**（L-V 规则）——回填映射据此合法持久化。
status 转换：active→superseded（同三元组新 active）；active/superseded→invalidated 仅用户/reconcile；superseded→active 仅经人工修正。

### 4.3 Cognition（认知）

```go
type Cognition struct {
    ID              string          `json:"id"`          // cog_
    MemoryMeta
    Status          CognitionStatus `json:"status"`      // known | wrong | forgotten | uncertain
    SubjectID       string          `json:"subjectId"`   // 仅 c_
    Content         string          `json:"content"`     // ≤200 字
    KnowledgeItemID string          `json:"knowledgeItemId,omitempty"` // 预留
    KnownSinceEvent string          `json:"knownSinceEvent,omitempty"` // evt_
}
```

转换：known↔wrong、known→forgotten（附证据/来源变更，§5.3）；uncertain→known/wrong（附证据）。KnowledgeItemID 本期仅保留字段契约。

### 4.4 StoryThread（伏笔）

```go
type StoryThread struct {
    ID                 string            `json:"id"`      // thread_
    MemoryMeta
    Status             StoryThreadStatus `json:"status"`  // open | developing | resolved | deferred | abandoned | conflicted
    Title              string            `json:"title"`   // ≤60 字
    Question           string            `json:"question,omitempty"` // ≤120 字
    IntroducedByEvent  string            `json:"introducedByEvent,omitempty"`
    RelatedEvents      []string          `json:"relatedEvents,omitempty"`
    InvolvedCharacters []string          `json:"involvedCharacters,omitempty"` // c_
    KnownClues         []string          `json:"knownClues,omitempty"`         // ≤100 字/条，≤8 条
    ResolutionCriteria []string          `json:"resolutionCriteria,omitempty"` // ≤100 字/条，≤6 条
    ResolutionEvent    string            `json:"resolutionEvent,omitempty"`
    AbandonReason      string            `json:"abandonReason,omitempty"`      // ≤200 字
}
```

| 转换 | 机械前置条件 |
|---|---|
| → `resolved` | ResolutionEvent 非空 且 该 evt 存在 且 该 evt.sourceRefs 非空 |
| → `abandoned` | AbandonReason 非空 |
| open/developing 互转、↔deferred | 允许 |
| resolved / abandoned | 终态；重开仅经人工修正 |

初始 `open`；`conflicted` 仅 Validator/reconcile 置入。

### 4.5 StoryPlan（未来计划，唯一正式计划模型）

```go
type StoryPlan struct {
    ID                 string          `json:"id"`      // plan_
    MemoryMeta
    Status             StoryPlanStatus `json:"status"`  // proposed | active | paused | completed | superseded | cancelled
    Level              string          `json:"level"`   // global | volume | near_term
    ParentPlanID       string          `json:"parentPlanId,omitempty"` // V1.2 新增（R2）：volume→global 计划；near_term→volume 或 global 计划；global 留空
    Title              string          `json:"title"`   // ≤80 字
    Objective          string          `json:"objective,omitempty"`          // ≤300 字
    Constraints        []string        `json:"constraints,omitempty"`        // ≤100 字/条，≤8 条
    CandidateBeats     []string        `json:"candidateBeats,omitempty"`     // ≤100 字/条，≤10 条
    DependsOnThreads   []string        `json:"dependsOnThreads,omitempty"`   // thread_
    InvolvedCharacters []string        `json:"involvedCharacters,omitempty"` // c_
    Flexibility        string          `json:"flexibility,omitempty"`        // high|medium|low，默认 high
    CreatedFromCommit  string          `json:"createdFromCommit,omitempty"`
}
```

**作用域模型（V1.2 重定义，R2 冻结）**：
- **作用域 = `(level, parentPlanID)`**。`parentPlanID` 为空视为挂靠作品根（作用域键 `(level, "")`）。
- **唯一性规则（机械）**：同一作用域内最多一个 `active`。**不再限制全作品每 level 只有一个 active**——不同 parent（如不同卷、未来不同剧情线）的 volume/near_term 计划可并行 active。
- **L-S 校验**：`parentPlanID` 非空时必须指向存在且 level 高一级的计划（volume 的 parent 须为 global；near_term 的 parent 须为 volume 或 global）；global 不得有 parent。
- **激活规则**：激活计划 P → 同作用域旧 active 自动置 `superseded`（同一提交内）。paused→active 恢复同样执行此规则。
- **父计划状态传播（V1.2 新增）**：父计划 → `cancelled` / `superseded` 时，其 `active` 子计划自动置 `paused`（同一提交内，等待重新挂靠或用户处置）；父 → `completed` 时**子计划不自动变更**，由 reconcile 生成提示。
- **第一期产品约束（显式声明，非模型限制）**：同一 parent 下仅创建/激活一个 `near_term` 计划（暂不支持并行剧情线）。数据模型本身支持多作用域并行 active，未来引入独立剧情线实体时无需改结构（待确认 §13.2-7）。

语义红线：completed 不证明细节按预期发生。与大纲关系（冻结决策 1）：`/api/outline` 语义=生成 global/volume 级 proposed 计划；outline.json 只能是导出投影。

### 4.7 校验分层（L-N / L-S / L-V）

| 层 | 职责 | 行为 | 允许改数据？ |
|---|---|---|---|
| **L-N Normalize**（纯函数） | 补默认值、格式标准化 | TrimSpace、UTC 统一、rune 截断、列表去重限长、ID 为空则 `NewID(prefix)` 补 | **不丢弃记录、不修 status/confidence/ref 前缀** |
| **L-S 结构校验**（机械，写路径） | 拒绝非法结构 | 必填缺失、ID/引用前缀不符、未知枚举、confidence 越界、EventLink 双空、SourceRef 区间非法、**event.status=confirmed 但 sourceRefs 为空**、plan.parentPlanID 指向不存在/层级错误 | 生成侧：无效候选直接丢弃；**持久化侧：拒绝整个操作** |
| **L-V 语义复核**（Validator/reconcile） | 保留记录 + 标注 | 引用存在性、证据过期/越界/quote 不一致、状态转换合法性、实体歧义、人工修订冲突、**无证据的 active State** | 仅置 `needsReview/reviewReason` 或产出冲突提案 |

**持久化边界（冻结）**：
1. 回填：L-S 失败的单条映射跳过并计入 stats（允许部分成功→partial）；
2. 正式提交 / 人工修正：L-S 任一失败 → **拒绝整个操作**，零写入；
3. **ChangeSet 完整性**：提案记录被判无效 → 提交失败，不得缺记录提交；
4. L-N 结果涉及语义字段变化 → 视为 L-S 失败。

---

## 5. 证据与引用一致性（SourceRef）

### 5.1 结构

```go
type SourceRef struct {
    ChapterID       string `json:"chapterId"`       // ch_
    ChapterRevision int64  `json:"chapterRevision"` // 必填 >0
    StartOffset     int    `json:"startOffset"`     // rune，左闭右开
    EndOffset       int    `json:"endOffset"`
    OffsetUnit      string `json:"offsetUnit"`      // 恒 "rune"
    Quote           string `json:"quote,omitempty"`
}
```

| 规则 | 冻结内容 |
|---|---|
| 偏移单位 | 恒 `rune`；其他值→L-S 拒绝 |
| Go 转换 | byte→rune 须经 `len([]rune(text[:byteIdx]))`；共享辅助 `SliceByRune(content, start, end) (string, error)`（#5 交付） |
| 区间 | `[start, end)`，`start ≥ 0`，`end > start`，`end ≤ runeLen`；违反→写入时 L-S 拒绝 / 既有记录 L-V 标注 |
| 版本不一致 | `chapterRevision != 当前 chapter.Revision` → L-V：`evidence_stale`；status 不自动变 |
| quote 不一致 | quote ≠ SliceByRune 结果 → L-V：`quote_mismatch` |
| 证据要求 | **仅 StoryEvent.confirmed 强制 sourceRefs 非空**（R1）；State 无证据须 needsReview；其余类型按各自转换规则 |
| 失效传播 | 依赖方不自动改写，仅 reconcile 生成复核提案 |
| 红线 | 任何证据失效不得自动删除/改写历史正文 |

### 5.2 ChapterID 与 SourceRefs 的一致性

`StoryEvent.ChapterID` 为主来源快捷字段：SourceRefs 非空时必须 == `SourceRefs[0].ChapterID`（否则 L-S 拒绝）；SourceRefs 为空（回填骨架）时可独立填写，但记录必须 `needsReview=true`。

### 5.3 证据变更与状态转换对照表

| 证据变更 | 判定 | 允许的状态转换 |
|---|---|---|
| **新增证据**（追加且通过 L-V） | 新 ref 不在原集合 | event：proposed→confirmed、conflicted→confirmed；cognition：uncertain→known/wrong、wrong→known；thread：open→developing |
| **替换证据**（同位置 ref 变更） | 位置相同、内容不同 | 保持 status 但 recordVersion+1；新证据与既有事实冲突→L-V 置 conflicted |
| **撤销证据**（移除 ref） | 集合缩小 | event：失去全部证据→needsReview + `evidence_removed`（**confirmed→proposed 禁止**，保持 confirmed+needsReview 或由用户置 invalidated）；thread：resolved 失去解决事件证据→conflicted |

---

## 6. 实体 ID 统一与归并

### 6.1 引用类型约束

| 字段 | 允许前缀 |
|---|---|
| StoryEvent.Participants、StoryThread.InvolvedCharacters、StoryPlan.InvolvedCharacters、**Cognition.SubjectID** | `c_` |
| **StoryState.SubjectID** | **`c_` 或 `w_`**（按记录 Category 判定：人物状态用 c_、world 类状态用 w_；写入侧按前缀校验） |
| StoryEvent.RelatedEntities | `w_` |
| StoryEvent.ChapterID、StoryState.LastConfirmedChapter、Origin.Chapter | `ch_` |
| 事件引用（Preconditions/Consequences.EventID、RelatedEvents、IntroducedByEvent、ResolutionEvent） | `evt_` |
| StoryPlan.DependsOnThreads | `thread_` |
| StoryPlan.ParentPlanID | `plan_`（且层级须为父级，§4.5） |

### 6.2 归并算法统一（#6 实现契约）

1. `analysis.matchCharacter/matchWorldEntry` 的"名称+别名 key 集合相交"算法提升为 novel 导出：
   ```go
   func MatchCharacter(chars []Character, incomingName string, incomingAliases []string) (index int, ambiguous bool)
   func MatchWorldEntry(entries []WorldEntry, incomingTitle string, incomingAliases []string) (index int, ambiguous bool)
   ```
2. `analysis/merge.go` 改调 novel 版，删除私有实现。
3. `novel.Normalize` 的主名精确合并保留为加载期兜底；候选匹配一律走导出函数。
4. 歧义：多实体命中→`ambiguous=true`，**不自动合并、不立即注册正式实体**——进入候选提案（§6.3）。

### 6.3 歧义实体候选（V1.2 新增，R3 冻结）

**原则**：不建第二套**正式**实体注册表（决策 4 不变）≠ 不能有**待确认实体提案**。歧义匹配结果存独立提案队列，用户确认后才注册/归并正式实体。

**存储**：`data/memory/entity_candidates.json`（`[]EntityCandidate`，整体覆盖写）：

```go
type EntityCandidate struct {
    ID          string      `json:"id"`           // cand_
    Name        string      `json:"name"`         // 候选名称 ≤40 字
    Aliases     []string    `json:"aliases,omitempty"`
    PossibleIDs []string    `json:"possibleIds,omitempty"`  // 可能匹配的既有实体（c_/w_）
    SourceRefs  []SourceRef `json:"sourceRefs,omitempty"`   // 来源证据
    Reason      string      `json:"reason"`       // ambiguous_alias 等
    Status      string      `json:"status"`       // pending | register | merge | dismissed
    Resolution  string      `json:"resolution,omitempty"`   // 处置说明（merge 时=目标实体 ID）
    CreatedAt   time.Time   `json:"createdAt"`
    ResolvedAt  time.Time   `json:"resolvedAt,omitempty"`
}
```

| 规则 | 冻结内容 |
|---|---|
| 产出方 | 本期（迭代 1）回填不产生（复用既有实体，无新实体）；深度提取/Extractor 遇 ambiguous=true 时写入（迭代 2 交付产出方）。**本期冻结结构与存储，迭代 2 交付产出与确认** |
| 确认行为 | `register`→经 knowledge 既有路径注册新实体（分配 c_/w_）；`merge`→目标实体别名并入（走归并路径，留 candidate 记录）；`dismissed`→丢弃。确认操作属**记忆提交**（memoryRevision+1） |
| 保护 | pending 候选**不参与**实体解析、不进入续写上下文（未确认） |
| 与 ChangeSet 关系 | 迭代 2 起，候选确认可作为 ChangeSet 操作类型（`register_entity` / `merge_entity`）；本期仅文件级确认 |

---

## 7. Chapter.Revision（任务 #3 行为契约）

| 规则 | 冻结内容 |
|---|---|
| 字段 | `Revision int64`（json:"revision"）加入 `novel.Chapter` 与 `ChapterMeta`（后者为缓存） |
| 初始值 | 新建/导入章节 = 1 |
| 递增条件 | SaveChapter 时 Content 或 Title 实际变化 → `Revision++` |
| 不递增 | 均未变 → Revision、UpdatedAt 不变、不回退 Analysis、不递增 project.Revision |
| 真源规则 | chapter 文件为 Content/Title/Revision 真源；meta 为缓存；不一致时加载以 chapter 文件修复 meta |
| 旧数据兼容 | 旧文件无 revision → 解析得 0 → 加载时置 1（幂等） |
| 与 Project.Revision | 职责分离，不互相推导 |
| 失败边界 | 章节文件写成功、project.json 写失败的残留：重启按真源规则自愈 meta |
| 联动收尾（R6/R9/R10 修订） | #3 **不新增空壳 API**：仅交付 `store` 包级回调变量 `chapterOrderHook`（**签名携带 error**）+ 章节重排/删除保存路径收尾处的调用点（锁释放后执行，默认 nil 零开销）；注册方=记忆模块（#7+），注册/读取经 RWMutex 互斥保护；回调返回 error 或 panic 仅记日志、不阻断不回滚章节操作。**#3 不实现尚不存在的记忆写入** |

---

## 8. memory 目录与 memory_revision（任务 #4 行为契约）

### 8.1 存储布局（冻结）

```
data/memory/
  meta.json                  # {schemaVersion, memoryRevision, lastCommitId, updatedAt}
  events.json                # []StoryEvent，整体覆盖写
  states.json                # []StoryState
  cognitions.json            # []Cognition
  threads.json               # []StoryThread
  plans.json                 # []StoryPlan
  entity_candidates.json     # []EntityCandidate（§6.3，V1.2 新增）
  commits.json               # []CommitRecord（§11）
  init_state.json            # §9.2
```

### 8.2 meta.json

`{ "schemaVersion": 1, "memoryRevision": 0, "lastCommitId": "", "updatedAt": "…" }`；不存在→首次访问时初始化；存量 knowledge.json 不迁入 memory（由 Initializer 回填）。

### 8.3 memory_revision 递增表（冻结）

| 操作 | 递增？ |
|---|---|
| 草稿生成/编辑、变更提案、改草稿、查询 | 否 |
| `CommitAnalysis` / `FinishAnalysis`（现有函数零改动） | 否 |
| 低成本回填整体完成（type=initialize 的一次 commit） | 是，+1 |
| 深度提取批次写入成功 | 是，+1/批次 |
| 人工修正成功 | 是，+1 |
| 实体候选确认（register/merge/dismiss） | 是，+1 |
| 正式提交（迭代 2） | 是，+1 |
| 无实际变化的分析/重复分析 | 否 |
| reconcile 批量标注 needsReview | 是，+1/次操作 |

每次递增载体=CommitRecord，**且 CommitRecord 记录 `targetMemoryRevision = basedOnMemoryRevision + 1`**（R4，§11）。

---

## 9. Memory Initializer（任务 #7 行为契约）

### 9.0 初始化前置检查（V1.2 新增，R5 冻结）

POST /api/memory/initialize 时按序检查，任一不满足则**不启动**：

| # | 检查 | 判定依据 | 不满足时 |
|---|---|---|---|
| 1 | 项目已迁移 | `project.json` 不含 legacy `manuscript` 字段（或 `chapters/` 目录存在且非空） | 409 `project_not_migrated`（提示：启动服务自动完成 migrateLegacy） |
| 2 | 知识可用 | `knowledge.json` 中 `characters`、`world`、`summaries` 至少一项非空 | 409 `knowledge_not_ready`（提示：先完成 /api/analyze） |
| 3 | 输入可解码 | project/knowledge 文件可正常解析 | 启动后进入 `failed`，lastError 记录解码错误 |

- **部分可用**：仅部分来源非空（如只有 characters 无 summaries）→ 允许初始化，空来源的映射跳过并计入 stats。
- **禁止零结果 ready（R5 红线）**：mapping 结束时若 `mappedEvents + mappedStates == 0` → **不得标 ready**，置 `partial` 且 `lastError="no_mappable_input"`。

### 9.1 两级初始化

第一级（本期实现）映射表：

| 来源 | 目标 | 规则 |
|---|---|---|
| knowledge.Characters | （无新建） | 复用 `c_` |
| Character.State 非空 | StoryState | subjectId=c_、Category=`general`、Attribute=`profile`、Value=State；status=`active`；**无 sourceRefs → needsReview=true**；confidence=0.5、reviewReason=`backfill_unverified`、origin={backfill, kind=character_state} |
| knowledge.Summaries 每章 | StoryEvent | eventType=`other`、Summary=摘要、ChapterID=章节、StoryOrder=Index、`participantsKnown=false`；**status=`proposed`（R1）**、needsReview=true、confidence=0.3、origin={backfill, kind=summary} |
| knowledge.World | （无新建） | 复用 `w_`；不生成 State |
| 既有大纲数据 | StoryPlan | 本期无来源；未来导入→proposed |

第二级（契约预留，本期不实现）：最近 N 章（默认 5，可配）/相关章节/用户触发；产物 origin.type=deep_extract；**有正文证据且过校验→confirmed，否则 proposed**。

### 9.2 初始化状态机

`memory/init_state.json`：`{status, startedAt, finishedAt, lastError, preconditions{migrated, knowledgeReady}, stats{mappedEvents, mappedStates, skippedInvalid, skippedManualProtected}}`

```
not_started → mapping → ready
                 │  └→ partial（部分来源为空/单条 L-S 跳过/零结果；基础检索可用）
                 └→ failed（保存 lastError）
failed / partial --重试--> mapping（续跑）
```

### 9.3 重复请求语义（V1.2 修订，R6 冻结——三种场景分开）

| 场景 | 前置状态 | 行为 |
|---|---|---|
| **首次/失败后续跑** | not_started / failed / partial | 202 `{initStatus:"mapping"}`，按 origin 查重续跑 |
| **进行中重试**（相同任务仍在执行） | mapping / deep_extracting | 409 `already_running` |
| **已完成操作重试**（幂等重放，无新副作用） | ready | **200** `{initStatus:"ready", "alreadyInitialized":true}`——不重新执行、不产生任何写入 |
| **请求全量重新初始化** | ready | 本期不支持：400 `reinit_not_supported`（显式拒绝；见待确认 §13.2-5） |

### 9.4 幂等与保护（冻结）

1. **origin 查重**：按 `(origin.type, origin.chapterId, origin.kind, subjectId)` 查重，已存在→跳过；
2. 实体天然幂等（不新建实体）；
3. **人工修订保护**：`lastSource=manual_edit` 回填跳过；深度提取遇人工记录→冲突提案，不覆盖；
4. **不升级**：回填产物保持 `proposed`/needsReview=true，永不因写入成功变 confirmed；
5. **部分成功恢复**：mapping 中断→停留 mapping，重试按 origin 查重续跑；未完成不得标 ready。

---

## 10. 存储与并发约束

1. 公共方法获取/释放 `store.mu`；内部实现一律 `*Locked`（Mutex 不可重入）；
2. **锁内禁止 LLM/不可控长耗时 IO**；
3. 正式提交（迭代 2）：同一次锁内完成"校验 → 写提交日志 → 写目标文件 → 置 committed → 对齐 memoryRevision"（Q1）；
4. 后台分析与提交同一写串行化；分析运行中禁止正式提交（409）+ 锁内二次校验；
5. 单文件原子 Rename ≠ 跨文件事务；跨文件一致性唯一保障 = 提交日志协议。

## 11. 提交日志协议（本期只冻结，不实现）

### 11.1 CommitRecord

```json
{
  "commitId": "commit_xxx",
  "type": "initialize | manual_fix | formal | reconcile | entity_confirm",
  "status": "prepared | committed",
  "basedOnMemoryRevision": 20,
  "targetMemoryRevision": 21,
  "payload": { "…目标文件→完整目标内容（覆盖写语义）…" },
  "createdAt": "…", "completedAt": "…"
}
```

**commits.json 自身持久化**：与其他文件一致的 temp+rename 整体覆盖写；恢复流程每推进一条记录即整体重写一次 commits.json（本批量级下可接受）。**commits.json 损坏（解码失败）→ 进入安全态：拒绝一切记忆写操作并报告错误，不猜测恢复**（R4 冻结）。

### 11.2 提交顺序（锁内）

1. **基线校验（R7 冻结）**：锁内读取当前 `meta.memoryRevision`，校验本次提交 `basedOnMemoryRevision == meta.memoryRevision`；不匹配 → **拒绝提交**（错误码 `revision_conflict`），**不落盘任何 CommitRecord、不写目标文件**——写方须基于最新 revision 重建载荷后重试（与现有 `Project.Revision` 乐观锁同构）；
2. 校验通过 → 写本记录(prepared, 含完整可重放载荷) → 依次覆盖写目标文件 → 置 committed → **对齐 meta（memoryRevision=targetMemoryRevision, lastCommitId=commitId）** → 解锁。

### 11.3 崩溃恢复矩阵（V1.2 重写，R4 冻结——六个崩溃点全覆盖）

| # | 崩溃点 | 重启后判定 | 恢复动作 | 最终 memoryRevision |
|---|---|---|---|---|
| 1 | prepared 日志写入前 | 无本提交记录 | 无（一切未发生） | basedOn（不变） |
| 2 | prepared 持久化后、目标文件写入前 | 扫描到 prepared | 按 payload 重放覆盖写 → 置 committed → meta 对齐 | target |
| 3 | 部分目标文件写入后 | 扫描到 prepared | 重放补齐（覆盖写幂等）→ committed → meta 对齐 | target |
| 4 | 全部目标文件写入后、committed 前 | 扫描到 prepared | 重放（无实际变化但幂等无害）→ committed → meta 对齐 | target |
| 5 | **committed 持久化后、meta 更新前**（V1.1 窗口） | 扫描到 committed | **不重放文件**；仅核对/修复 meta：`meta.memoryRevision < target` → 对齐至 target、lastCommitId=该记录 | target |
| 6 | meta 更新后、响应返回前 | 一切已持久化 | 无恢复动作；客户端超时重试确认 → 同 commit_id 幂等返回原结果 | target |

**恢复算法（R7 修订——两阶段：先验链，后收敛）**：
1. **提交链校验**：将 commits.json 全部记录按 createdAt 序排列，逐条校验连续性——首条 `basedOn == 0`（空存储起点；若磁盘 meta 已有更高值则以"链上无更早提交"语义核对）；此后每条须 `basedOn == 上一条 target`。发现**断链**（basedOn 跳跃，存在缺失提交）或**分叉**（两条记录 basedOn 相同且均非 aborted / target 重复）→ **进入安全态：拒绝一切记忆写操作并报告 `commit_chain_broken`，不得收敛 memoryRevision**，由人工处理（commits.json 与目标文件均为可读 JSON 文件，可人工裁定后修复）；
2. **链校验通过后**按上方崩溃恢复矩阵执行：prepared→重放+置 committed；committed→跳过文件重放。结束后执行 **meta 收敛**：`meta.memoryRevision = max(meta.memoryRevision, 链尾 committed 记录的 targetMemoryRevision)`（空链→不变），lastCommitId=链尾记录。**禁止"仅取全表最大 target"收敛**——必须以校验通过的连续链为收敛依据，避免掩盖异常提交顺序（R7）。

**不变量（R4 冻结 + R7 补充）**：
1. 同一 commit_id 可安全重放（覆盖写幂等）；
2. memoryRevision 每个正式提交**恰好**递增一次——以 committed 状态为准，meta 修复是"对齐到 target"而非"+1"；
3. 不允许提交日志、正式快照与 meta 互相矛盾却报告成功——恢复收敛后三者一致；确认接口返回前 meta 必已更新；
4. 不引入数据库事务；
5. **（R7）基线一致性**：任何提交进入执行前必须通过 `basedOn == 当前 memoryRevision` 锁内校验，不匹配拒绝且零落盘；
6. **（R7）链连续性**：恢复必须先验证提交链连续（basedOn→target 首尾相接、无分叉），断链/分叉视为数据损坏进安全态；meta 收敛只能以校验通过的连续链为依据，不得仅取最大值。

---

## 12. API 基线（本期仅契约，不实现）

### 12.1 `GET /api/memory`
200：`{ "workId": "work_001", "schemaVersion": 1, "memoryRevision": N, "initStatus": "…", "counts": { events, states, cognitions, threads, plans } }`；文件不存在→全 0 + `not_started`；500 读失败。

### 12.2 `GET /api/memory/characters/{id}`
`id` 过 `ValidID` 且 `c_` 前缀否则 400；200：`{ character, states(活跃), cognitions, events(参与事件摘要) }`；404。

### 12.3 `POST /api/memory/initialize`

| 前置/状态 | 返回 |
|---|---|
| legacy 未迁移 | 409 `project_not_migrated` |
| 知识不可用（全空） | 409 `knowledge_not_ready` |
| not_started / failed / partial | 202 `{initStatus:"mapping"}` |
| mapping / deep_extracting | 409 `already_running` |
| ready | **200** `{initStatus:"ready", alreadyInitialized:true}`（幂等重试，无副作用） |
| 输入损坏 | 启动后 failed（202 先返回，状态经 /status 查询） |

### 12.4 `GET /api/memory/initialize/status`
200：`{status, startedAt, finishedAt, lastError, preconditions, stats}`；500。

---

## 13. 已冻结决策与待确认事项

### 13.1 已冻结决策（不得重开）

1. StoryPlan 唯一正式计划模型；outline.json 仅导出投影
2. 先回填存量再启用记忆检索
3. Chapter.Revision 与 memory_revision 分离
4. 实体 ID 复用 `c_xxx`/`w_xxx`；实体注册表 = knowledge 本身（**候选提案队列不算第二套注册表**）
5. 正式提交采用可恢复日志协议（重放式）
6. ContextChars 语义不变；新增 `storyBudgetChars`
7. 快速试写可转正式草稿，必须重走检查确认
8. 标题实际变化也递增 Chapter.Revision
9. 单作品架构，URL 无 workId；work_id 恒 `work_001`
10. 认知一律走 Cognition；StoryState 不设认知类别
11. 本期分类单文件存储，整体覆盖写
12. 回填骨架 `participantsKnown=false`、Category=`general`
13. 证据失效只标 needsReview，不自动改 status/删改正文
14. Status 为各模型独立专用类型字段，MemoryMeta 不含 Status
15. 校验三层分层；持久化侧结构校验失败拒绝整个操作；ChangeSet 缺记录即提交失败
16. StoryOrder 为可重建派生字段，章节序号变化时写路径收尾强制重算
17. StoryState 唯一键 = `(subjectId, category, attribute)` 三元组
18. 事件参与者的"未识别/确认无"由 `participantsKnown` 区分
19. **（V1.2/R1）StoryEvent 增加 `proposed` 状态：无正文证据的事件以 proposed 合法持久化；仅 confirmed 强制证据；检索层只把 confirmed 当已确认事实**
20. **（V1.2/R2）StoryPlan 作用域 = `(level, parentPlanID)`；唯一性按作用域；父 cancelled/superseded 时 active 子计划自动 paused；第一期"同 parent 单 near_term"为显式产品约束**
21. **（V1.2/R3）歧义实体走 `entity_candidates.json` 提案队列（pending→register/merge/dismiss），确认前不注册正式实体、不进上下文**
22. **（V1.2/R4）CommitRecord 含 targetMemoryRevision；恢复覆盖六个崩溃点；meta 以"对齐到 target"收敛；commits.json 损坏进安全态**
23. **（V1.2/R5）初始化前置检查（迁移完成+知识可用）；零映射结果不得标 ready**
24. **（V1.2/R6）ready 后重复 POST initialize 幂等返回 200 无副作用；全量重初始化显式 400**
25. **（V1.2/R7）提交须锁内校验 `basedOn == 当前 memoryRevision`，不匹配拒绝（revision_conflict）且零落盘；恢复先校验提交链连续性，断链/分叉进安全态（commit_chain_broken），禁止仅取最大值收敛**
26. **（V1.2/R8）recordVersion 递增表为穷尽定义：needsReview/reviewReason、lastSource、confidence 等一切影响语义或覆盖保护的可持久化字段变化均 +1，且受 expectedRecordVersion 乐观校验**
27. **（V1.2/R9/R10）#3 不新增空壳 API：章节收尾钩子 = store 包级回调变量（签名携带 error）+ 锁外调用点，记忆模块（#7）注册且注册/读取互斥保护；回调返回 error 或 panic 仅记日志，不阻断、不回滚章节操作**
28. **（V1.2/R11）SaveChapter 无变化短路前仍以章节文件值修复 meta 缓存（覆盖"章节写成功、project.json 写失败"残留）；缓存修复不递增任何 Revision**

### 13.2 待确认事项（不阻塞 #3–#7 编码）

| # | 歧义 | 影响 | 初步建议 |
|---|---|---|---|
| 1 | 章节删除后事件处置（needsReview vs invalidated） | #7/#8 | 仅 needsReview，invalidated 留用户/reconcile |
| 2 | 章内事件时序 | 迭代 2 | 引入章内序号字段，本期不做 |
| 3 | StoryState.Value 枚举化 | 提取质量 | 本期自由文本 |
| 4 | 单文件规模阈值 | 长期性能 | >5000 条或 >5MB 再拆 |
| 5 | 全量重新初始化入口（显式参数/重置流程） | #8 | 本期 400 拒绝；删 memory 目录等效重置 |
| 6 | `storyBudgetChars` 默认值/上下限 | #3+ | 建议 6000/上限 40000 |
| 7 | 独立剧情线（storyline）作用域实体 | 迭代 3 | 本期 parentPlanID 近似；引入多剧情线时再评估独立实体 |

### 13.3 源码事实与冻结设计的冲突清单

无直接冲突。衔接变更点（#3 按 §7 执行）：① `LoadChapter/LoadChapters` 真源调整；② `SaveChapter` 无变化短路；③ 章节收尾钩子（R9：store 包级回调变量 + 锁外调用点，默认 nil 零开销起步）。

---

## 14. 关键字段与现有代码衔接说明

| 新结构/字段 | 衔接点 | 变更文件（#3–#8 时） |
|---|---|---|
| Chapter.Revision | `novel.Chapter` L14；`SaveChapter` L226–272；`LoadChapter/LoadChapters` L178–224 | novel/project.go、store/store.go |
| ChapterMeta.Revision | `novel.ChapterMeta`、`Chapter.Meta()` | novel/project.go |
| MemoryMeta/Origin（OriginType 专用类型） | 新文件，风格对齐 `NewID/ValidID/CountChars` | novel/memory.go（新） |
| 五类模型 + 专用 Status 类型 + Plan.ParentPlanID | 新文件；`Valid()` 与 `ValidID` 同风格 | novel/memory.go（新） |
| SourceRef/SliceByRune | rune 语义对齐 `CountChars` | novel/memory.go |
| MatchCharacter/MatchWorldEntry 导出 | `analysis.merge` 私有实现改调用 | novel/project.go、analysis/merge.go |
| EntityCandidate + entity_candidates.json | 新文件；产出方在迭代 2 | novel/memory.go、store/memory.go |
| memory 子系统 | `store.Store` 新增（全 *Locked）；**CommitAnalysis/FinishAnalysis 零改动** | store/store.go、store/memory.go（新） |
| init 状态机 + 前置检查 | store 新增；前置检查读 project/knowledge 现有加载路径 | store/memory.go |
| API | 路由表追加 4 条 | server/server.go、server/memory.go（新） |
| work_id 常量 | `const internalWorkID = "work_001"` | novel/memory.go |
| features.md F20–F23 | 语义改写为 StoryPlan（后续文档任务） | docs/features.md（后续） |

---

## 15. 验收用例（U1–U35）

| # | 用例 | Given / When / Then |
|---|---|---|
| U1 | 内容变化递增 | R=3 保存变化正文 → R=4、meta 同步、Analysis 回退 pending |
| U2 | 无变化不递增 | R=3 相同 Title+Content 保存 → R 仍 3、UpdatedAt 不变、Analysis 不回退、project.Revision 不变 |
| U3 | 标题变化递增 | 仅改 Title → R=4 |
| U4 | 旧数据兼容 | 无 revision 字段旧文件加载 → R=1；首次变化保存 → R=2 |
| U5 | meta 初始化 | 无 memory/ → GET /api/memory → meta.json `{schemaVersion:1, memoryRevision:0}` |
| U6 | 回填幂等 | 初始化完成后再触发（partial/failed 续跑）→ 记录数不变 |
| U7 | 人工修订保护 | manual_edit 的 StoryState + 重新回填 → Value 不被覆盖 |
| U8 | 实体 ID 复用 | knowledge.Characters 含 c_a1 → 回填 state.subjectId=="c_a1" |
| U9 | 证据过期 | sourceRef.revision=5，章节改至 R=6 校验 → needsReview=true、evidence_stale、status 不变 |
| U10 | quote 不一致 | quote ≠ SliceByRune → needsReview=true、quote_mismatch |
| U11 | rune 偏移 | 含 emoji 正文按 rune [s,e) 切片 == quote；byte 下标混用报错 |
| U12 | 锁所有权（评审项） | memory 提交方法锁内不得调公共 Store 方法（仅 *Locked）、无 LLM/网络 |
| U13 | 崩溃恢复（prepared） | prepared 记录 + 重启 → 重放完成、置 committed、meta 对齐 target |
| U14 | 幂等重放 | 已 committed 的 commit_id 再重放 → 数据不变、memoryRevision 不再 +1 |
| U15 | API 状态机 | mapping 中 POST → 409 already_running |
| U16 | 持久化拒绝静默丢弃 | 提交载荷含必填缺失记录 → L-S 拒绝整个操作、零写入、memoryRevision 不变 |
| U17 | StoryOrder 重算（钩子边界，R9） | 第 10 章前插入新章 + 记忆模块已注册 → 回调收到 oldIndexes/newIndexes 快照、storyOrder 重算留痕；**记忆模块未注册时回调为 nil、章节保存零额外开销、不受影响** |
| U18 | State 唯一键三元组 | (physical,injury) active 存在时写 (world,injury) → 共存互不替代 |
| U19 | participantsKnown 语义 | 回填骨架 false；深度提取确认无参与者 → true+空列表 |
| U20 | **（R1）回填骨架不入事实集** | 回填摘要事件 → status=proposed、needsReview=true，通过初始化；confirmed 集合计数为 0；proposed→confirmed 仅在补证据并通过校验后发生 |
| U21 | **（R2）多卷并行 active** | 两个不同 parent 的 volume 计划先后激活 → 均 active 共存 |
| U22 | **（R2）同作用域激活替代** | 同 parent 激活新 volume → 旧 active 自动 superseded（同一提交） |
| U23 | **（R2）父计划取消传播** | 父 volume cancelled → 其 active near_term 子计划自动 paused |
| U24 | **（R3）歧义候选不注册实体** | 提取遇 ambiguous → entity_candidates.json 落盘 status=pending；knowledge.Characters 数量不变；候选不进续写上下文 |
| U25 | **（R4）committed 后 meta 崩溃窗口** | 构造 committed 记录 + meta.memoryRevision=basedOn → 重启恢复 → memoryRevision 对齐 target，文件不重放 |
| U26 | **（R5）未迁移拦截** | legacy project.json（含 manuscript、无 chapters/）时 POST initialize → 409 project_not_migrated，零写入 |
| U27 | **（R5）知识未生成拦截** | chapters 存在但 knowledge 全空 → 409 knowledge_not_ready |
| U28 | **（R5）零结果不 ready** | 前置通过但映射产出 0 → partial + lastError=no_mappable_input，initStatus ≠ ready |
| U29 | **（R6）同值状态 no-op** | 同 (subject,category,attribute) 写入相同 Value → 不新建、不 superseded、recordVersion 不变 |
| U30 | **（R6）ready 幂等重试** | ready 后 POST initialize → 200 alreadyInitialized=true，memoryRevision 与文件均无变化 |
| U31 | **（R7）基线冲突拒绝** | 构造 basedOnMemoryRevision 过期的提交请求（≠ 当前 meta.memoryRevision）→ 拒绝 `revision_conflict`；零落盘（无新 CommitRecord、目标文件不变）；基于最新 revision 重建后重试成功 |
| U32 | **（R7）恢复断链安全态** | commits.json 含断链（basedOn 跳跃）或分叉（双 basedOn 均未 aborted / target 重复）→ 恢复进安全态、报告 commit_chain_broken、拒绝记忆写、memoryRevision 不收敛 |
| U33 | **（R8）标记字段变更递增** | reconcile 批量标注 needsReview=true → recordVersion +1 且 expectedRecordVersion 乐观校验生效；lastSource 写入变化同理；重复写入相同标记（同值）不递增 |
| U34 | **（R9/R10）回调失败不阻断** | 注册的 chapterOrderHook 返回 error 或 panic → 章节保存仍成功返回，错误仅入日志；下次重算时记忆侧自愈 |
| U35 | **（R11）短路路径修复过期 meta** | 章节文件新（R2）、meta 旧（模拟 project.json 写失败残留）→ 以文件相同内容保存走短路 → 返回文件真值、meta 被修复为 R2，所有 Revision 不递增 |

---

## 16. 修改文件清单与声明

**本次修改/新增文件**：仅 `docs/memory_model_spec.md`（V1.2 定点修订稿）。
**声明：本次仅修改设计文档，未修改业务代码**——`internal/`、`main.go`、`web/`、`data/` 零改动。

---

## 17. 修改记录

### 17.1 V1.0 → V1.1（12 项，对应首轮审查）

| # | 类型 | 修订内容 | 对应意见 |
|---|---|---|---|
| 1 | P0 | Status 拆出 MemoryMeta：五类模型独立 Status 字段 + 专用状态类型（各带 Valid()） | P0-1 |
| 2 | P0 | 校验三层分层（L-N/L-S/L-V）；废除三项静默行为；持久化边界冻结 | P0-2 |
| 3 | P0 | StoryOrder 声明为可重建派生字段 + 重算留痕 | P0-3 |
| 4–10 | 明确 | OriginType 专用类型；recordVersion 递增判定；ChapterID=主来源快捷字段；ParticipantsKnown；证据变更↔状态转换对照表；manual_edit 冲突提案协议；State 唯一键三元组 | 表格 6 项 |
| 11–12 | 用例/结构 | 新增 U16–U19；冻结决策追加 14–18 | — |

### 17.2 V1.1 → V1.2（R1–R6 定点修订，对应整体审查）

| # | 严重度 | 修订内容 | 落点章节 |
|---|---|---|---|
| R1 | P0 | **StoryEvent 新增 `proposed` 状态**：无正文证据的事件（回填骨架）以 proposed 合法持久化；confirmed 强制 sourceRefs（仅事件）；proposed→confirmed 需证据过校验；confirmed→proposed 禁止；检索层区分事实/候选；State 证据要求改为"无证据须 needsReview"（不强制 active 有证据）；回填映射表同步（骨架 status=proposed）；新增 U20 | §3.2、§4.1.6、§4.2、§4.7、§5.1、§9.1 |
| R2 | P0 | **StoryPlan 作用域模型**：新增 `ParentPlanID`；唯一性改为按 `(level, parentPlanID)` 作用域；L-S 校验父层级；激活替代规则限定作用域内；父 cancelled/superseded→active 子自动 paused、completed 不自动变；第一期"同 parent 单 near_term"显式为产品约束；新增 U21–U23 | §4.5、§6.1 |
| R3 | P0 | **歧义实体候选**：新增 `EntityCandidate` + `entity_candidates.json`；pending→register/merge/dismiss；确认前不注册正式实体、不进上下文；确认属记忆提交；产出方（Extractor）归迭代 2，本期冻结结构；新增 U24 | §2、§6.3、§8.1、§8.3、§13.1 |
| R4 | P0 | **提交协议崩溃恢复闭环**：CommitRecord 增 `targetMemoryRevision`；六崩溃点恢复矩阵（含"committed 后 meta 未更新"窗口=核对/修复 meta 对齐）；恢复算法与不变量（对齐而非+1）；commits.json 持久化方式与损坏安全态；新增 U25 | §8.3、§11 |
| R5 | P1 | **初始化前置检查**：三步检查（迁移完成/知识可用/可解码）+ 409 错误码；部分可用允许部分映射；零映射结果强制 partial（no_mappable_input），禁止零结果 ready；init_state 增 preconditions；新增 U26–U28 | §9.0、§9.2、§12.3 |
| R6 | P1 | **次要一致性**：§6.1 SubjectID 行重写（c_ 或 w_）；同值状态写入 no-op（§4.2，U29）；#3 钩子边界（§4.1.4/§7——#3 只定义调用点与接口，重算实现归 #7+，U17 更新）；三种重复请求语义分开（§9.3/§12.3——ready 重试 200 幂等无副作用、全量重初始化 400，U30）；冻结决策追加 19–24；待确认追加 #7（storyline 实体） | 多处 |
| R7 | P1 | **提交协议 revision 冲突闭环（复审补齐）**：提交前锁内校验 `basedOn == meta.memoryRevision`，不匹配拒绝 `revision_conflict` 且零落盘；恢复改为两阶段（先验链后收敛）——断链/分叉进安全态 `commit_chain_broken`，禁止仅取最大值收敛；不变量追加 5–6 | §11.2、§11.3 |
| R8 | P1 | **recordVersion 递增判定穷尽化（复审补齐）**：递增表声明为穷尽定义；补齐 needsReview/reviewReason、lastSource、confidence、RelatedEntities、EventType、EventLink 变化均 +1，且受 expectedRecordVersion 乐观校验；同值重复写入不递增 | §3.1、§3.3 |
| R9 | P2 | **章节收尾钩子契约具体化（复审补齐）**：废弃"Store 方法形可替换扩展点"表述，改为 store 包级回调变量 `onChapterOrderChanged` + 锁释放后调用点；定义注册方（#7）、生命周期（一次性赋值、本期无反注册）、失败处理（仅记日志，不阻断/不回滚章节操作）、死锁防护（回调不得调用 Store 公共加锁方法）；U17 更新 | §4.1.4、§7、§13.3 |
| R10 | P1 | **回调错误契约统一（#3 实现审查）**：回调签名改为携带 error 返回（`func(oldIndexes, newIndexes map[string]int) error`），调用方对返回 error 与 panic 统一仅记日志不阻断，与 U34 验收要求对齐 | §4.1.4、§7、U34 |
| R11 | P2 | **短路路径过期 meta 修复 + 注册并发保护（#3 实现审查）**：SaveChapter 无变化短路前以文件值修复 meta 缓存（冻结决策 28，U35）；钩子注册/读取经 sync.RWMutex 互斥保护，注册约定限启动阶段 | §4.1.4、§7、§13.1 |

**当前状态：V1.2（含复审补齐 R7–R9）已冻结。进入 #3 Chapter.Revision。**
