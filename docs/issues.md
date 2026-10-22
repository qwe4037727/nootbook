# V1.0 开发问题清单（迭代 1）

> 约定：开发过程中遇到的问题/规格歧义/环境限制先记录于此，**最后统一解决**并在本文档标记结论。
> 规格基准：`docs/memory_model_spec.md` V1.2 冻结版。
> **状态：迭代 1 收尾，各问题已统一裁决（2026-10-22）。**

| # | 类别 | 问题描述 | 影响 | 状态 | 结论/处理 |
|---|---|---|---|---|---|
| 1 | 规格歧义 | spec §11 标题为"提交日志协议（本期只冻结，不实现）"，但 §8.3 递增表要求回填完成以 CommitRecord 载体 +1，且 U13/U25/U31/U32 为协议验收用例——两者矛盾 | #4/#7 | ✅ 已裁决 | **迭代 1 即实现提交日志协议**（initialize/reconcile 类型），"不实现"仅指迭代 2 的 ChangeSet 正式提交流（manual_fix/formal 类型）。已在 #4 落地并通过 U13/U14/U25/U31/U32 测试。**待 yj 追认后修订 spec §11 标题** |
| 2 | 规格歧义 | StoryOrder 重算要求"留痕 CommitRecord"，但 §8.3 递增表未显式列出该操作类型 | #7 | ✅ 已裁决 | 归类 `reconcile` 类型提交（+1/次重算），已在 `handleChapterOrderChanged` 落地（TestStoryOrderRecalcOnDelete 断言 revision+1） |
| 3 | 实现偏差 | spec 说钩子由"#7 初始化完成后注册"；实现为 Store 构造时即注册 + 钩子内部自守卫（events.json 不存在时 no-op） | #7 | ✅ 已裁决 | 行为等价且避免初始化状态与注册时序耦合，已注释说明；规格语义（无记忆时零开销）不受影响 |
| 4 | 环境限制 | 本机无 C 编译器（gcc/cgo），`go test -race` 无法运行 | 测试 | ⚠️ 保留 | 结构性互斥（RWMutex）+ 并发压力测试（TestChapterOrderHookRegisterRace）兜底；**建议安装 mingw-w64 后补跑 `CGO_ENABLED=1 go test -race ./...`** |
| 5 | 安全遗留 | `data/config.json` 含明文 apiKey（已 gitignore 不入库） | 安全 | ⚠️ 保留 | 不阻塞 V1.0；建议后续优先迁移环境变量 `OPENAI_API_KEY` |
| 6 | 规格待确认 | spec §13.2 待确认 1–7 项 | 迭代 2+ | ✅ 按初步建议执行 | 逐项见 spec §13.2；本期已按建议实现（如章节删除事件→needsReview=chapter_deleted） |
| 7 | 文档任务 | features.md F20–F23 旧大纲模块语义 | 文档 | ✅ 已解决 | 已改写为 StoryPlan 语义（F20–F23 映射 global/volume 计划 + 计划树 API，标注迭代 2 交付） |
| 8 | 实现新增 | 回填遇"非 manual 的既有异值 active 状态"时跳过，递增表无对应计数 | #7 | ✅ 已裁决 | 新增 stats 字段 `SkippedExistingActive`（回填永不替代既有 active，保护语义） |
| 9 | 实现细节 | 记忆字段截断总长需含省略号 ≤limit（project.go 的 truncateRunes 会到 limit+1） | #5 | ✅ 已解决 | novel/memory.go 新增 `memTruncate`（≤limit 含省略号），记忆模型全部使用 |
| 10 | 实现新增 | 章节删除后事件处置（§13.2-1）落地取值 | #7 | ✅ 已裁决 | 事件置 needsReview=true、reviewReason="chapter_deleted"、storyOrder=0；invalidated 留用户/reconcile |
| 11 | 阻塞缺陷 | **初始化中途重启后无法继续**：进程死亡遗留孤儿 mapping 状态，重启后 initialize 永远 409 already_running | #7/#8 | ✅ 已修复 | 启动恢复（recoverMemoryLocked）检测孤儿 mapping/deep_extracting → 重置 failed + lastError="interrupted_by_restart"，走 §9.3 失败续跑路径；runMemoryMapping 增加 panic→failed 防护。测试 TestRecoveryResetsOrphanMapping |
| 12 | 阻塞缺陷 | **提交写入失败后重试造成提交链分叉**：prepared 落盘后失败不清理，重试 append 同 basedOn 记录 → 链校验判分叉进安全态 | #4 | ✅ 已修复 | 失败路径将 prepared 置 **aborted**（协议既有状态，实现遗漏）；链校验/恢复跳过 aborted；abort 写盘失败保留 prepared（恢复幂等重放，安全方向）；已 committed 但 meta 对齐失败保留 committed（崩溃点 5 收敛）。测试 TestCommitFailureAbortsPreparedNoFork |
| 13 | 阻塞缺陷 | **恢复流程接受非法提交记录**：status/type/target/commitId/createdAt 未校验，非法值被静默跳过 | #4 | ✅ 已修复 | 恢复新增阶段 0 记录级校验：status∈{prepared,committed,aborted}、type∈五种、target==basedOn+1、commitId 过 ValidID、createdAt 非零——任一违反进安全态。测试 TestRecoveryRejectsInvalidCommitRecords（4 子用例） |
| 14 | 阻塞缺陷 | **导入新书后保留旧书记忆并显示初始化完成**：ImportChapters 只重置 knowledge，memory/ 旧记录、commits、ready 状态全残留 | #7/#8 | ✅ 已修复 | importChaptersLocked 新增 resetMemoryLocked：五类记录/候选/提交日志/init_state 清空 + meta 归零；配套将 InitializeMemory 改为"先 store.mu 前置检查、释放后进 memoryMu"，使全仓库持序唯一为 store.mu→memoryMu（import 重置路径），消除反序死锁。测试 TestImportResetsMemory |

（开发过程中新增问题持续追加于此表。）
| 15 | 阻塞缺陷 (P1) | **部分文件写入后失败，标记 aborted 未撤销已写数据**：多文件 payload 写到一半失败，已写入的目标文件残留新内容，meta 却未推进——重启恢复仍报健康，数据与版本不一致 | #4 | ✅ 已修复 | CommitRecord 新增 **RevertPayload**（payload 各目标文件的写前快照，`null`=提交前不存在）：prepared 落盘前采集并随记录持久化；失败路径 abort 落盘成功后按快照回滚（`null`→删除文件，否则重写原内容），回滚失败进安全态；恢复阶段对**日志最后一条** aborted 记录补做回滚（崩溃间隙），非最后一条不重放——同进程 defer 链已完整执行过回滚，重放会用过期快照覆盖后续提交的新数据。测试 TestCommitFailureRevertsPartialWrites / TestRecoveryReplaysAbortedRevert |
| 16 | 阻塞缺陷 (P1) | **meta.json 写入失败后，同进程重试仍会造成版本链分叉**：committed 已落盘但 meta 对齐失败时仅记日志，重试基于旧 meta append 同 basedOn 记录 → 链分叉 | #4 | ✅ 已修复 | commitMemoryLocked 新增**运行期链尾一致性校验**：从尾部跳过 aborted，第一条非 aborted 记录若为 prepared（同进程失败残迹）或与 meta（revision/lastCommitId）不对齐 → 拒绝提交进安全态（零落盘）；committed 但 meta 对齐失败由"仅记日志"改为**进入安全态**（重启恢复按链尾收敛 meta 后自动解除）。测试 TestRuntimeMetaLagRejectedNoFork |
| 17 | 阻塞缺陷 (P1) | **导入重置未实际获取 memoryMu**：resetMemoryLocked 注释声称"调用方持 store.mu 时获取 memoryMu"，但无任何调用方获取——重置写盘可与记忆读/写并发 | #4/#7 | ✅ 已修复 | resetMemoryLocked 内部自行 `memoryMu.Lock()`（调用方持 store.mu，构成全仓库唯一持序方向 store.mu→memoryMu）；重置成功后同时清除 memorySafe 标志（记忆子系统整体重建，旧数据触发的安全态随之解除）；同步修正 memory_init.go 头部过时的锁时序注释。测试 TestImportResetConcurrentWithMemoryOps（20 导入 × 并发记忆读） |
| 18 | 阻塞缺陷 (P1) | **导入重置后，旧初始化任务仍会把旧角色状态写入新书**（第二复审 S01）：`runMemoryMapping` 分别读 knowledge/project/chapters（`store.mu` 域）再取 `memoryMu` 写记忆；`resetMemoryLocked` 虽正确获取 `memoryMu`，却既未取消旧任务、也未提供可校验的"任务代次"——合法交错下旧任务用旧书快照向新书提交人物状态（新书 states.json 出现 `c_old`/old state），并覆盖新书 init_state | #7/#8 | ✅ 已修复 | 新增**两级任务凭据**（spec §9.2 R12）：①**记忆代次** `memoryGeneration`——每次导入递增，旧书快照作废；②**运行号** `initCurrentRun`/`init_state.runId`——每次授权新任务递增，同代次下的旧任务作废。校验点三处覆盖：快照读取后（memoryMu 域内）、提交前、终态回写前；`failMemoryInit` 同样校验，失效任务**不写记忆记录、不回写 init_state**。明确不依赖 `memoryRevision` 判别（导入归零致新旧同值）。用户探针 `TestAcceptanceSecondRecheckImportDuringMapping` 修复前 3/3 复现、修复后 5/5 通过（并经"临时禁用校验"验证探针敏感性：禁用后立即 3/3 失败）。回归 TestImportInvalidatesInFlightInitTask / TestInitTokenGuardsTerminalWrite / TestInitGenerationInvalidatedByImport |
| 19 | 实现隐患 | **映射阶段 panic 时的自死锁**：`runMemoryMappingFor` 持 `memoryMu` 期间若 panic，recover 处理器调用 `failMemoryInit` 会再次获取同一把非重入 `sync.Mutex` → 永久挂起（进程内该初始化任务及其后所有记忆操作全部卡死） | #7 | ✅ 已修复 | 持锁区段抽为独立方法 `mapMemoryUnderLock`（`defer s.memoryMu.Unlock()` 保证 panic 展开时释放锁），外层 `runMemoryMappingFor` 只做快照读取 + 错误转译；recover 路径此时可安全获取锁写入 failed 状态。重构同时消除全部手工 Unlock 分支，降低遗漏风险 |
