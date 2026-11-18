# issues #60 根因定位：经深度提取采纳的伏笔为何恒不进注入

> 日期：2026-10-24｜隔离环境：真实产品二进制 `output/novelagent-g6.exe`（工作区 HEAD + 并行 agent 未提交改动，本轮 build/vet/test 全绿）+ 端口 8097 + 独立数据目录 `output/e2e-60-thread-gate-*`（每个变体一份 `output/e2e-g6-rec-base` 的副本）。**无模型密钥**：`POST /api/continue/preview` 与 SSE `meta` 的注入读数等价已由 issues #57 实测，注入度量不需要模型。
> 台账：[task/e2e-stub-chain-acceptance.md](../../../../task/e2e-stub-chain-acceptance.md) §10、[docs/issues.md](../../issues.md) #60、[task/roadmap.md](../../../../task/roadmap.md) 风险台账。
> 可重放件：本目录下 `e2e_60_thread_gate.mjs`、`e2e-60-thread-gate-summary.json`。

## 1. 结论（一句话）

**提取通道产出的 `StoryThread` 结构上不可能通过检索的相关性门槛**：`add_thread` 的记录只带 `title / knownClues / status`，**从不填 `involvedCharacters`，也不填 `question`**，而检索层判定「这条伏笔与本次续写相关」只有三条路——标题原文出现在近期正文窗口、问题原文出现在近期正文窗口、关联角色与角色种子相交（[internal/store/memory_retrieve.go:182](../../../../internal/store/memory_retrieve.go#L182)）。第三条路对提取记录恒假，前两条要求把「概括性标题」当字面串在正文里找到，实测不成立。**这不是环境问题，也不是容量问题**（伏笔候选上限 5 条、库里只有 1 条；伏笔子预算 600 字、实测注入 174/176 字远未触顶）；夹具伏笔之所以进得去，是因为手写记录里带了 `involvedCharacters:["c_lin"]`。

## 2. 方法（三步，缺一不可）

1. **代码侧先确定门槛与产出面**：检索门槛 `memory_retrieve.go:177-204`（顺序 = 状态 → 相关性 → `needsReview`）；提取产出 `internal/analysis/extract.go:395-478`（record 只含 `MemoryMeta/Title/KnownClues/Status/ResolutionEvent`）；提取 DTO `internal/analysis/extract_prompt.go:119-124`（`extractThread = {title, action, clue, quote}`，**没有主体/角色字段**）。对照：事件有 `participants → resolveParticipants`、状态/认知有 `subject → resolveSubject`、计划有 `resolvePlanCharacters`（[internal/plan/generate.go:209](../../../../internal/plan/generate.go#L209)）——**实体解析在其它四类推演里都存在，唯独伏笔没有**。
2. **真实产出记录取证**：两轮真实 HTTP 链路落盘的 `threads.json` 都是同一形状（无 `involvedCharacters`、无 `question`）——`output/e2e-stub-data/memory/threads.json`（`thread_83ec6a9c…`，引文已定位）与 `output/e2e-g6-rec-base/memory/threads.json`（`thread_7a0ae82d…`，`evidence_unlocated`）。并静态确认标题字面串 `青玉符的来历` 在全部 9 章正文里 **0 次出现**。
3. **单变量活体判别**：5 个变体各起一个新进程（避免 store 缓存把磁盘改动掩掉），每个变体只改 `threads.json` 的一个字段，取同一方向的预览读数；同时记录启动期是否发生 `reconcile`（`churnedDuringRun` 全为 false）与 `safeState`（全为 false），保证读数是同一口径。**磁盘改动只用于判别门槛，不是产品写入路径的产物**——产品侧「字段没被填」由第 1、2 步证明。

## 3. 判据表（`used` = 记忆注入字数，`skippedReview` = 检索候选里被待复核挡掉的条数）

| 变体 | 改了哪一个变量 | `recalled.threads` | `skippedReview` | `used` | 注入里的伏笔档 |
|---|---|---|---|---|---|
| V0 原样（真实提取形状，`needsReview=true`） | — | **0** | 3 | 145 | 无 |
| V1 标题改为正文里的字面串 | 相关性 → 满足 | **0** | **4** | 145 | 无 |
| V2 标题字面串 + 清 `needsReview` | 复核 → 放行 | **1** | 3 | **176**（+31） | `- 沈青禾说这印属于｜已知线索：青玉符上的刻痕` |
| V3 原标题不动 + 清 `needsReview` + 加 `involvedCharacters:[林砚]` | 实体链接 → 补上 | **1** | 3 | **174**（+29） | `- 青玉符的来历｜已知线索：青玉符上的刻痕` |
| V4 原标题不动 + 只清 `needsReview` | 复核放行、实体仍缺 | **0** | 3 | 145 | 无 |

读表（每一格都只由一个变量决定）：

- **V1 与 V0 的差别**：`skippedReview 3→4` 且注入不变 → 相关性一旦满足，记录就进到复核门前并被**计数**；反过来看，V0 的那条伏笔在 V0 里连计数都没有，说明它是在**相关性这一步**就被丢掉，`needsReview` 根本没轮到。
- **V4 是决定性对照**：把 `needsReview` 清掉、其余不动，注入依旧是 0 → **「复核标记」不是根因**，概括性标题走不通字面串这条路。
- **V3 是修复方向的实证**：标题保持原样（概括语），只补一个真实存在的角色 ID（`c_9c9d6a711975f878fd` 林砚，V0 预览的 `seeds` 里 score 10.4 / `recent_mention`），伏笔档立刻出现，注入 145→174 字。夹具轮能被注入正是这个原因。
- **V2 顺带暴露一个语义问题**：走标题这条路时，注入档的正文变成从章节里**整段抄来的句子**（`沈青禾说这印属于…`），即「标题=正文字面串」这条通道即使打通，注入的也不是「未回收的债」的描述。

## 4. 附带确认（同族可观测性问题，不另开条目）

- **静默丢掉不留任何信号**：五个变体的 `memoryDisabledReason` 全是空（字段直接缺席），V0/V4 的伏笔既不在注入里、也不在 `skippedReview` 里 → 与 #61（全部候选被停投时无 reason）、#55（低 ctx 归零无 reason）、#58（计划停投无原因）同族；本条更隐蔽，因为**连「被筛掉」这件事都没有计数器**。
- **人工也无法补救**：`internal/server` 里没有任何 `StoryThread` 引用，记忆侧 API 只有 `GET /api/memory`、`initialize(/status)`、`characters/{id}`、`changesets*`、`candidates*`、`reconcile`（[internal/server/memory.go:16-19](../../../../internal/server/memory.go#L16-L19)、[internal/server/memory_review.go:20-29](../../../../internal/server/memory_review.go#L20-L29)）——**没有编辑记忆记录的入口**，审阅者不能给伏笔补角色，所以这条记录一旦产出就永久不进注入。
- **`needsReview` 是第二道闸**：`evidence_unlocated`（提取引文没定位到）与 `evidence_stale`（改章后失效）都会把伏笔压到「即使相关也不注入」；本轮 V1 实测该档会计入 `skippedReview`，所以只要相关性先过，这一层至少可观测。

## 5. 明确不做

- 不改任何产品代码（`extract.go` 补字段、`memory_retrieve.go` 加回退路线、server 加编辑接口都要 yj 拍板，且并行 agent 正在改同一批文件）。
- 不宣称 G2/G3 结论变化：本轮只把 #60 从「已复现未定位」推进到「根因定位到具体字段与具体行」，提取质量与文学质量一侧仍无密钥、零结论。
- 不做真实模型验证：桩产出的标题/线索本就是自撰的，但**门槛判定与产出字段缺失与模型无关**（DTO 里根本没有那个字段，模型无从提供）。
- 不碰 `data/`；不引入密钥；不自动提交。

## 6. 待拍板（修法三选一，代价不同）

1. **提取侧补实体链接**（与事件/状态/计划一致的做法）：`extractThread` 增加主体/角色字段并在装配时走已有的 `resolveEntity`/`resolveParticipants`。语义最正，代价是提示词与解析器要动，且会新增 `entity_candidates` 歧义分支。
2. **检索侧加回退路线**：相关性判定接受 `knownClues` 字面串，或按 `origin.chapterId` 的那批事件参与者继承角色集。不改提取管线、代价最小；但第 2 种继承会把「同章的人」都当成「这条伏笔的人」，相关性会变宽。
3. **审阅侧补编辑入口**：给记忆记录加最小改写接口（至少能挂角色）。这条同时解掉「人工无法补救」，但属产品能力扩展，不是缺陷修复。

无论选哪条，**#61 的「被筛掉无计数」都建议一并处理**：至少在预览/SSE 里给一个 `droppedByRelevance`，否则修完也无法证明修好了。
