# 迭代 1（V1.0 记忆基础设施）验收报告

> 基准：`docs/memory_model_spec.md` V1.2 冻结版（冻结决策 28 条 / 验收用例 U1–U35）。
> 结论：**U1–U35 全部闭环**——已实现 ✅ / 本期部分覆盖（完整入口在迭代 2）◐ / 显式超出本期范围 ➖（附去向）。
> 验证命令：`go build ./...`、`go vet ./...`、`go test ./... -count=1` 全绿（2026-10-22）。

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
| U9 证据过期（L-V） | ◐ | 辅助函数 `CheckSourceRefAgainstContent` 已实现并测（novel/memory_test.go）；sourceRef 校验入口在迭代 2 深度提取 |
| U10 quote 不一致 | ✅ | novel/memory_test.go `TestCheckSourceRefAgainstContent` |
| U11 rune 偏移 | ✅ | novel/memory_test.go `TestSliceByRune` |
| U12 锁所有权 | ✅ | 结构性保证：全 `*Locked` 内部方法 + memoryMu 独立锁 + 两锁不嵌套（代码评审通过；锁内无 LLM/网络） |
| U13 崩溃恢复 prepared | ✅ | store/memory_test.go `TestMemoryRecoveryPrepared` |
| U14 幂等重放 | ✅ | store/memory_test.go `TestMemoryRecoveryIdempotent` |
| U15 API 状态机 | ✅ | store/memory_init_test.go `TestInitAlreadyRunning` + server `TestMemoryInitializeStateMachine` |
| U16 持久化拒绝静默丢弃 | ◐ | 回填侧单条 L-S 跳过已测；"整个操作拒绝"针对迭代 2 ChangeSet 提交 |
| U17 StoryOrder 重算钩子 | ✅ | store/revision_test.go（钩子边界×3）+ memory_init_test.go `TestStoryOrderRecalcOnDelete` |
| U18 State 唯一键三元组 | ◐ | 唯一键/同值跳过逻辑已实现（回填路径）；跨类别共存写入口在迭代 2 |
| U19 participantsKnown | ◐ | 回填恒 false 已测（U20）；"确认无=true+空列表"在迭代 2 |
| U20 回填骨架不入事实集 | ✅ | store/memory_init_test.go `TestInitBackfillMapping`（proposed/needsReview/confidence 0.3） |
| U21 多作用域并行 active | ✅ | novel/memory_test.go `TestValidateStoryPlanScopes` |
| U22 同作用域激活替代 | ✅ | 同上（duplicate active 拒绝；激活替代的写入口在迭代 2 计划 API） |
| U23 父计划取消传播 | ◐ | 规则冻结于 §4.5；传播写入口在迭代 2 计划状态变更 API |
| U24 歧义候选不注册实体 | ◐ | `EntityCandidate` 结构+存储冻结（novel/memory.go）；产出方/确认在迭代 2；analysis 归并已改 ambiguous 不自动合并 |
| U25 committed 后 meta 崩溃窗口 | ✅ | store/memory_test.go `TestMemoryRecoveryCommittedMetaWindow` |
| U26 未迁移拦截 | ✅ | store/memory_init_test.go `TestInitLegacyProjectRejected` |
| U27 知识未生成拦截 | ✅ | store/memory_init_test.go `TestInitKnowledgeNotReady` |
| U28 零结果不 ready | ✅ | store/memory_init_test.go `TestInitZeroResultPartial` |
| U29 同值状态 no-op | ◐ | 回填路径同值跳过已实现；显式写入口在迭代 2 |
| U30 ready 幂等重试 | ✅ | store/memory_init_test.go `TestInitReadyIdempotentRetry` + server 同名用例 |
| U31 基线冲突拒绝 | ✅ | store/memory_test.go `TestMemoryCommitBaselineConflict` |
| U32 恢复断链安全态 | ✅ | store/memory_test.go `TestMemorySafeStateOnBrokenChain` + `TestMemorySafeStateOnCorruptCommits` |
| U33 标记字段变更递增 | ✅ | store/memory_init_test.go `TestStoryOrderRecalcOnDelete`（needsReview 变化 → recordVersion+1、纯重排不递增） |
| U34 回调失败不阻断 | ✅ | store/revision_test.go `TestChapterOrderHookFailureNotBlocking` + `TestChapterOrderHookErrorNotBlocking` |
| U35 短路修复过期 meta | ✅ | store/revision_test.go `TestSaveChapterNoOpRepairsStaleMeta` |

## 二、端到端集成冒烟

| 场景 | 测试 |
|---|---|
| legacy 迁移 → 知识 → 回填 → 概览/角色聚合 全链路 | store/memory_init_test.go `TestLegacyToEndMemoryFlow` |
| API 层：409 → 202 → 200 幂等 → /status → characters 400/404/200 | server/memory_test.go 三个用例 |
| 章节删除联动记忆重算（跨模块） | `TestStoryOrderRecalcOnDelete` |

## 三、范围说明（迭代 2 去向）

- ◐ 用例的完整入口均属迭代 2 交付：深度提取（U9/U19）、ChangeSet 正式提交与整个操作拒绝（U16/U18 写入口/U29 写入口/U33 乐观校验入口）、计划状态变更 API（U22 激活替代/U23 父传播）、歧义候选产出与确认（U24）。
- 本期已冻结其数据结构与行为契约，无阻塞性缺口。
