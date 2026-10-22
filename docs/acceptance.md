# 迭代 1（V1.0 记忆基础设施）验收报告

> 基准：`docs/memory_model_spec.md` V1.2 冻结版（冻结决策 28 条 / 验收用例 U1–U35）。
> 发布对象：**V1.0 记忆基础设施**，不是原始《详设.md》全量交付。当前执行结果及发布结论见 [发布验收报告](v1.0_release_acceptance_20261022.md)。
> 状态：✅ 通过该行限定行为；◐ 部分覆盖（不得计为完整通过）；➖ 后续未交付。不存在“部分覆盖也算全部闭环”的判定。
> 原始详设中的事实提取、草稿隔离、联合确认提交及近期规划仍是后续目标，不能因本期范围收敛而删除。历轮复审报告保留原提交时点的判断，本表更新 D01/D02 后的发布范围。

## 一、U 用例覆盖矩阵

| 用例 | 状态 | 覆盖测试 |
|---|---|---|
| U1 内容变化递增 | ✅ | store/revision_test.go `TestSaveChapterChangeIncrements` |
| U2 无变化不递增 | ✅ | store/revision_test.go `TestSaveChapterNoOpShortCircuit` |
| U3 标题变化递增 | ✅ | 同 U1（第二段断言） |
| U4 旧数据兼容 | ✅ | store/revision_test.go `TestSaveChapterLegacyRevisionCompat` |
| U5 meta 初始化 | ✅ | store/memory_test.go `TestMemoryMetaInitializedOnFirstAccess` + server/memory_test.go `TestMemoryOverviewEmpty` |
| U6 回填幂等 | ✅ | store/memory_init_test.go `TestInitIdempotentRerun` |
| U7 人工修订保护 | ✅ | store/memory_init_test.go `TestInitManualEditProtected` |
| U8 实体 ID 复用 | ✅ | store/memory_init_test.go `TestInitBackfillMapping`（subjectId=="c_a1"） |
| U9 证据过期（L-V） | ◐ | 辅助函数只核对正文与 quote/偏移，不接收当前章节 revision、不写 evidence_stale/needsReview；完整版本失效检查与入口后续交付 |
| U10 quote 不一致 | ✅ | 辅助函数层：novel/memory_test.go `TestCheckSourceRefAgainstContent`；不表示完整提取/提交路径已交付 |
| U11 rune 偏移 | ✅ | 辅助函数层：novel/memory_test.go `TestSliceByRune` |
| U12 锁所有权 | ✅ | 限定为已审查路径：memoryMu 独立锁；允许 store.mu→memoryMu，禁止反序；锁内无 LLM/网络；原锁归属探针与并发读/导入回归通过，race detector 未执行 |
| U13 崩溃恢复 prepared | ✅ | store/memory_test.go `TestMemoryRecoveryPrepared` |
| U14 幂等重放 | ✅ | store/memory_test.go `TestMemoryRecoveryIdempotent` |
| U15 API 状态机 | ✅ | store/memory_init_test.go `TestInitAlreadyRunning` + server `TestMemoryInitializeStateMachine` |
| U16 持久化拒绝静默丢弃 | ◐ | 回填侧单条 L-S 跳过已测；"整个操作拒绝"针对迭代 2 ChangeSet 提交 |
| U17 StoryOrder 重算钩子 | ✅ | store/revision_test.go（钩子边界×3）+ memory_init_test.go `TestStoryOrderRecalcOnDelete` |
| U18 State 唯一键三元组 | ◐ | 唯一键/同值跳过逻辑已实现（回填路径）；跨类别共存写入口在迭代 2 |
| U19 participantsKnown | ◐ | 回填恒 false 已测（U20）；"确认无=true+空列表"在迭代 2 |
| U20 回填骨架不入事实集 | ✅ | store/memory_init_test.go `TestInitBackfillMapping`（proposed/needsReview/confidence 0.3） |
| U21 多作用域并行 active | ✅ | 辅助校验层：novel/memory_test.go `TestValidateStoryPlanScopes`；不表示计划业务入口已交付 |
| U22 同作用域激活替代 | ◐ | 仅 duplicate active 拒绝校验通过；自动 superseded 及激活写入口未交付，不能判完整通过 |
| U23 父计划取消传播 | ➖ | 规则冻结于 §4.5；传播实现及写入口后续交付 |
| U24 歧义候选不注册实体 | ◐ | `EntityCandidate` 结构+存储冻结（novel/memory.go）；产出方/确认在迭代 2；analysis 归并已改 ambiguous 不自动合并 |
| U25 committed 后 meta 崩溃窗口 | ✅ | store/memory_test.go `TestMemoryRecoveryCommittedMetaWindow` |
| U26 未迁移拦截 | ✅ | store/memory_init_test.go `TestInitLegacyProjectRejected` |
| U27 知识未生成拦截 | ✅ | store/memory_init_test.go `TestInitKnowledgeNotReady` |
| U28 零结果不 ready | ✅ | store/memory_init_test.go `TestInitZeroResultPartial` |
| U29 同值状态 no-op | ◐ | 回填路径同值跳过已实现；显式写入口在迭代 2 |
| U30 ready 幂等重试 | ✅ | `TestInitReadyIdempotentRetry`、`TestMemoryInitializeStateMachine`、`TestMemoryInitializeRequestContract`；空体/空对象/false 幂等，true 显式 400，拒绝与幂等前后数据文件哈希不变 |
| U31 基线冲突拒绝 | ✅ | store/memory_test.go `TestMemoryCommitBaselineConflict` |
| U32 恢复断链安全态 | ✅ | store/memory_test.go `TestMemorySafeStateOnBrokenChain` + `TestMemorySafeStateOnCorruptCommits` |
| U33 标记字段变更递增 | ◐ | `TestStoryOrderRecalcOnDelete` 仅证明 needsReview 变化 +1、纯重排不递增；所有语义字段变更与 expectedRecordVersion 乐观校验入口未交付 |
| U34 回调失败不阻断 | ✅ | store/revision_test.go `TestChapterOrderHookFailureNotBlocking` + `TestChapterOrderHookErrorNotBlocking` |
| U35 短路修复过期 meta | ✅ | store/revision_test.go `TestSaveChapterNoOpRepairsStaleMeta` |

## 二、端到端集成冒烟

| 场景 | 测试 |
|---|---|
| legacy 迁移 → 知识 → 回填 → 概览/角色聚合 全链路 | store/memory_init_test.go `TestLegacyToEndMemoryFlow` |
| API 层：409 → 202 → 200 幂等 → /status → characters 400/404/200 | server/memory_test.go 三个用例 |
| 章节删除联动记忆重算（跨模块） | `TestStoryOrderRecalcOnDelete` |

## 三、范围说明（迭代 2 去向）

- 后续目标：深度提取（U9/U19）、ChangeSet 正式提交与整体拒绝（U16/U18/U29/U33）、计划状态变更（U22/U23）、歧义候选产出与确认（U24）。冻结结构与契约不等于实现或验收通过。
- 本期交付：章节/记忆版本、基础模型校验和实体复用、低成本候选事件/人物状态回填、initialize/reconcile 日志与恢复、章节删除联动、四个基础记忆 API、换书隔离及异步任务失效。
- 本期未交付：正文深度提取与事实确认、Draft/ChangeSet、正文+记忆联合确认、记忆 Retriever、认知/伏笔业务、StoryPlan 生成与激活/传播/续写回灌、历史依赖修复及全量重初始化。
- 当前回归只证明列出的用例和故障窗口；race、长篇模型质量及完整 UI 操作不在本次发布契约复测覆盖内，详见发布报告。原始详设的全量验收仍未完成。
