# G4 记忆自我维护（S3）验收记录

> 日期：2026-10-23
> 规格依据：`详设.md` §7、`docs/memory_model_spec.md` §5.1/§5.2/§13.2、`task/roadmap.md` S3。
> 结论：**代码完成、自动化用例全绿**；真实模型/真实语料端到端未执行（与 I3-14/I3-15 同因：模型通道阻断）。
> G4 退出判据（改中段一章 → 下游受影响记录被标记 + 冲突提示，绝不被自动覆盖；reconcile no-op 不递增；检索红线不误注入）已由自动化用例覆盖。

## 1. 正文变化分级与处置

| 变化类型 | 触发路径 | 处置 |
|---|---|---|
| 局部改（改/存章节） | `SaveChapter` → `reconcileForChapter` | Revision 变化 → `evidence_stale`；quote 与现正文不符 → `quote_mismatch`；随后沿依赖链传播 |
| 删章 | `DeleteChapter` → `reconcileStoryOrderFromSource` | 事件 `StoryOrder=0` + `chapter_deleted`；五类记录任一 `SourceRef.ChapterID` 指向已删章节 → `chapter_deleted`；状态 `LastConfirmedChapter` 缺失同样标记 |
| 重排（仅调整章节顺序） | 章节序号钩子 / 本地兜底对账 | 仅重算 `StoryOrder`，不递增 `recordVersion`，留痕 reconcile |
| 换书（导入新书） | `ImportChapters` → `resetMemoryLocked` | 五类记录/提交日志/初始化状态整体重置（既有 G2 行为，未改动） |
| 启动自愈 | `New()` → `reconcileStoryOrderOnStartup` | 钩子失败后残留的失效事件/记录在重启后收敛 |

所有标记只写 `needsReview` / `reviewReason` / `recordVersion` / `updatedAt`，**不改 status、不改计划 objective/title、不改历史正文**（spec §5.1 红线）。

## 2. 依赖传播（详设 §7.2）

`store.propagateReviewDependencies` 沿可追溯依赖链逐层标记 `dependency_stale`：

- 事件 → 事件：`preconditions` / `consequences` 引用失效事件，传递闭包；
- 事件 → 状态：`validFromEvent`；
- 事件 → 认知：`knownSinceEvent`；
- 事件 → 伏笔：`introducedByEvent` / `resolutionEvent` / `relatedEvents`；
- 伏笔 → 计划：`dependsOnThreads`；
- 父计划 → 子计划：`parentPlanId`（U23；S4 未开始，本期只交付依赖图与标记，**不自动取消**子计划）。

已有的更具体原因（`evidence_stale` / `chapter_deleted` / `quote_mismatch`）不会被 `dependency_stale` 覆盖；`proposed` 骨架事件不作为传播源，避免初始化骨架造成全量误标。

## 3. 批量入口与 no-op 语义

- `POST /api/memory/reconcile`（既有）支持 `chapterIds` 为空的全量复核，作为**历史依赖修复**入口：一次重算直接证据 + 全图传播。
- 有实际标记变化才提交 `reconcile`（`memoryRevision+1`）；无变化不提交、不递增。删章/改章/启动路径均遵循该语义。
- 前端「记忆审阅」中的「证据复核」按钮继续调用该入口。

## 4. 冲突提示（用户可见）

- SSE `meta` 已携带 `skippedReview`；续写状态栏显示 `待复核跳过 N 条`，并在生成开始时弹出提示「有 N 条记忆因待复核未注入，请在「记忆审阅」中处理」。
- `/api/continue/preview` 面板原有 `待复核跳过 N 条` 保持不变。
- 检索红线：`needsReview=true` 的记录一律不注入提示词（G3 既有红线，新标记同样生效）。

## 5. `cognitions` 产出方（S3 交付项）

- 深度提取 schema 新增 `cognitions[{subject,content,status,quote}]`（`internal/analysis/extract_prompt.go`）。
- 组装阶段（`internal/analysis/extract.go`）产出 `add_cognition`：
  - 主体必须解析为既有角色（`c_`）；未知/歧义主体转 ConflictItem 并产出实体候选；
  - 世界条目、非法 status 跳过；同章同主体同内容去重；
  - quote 服务端定位偏移；定位失败 → `needsReview=evidence_unlocated`；
  - 同证据命中本章事件时绑定 `knownSinceEvent`。
- accept 路径复用既有 `applyCognitionOp` + `ValidateCognition`（仅 `c_` 主体）。

## 6. 自动化用例

| 用例 | 覆盖点 | 结果 |
|---|---|---|
| `TestDeleteChapterMarksAllMemoryKinds` | 删章：五类标记、计划/子计划传播、status/内容不变、未涉及章节不受影响、revision=1 | PASS |
| `TestEditMiddleChapterPropagatesWithoutOverwrite` | 改中段：evidence_stale → 下游 dependency_stale、计划不被覆盖、检索红线、全量 reconcile no-op | PASS |
| `TestStartupHealsDeletedChapterAcrossKinds` | 非事件记录（状态）在重启后按来源章节自愈 | PASS |
| `TestContinueReportsReviewSkippedMemory` | SSE meta `skippedReview` 上报 + 待复核记录不进提示词 | PASS |
| `TestAssembleCognitionsProduced` | 认知落 op、事件绑定、去重、未知主体冲突、世界/非法状态跳过 | PASS |
| `TestAssembleCognitionWithoutQuoteNeedsReview` | 认知证据无法定位 → evidence_unlocated | PASS |
| `TestAcceptCognitionOpPersists` | add_cognition 经 accept 正式落盘 | PASS |
| 既有 `TestStoryOrderReconcile*` / `TestFunctionalOrderReshuffles` | 重排与 no-op 语义不回归 | PASS |

## 7. 自测命令与结果

```text
go build ./...                                      # PASS
go vet ./...                                        # PASS
go test ./... -count=1                              # 6 个含测试包全 ok
```

### 7.1 5M 字真实 HTTP 规模实测（2026-10-24，夹具记忆 + 人工补写章节级证据）

把 `cmd/longeval` 的 500 章 / 5,020,294 rune 隔离目录复制一份，用 `output/g4_5m_seed_refs.mjs` 给夹具记录补上 `sourceRefs`（第3/12章精确引文、第5章「修订号对得上但引文不符」）与依赖链（事件→伏笔→计划），再在真实进程上走 HTTP。详见 [run doc](../docs/acceptance-evidence/20261024/g4-5m-heal/g4-5m-heal-run.md)。

| 判据 | 结果 |
|---|---|
| 三种失效原因在 5M 上真实触发 | 通过：改第12章→`evidence_stale`（PUT 280 ms 内联复核）、全量复核→`quote_mismatch`（`marked=1`，只有全量扫才抓得到）、删第3章→`chapter_deleted` |
| 依赖传播且不覆盖计划 | 通过：`thread_jade`、`plan_near` 被标 `dependency_stale`，`plan_near` 的 `status=active`、`title/objective/dependsOnThreads` 逐字未变，只有 `needsReview/reviewReason/recordVersion 0→1` 变化 |
| 注入侧后果可测 | 通过：`memorySection` 242→212→195→102 字，每次收缩恰对应刚被标记的那条记录退出注入；`skippedReview=6` 在预览里可直接读到（§4 的界面口径有数据源） |
| 计划停投与记忆同向 | 观察：`planCount 1→0`（`LoadActivePlans` 过滤 `NeedsReview`），语义是「停投不改稿」；但预览/SSE 无停用原因字段 → issues #58 |
| 审计与最小写集 | 通过：`commits.json` 3 条全为 `reconcile`、`basedOn→target` 12→13→14→15 每次 +1；13 只写 `states.json`、14 只写 `cognitions.json`、15 写 `events+plans+threads` |
| 规模成本 | 全量复核（扫 500 章）352 ms、单章复核 4 ms、删章 307 ms（500→499 章），预览时延仍由召回主导（1.29–1.48 s） |
| G7 闸门 | 变更后仍 `400 reinit_not_supported` 且 `memoryRevision` 不变 |
| 夹具缺陷 | **写入记忆后重启进程即 `memory_safe_state`**：`eval/cases/memory/` 带 `meta.json{memoryRevision:12}` 却无 `commits.json`，链校验要求首条 `basedOn=0` → 实测报 `commit chain broken … basedOn=12 want=0`，记忆/计划/召回注入同时归零；把 3 条提交重编号为 0→1→2→3 并修 meta 后重启即恢复（issues #59） |

## 8. 未完成边界（须在 G3/真实通道恢复后复核）

> 2026-10-24 更新：本节第 1、4 条仍成立；但「链路本身在长篇规模上是否成立」已由 §7.1 的 5M 真实 HTTP 实测回答（三种失效原因、依赖传播、计划不改稿、注入收缩、审计最小写集全部通过），不再是只有单测的结论。夹具依赖链只构造了一条（事件→伏笔→计划），多层计划树与 `parent_completed`、`evidence_unlocated` 等原因码仍只有自动化覆盖。

1. **真实模型未验证**：认知抽取 schema 与提示词未在真实语料上跑过，抽取质量、误报率未知；`cognitions` 的实际产出依赖深度提取，而深度提取又依赖模型通道。
2. **无逐条「确认/忽略」复核入口**：当前仅展示 `needsReview` 原因；修复路径是重新提取并接受 ChangeSet（或人工修订记录）。事件侧提取只做 add + 摘要去重，已失效事件的自动修复提案尚未实现；「历史依赖修复」目前是全量重算标记，而非自动生成修复变更集。
3. **U23 仅标记**：父计划取消/复核向子计划传播只做标记，取消语义留待 S4（计划链）落地。
4. **G4 的收益判定**：需真实模型通道恢复后按「改中段一章 → 影响面 → 续写是否避开失真记忆」做一次端到端观察。

## 9. 变更文件

- `internal/store/memory_init.go`：删章/启动对账扩展到五类记录 + 依赖传播。
- `internal/store/memory_reconcile.go`：直接证据复核后接入依赖传播、`markReview*`/`markMissingSourceRefs`、计划树传播。
- `internal/store/memory_heal_test.go`（新增）：G4 store 用例。
- `internal/server/continue_test.go`：SSE 冲突提示用例。
- `internal/analysis/extract_prompt.go` / `internal/analysis/extract.go`：`cognitions` 抽取 schema 与组装。
- `internal/analysis/extract_test.go`：认知抽取用例。
- `internal/store/memory_iter2_test.go`：认知 accept 用例。
- `web/app.js`：续写状态栏与弹窗展示待复核数量。
