# 桩模型端到端链路验收（G1/G2/G3/G4/G5/G6 的「链路」一侧，2026-10-24）

状态：**链路已在真实进程 + 真实 HTTP 上跑通一次；模型输出质量仍未验证**。本轮零代码改动（无 Go、无前端），只新增证据与台账。

> §1–§7 记录第一轮（G1–G5：analyze → initialize → extract → accept → 注入 → 计划 → 改稿自愈）。§8 记录同日续轮（G6：草稿 → 提取 → 一次联合确认），证据在 [docs/acceptance-evidence/20261024/e2e-g6-stub-chain/](../docs/acceptance-evidence/20261024/e2e-g6-stub-chain/g6-stub-chain-run.md)。§9 记录同日第三轮（G6 联合提交的六崩溃点恢复矩阵：真实进程硬杀重启后的收敛），证据在 [docs/acceptance-evidence/20261024/e2e-g6-crash-matrix/](../docs/acceptance-evidence/20261024/e2e-g6-crash-matrix/g6-crash-matrix-run.md)。§10 记录同日第四轮（issues #60 根因定位：伏笔恒不进注入的五变体单变量判别），证据在 [docs/acceptance-evidence/20261024/issue60-thread-gate/](../docs/acceptance-evidence/20261024/issue60-thread-gate/issue60-thread-gate-run.md)。

授权与边界：用户在 G7 决策中明确「暂不批准，先做 G8 基础设施」，故本任务不实现 G7、不修改冻结条款（spec R13）、不触碰用户 `data/`、不自动提交。同一工作区有并行 agent（workbuddy）在改产品代码，因此本轮只用**独立二进制** `output/novelagent-e2estub.exe`、**独立端口** 8097/8098、**独立空数据目录** `output/e2e-stub-data`。

## 1. 为什么做这一轮

G1/G2 此前的状态是「代码完成 + Go 单测 mock 通过，从未在真实进程里跑过一次」，卡点是密钥与上游网关（issues #43）。但卡点只卡**质量**一侧：`/api/analyze`、`/api/memory/extract`、`/api/plans/generate`、`/api/continue` 都是普通 HTTP 客户端调用，用一个本地 OpenAI 兼容桩就能把「调用是否发出、响应是否被正确解析、落盘是否正确、注入是否随之变化」全部量出来。于是把「零链路验证」推进到「链路已验证、质量未验证」。

## 2. 口径声明（冻结）

- ✅ 证明：契约与链路成立（详见 §3 判据表）。
- ❌ 不证明：提取准确率、幻觉率、schema 遵从度、续写文学质量——桩对每个请求返回同一份固定 JSON。**G1/G2/G3 的完成判定不变。**
- 密钥口径：`apiKey` 填字面占位串 `stub-key-not-a-secret`（深度提取要求 effective key 非空才发起调用，`internal/analysis/extract.go:54-58`）；全程没有任何真实密钥进入命令、配置或证据文件。
- 证据来源口径（issues #56 的要求）：本轮记忆**不是夹具**，而是 `initialize` 映射 + `extract` 提议 + `accept` 采纳，因此「记忆进了 prompt」这次可以归因到真实管线；夹具轮与本轮要分开引用。

## 3. 判据

| # | 判据 | 结果 | 证据 |
|---|---|---|---|
| 1 | `/api/analyze` 真实按章调用模型并合并知识库 | 通过：8 章 → `analysis={status:ready, analyzedCount:8, totalCount:8}`；桩收到 8 次按章 + 1 次总览调用 | [run doc](../docs/acceptance-evidence/20261024/e2e-stub-chain/e2e-stub-chain-run.md) §2｜[stub-calls.json](../docs/acceptance-evidence/20261024/e2e-stub-chain/stub-calls.json) |
| 2 | `initialize` 只做映射、不调模型 | 通过：`stats={mappedEvents:8, mappedStates:1}`、`revision=1`，桩在该步零新增调用 | run doc §2 |
| 3 | `/api/memory/extract` 调模型并产出 `proposed` ChangeSet | 通过：`chaptersDone=1, changeSetsCreated=1, candidatesCreated=1, opsTotal=4, failedChapters=0` | run doc §2 |
| 4 | `accept` 原子落盘并递增 revision | 通过：`{status:applied, commitId:commit_fbc52a5e…, memoryRevision:1→2}`，counts `{events:9, states:2, cognitions:1, threads:1}`（认知与伏笔由 schema 真实产出） | run doc §2 |
| 5 | 已采纳记忆进入注入 | 通过：预览 `memorySection` 105 字（状态/认知/事件三档）、`records` 3 条、种子 3 个 | [summary.json](../docs/acceptance-evidence/20261024/e2e-stub-chain/e2e-stub-chain-summary.json) `16_preview_after_accept` |
| 6 | 计划生成/激活并回灌续写 | 通过：`/api/plans/generate` 出 global+1 卷（`revision=3`）、`activate` 后 `revision=4`，预览出现 `planSection` | run doc §2 |
| 7 | **发给模型的 prompt 里真的有已采纳记忆与已激活计划（闭环）** | 通过：桩把入站 messages 落盘；续写那次 user prompt 833 字，含 `【已确认剧情记忆】- 林砚｜伤势：左臂旧伤开始发凉 / 关键事件 - 林砚在县衙外握紧青玉符` 与 `【计划对齐】全书《旧案主线》｜目标…`；这些文本本身就是桩上一轮返回的字符串 → 输出→落库→被下一次调用消费 的闭环第一次被直接观测 | [continue-user-prompt.txt](../docs/acceptance-evidence/20261024/e2e-stub-chain/continue-user-prompt.txt)｜`promptEvidence.contains`（事件 ✅ 状态 ✅ 计划 ✅ **伏笔 ❌**） |
| 8 | `/api/continue` 真实流式往返 | 通过：SSE `meta→delta→done`，meta `memoryUsedChars=105, planAligned=true, planCount=1, promptSections{memories:105, plans:108}, promptOverBudget=false, truncated=false`，`done.chars=67` | run doc §2 |
| 9 | 改正文 → 已采纳记忆被标记 → 注入随之清零（G4 在采纳路径上） | 通过：`PUT /api/chapters/{ch3}` → 章节 `revision 1→2` → `memoryRevision 4→5` + 一条 `reconcile` 提交；事件/状态/认知/伏笔各 +1 `recordVersion` 并标 `evidence_stale`，`status`（confirmed/active/open）与文本逐字未变；预览 `memorySection=""`、`used=0`、`skippedReview=4` | run doc §4｜[heal-detail.json](../docs/acceptance-evidence/20261024/e2e-stub-chain/heal-detail.json) |
| 10 | 计划不因改稿被自动覆盖（G4 红线） | 通过：`planCount` 仍 1，计划文本未变。新判据：**传播只在计划声明了 `dependsOnThreads` 等显式依赖时发生**（对照 5M 夹具轮删被依赖章 → 计划停投） | run doc §4 |
| 11 | G7 闸门仍生效 | 通过：`{reinitialize:true}` → `400 reinit_not_supported` | run doc §2 |
| 12 | 全轮 `safeState` 恒为 false（空目录起步，不用 longeval 夹具） | 通过：反证 issues #59 属夹具不自洽 | `memoryAfterHeal` 读数 |
| 13 | 回归不因此轮退化 | 通过：本轮前复跑 `go build/vet/test ./...` 全绿（9 包含测试包）；本轮零代码改动 | 会话记录 |

## 4. 发现（只记录，不改代码）

| # | 发现 | 状态 |
|---|---|---|
| issues #60 | 经提取采纳的 `threads`（伏笔）零注入，而夹具伏笔在 5M 轮被注入 → 差异条件未定位（候选：字段缺失 `introducedByEvent`/`knownSinceEvent`、打分门槛、子预算筛除） | 🚧 已复现，待定位 |
| issues #61 | 全部候选因待复核被停投时，`memoryDisabledReason` 字段缺失（空值被省略），只能靠 `skippedReview` 反推 | ⚠️ 记录 |
| issues #62 | `GET /api/memory/characters/{id}` 的 `events[]` 不透出 `needsReview`/`reviewReason`/`sourceRefs`，而 `events.json` 里同一记录已标 `evidence_stale` → 审阅界面「看得到状态/认知失效，看不到事件失效」 | ⚠️ 记录 |
| 基础设施 | 桩模型链路（`stub_provider.mjs` + `e2e_stub_chain.mjs`）可复用：换 `baseUrl` 即切真实模型通道，可继续覆盖 G6 草稿链与崩溃点矩阵 | ✅ 三份脚本随证据留档 |

另记一条操作坑：`8123/8124` 落在 Windows 保留端口段，`bind` 报 `EACCES`（不是权限问题而是系统排除列表），换 8097/8098 即可。

## 5. 明确不做

- 不做任何产品代码修改（`threads` 零注入已在 §10 定位到具体字段与具体行，但改哪一段仍由 yj 拍板）。
- 不宣称 G1/G2/G3 达成：本轮没有真实模型参与，质量一侧仍为空白。
- 不做人评、不做文学质量 A/B、不引入真实语料（本轮语料是自撰 8 章合成文本）。
- 不实现 G7（spec R13 仍冻结）。

## 6. 待拍板

1. 是否把桩模型链路固化为 `cmd/` 下的 e2e 用例或 `internal/eval` 的一个 stop 模式（这样并行 agent 改产品代码后能自动守住「链路不回归」，而不必每次手写脚本）。
2. issues #60 **根因已定位（2026-10-24，见 §10）**，待拍板的是修法与排序：提取侧补实体链接 / 检索侧加回退路线 / 审阅侧加编辑入口（三选一，代价见 §10.4）。在选定之前，G3/G8 的人评判据不能把「伏笔是否被消费」算作已覆盖项——`threads` 档在当前提取通道下恒为空。
3. #61/#62 这类纯观测字段扩展是否合并成一个「审阅可见性」小批一起改（与 #55/#58 同族，共 4 条）。
4. issues #67（`storyOrder=0` 的跨会话注入抖动）是否修：修法是在 `ConfirmDraft` 的同一次提交里就写入新章序号（属产品语义，非纯观测），另一条更便宜的路是在联合提交落地后同进程内触发一次 `storyOrder` 对账（需先确认章节序号回调对新章是否触发，本轮未验证）。不修也可接受，但要把它写成 S6 跨会话人评的**前置动作**（每轮先重启一次再取读数）。**2026-11-02 更新（原两处「未验证」都已有答案，见 `docs/acceptance-evidence/20261024/issue67-deepextract-storyorder/`）**：①这条抖动**不是草稿路径专属**——全仓 `StoryOrder` 只在 `memory_init.go:344`（`initialize` 回填）与 `:633/:639`（重启对账重算）赋值，`grantEventStatus`（`memory_changeset.go:538-559`）与草稿提交都不填，实测深度提取采纳的那条事件磁盘上就是 `0`、起一次进程后 `0 → 3`（来源章序号）并 +1 条 `reconcile` 提交；②「同进程触发一次对账」**后果已被量化且幂等**——第二次开进程零副作用（revision/提交数/链尾 commitId 不变），所以「每轮先重启一次」这个前置动作不会把修订号反复刷上去。仍待拍板的只是：走「采纳时写对序号」（需给 `chapterSnap` 补章节次序，它现仅有 `Content/Revision`）还是走「提交后同进程对账」（同源、代价是同一次采纳多一次修订递增）。另建议一个纯只读观测项：`GET /api/memory` 当前不透出 `storyOrder`，只能经角色事件视图或磁盘核对。
5. issues #66（committed 后目标文件被外部改坏时无检测）是否值得加 payload 摘要校验——现设计只在 `prepared` 窗口内保证原子性，跨窗口的外部损坏一律静默。加校验会增加启动开销，需权衡。

## 7. 变更文件（本轮）

- 新增证据目录 `docs/acceptance-evidence/20261024/e2e-stub-chain/`：`e2e-stub-chain-run.md`、`e2e-stub-chain-summary.json`、`heal-detail.json`、`continue-user-prompt.txt`、`continue-system-prompt.txt`、`stub-calls.json`，以及可重放脚本 `stub_provider.mjs`、`e2e_stub_chain.mjs`、`e2e_stub_probe.mjs`。
- 新增本文件 `task/e2e-stub-chain-acceptance.md`。
- `docs/issues.md`：新增 #60/#61/#62 三行（并修复 #57 行在此前编辑中被串行拼接的表格损坏）。
- `task/g8-acceptance.md`：新增 §4.8（G4 5M 自愈规模口径）、§4 表新增一行、§6 新增第 8 条（夹具 `commits.json` 缺失的验收协议红线）。
- `task/roadmap.md`：§2 验证维度、S3 状态追加、S5 开工前置项、S6 状态追加、§6 风险台账两行。
- `task/g4-acceptance.md`：新增 §7.1（5M 字真实 HTTP 规模实测）。
- 产品代码零改动；`data/` 零改动。

## 8. 同日续轮：G6「正文草稿 + 记忆变更一次联合确认」链路

驱动：`output/e2e_g6_stub_chain.mjs`（36 步，护栏阶梯 + 编辑后退化路径）与 `output/e2e_g6_confirm_probe.mjs`（17 步，正向联合提交 + 闭环 prompt + G4 自愈 + 批次复活探测）。同一套桩（`stub_provider.mjs`，`STUB_PORT=8098`，请求体全量留档 `output/stub-requests-g6.jsonl`）、同一独立端口 8097、独立空目录 `output/e2e-g6-data`；二进制换成本工作区 HEAD + 并行 agent 未提交改动重新构建的 `output/novelagent-g6.exe`（构建/vet/test 同轮全绿）。全程无真实密钥（`apiKey` 是字面占位串）。

### 8.1 判据（真实 HTTP + 落库核对）

| # | 判据 | 结果 |
|---|---|---|
| 14 | 一次 `confirm` = 一个提交 = 正文 + 记忆 | 通过：`commit_93a191ad13bbb25cfe`，`commitCountDelta=1`，`type=formal`、`basedOn=3 → target=4`，payload 同时含 `chapter:ch_41ed9b9a…` 与 `events/cognitions/threads/states/changesets/drafts/project` 七个目标文件；`memoryRevision 3→4`、`events 7→8`、`cognitions 1→2` |
| 15 | 联合提交产出的记忆真的进了下一次模型调用 | 通过：紧随的 `/api/continue` meta `memoryUsedChars 105→137`、`recalledByKind.cognitions 1→2`，桩侧留档 user prompt 同时含草稿派生事件文本与新章正文（`continue-user-prompt-after-joint-confirm.txt`） |
| 16 | 失败路径拒绝且零部分副作用 | 通过：过期 `expectedMemoryRevision` → 409 `revision_conflict`，`memoryRevision`/`counts`/提交数/章节数/草稿状态全部逐字不变；旧 `draftRevision` → 409 `draft_conflict` |
| 17 | 草稿派生批次不可被单独采纳 | 通过：`accept`/`rebase` → 409 `draft_managed_changeset`（真实 HTTP，非单测） |
| 18 | 驳回零副作用 | 通过：`reject` 后 `memoryRevision` 不变、提交数不变、目标章节正文与 `revision` 逐字未变、批次置 `rejected` |
| 19 | G6 提交进来的记录同样会被 G4 自愈标记 | 通过：改第 8 章埋点句 → 草稿派生 `evt_048eb577…` 置 `evidence_stale`（`recordVersion` 1→2，文本与 `status` 未变），只多一个 `reconcile` 提交，预览 `memorySection` 137→105 |
| 20 | G7 闸门仍生效 | 通过：`400 reinit_not_supported` |
| 21 | `confirm` 幂等重放 | 通过：第二次 `confirm` 返回同一 `commitId`，提交数不变（印证 `g6-acceptance.md` §2.6，非缺陷） |

### 8.2 判定不变的理由

桩对每个请求返回同一份 JSON，因此本轮**只证机制**：`conflicts=0` 不等于真实模型不会产出冲突；提取准确率、op 分布、文笔全部未验证。故 `task/g6-acceptance.md` 的结论仍是「代码 + 自动化 + Mock UI E2E + 真实 HTTP 链路」，**G6 不因此轮升格为「已交付」**。

### 8.3 本轮新发现（只记录，零代码改动）

- issues #63（🚧 需拍板）：`PATCH` 草稿会作废并解绑已提取批次，随后 `confirm` 仍返回 200 但**静默退化为「只提交正文」**——`memoryRevision` 照样 +1、`memoryChangeSetId` 被清空、批次永久 `rejected`，API 层没有「本次提交不含记忆变更」的任何信号。这与 `g6-acceptance.md` §2.3「草稿被编辑 → `draft_needs_recheck` 拒绝提交」的措辞不一致：机制上 `PATCH` 先解绑，`ConfirmDraft` 的变更集校验分支（`draft.ChangeSetID != ""` 才进）根本不执行。前端靠 toast 提醒（`web/app.js:1905`），脚本/agent 调用方会静默丢记忆。
- issues #64（✅ 实测印证既有契约）：`confirm` 在终态是幂等重放，而同族 `PATCH`/`reject`/`extract` 返回 409 `draft_terminal`；验收一律以提交数 + commitId 为判据，不看 `confirm` 状态码。
- issues #65（⚠️ 记录）：章节编辑后分析自动重排队时，`accept` 先撞 409 `analysis_running`，而同一批次 `rebase`/`reject` 报 `changeset_terminal`——同一情形给出不同错误码，会诱导重试一个已永久终态的批次。

### 8.4 本轮变更文件

- 新增证据目录 `docs/acceptance-evidence/20261024/e2e-g6-stub-chain/`：`g6-stub-chain-run.md`、`e2e-g6-stub-chain-summary.json`（36 步）、`g6-confirm-probe.json`（17 步）、`continue-user-prompt-after-joint-confirm.txt`、`stub-calls.json`，以及可重放脚本副本 `e2e_g6_stub_chain.mjs`、`e2e_g6_confirm_probe.mjs`。
- `docs/issues.md`：新增 #63/#64/#65。
- `task/g6-acceptance.md`：新增 §5.5（真实 HTTP 链路证据）与 §6 第 8 项边界；§6.1 质量口径不变。
- `task/roadmap.md`：S5 状态追加、§6 风险台账两行。
- 本文件新增 §8。
- 产品代码零改动；`data/` 零改动。

## 9. 同日第三轮：G6 联合提交的六崩溃点恢复矩阵（真实进程，无密钥）

§8 证的是**拒绝时**不留部分副作用；本轮补的是**中断时**不留部分副作用（spec §11.3 R4 冻结的六个停点），仍然不需要密钥。驱动 `e2e_g6_crash_matrix.mjs`（10 场景）+ `e2e_g6_storyorder_probe.mjs` + `e2e_g6_restart_convergence.mjs`，证据与逐场景数字：`docs/acceptance-evidence/20261024/e2e-g6-crash-matrix/g6-crash-matrix-run.md`。

方法上的三个门槛（下次复跑必须先知道，否则读数会骗人）：

1. **基准 = 崩溃点目录 + 一次对照重启**。联合提交后的第一次启动必做一轮确定性 `reconcile`（`storyOrder` 重算），4 轮重启取证显示只有首轮抖动（`restart-convergence.json`：round0 = 1 提交 + 5 文件变更，round1–3 = 0/0）。
2. **文件比对必须按 ISO 时间戳归一化**（提交把 `updatedAt` 写成 `now`），`commits.json`/`meta.json` 改做结构断言（commitId 每次运行都是新随机串）。首轮未做归一化时，C2/C3/C4 报了 2 处「假差异」。
3. **崩溃建模用 `taskkill /T /F` 硬杀**，注入 = 重启前按 payload 键改写磁盘整文件；这不是随机磁盘损坏（见 §9.2）。

### 9.1 判据（全部为真实进程 + 真实 HTTP + 落库核对）

| # | 判据 | 实测 |
|---|---|---|
| 9 | 停点①：prepared 未落盘 | 重启后不被重放，`revision=8`、新章文件不存在、`events.json` 无新事件；注入 145 字 = 对照 182 − 37（恰为「少那一条记录」） |
| 10 | 停点②/③/④：prepared 后任意交错 | 重放 → `committed`、`occurrences=1`、8 个 payload 目标与对照 **0 处差异**；④ 的新事件在文件中恰好 1 次（未双重生效） |
| 11 | 停点⑤：committed 后 meta 未更新 | **不重放文件**（正文里的 8 字探针标记仍在，`manuscriptChars` 471 = 463 + 8）、meta 对齐到**链尾 10** 而非 +1、`lastCommitId` 指回链尾 |
| 12 | 停点⑥：一致态重启 | 与对照同形（0 处差异、只一条 `reconcile`、commitId 全唯一） |
| 13 | 链尾 `aborted` | 恢复补做回滚：新章删除、新事件消失、`aborted` 不计链尾、meta 与最后一条非 aborted 对齐 |
| 14 | 链断 / 分叉 fail-closed | `safeState=true` + `commit_chain_broken`（分别 `basedOn=2 want=1` / `basedOn=7 want=8`）；写侧 500 `memory_safe_state`，`GET /api/project` 与章节读仍 200 |
| 15 | 恢复后 HTTP 重放 `confirm` | 200 且**同一 `commitId`**、提交数 10→10、`counts` 不变（issues #64 的幂等契约在恢复后仍成立） |
| 16 | 原子性的续写侧表述（本轮新增的判据形式） | 恢复后的**注入读数**与不崩溃对照**逐字相等**（`memoryUsedChars 182`、`recalledByKind events=3/cognitions=3/states=1`），未生效的提交则与「从未发生」相等 |

### 9.2 本轮新发现（只记录，零代码改动）

- issues #66（⚠️ 记录）：`committed` 之后目标文件被外部改坏时**没有检测**——恢复按状态决定不重放，于是「正文已提交、记忆还在提交前」的分裂态静默存在（`safeState=false`、meta 正常收敛、注入少 37 字）。该态不可由进程崩溃到达（写序全程 `status=prepared`，崩溃必落在停点③/④被补齐），只能由外部损坏/手工回滚造成；因此不是缺陷而是设计边界的可观测空白（可选修法：payload 摘要校验，需拍板）。
- issues #67（🚧 判读红线）：联合提交写入的新事件在**同会话内 `storyOrder=0`**，重启后 reconcile 才改成章节序号（实测 0→10）。检索按 `StoryOrder` 降序排事件候选，且 `0` 同时是「来源章已删」的取值，于是同一次提交在重启前后的记忆注入分别是 **177 / 182 字**（逐行差见 `storyorder-probe.json`：新事件那条从「无章号、排在最后」变成「第10章、排在最前」）。**跨轮次/跨会话比较注入前必须先重启一次**，否则差值可能全来自这条抖动——G3 的 memory-on/off A/B 与 S6 跨会话人评都受影响。

### 9.3 判定不变的理由与本轮变更文件

桩对每个请求返回同一份 JSON，恢复矩阵测的是协议不变量而非模型行为；**G1–G6 判定一律不变**，G6 质量一侧仍空白，不升格为「已交付」。

- 新增证据目录 `docs/acceptance-evidence/20261024/e2e-g6-crash-matrix/`：`g6-crash-matrix-run.md`、`g6-crash-matrix.json`、`crash-matrix-run.log`、`restart-convergence.json`、`storyorder-probe.json` + 三个可重放脚本副本。
- `task/g6-acceptance.md`：新增 §5.6（六崩溃点实测），§6 第 8 项由「未做」改为「已做 + 剩余边界（真断电/磁盘随机损坏、#66 的静默分裂态）」，头部退出判据与结论同步。
- `docs/issues.md`：新增 #66/#67。
- `task/roadmap.md`：S5 状态追加恢复矩阵一行；风险表把 issues #59 那行的「先修夹具再跑恢复矩阵」更正为**已在自洽目录上跑完**，并补 #67 的跨会话判读红线。
- 产品代码零改动；`data/` 零改动。

## 10. 同日第四轮：issues #60 根因定位（伏笔恒不进注入，无密钥）

台账侧的判据细节、五变体实验与代码行号在 [docs/acceptance-evidence/20261024/issue60-thread-gate/issue60-thread-gate-run.md](../docs/acceptance-evidence/20261024/issue60-thread-gate/issue60-thread-gate-run.md)；本节只记结论与影响。环境：真实二进制 `output/novelagent-g6.exe`（HEAD + 并行 agent 未提交改动，本轮 `build/vet/test` 全绿）、端口 8097、每个变体一份 `output/e2e-g6-rec-base` 副本、`POST /api/continue/preview` 取注入读数（等价性由 issues #57 实测背书，不需要密钥）。

**根因**：检索层判定伏笔相关性只有三条路（[internal/store/memory_retrieve.go:182](../internal/store/memory_retrieve.go#L182)：标题字面串 / 问题字面串 / 关联角色与角色种子相交），而 `add_thread` 装配的记录**从不填 `involvedCharacters`、也不填 `question`**（[internal/analysis/extract.go:395-478](../internal/analysis/extract.go#L395-L478)，DTO `extractThread={title,action,clue,quote}` 根本没有主体字段）→ 第三条路恒假，前两条要求把概括性标题在正文里逐字找到（实测 `青玉符的来历` 在 9 章正文 0 次出现）。实体解析在事件/状态/认知/计划四类推演里都存在（`resolveParticipants` / `resolveSubject` / `resolvePlanCharacters`），**唯独伏笔没有**。

**判据（每个变体只改一个字段、各起一个新进程；`churnedDuringRun` 与 `safeState` 全程 false，读数同口径）**：

| 变体 | 单一改动 | `recalled.threads` | `skippedReview` | 注入字数 |
|---|---|---|---|---|
| V0 | 原样 | 0 | 3 | 145 |
| V1 | 标题→正文字面串 | 0 | **4** | 145 |
| V2 | +清 `needsReview` | 1 | 3 | 176 |
| V3 | 原标题 + 清复核 + 补角色 ID | 1 | 3 | 174 |
| V4 | 原标题 + 只清复核 | **0** | 3 | **145** |

- V4 决定性地排除「`needsReview` 是根因」这一读法：清掉复核标记后依旧不注入。V1 的 `skippedReview 3→4` 证明门槛顺序是**相关性在前、复核在后**——被相关性筛掉的记录连计数都没有。
- V3 证明补一个真实角色 ID 就能注入（145→174 字，伏笔档出现），夹具轮 `thread_jade` 正是这条路。
- V2 顺带暴露语义问题：走标题这条路时，注入的伏笔档内容是从正文整段抄来的句子，不是「未回收的债」的描述。

**两条附带确认**：①server 层没有任何编辑记忆记录的接口（`internal/server` 内零 `StoryThread` 引用；记忆侧只有 overview / initialize(+status) / characters/{id} / changesets* / candidates* / reconcile），人工也无法给伏笔补角色 → 记录一旦产出就永久不进注入；②被筛掉无计数、`memoryDisabledReason` 恒空，与 #55/#58/#61 同族但更隐蔽。

**影响排序**：#60 不是「G6 之后的锦上添花」，它直接封顶 G3 的一类核心收益（吞伏笔），也让 G8 人评判据里「伏笔是否被消费」在当前代码下恒为否。**G1–G6 判定一律不变**（本轮零产品代码改动，定位不等于修复）；修法三选一待 yj 拍板（run doc §6）。

**本轮变更文件**：新增证据目录 `docs/acceptance-evidence/20261024/issue60-thread-gate/`（run doc + `e2e-60-thread-gate-summary.json` + 可重放脚本 `e2e_60_thread_gate.mjs`）；`docs/issues.md` #60 由「🚧 已复现未定位」改为「✅ 根因已定位」并附判据；本节；`task/roadmap.md` 风险台账 #60 行与 S2/S3 表述同步更新；§5「明确不做」与 §6 第 2 条的旧表述（「只定位到现象」「定位是否排在 G6 之前」）同步改为已定位后的排序问题。

