# 迭代 2 验收评审报告（2026-10-23）

> 验收对象：workbuddy 按 [iter2-详设.md](../task/iter2-详设.md) 实施的迭代 2 工作区改动（未提交，8 新文件 + 12 修改文件，+951/-10）。
> 验收基准：iter2-详设 §9 自测方法、§10 验收用例表；spec V1.2 冻结条款。

## 结论：不通过（打回整改）

主体架构与关键提交路径实现忠实于详设，既有回归零破坏；但存在 3 项阻塞缺陷（B1–B3）与 8 项规格偏差（D1–D8），且任务工作流记录缺失。整改后复审。

## 一、本轮执行与结果

| 验证 | 结果 |
|---|---|
| `go build ./...` / `go vet ./...` | 通过，无诊断输出 |
| `go test ./... -count=1` | 6 测试包全 PASS（**但不含任何迭代 2 新测试**，见 B1） |
| 实机 HTTP 冒烟（临时空库，127.0.0.1:18177，已停止清理） | 路由 10 条可达；错误码映射正确：extract 前置 409 knowledge_not_ready、非法 ID 400、缺失 changeset 404、未知字段 400；reconcile 空库 200 {marked:0}；首页 200 |
| 尾随 JSON 契约实机验证 | `{} garbage` → 409 继续处理（**未拒绝**，见 D1） |
| `node --check web/app.js` | 语法通过 |
| 代码审查（AcceptChangeSet / extract / reconcile / candidates / BeginExtract / API 层） | 关键路径语义正确项见第四节；缺陷见第二、三节 |
| 范围合规 | ✅ `agent/prompt.go`、`internal/llm/client.go`、`memory_model_spec.md` 零改动；提交协议实现零改动（store/memory.go 仅 +2 行孤儿恢复调用）；既有文件改动克制 |

## 二、阻塞缺陷（必须修复）

### B1 零新增自动化测试

详设 §9 每个任务的自测方法与 §10 验收用例表（U9/U10 完整版、U16/U18/U19/U24/U29/U31/U33 写入口、I2-A~I2-H 共 17 项）**无一落地**。`go test -run ChangeSet ./...` 匹配 0 个测试；git 改动中仅 2 个既有测试文件因 `New()` 签名各改了 2/4 行。验收无法对行为正确性签字。

整改要求：按 §10 表补齐测试（store 层为主，server 层补 API 契约），`-tags acceptance_review` 探针同步回归。

### B2 U29 同值跳过未实现（写入口语义违规）

`analysis/extract.go` 状态循环：`matchActiveState` 三元组命中即无条件生成 `update_state`，**不比较新旧 Value**。同值重提会把旧记录置 superseded（recordVersion+1）并新建记录——违反 U29「同值 → 不新建、不 superseded、recordVersion 不变」，而 U29 显式写入口正是本期交付目标。

整改要求：暂存时同值（Value 及语义字段一致）跳过产出 op；补 U29 写入口用例。

### B3 未知主体 state op 毒化整个 ChangeSet

`resolveSubject` 未命中（unknown）时 `SubjectID=""` 仍生成 `add_state` op；accept 时 `ValidateStoryState` 对空前缀必然 L-S 失败 → **整个 ChangeSet 永远 422 无法提交**。小说中未登记新人物极其常见，一条未知主体状态即阻塞同批次全部有效 ops。

整改要求：未知/歧义主体的状态条目在暂存时转为 ConflictItem（候选已产出），不进入 Ops；补对应用例。

## 三、规格偏差（建议随整改一并处理）

| # | 偏差 | 位置 |
|---|---|---|
| D1 | API 契约未与 §12.3 同构：尾随 JSON 不拒绝（实机复现）；超限无 413（decodeJSON 统一 400；decodeOptionalJSON 用 io.LimitReader 静默截断，「合法 JSON + 超限空白」可被接受） | server/memory_review.go |
| D2 | `force=true` 跳过全部去重，含已 applied 同键（设计：applied 同键始终跳过） | store/memory_extract.go BeginExtract |
| D3 | 章节溯源靠解析 Reason 字符串（Sscanf "chapter ch_x rev N"），格式变动即静默失效；应加结构化字段 | store/memory_extract.go changeSetCoversChapter |
| D4 | manual_edit 冲突仅提交时点检测，暂存时点未产出 ConflictItem（I2-B 双时点；安全性由提交时点兜底，属 UX 偏差） | analysis/extract.go 状态循环 |
| D5 | register 预写 knowledge 未走 MatchCharacter 查重；预写成功 + entity_confirm 失败时重试 register 会重复注册同一实体 | store/memory_candidates.go |
| D6 | register 成功后 Resolution 未记录新实体 ID（可追溯性） | store/memory_candidates.go |
| D7 | 事件去重仅覆盖 events.json + 批内，未覆盖既有 pending ChangeSets（§3.5）；force 场景可产近似重复 | analysis/extract.go |
| D8 | quote 多处命中取第一处但未记 evidenceNote（§3.3-6） | analysis/extract.go locateQuoteRefs |

## 四、评审通过的关键项（代码审查 + 实机）

- AcceptChangeSet：analysis_running 前置 409、applied 幂等重放、终态拒绝、双次基线校验（锁窗前后）、锁外快照章节 + 锁内重读 changeset、逐 op L-S 失败整体零写入、expectedRecordVersion 乐观校验、manual_edit 提交时点转冲突、零有效 ops 空提交不递增、`commitMemoryLocked(CommitFormal)` 复用既有协议、终态回填失败仅记日志。
- grantEventStatus / reviewEvidence：无证据 → proposed+needsReview；revision 不符 → evidence_stale 降级；quote 不符 → quote_mismatch；通过 → confirmed。符合 §4.2-5。
- applyUpdateStateOp：旧 superseded（recordVersion+1、lastSource=agent_extraction）+ 新 active（recordVersion=1），符合 §2.2-4；非 active 目标拒绝。
- reconcile：锁序 store.mu→memoryMu、no-op 不提交不递增、同值不递增、status 不变红线、已删章节跳过、reconcile 类型提交。
- BeginExtract：前置检查（migrated/knowledge/config/analysis_running/already_running/memorySafe）、两级凭据（memoryGeneration + extractCurrentRun）、孤儿 extracting 重启置 failed、导入重置清理 extract_state。
- ResolveCandidate：pending 双次校验、锁外预写 knowledge + entity_confirm 提交分离（§4.5 冻结结论）、三动作状态机正确。
- 前端 app.js 语法通过；提取/审阅面板路由与轮询模式与现有代码一致。

## 五、流程缺口

1. 无 I2-x 任务 MD 及配套 -changes.md / -issues.md（AGENTS.md 任务工作流要求）。
2. 代码注释引用「见 docs/issues.md」但未实际新增条目（w_ 注册分类待确认事项）。
3. 改动未提交——符合「不自动 commit」约定，验收通过后再由 yj 决定提交。

## 六、整改后复审要点

1. B1–B3 修复 + §10 全部用例测试落地，`go test ./... -count=1` 与 `-tags acceptance_review ./... -count=2` 双绿。
2. D1–D3 至少修复 D1（契约同构）与 D2（force 语义）；D3–D8 可记入 issues 台账统裁。
3. 补齐任务 MD 记录后，复审只做增量核对。
