# G6 正文草稿 + 记忆变更联合确认提交（S5）验收记录

> 日期：2026-10-23（代码与 Mock 验收）｜2026-10-24 追加 §5.5 真实进程 + 真实 HTTP 链路证据、§5.6 真实进程六崩溃点恢复矩阵
> 规格依据：`task/iter2-详设.md` §4/§8、`docs/memory_model_spec.md` §8.3/§11.3、`task/roadmap.md` S5。
> 结论：**代码完成、自动化用例全绿、Mock 模型 UI 端到端跑通、真实 HTTP 上「一次确认 = 一个提交 = 正文 + 记忆」已实测、spec §11.3 六个崩溃点在真实进程上逐点注入并重启收敛已实测（恢复后注入读数与不崩溃的对照逐字相等）**；真实模型下的提取质量与「草稿→确认」人工核对体验未验证（与 G1–G5 同因：上游网关阻断）。
> G6 退出判据覆盖情况：「正文 + 记忆变更一次提交」→ 自动化 + §5.5 真实 HTTP；「拒绝时零部分副作用」→ §5.5；「**中断时**不留部分副作用」→ **§5.6 真实进程六崩溃点恢复矩阵（2026-10-24 已补）**；「编辑使旧提取作废」→ 自动化 + §5.5（但退化为只提交正文物无 API 信号，见 issues #63）；「基线过期拒绝」→ §5.5 实测 `revision_conflict`。真实数据验收待通道恢复。

## 1. 交付映射

| 能力 | 实现 |
|---|---|
| 正文草稿态 | `novel.Draft`（`draft_` 前缀，`data/memory/drafts.json`）：`pending_confirmation → committed / rejected`，含 `draftRevision`、`baseChapterRevision`、`baseMemoryRevision`、`targetChapterId`、`newChapter`、`memoryChangeSetId` |
| 草稿 CRUD | `store.CreateDraft/UpdateDraft/LoadDraft/LoadDrafts`；编辑递增 `draftRevision` 并作废已提取变更集（`ErrDraftNeedsRecheck`） |
| 草稿深度提取 | `analysis.ExtractLauncher.ExtractDraft`：单次 LLM 调用产出 `mcs_` ChangeSet，绑定 `draftId + draftRevision + 确认后章节 revision`；同一草稿重复提取覆盖旧批次 |
| 联合确认提交 | `store.ConfirmDraft`：一次 `CommitFormal` 同时写正文（或新章节）、`project.json`、五类记忆文件、`changesets.json`、`drafts.json`；提交 ID 预分配，任一文件失败按 `RevertPayload` 回滚 |
| HTTP API | `POST/GET /api/drafts`、`GET/PATCH /api/drafts/{id}`、`POST /api/drafts/{id}/extract|confirm|reject` |
| 前端草稿台 | 续写草稿「存入草稿台」→ 顶栏「草稿台」列表/详情/编辑/提取/确认/拒绝；确认成功自动打开章节并刷新待确认计数 |

## 2. 关键语义与不变量

1. **草稿区不触碰正式区**：创建/编辑/提取只写 `memory/drafts.json` 与 `changesets.json` 中的 `proposed` 批次，`memoryRevision` 不变，正文文件不变（`TestDraftLifecycleDoesNotTouchFormalArea`）。
2. **确认是替换而非追加**：对既有章节，`ConfirmDraft` 用草稿 `content` **覆盖**章节正文。前端「并入本章」在创建草稿前把「原章节正文 + 续写增量」合并为完整草稿再 POST，避免覆盖丢正文。
3. **三方基线绑定**：确认请求携带 `draftRevision`、`expectedChapterRevision`、`expectedMemoryRevision`；草稿被编辑（`draft_needs_recheck`）、章节被他人改动（`chapter_conflict`）、记忆基线前进（`revision_conflict`）均拒绝提交。
   > 实测口径修正（2026-10-24 桩链路，见 §5.5 与 issues #63）：「草稿被编辑 → 拒绝确认」这条在真实路径上**不可达**——`PATCH` 已先作废并解绑批次，`ConfirmDraft` 的变更集校验分支不进入，`confirm` 因此成功但退化为「只提交正文」。记忆基线过期在真实 HTTP 上实测返回 409 `revision_conflict`（`draftRevision` 过期则 409 `draft_conflict`）；章节基线冲突本轮未被直接触发，仍只有 store 用例 `TestConfirmDraftExistingChapterBindsRevisions` 覆盖。此措辞与实现的分歧属语义决策，待 yj 拍板后修订本条或改实现。
4. **草稿派生变更集不可单独采纳**：`accept/rebase` 返回 `409 draft_managed_changeset`，必须走草稿确认（`TestDraftEditInvalidatesExtraction`、server 契约用例）。
5. **原子性**：提交前预分配 commit ID，payload 覆盖正文/项目/五类记忆/草稿/变更集；失败路径回滚已写文件；重启恢复按 `prepared/committed` 重放，覆盖新 payload（`TestConfirmDraftFailureLeavesNoPartialWrite`、`TestRecoveryReplaysDraftCommitTargets`）。
6. **幂等终态**：已 `committed` 草稿重复确认返回既有结果（不重复写记忆）；`rejected` 后确认/提取/编辑均为 `409 draft_terminal`。
7. **空变更集确认允许**：只落正文也是合法联合提交；记忆变更可为 0 操作。
8. **冲突不阻断确认**：提取冲突以候选/冲突计数返回（`result.conflicts`），由用户核对后决定是否提交；不会静默丢弃，也不会自动改知识库。

## 3. API 契约摘要

| 端点 | 请求 | 成功 | 主要错误码 |
|---|---|---|---|
| `POST /api/drafts` | `{chapterId,title,content}`；`chapterId` 为空 = 新章节 | `201 Draft` | `422 invalid_draft`、`404 chapter_not_found`、`500 memory_safe_state`、`413 request_too_large` |
| `GET /api/drafts?status=` | `pending_confirmation|committed|rejected|空` | `200 [DraftSummary]` | `400 invalid_status` |
| `GET /api/drafts/{id}` | — | `200 Draft` | `400 invalid_draft_id`、`404 draft_not_found` |
| `PATCH /api/drafts/{id}` | `{draftRevision,title,content}` | `200 Draft`（`draftRevision+1`，旧 ChangeSet 作废） | `409 draft_conflict/draft_terminal`、`422 invalid_draft` |
| `POST /api/drafts/{id}/extract` | 空体或 `{}` | `200 {draft, changeset}` | `409 draft_needs_recheck/revision_conflict/analysis_running/knowledge_not_ready/config_missing`、`503`（提取器未启用） |
| `POST /api/drafts/{id}/confirm` | `{draftRevision,expectedChapterRevision,expectedMemoryRevision}` | `200 {commitId,memoryRevision,chapter,project,draft,conflicts}` | `409 chapter_conflict/revision_conflict/draft_needs_recheck/draft_terminal/analysis_running`、`404 chapter_not_found`、`500 changesets_corrupted/memory_safe_state` |
| `POST /api/drafts/{id}/reject` | 空体或 `{reason}` | `200 Draft`（`rejected`） | `404 draft_not_found`、`409 draft_terminal` |

错误响应统一为 `{"error": "...", "code": "..."}`；请求体上限 16 MB。

## 4. 自动化用例

| 包 | 用例 | 覆盖点 | 结果 |
|---|---|---|---|
| `internal/store` | `TestDraftLifecycleDoesNotTouchFormalArea` | 创建/编辑/拒绝不改正文与 revision | PASS |
| `internal/store` | `TestConfirmDraftNewChapterCommitsChapterAndMemoryTogether` | 新章节 + 记忆一次提交、项目元数据 | PASS |
| `internal/store` | `TestConfirmDraftExistingChapterBindsRevisions` | 覆盖既有章节、章节 revision 绑定 | PASS |
| `internal/store` | `TestDraftEditInvalidatesExtraction` | 编辑使变更集失效、草稿派生批次禁止 accept/rebase | PASS |
| `internal/store` | `TestConfirmDraftRejectsStaleMemoryBaseline` | 记忆基线冲突拒绝 | PASS |
| `internal/store` | `TestConfirmDraftReconcilesEvidenceInSameCommit` | 同一次提交内完成章节溯源/证据核对 | PASS |
| `internal/store` | `TestConfirmDraftFailureLeavesNoPartialWrite` | 写失败回滚、无部分落盘 | PASS |
| `internal/store` | `TestRecoveryReplaysDraftCommitTargets` | 重启恢复重放草稿提交目标文件 | PASS |
| `internal/store` | `TestCorruptChangeSetsDraftRejectedFormalReadable` | 草稿区损坏时拒绝提交、正式区可读 | PASS |
| `internal/server` | `TestDraftAPIContractAndTerminalStates` | 状态机、错误码、幂等确认 | PASS |
| `internal/server` | `TestDraftAPIExtractThenAtomicConfirm` | 提取→编辑冲突→重新提取→确认全链路 | PASS |
| `internal/server` | `TestDraftAPIExtractRequiresEnabledService` | 提取器未启用 503 | PASS |
| `internal/server` | `TestDraftAPIExtractConflictAfterEdit` | 编辑后提取 409 `draft_needs_recheck` | PASS |

全量回归（2026-10-23）：

```text
go build ./...                                        # PASS
go vet ./...                                          # PASS
go test ./...                                         # PASS（7 包）
go test -tags acceptance_review ./...                 # PASS（7 包）
node --check web/app.js                               # PASS
```

## 5. Mock 模型 UI 端到端证据（2026-10-23）

隔离环境：`.tmp_e2e/g6`（gitignored），本地 Mock OpenAI Provider（18081）+ 真实服务（18080，数据目录 `.tmp_e2e/g6`）+ Playwright headless Chromium 走真实 HTTP/前端路径。种子数据：1 章 22 字、知识库含角色「林砚」。

流程与结果：

1. 打开页面 → 点击「开始续写」→ Mock SSE 产出 `林砚推门而入，肩头湿透。他压低声音说，旧账该清了。`。
2. 点击「存入草稿台（并入本章）」→ 草稿台自动打开：待确认计数 `1`，详情 `待确认 · r1`，目标「并入第 1 章 第1章 雨夜 · 正文基线 r1 · 记忆基线 revision 0」。
3. 点击「提取记忆变更」→ `已提取 1 操作 · 0 冲突`；变更集 `mcs_8f18cc7b505b37bb42`（1 条 `add_event`，`quote=林砚推门而入`，rune 偏移 26–32）。
4. 确认对话框：`确认联合提交？将覆盖第 1 章 第1章 雨夜，并同时写入已提取的记忆变更。` → 接受。
5. 提交结果：`commit_0a24ce0a722800d1b3`、`记忆 revision 1`；编辑器 47 字（原 22 字 + 续写 25 字），侧栏章节字数 `47 字`，待确认计数 `0`。
6. 数据核对：草稿 `draft_3cfe7eb40912496ef2` = `committed`；变更集 = `applied`；事件 `evt_8113dd702b103164ef` = `confirmed`；`project.json` 章节 revision 1→2；`meta.memoryRevision` 0→1。
7. 前端状态：草稿卡显示「已联合提交 · 正文与记忆已写入」，仅保留「丢弃」；浏览器控制台 0 错误。

截图：`g6-1-generated.png`（生成）、`g6-2-draftbox.png`（草稿台）、`g6-3-extracted.png`（提取后）、`g6-4-committed.png`（联合提交后）。结果 JSON：`e2e-result.json`；复现脚本：`seed.js`、`mock-provider.js`、`e2e.js`。

补充场景（编辑 → 重新提取 → 拒绝，`e2e-reject.js`）：

1. 生成并存入草稿台（r1）→ 修改正文并「保存修改」→ 徽标变为 `待确认 · r2`，`draftRevision` 1→2。
2. 修改后重新提取 → `已提取 1 操作 · 0 冲突`；变更集 `mcs_1fa84811f07e02aa7c` 绑定 r2。
3. 填写拒绝原因「方向不对，重写」→ 确认拒绝 → 草稿徽标 `已拒绝 · r2`，详情显示拒绝原因，变更集状态 `rejected`。
4. 未写入正式区：章节 revision 仍 1、正文仍为种子 22 字、`memoryRevision` 仍 0、待确认计数 0。
5. 前端状态恢复：续写草稿卡显示「草稿已拒绝 · 可重新存入草稿台」，「存入草稿台」按钮重新可见；控制台 0 错误。
6. 截图：`g6-5-edited-extracted.png`、`g6-6-rejected.png`；结果：`e2e-reject-result.json`。

> 本轮 E2E 发现并修复 3 处前端缺陷：①确认成功后草稿卡仍提示「请在草稿台提取记忆并确认」；②侧栏章节字数停留在提交前（`applyProjectMeta` 用旧编辑器内容覆盖服务端元数据）；③favicon 404 控制台噪声。

## 5.5 真实进程 + 真实 HTTP 链路证据（2026-10-24，本地桩模型）

隔离环境：独立二进制 `output/novelagent-g6.exe`（工作区 HEAD + 并行 agent 未提交改动，构建/vet/test 同轮全绿）、服务 8097、OpenAI 兼容桩 8098（请求体全量留档）、独立空数据目录 `output/e2e-g6-data`；6 章自撰语料，先跑 analyze/initialize/extract/accept 建立基线记忆再进草稿链路。无真实密钥。完整run doc：[docs/acceptance-evidence/20261024/e2e-g6-stub-chain/g6-stub-chain-run.md](../docs/acceptance-evidence/20261024/e2e-g6-stub-chain/g6-stub-chain-run.md)；台账：[task/e2e-stub-chain-acceptance.md](./e2e-stub-chain-acceptance.md) §8。

本轮把 §2 的不变量第一次在真实 HTTP 上量出来（此前只有 Go 单测与 Mock UI 点击）：

| 不变量 | 真实 HTTP 观测 |
|---|---|
| §2.5 原子性（一次提交） | `commit_93a191ad13bbb25cfe`，**`commitCountDelta=1`**，`type=formal`、`basedOn=3→target=4`，一个 payload 覆盖正文 + `project.json` + 五类记忆 + `changesets.json` + `drafts.json`；`memoryRevision 3→4`、`events 7→8`、`cognitions 1→2` |
| §2.1 草稿区不触碰正式区 | `reject` 后 `memoryRevision`、提交数、目标章节正文与 `revision` 逐字未变，批次置 `rejected` |
| §2.4 派生批次不可单独采纳 | `accept`/`rebase` → 409 `draft_managed_changeset` |
| §2.3 基线绑定 | 过期 `expectedMemoryRevision` → 409 `revision_conflict` 且**零部分副作用**（记忆/提交数/章节数/草稿状态全不变）；旧 `draftRevision` → 409 `draft_conflict` |
| §2.6 幂等终态 | 第二次 `confirm` 返回同一 `commitId`、提交数不变（issues #64 印证）；终态 `PATCH`/`reject`/`extract` → 409 `draft_terminal` |
| §2.7 空变更集确认 | 「编辑后退化为只提交正文」仍返回 200 且 `memoryRevision` +1——合法但**无信号**，见 issues #63 |
| G6×G4 交界 | 改掉埋点句后，**经 G6 提交进来**的 `evt_048eb577…` 置 `evidence_stale`（`recordVersion` 1→2，文本/`status` 未变），只多一个 `reconcile`，预览 `memorySection` 137→105 |
| G7 闸门 | `POST /api/memory/initialize {reinitialize:true}` → 400 `reinit_not_supported`（本轮未绕开） |

闭环一侧：紧随的 `/api/continue` meta 显示联合提交把记忆段从 105 字抬到 137 字（`recalledByKind.cognitions 1→2`），桩侧留档的 user prompt 同时含草稿派生事件文本与新章正文（`continue-user-prompt-after-joint-confirm.txt`）——草稿正文与草稿派生记忆在同一次模型调用里都可见。

**本轮不证明**：桩对每个请求返回同一份 JSON，`conflicts=0` 只说明链路跑得通，不说明真实模型不会产出冲突；提取准确率、op 分布、文笔、spec §11.3 的六崩溃点恢复矩阵（见 §5.6）均不在本轮覆盖内。**G6 判定不变**：代码 + 自动化 + Mock UI E2E + 真实 HTTP 链路，真实模型质量一侧仍空白，不因此轮升格为「已交付」。

## 5.6 真实进程六崩溃点恢复矩阵（2026-10-24，补 §6 第 8 项的空白）

隔离环境同上（独立二进制 + 8097 + 桩 8098 + `output/` 下隔离目录 + 无真实密钥），驱动 `e2e_g6_crash_matrix.mjs`；完整判读与逐场景数字见 [docs/acceptance-evidence/20261024/e2e-g6-crash-matrix/g6-crash-matrix-run.md](../docs/acceptance-evidence/20261024/e2e-g6-crash-matrix/g6-crash-matrix-run.md)。**本轮零产品代码改动。**

方法要点（不写清就无法复跑）：崩溃建模 = `taskkill /T /F` 硬杀；停点由脚本在重启前改写磁盘状态得到；**基准不是崩溃前的目录，而是「崩溃点目录 + 一次对照重启」**，因为联合提交后的第一次启动必做一轮确定性 `reconcile`（`storyOrder` 重算，实测 4 轮重启只有首轮有抖动）。payload 的 8 个目标文件按 SHA 逐一比对，**且必须按 ISO 时间戳归一化**（提交把 `updatedAt` 写成 `now`）；`commits.json`/`meta.json` 不做字节比较，改做结构断言。

| §11.3 停点 | 真实进程实测（`commit_24644d4a0f4fb5dba5`，`formal 8→9`，8 个 payload 目标） | 判定 |
|---|---|---|
| ① prepared 未落盘 | 删记录 + 按 `revertPayload` 回滚 → 重启后不被重放：`revision=8`、新章文件不存在、`events.json` 无新事件 ID、`safeState=false` | ✅ 等价于「这次提交从未发生」 |
| ② prepared 后、目标文件前 | 重放 → `committed`（`occurrences=1`）、8 目标与对照 **0 处差异**、meta 收敛到链尾 | ✅ 正文与五类记忆一起补齐 |
| ③ 部分目标已写 | 回滚 5 个（含删掉新章文件）、留 3 个在提交后状态 → 重放补齐 → **0 处差异** | ✅ 覆盖写幂等，不产生第三种状态 |
| ④ 全目标已写、committed 未落盘 | 重放为内容 no-op → 翻 `committed`、**新事件在文件中恰好 1 次**（未双重生效） | ✅ 不重复计数 |
| ⑤ committed 已落盘、meta 未更新 | meta 从 8 **对齐到链尾 10**（非 +1）、`lastCommitId` 指回链尾、**不重放文件**（正文里的 8 字探针标记仍在，`manuscriptChars` 471 = 对照 463 + 8） | ✅ 与 §11.3 第 5 行一致 |
| ⑥ 一致态 | 与对照重启同形：0 处差异、只有那一条确定性 `reconcile`、`commitId` 全唯一 | ✅ |
| 附带：链尾 `aborted` | 恢复补做回滚（P1-1）：新章删除、新事件消失、`aborted` 不计链尾、meta 与最后一条非 aborted 对齐 | ✅ |
| 附带：链断 / 分叉 | `safeState=true` + `commit_chain_broken`；`POST /api/drafts` → 500 `memory_safe_state`，`GET /api/project`、章节读仍 200 | ✅ fail-closed 且正文可读 |
| 附带：恢复后重放 `confirm` | 200 且 **同一 `commitId`**、提交数 10→10、`counts` 不变 | ✅ 幂等（印证 issues #64 判读口径） |

最有价值的一条：原子性第一次不只落在「文件对不对」上，而是落在**续写读到了什么**——停点 ②③④⑥ 重启后的注入读数与不崩溃的对照**完全相等**（`memoryUsedChars 182`、`recalledByKind events=3/cognitions=3/states=1`），①⑦ 与「提交从未发生」相等（145，−37 字 / 召回事件 −1）。

两条新判读口径入台账：恢复后 meta **按链尾对齐**（不能按崩溃那次的 target 比，否则把正确收敛读成多写一次）；联合提交的新事件在**同会话内 `storyOrder=0`**、重启后才等于章节序号，而检索按 `StoryOrder` 降序排事件，于是同一次提交在重启前后的注入分别是 177 / 182 字（issues #67）——**跨轮次比较注入前必须先重启一次，否则差值可能全来自这条抖动**。G6 判定不因本轮升格：仍缺真实模型提取质量与人工核对体验。

## 6. 未完成边界（须在真实通道恢复后复核）

1. **真实模型提取质量未验证**：草稿提取沿用深度提取管线，单测/Mock 只证明管线与原子性；真实语料下的事实抽取准确率、冲突判定质量待 G2 真实数据验收同步复核。
2. **无草稿版本历史**：编辑只保留最新内容（`draftRevision` 递增），不保留历史版本，也不支持回滚到上一版草稿。
3. **冲突需人工消化**：草稿确认不会自动解决 `conflicts`；用户只能整体确认或整体拒绝，不能在草稿台内逐条接受/跳过某条记忆变更。
4. **无草稿与章节的并发协同**：多标签页同时改同一章节依靠 `chapter_conflict` 拒绝，无实时刷新/合并。
5. **分析状态联动**：确认提交后项目进入 `pending` 待重新分析，但不会自动触发分析任务；需用户按现有入口触发。
6. **UI E2E 覆盖面**：浏览器端覆盖「并入既有章节 → 确认」与「编辑 → 拒绝」两条路径；「作为新章节」分支由 `TestConfirmDraftNewChapterCommitsChapterAndMemoryTogether` 等自动化用例覆盖，未单独跑浏览器路径。
7. **G7 未包含**：全量重初始化仍按 spec R13 返回 `400 reinit_not_supported`，需 yj 批准修订冻结条款后另行实施。
8. **恢复矩阵已做真实进程版（§5.6），仍缺「真断电/文件系统随机丢块」这一类**：spec §11.3（R4 冻结）的六个崩溃点——①prepared 落盘前、②prepared 后目标文件前、③部分目标文件后、④全部目标文件后 committed 前、⑤committed 持久化后 meta 更新前、⑥meta 更新后响应返回前——除 store 用例（`memory_test.go` 手工把状态改回 prepared 模拟崩溃点 4/2/3，`TestConfirmDraftFailureLeavesNoPartialWrite`、`TestRecoveryReplaysDraftCommitTargets` 覆盖草稿提交目标）之外，2026-10-24 已在真实进程上对 **G6 联合提交**（payload 含正文 + `project.json` + 五类记忆 + `drafts.json`/`changesets.json`）逐点注入并以 `taskkill /T /F` 硬杀后重启收敛，含链尾 `aborted` 补回滚、链断/分叉 fail-closed、恢复后 `confirm` 幂等重放三组附带场景。**仍未覆盖**：真实断电与文件系统层面的随机部分写（本轮注入是按 payload 键改写整文件，模拟的是写序间隙而非磁盘损坏）；以及 committed 之后目标文件被外部改坏的情形——恢复按状态不重放，因此分裂态静默（§5.6 的 5b 场景，issues #66）。
9. **「编辑后退化为只提交正文」无 API 信号**（issues #63）：`confirm` 响应不带「本次应用了几条记忆操作」，调用方无法判定这次是否真的带走了记忆；前端 toast 是唯一的提醒。改语义（`draft_needs_recheck` 拒绝）还是加观测字段（`memoryOpsApplied`）待 yj 拍板。
