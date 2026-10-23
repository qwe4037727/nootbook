# 已实现功能补测报告（2026-10-22）

依据此前盘点的七类已实现但测试不完整的区域，本轮采用真实浏览器操作、独立便携 GCC 竞态测试、真实外部模型、进程强杀恢复探针与边界矩阵进行了完整补测。凭据仅存在于临时测试子进程的内存环境变量中，未落盘也未计入报告。

结论前置：**已完成全部可执行项的实机测试，但不能判定为“全量通过”。** 补测中发现了 2 个确凿的代码缺陷，并确认了 1 个上游网络依赖受阻点。

## 测试结果汇总表

| 编号 | 测试领域 | 验证方式 | 结论 | 证据路径 / 备注 |
|---|---|---|---|---|
| RACE-01 | 动态数据竞争 | 便携 GCC + `go test -race ./...`（6 包）及新增受保护探针 | **PASS** | `output/executions/functional-20261022/race.txt` |
| BOUND-01 | 标量/列表/元数据边界 | 表驱动单测：80 标量 + 40 列表 + 150 元数据 | **FAIL (发现缺陷)** | 4 类列表单条超长截断后为 101 rune，超规格上限 100；标量与元数据通过 |
| RECOVER-01 | 进程退出与崩溃恢复 | 子进程在持久化 7 个阶段强杀，父进程重启核对 | **PASS** | 7/7 阶段链校验、重放及安全态一致；`process-recovery.txt` |
| ORDER-01 | 章节重排与派生字段 | 算法级前插、逆序、删中间章节测试 | **PASS** | StoryOrder 重算正确，无翻转，纯重排不递增 RecordVersion |
| HEAL-01 | 序号回调失败后收敛 | 句柄占用模拟写入失败，验证重启/后续操作 | **FAIL (发现缺陷)** | 单纯重启与新增章节均不修复遗漏的旧事件；仅在后续删除时才收敛 |
| UI-01 | 导入/编辑/保存/新建/删除 | 真实浏览器操作 + Project/Chapter API + 磁盘核对 | **PASS** | 导入 3 章、标题正文修改、保存刷新一致、新建并确认删除 1 章；已取证 |
| UI-02 | SSE 续写/追加/另存新章 | 真实模型 SSE 生成 + 浏览器界面按钮 + 版本核对 | **PASS** | 正式生成 476 字，追加后 revision 升为 2，刷新仍在；另存为第 4 章成功 |
| UI-03 | 停止/重试/草稿不污染 | 续写中点击停止、重新生成、未保存刷新核对 | **PASS** | 停止后状态正确，重试成功，未保存草稿刷新后各章文件 Hash 完全一致 |
| MODEL-01 | 真实多章分析与窗口截断 | 外部接口（gpt-6.1-sol）+ 1,873 字样本 | **blocked / needs_review** | 窗口截断至 1,000 字在界面与请求中生效；全书多章分析因上游重置未全量收敛 |

## 发现的实质缺陷

### 1. F-BOUND-01（P2）：四类列表字段单条超长截断后超出规格上限
- **影响范围**：`internal/novel/memory.go` 中的 `StoryThread.KnownClues`、`StoryThread.ResolutionCriteria`、`StoryPlan.Constraints`、`StoryPlan.CandidateBeats`。
- **原因**：规范要求单条不超过 100 字符。当前 `truncateList` 调用的是通用 `truncateRunes`（截取前 100 个 rune 并强行追加 `…`），导致处理后长度变为 101 rune。标量字段使用 `memTruncate`（前 99 个 rune + `…`）则为 100 rune。
- **复现证据**：`internal/novel/memory_functional_test.go` 中 `TestFunctionalMemoryListBoundaries` 下 4 个 `item/101` 子用例明确报 FAIL。

### 2. F-HEAL-01（P1）：章节删除回调失败后无法通过重启或新增操作自愈
- **影响范围**：`internal/store/memory_init.go` 中的 `handleChapterOrderChanged` 与启动期恢复。
- **表现**：当章节删除成功但序号回调因文件锁定等原因失败后，`events.json` 会保留被删章节的旧事件。服务重新启动时仅校验提交链，未对章节与事件进行对账；新增章节也不会触发顺序重算。系统只有在未来某次再次执行删除章节动作时，才能通过最新的章节映射将旧事件修复为 `NeedsReview=true, StoryOrder=0`。
- **复现证据**：`internal/store/memory_functional_test.go` 中 `TestFunctionalFailedDeleteHookSelfHealing` 与 `TestFunctionalFailedDeleteHookNextMutation` 报 FAIL；`TestFunctionalFailedDeleteHookNextDelete` 报 PASS。

## 外部环境受阻记录

- **F-UPSTREAM-01**：在对 3 章全书文稿执行后台自动分析时，外部模型网关与本地代理多次出现 `timeout awaiting response headers`、`unexpected EOF` 以及 `connection refused`。虽证明了错误提示和重试机制可以正常工作，但未能得到完整的全书知识汇总产物，因此 MODEL-01 不标为通过。
