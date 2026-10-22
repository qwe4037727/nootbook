# V1.0 开发问题清单（迭代 1）

> 约定：开发过程中遇到的问题/规格歧义/环境限制先记录于此（状态=待解决），**最后统一解决**并在本文档标记结论。
> 规格基准：`docs/memory_model_spec.md` V1.2 冻结版。

| # | 类别 | 问题描述 | 影响 | 状态 | 结论/处理 |
|---|---|---|---|---|---|
| 1 | 规格歧义 | spec §11 标题为"提交日志协议（本期只冻结，不实现）"，但 §8.3 递增表要求回填完成以 CommitRecord 载体 +1，且 U13/U25/U31/U32 为协议验收用例——两者矛盾 | #4/#7 | 待统一解决 | **采用解释：迭代 1 即实现提交日志协议**（initialize/reconcile 类型），"不实现"仅指迭代 2 的 ChangeSet 正式提交流（manual_fix/formal 类型）。待 yj 确认 |
| 2 | 规格歧义 | StoryOrder 重算要求"留痕 CommitRecord"，但 §8.3 递增表未显式列出该操作类型 | #7 | 待统一解决 | **采用解释：归类 `reconcile` 类型提交（+1/次重算）**，与"reconcile 批量标注 +1/次操作"同类 |
| 3 | 实现偏差 | spec 说钩子由"#7 初始化完成后注册"；实现为 Store 构造时即注册 + 钩子内部自守卫（events.json 不存在时 no-op）——行为等价但注册时机提前 | #7 | 待统一解决 | 初始化前 events 为空，重算天然 no-op；避免初始化状态与注册时序耦合 |
| 4 | 环境限制 | 本机无 C 编译器（gcc/cgo），`go test -race` 无法运行；竞态防护以 RWMutex 结构性互斥 + 并发压力测试（TestChapterOrderHookRegisterRace 等）兜底 | 测试 | 待统一解决 | 建议安装 mingw-w64 后补跑 `CGO_ENABLED=1 go test -race ./...` |
| 5 | 安全遗留 | `data/config.json` 含明文 apiKey（已 gitignore 不入库）；建议迁移环境变量 `OPENAI_API_KEY` | 安全 | 待统一解决 | 不阻塞 V1.0 |
| 6 | 规格待确认 | spec §13.2 待确认 1–7 项（章节删除事件处置/章内时序/Value 枚举/文件阈值/全量重初始化/storyBudgetChars 默认值/storyline 实体）——均声明不阻塞本期编码 | 迭代 2+ | 按初步建议执行 | 逐项见 spec §13.2 |
| 7 | 文档任务 | features.md F20–F23 仍为旧大纲模块语义，需改写为 StoryPlan 语义（spec §14 末行"后续文档任务"） | 文档 | 待统一解决 | V1.0 收尾时改写 |

（开发过程中新增问题持续追加于此表。）
