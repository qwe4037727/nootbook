# 迭代 3：续写记忆注入（G3）验收记录

状态：**功能代码完成、自动化用例全绿；I3-14/I3-15 未执行（真实模型通道阻断）**。因此 G3 不能宣告完整交付，“长程一致性可观测提升”仍缺真实模型证据（2026-10-23）。I3-6/I3-7（R1/R2 增补）已于同日实现并自动化验证（I3-16/I3-17）。

授权：用户要求“按顺序完成 G1–G8”，本任务承接 G3；实现依据 `task/iter3-详设.md` §3–§9 与 §10 验收表。

允许改动范围：`internal/novel/{memory_context,mention}.go`、`internal/store/memory_retrieve.go`、`internal/agent/{prompt.go,memory_render_test.go}`、`internal/server/{server.go,memory_preview.go,continue_test.go}`、`web/{index.html,app.js,style.css}`、`internal/agent/testdata/`、本任务及配套记录。未改动用户 `data/`，未提交/推送。

## 1. 实现摘要

1. **检索层（I3-1）**：新增 `novel.ContinuationMemory/MemorySeed/MemoryRecord` 与共享提及打分（`novel/mention.go`）；`store.RetrieveContinuationMemory` 只持 `memoryMu`，按 P0 状态/世界规则、事件（StoryOrder 近因 + 上限 6）、未解决伏笔、错误认知、计划五类召回，只取 `active/confirmed` 且非 `needsReview`，歧义提及两个主体都不注入；所有排序带 ID 兜底，输出确定性。
2. **预算与渲染（I3-2/I3-3）**：`memoryBudget = clamp(ContextChars/4, 800, 3000)`，`ContextChars ≤ 1600` 时为 0；正文窗口 = `ContextChars − memoryBudget`。`agent.Build` 在【相关设定】与【前情提要】之间插入【已确认剧情记忆】，仅在有注入时追加系统规则 3.1（与最近正文冲突以正文为准）。零记忆输出与迭代 2 golden 逐字节一致。
3. **续写接线与预览（I3-4）**：`POST /api/continue/preview` 只读预览（不建 LLM 连接、不落盘、不改 revision），严格 JSON（单对象、未知字段/尾随 JSON/类型错误 400，>4096B 413，空体合法）；`/api/continue` 支持 `useMemory`（缺省 true），SSE `meta` 增加 `memoryEnabled/memoryRevision/memoryBudgetChars/memoryUsedChars/seeds/recalledByKind/droppedByBudget/skippedReview/skippedAmbiguous/memoryDisabledReason`；记忆读取失败只记日志，绝不阻断续写。
4. **前端（I3-5）**：续写面板增加“记忆注入”开关与“预览本次记忆”；预览按五类分组展示条目、`needsReview` 标记与记录 ID，底部展示 used/budget、召回数、预算裁掉、待复核跳过、歧义提及跳过与 revision；生成前静默刷新预览，SSE meta 摘要写入状态栏，`safe_state/read_error` 额外 toast；关闭开关立即隐藏面板且请求体携带 `useMemory:false`。
5. **截断可见性（I3-6/R1）**：`llm.Result{Content,FinishReason}` + `IsTruncated`（`length|max_tokens|max_output_tokens`，其余不推断）；流式取最后一个非空 `finish_reason`，非流式取 `choices[0]`。章节分析/总览/草稿提取/计划生成的解析失败按截断分流为专属文案；`/api/continue` 的 SSE `done` 增加 `truncated`，前端状态行与 toast 提示“输出达到模型长度上限，已截断”。重试语义不变，截断结果按成功返回、不自动重试。
6. **全量预算计量（I3-7/R2）**：`Prompt.TotalChars = system + user`，`Sections` 覆盖 overview/characters/world/memories/plans/summaries/manuscript（含段标题与换行）；`ContextChars` 仍是正文预算，语义不变。超预算按冻结顺位收缩：角色字段 Notes→Appearance→Personality→Relationships → 世界别名 → 低分角色 → 低分设定 → 最早摘要 → 记忆 P1 事件 → P1 伏笔 → 正文尾窗（硬下限 `ctx/2`，行界裁剪后不足时自动补足）→ 仍不满足才 `OverBudget=true`。SSE `meta` 增加 `promptTotalChars/promptOverBudget/promptSections`，预览增加 `promptTotalChars/sections/promptOverBudget`；`memoryUsedChars`/预览 `used` 改为报告**实际注入**的记忆段 rune 数（全量收缩可能再裁掉记录）。
   > **2026-10-24 更正**：本条写就时 R4 尚未存在，「`ContextChars` 仍是正文预算，语义不变」与 `Sections` 清单都已过时。现状以代码为准（`internal/agent/prompt.go:152-164`）：`recallBudget = min(800, ctx/4)`（`ctx ≤ 1600` 或不满 20 章时为 0），`manuscriptWindow = ctx − memoryBudget − recallBudget`；`Sections` 另有 `recall` 一档；召回零命中时预算归还正文窗。完整口径见 `task/g8-acceptance.md` §1.6/§4.4 与 `docs/issues.md` #46/#50/#51。

## 2. 自测方法

```
go build ./...
go vet ./...
go test ./... -count=1
go test -tags acceptance_review ./... -count=2
```

结果（2026-10-23）：

- `go build ./...`、`go vet ./...` 无诊断。
- `go test ./... -count=1`：agent/analysis/llm/novel/server/store 6 包全 `ok`。
- `go test -tags acceptance_review ./... -count=2`：6 包全 `ok`，无 FAIL（连续两轮）。
- 前端：启动真实服务 + 临时 mock SSE provider，在 Qoder 内置浏览器用 DOM 脚本验证：预览面板分组/ID/底部统计、开关隐藏与 toast、带记忆提示词含记忆段与规则 3.1、`useMemory:false` 提示词两者皆无、mock 生成端到端返回草稿。临时数据目录与脚本已删除，未留存截图（内置浏览器截图接口在该环境不可用）。
- I3-6/I3-7 增补后复跑（2026-10-23）：`go build ./...`、`go vet ./...` 无诊断；`go test ./... -count=1` 与 `go test -tags acceptance_review ./... -count=2` 均 6 包全 `ok`。R1 覆盖 `internal/llm`（`IsTruncated`、流式/非流式 `finish_reason`）、`internal/analysis`（截断 vs 格式异常文案分流）、`internal/server`（`done.truncated` 四态：`length/max_tokens/stop/缺失`）；R2 覆盖 `internal/agent` 四档预算守卫 + `OverBudget` 边界 + 固定开销跨档一致，以及 `internal/server` 的 meta/预览计量字段（含 `useMemory=false`）。

## 3. 验收用例映射

| # | 用例 | 自动化覆盖 | 结论 |
|---|---|---|---|
| I3-01 | 零记忆基线一致 | `TestBuildGoldenBaselineWithoutMemory`；`TestContinuePreviewDegradations/memory_absent` | ✅ 通过 |
| I3-02 | P0 状态注入 | `TestRetrieveP0StatesWithRedLines`（含 `attribute：value` 渲染） | ✅ 通过 |
| I3-03 | needsReview 红线 | `TestRetrieveP0StatesWithRedLines`、`TestRetrieveEventsRecentFirstWithCap`、`TestRetrieveThreadsCognitionsAndPlans`（`skippedReview` 计数） | ✅ 通过 |
| I3-04 | proposed/失效不入事实集 | `TestRetrieveP0StatesWithRedLines`（superseded）、`TestRetrieveEventsRecentFirstWithCap`（proposed/invalidated）、`TestRetrieveThreadsCognitionsAndPlans`（resolved/abandoned/forgotten） | ✅ 通过 |
| I3-05 | 事件按 StoryOrder 近因 | `TestRetrieveEventsRecentFirstWithCap`（8 条 confirmed 截 6，保留 7..2） | ✅ 通过 |
| I3-06 | 歧义别名不注入 | `TestRetrieveAmbiguousAliasSkipsBothSeeds` | ✅ 通过 |
| I3-07 | 预算不变式 | `TestBuildMemoryBudgetWindows`（12000→3000/9000，窗口 ≤9000）、`TestRetrieveBudgetTrimAndDeterminism`（50 条：used≤3000、dropped>0） | ✅ 通过 |
| I3-08 | 小窗口退化 | `TestBuildMemoryBudgetWindows`（≤1600→0，带记录仍与 golden 逐字节一致）、`TestRetrieveDegradations/budget_zero_skips_everything`（不读文件） | ✅ 通过 |
| I3-09 | 确定性 | `TestRetrieveBudgetTrimAndDeterminism`（两次 JSON 与记录逐字节/深度相等） | ✅ 通过 |
| I3-10 | safeState 降级 | `TestContinuePreviewDegradations/safe_state`、`TestContinueSafeStateDoesNotBlock`（续写 200、零注入、provider 仍被调用） | ✅ 通过 |
| I3-11 | 五类文件损坏 | `TestContinuePreviewDegradations/read_error/{states,events,threads,cognitions,plans}.json`（逐一损坏）、`TestRetrieveDegradations/read_error_does_not_block`、`TestContinueInjectsMemoryIntoPromptAndMeta`（损坏后续写仍 200 并调用模型） | ✅ 通过 |
| I3-12 | 预览接口契约 | `TestContinuePreviewContract`（空体、严格 JSON、413、只读文件哈希、`useMemory=false`） | ✅ 通过 |
| I3-13 | 关闭开关 | `TestContinuePreviewContract`、`TestContinuePreviewDegradations/read_error/*`、`TestContinueInjectsMemoryIntoPromptAndMeta`（损坏文件 + `useMemory:false` 不读记忆） | ✅ 通过 |
| I3-14 | systemPrompt 优先级 | 规则存在性由 `TestBuildInjectsMemorySectionAndRule` 断言；**“产出正文不复述被否决记忆”需人评** | ⚠️ 未执行（待真实模型） |
| I3-15 | 质量对照 | 需同一停点开/关记忆各生成一次并留证 `docs/acceptance-evidence/` | ⚠️ 未执行（上游网关 F-UPSTREAM-01） |
| I3-16 | 截断可见性 | `TestIsTruncated`、`TestStreamChatReportsFinishReason`、`TestCompleteChatReportsFinishReason`；`TestContinueReportsTruncatedOutput`（`length/max_tokens` → true，`stop/缺失` → false，均 200 且 `chars>0`）；`TestManagerReportsTruncatedOutput`（截断→“模型输出被截断”，普通坏 JSON→“分析结果格式异常”） | ✅ 通过 |
| I3-17 | 全量预算守卫 | `TestBuildFullBudgetInvariants`（ctx 1600/4000/12000/30000：`user≤ctx`、`sum(Sections)+固定文本+system=TotalChars`、正文≥ctx/2、1600 零记忆；`over_budget` 子用例证明收缩到底仍超限才置真）；`TestBuildFixedOverheadStableAcrossContexts`；`TestContinuePreviewContract`/`TestContinueInjectsMemoryIntoPromptAndMeta` 校验 meta 与预览计量（`promptSections.memories == 实际注入 used`） | ✅ 通过 |

## 4. 未决与边界

1. **I3-14/I3-15 未执行**：本机上游网关不可用（F-UPSTREAM-01），无法完成真实模型人评与开/关质量对照；G3 的“长程一致性可观测提升”尚无真实证据，不能标记完成。
2. **`ContextChars` 语义变更**：相同配置下正文窗口减少最多 25%（12000→9000），已在 UI 文案说明并记入 `docs/issues.md` #29；真实观感变化需在有模型通道后复核。
3. **提取质量前置**：迭代 2 的深度提取尚未做真实语料验收（B1 未落地），记忆注入可能放大抽取错误；当前以 confirmed/active/非 needsReview 红线与预览接口先行暴露内容缓解，最终效果依赖 I3-15。
4. **全量预算的收缩代价**：I3-7 落地后，知识条目/摘要/记忆在极端配置下会让位于正文尾窗（冻结顺位），因此“注入了多少知识”可能少于按条数选择的上限；`OverBudget` 仅在正文降到底限仍放不下时置真。既有配置 30000 字下若出现预算告警，应优先减少知识条目或调低 `ContextChars`，已记入 `docs/issues.md` #40。
5. **计量口径调整**：`memoryUsedChars` 与预览 `used` 从“检索阶段裁剪后大小”改为“实际注入段 rune 数”，保证与 `promptSections.memories` 一致；检索 DTO 的 `ContinuationMemory.UsedChars` 语义不变，仍供 store 层断言使用（#40）。
6. **截断判定的边界**：仅信任上游 `finish_reason`，不因 JSON 解析失败反推截断；缺失该字段时 `truncated=false`（详设 §11 风险 7）。部分网关不返回该字段时，截断提示会缺席，但不会误报。
