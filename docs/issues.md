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

（开发过程中新增问题持续追加于此表。）
