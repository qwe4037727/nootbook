# G6 联合提交六崩溃点恢复矩阵（真实进程 + 真实 HTTP，无模型密钥）

> 日期：2026-10-24
> 规格依据：`docs/memory_model_spec.md` §11.2/§11.3（R4 冻结的六个停点，含「对齐而非 +1」不变量）、`task/iter2-详设.md` §4/§8.2、`task/g6-acceptance.md` §5.6 与 §6 第 8 项。
> 命令：`node output/e2e_g6_crash_matrix.mjs`（驱动，10 个场景）+ `node output/e2e_g6_storyorder_probe.mjs`（渲染差取证）+ `node output/e2e_g6_restart_convergence.mjs`（重启抖动取证）。
> 结论：**spec §11.3 的六个停点在真实进程上逐点注入并重启后，恢复语义与台账一致；恢复后的注入读数与「没崩过的对照」逐字相等**。这是机制/原子性证据，不是任何质量证据。**本轮零产品代码改动。**

## 1. 环境与判读前提

| 项 | 值 |
|---|---|
| 被测进程 | `output/novelagent-g6.exe -addr 127.0.0.1:8097 -data <隔离目录>`（本轮独立编译产物，未动任何 Go/前端代码） |
| 模型 | 本地 OpenAI 兼容桩 `output/stub_provider.mjs`（127.0.0.1:8098），`apiKey` 为字面占位串 `stub-key-not-a-secret`，无任何真实密钥进入命令行、配置或证据 |
| 语料 | 上一轮 G6 桩链路跑出的 9 章自撰文本目录 `output/e2e-g6-rec-base`（`memoryRevision=8`，已经重启收敛） |
| 崩溃建模 | `taskkill /T /F` 直接杀死进程（不给优雅退出机会），注入点由驱动脚本在重启前改写磁盘状态得到 |
| 比对口径 | payload 覆盖的 8 个目标文件逐一 SHA-256，**但按 ISO 时间戳归一化后比较**（提交把 `updatedAt` 写成 `now`，跨运行不可能字节相等）；`commits.json`/`meta.json` 不做字节比较，改为结构断言（每次运行 commitId 都是新随机串） |

### 1.1 为什么需要「对照重启」（这是本轮能成立的前提）

首轮试跑发现：每次联合提交后的**第一次启动会做一轮确定性 reconcile**，因此不能拿「崩溃前目录」当基准。取证（`restart-convergence.json`，同一目录连开四轮）：

| 轮次 | 新增提交 | 变更文件 |
|---|---|---|
| 0（刚做过联合提交） | 1 条 `reconcile`（7→8，payload=`events.json`） | `events.json`/`meta.json`/`commits.json`/`project.json`/`knowledge.json` |
| 1 | 0 | 0 |
| 2 | 0 | 0 |
| 3 | 0 | 0 |

所以基准定义为 **P2 = 崩溃点目录 + 一次对照重启**（golden），每个注入场景也都经历「一次重启 + 同一轮 reconcile」，比较才是同口径的。本轮实测该 reconcile 的成因：`reconcileStoryOrderLocked` 把联合提交刚写入的新事件从 `storyOrder=0` 改成它的章节序号（见 §4.3 / issues #67）。

### 1.2 崩溃点怎么造（P1）

在 golden 之前的目录上跑一次真实草稿联合提交，让它成为提交链的**链尾**，于是磁盘上任何状态都能沿 `commitId` 定位到「这次提交的哪一个停点」。实测：

- `POST /api/drafts` → `POST /api/drafts/{id}/extract`（200，3 个 op：`add_event`/`add_cognition`/`update_thread`，`mcs_48738e9d2ec2b07bf3`）
- `POST /api/drafts/{id}/confirm {draftRevision, expectedMemoryRevision:8}` → 200，`commit_24644d4a0f4fb5dba5`、`type=formal`、`basedOn=8 → target=9`、新章 `ch_ebd902ae71d7f14d27`（`revision=1`）、`conflicts=0`
- 该提交的 payload 目标（8 个）：`memory/changesets.json`、`chapters/ch_ebd902ae71d7f14d27.json`、`memory/cognitions.json`、`memory/drafts.json`、`memory/events.json`、`project.json`、`memory/states.json`、`memory/threads.json`
- 落库后果：`memoryRevision 8→9`、`counts.events 9→10`、`counts.cognitions 3→4`、提交数 8→9

## 2. 六个停点的注入与判定（spec §11.3）

统一判据列：**链是否健康**（`GET /api/memory` 的 `safeState`）、**meta 与链尾是否对齐**、**payload 目标是否与 golden 逐字一致（按时间戳归一）**、**注入读数与 golden 的差**。

| # | 停点（spec 原语义） | 注入手法 | 实测 | 判定 |
|---|---|---|---|---|
| 1 | prepared 记录尚未落盘 | 从 `commits.json` 删除该条 + 按 `revertPayload` 回滚 8 个目标 + `meta→8` | 重启后 `revision=8`、链尾仍是原 `reconcile`、`safeState=false`、新章文件**不存在**、`events.json` 里搜不到新事件 ID；注入 145 字（golden 182，**−37**）、召回事件 2（**−1**） | ✅ 未进日志的提交不被重放；世界与「没提交过」逐字一致（连注入侧的差额都等于「少那一条记录」） |
| 2 | prepared 已落盘，目标文件未写 | 记录置 `prepared`、`completedAt=null` + 回滚 8 个目标 + `meta→8` | 重放→`committed`、`occurrences=1`、`revision` 收敛到 10（=对照链尾，非 8+1）、8 个目标与 golden **0 处差异**、新章文件存在 | ✅ 正文 + 五类记忆 + `project`/`drafts`/`changesets` **一起补齐**，无半套 |
| 3 | 部分目标已写、部分未写（半写） | `prepared` + 只回滚 `events/cognitions/threads/states` + 新章（5 个，章文件按「提交前不存在」删除），其余 3 个留在提交后状态 + `meta→8` | 重放补齐缺口 → 8 个目标与 golden **0 处差异**、`committed`、`safeState=false` | ✅ 覆盖写幂等：交错残留被补齐，不产生第三种状态 |
| 4 | 全部目标已写，`committed` 标记未落盘 | 记录置 `prepared`，文件保持崩溃点原样，`meta→8` | 重放后 8 个目标与 golden **0 处差异**（重放是内容上的 no-op）、状态翻成 `committed`、`occurrences=1`、**新事件在 `events.json` 里恰好出现 1 次**（未双重生效）、`revision=10` | ✅ 幂等重放不重复计数，不重复投递 |
| 5 | `committed` 已落盘，meta 未对齐 | 保留 `committed`，`meta→8`；**判别子**：往新章正文尾部追加 8 字标记 `｜恢复探测标记甲` | 标记**仍在**（若误重放文件就会被 payload 覆盖掉）、其余 7 个目标与 golden 0 处差异、`meta` 收敛到 10 且 `lastCommitId` 指向链尾、`GET /api/memory` 与 meta 一致；`manuscriptChars` 471 = golden 463 + 8（正好是标记本身的长度，说明除了标记什么都没动） | ✅ 「只对齐 meta，不重放文件」；对齐是**取链尾**，不是 +1 |
| 6 | 完全一致状态 | 崩溃点目录原样复制（不注入） | 与对照重启同形：8 个目标 0 处差异、1 条确定性 `reconcile`、`commitId` 全部唯一、`safeState=false` | ✅ 一致态重启不产生额外写入 |

补充两个非停点但同为 §11.3 语义的场景：

| 场景 | 注入 | 实测 |
|---|---|---|
| **5b**（committed + 记忆文件被回滚） | `committed` 保留 + 回滚 `events/cognitions/threads/states` + `meta→8` | 恢复**按状态不重放**（这是停点 5 的正确语义），于是 4 个记忆文件**永久停在提交前**而 `meta`/API 收敛到 9、`safeState=false`、注入 145 字（−37、召回事件 −1），正文却已在位 → 「正文有、记忆无」的分裂态无任何信号。该态**不可由进程崩溃到达**（写序 `changesets→chapter→cognitions→drafts→events→project→states→threads` 全程 `status=prepared`，崩溃必被停点 3/4 覆盖），只能由外部损坏/手工回滚造成 → 记 issues #66 |
| **7**（链尾 `aborted` + 文件留在崩溃点） | 记录置 `aborted`、文件不动、`meta→8` | 恢复补做回滚（P1-1）：新章文件被删除、新事件不在 `events.json`、`aborted` 不被重放也不计入链尾、`meta`(8) 与最后一条非 aborted 提交对齐、`lastCommitId` 指回它、`safeState=false`、注入回到 145/−37 |

## 3. 失败路径与重放（两个 fail-closed 对照 + HTTP 重放）

| 场景 | 注入 | 实测 |
|---|---|---|
| **X1** 链断（删中间一条） | 删除 `commit_81a10c650310ce7bbf` | `safeState=true`、`safeStateError="commit chain broken at commit_5c54771fdcd279868f: basedOn=2 want=1 (commit_chain_broken)"`；`POST /api/drafts` → **500 `memory_safe_state`**；`GET /api/project` 与 `GET /api/chapters/{id}` 仍 200（正文可读，只有记忆侧拒绝写）。该目录未做 reconcile，故 `events.json` 与 golden 差 1 处——是「少了一轮抖动」，不是损坏 |
| **X2** 链分叉（复制一条同 target 的记录插在链尾前） | 复制 `commit_6a2747608d2851da99` | 同样 `safeState=true` + `commit_chain_broken`（`basedOn=7 want=8`）：分叉被检测为链不连续，进安全态 |
| **C6b** 恢复后重放 `confirm` | 在对照目录上对**已提交**的草稿再 `POST .../confirm` 一次 | 200 且 `commitId` 与首次**完全相同**（`commit_24644d4a0f4fb5dba5`）、提交数 10→10、`revision` 10→10、`counts` 逐字段不变 → 幂等重放不产生第二次联合提交（印证 issues #64 的契约，判据一律看提交数与 commitId） |

## 4. 三条会影响今后验收判读的新口径

### 4.1 「恢复后的注入」与「没崩过的注入」是同一个数

停点 2/3/4/6 四个场景在重启后的注入读数与对照 golden **完全相等**（`memoryUsedChars=182`、`recalledByKind.events=3`、`cognitions=3`、`states=1`），停点 1/7 与「该提交从未发生过」相等（145 / −37 / 召回事件 −1），停点 5 与 golden 相等且正文只多探针标记本身。**这是本轮最有价值的一条**：原子性不再只是「文件对不对」，而是「续写读到的东西对不对」——崩溃恢复之后进入 prompt 的记忆内容与不崩溃时逐字一致。

### 4.2 meta 收敛取链尾，不是 +1（停点 5 的可复核形式）

`meta→8` 而链尾 `target=10`（对照的 reconcile 也算链尾）时，恢复把 meta 写成 **10**，并把 `lastCommitId` 指向 reconcile 记录；API 与 meta 一致。今后判读「对齐」要按链尾 `target` 比，不能按「崩溃那次提交的 target」比，否则会把正确的收敛读成多写了一次。

### 4.3 联合提交的新事件在当次会话里 `storyOrder=0`，重启后才等于章节序号（issues #67）

同一个目录、同一个 `direction`、同一次提交之后：

| 读数 | 重启前（同会话） | 重启后（reconcile 已跑） |
|---|---|---|
| `memoryUsedChars` | **177** | **182**（+5） |
| 新事件的 `storyOrder` | **0** | **10** |
| `memorySection` 第 9–11 行 | `- 第9章：…` / `- 第3章：…` / `- 林砚在县衙外握紧青玉符`（无章号） | `- 第10章：…` / `- 第9章：…` / `- 第3章：…` |

`recalledByKind` 两侧都是 `events=3`，所以不是「少召回一条」，而是**排序与渲染差**：检索层按 `StoryOrder` 降序排事件候选（`internal/store/memory_retrieve.go:221-226`），而 `0` 又恰好是「来源章已删」用的取值（`internal/store/memory_init.go:596-599`），于是刚提交的新事件在同会话里与「被删章事件」同档排在最后，直到下次启动的 `reconcileStoryOrderLocked` 把它改对。取证脚本与逐行差：`storyorder-probe.json`。

判读后果（此后一律适用）：**跨轮次/跨会话比较注入字数前，必须先做一次重启（或确认 reconcile 已发生），否则两臂的 prompt 长度差可能完全来自这条抖动，而不是被测变量。** 这条对 G3 的 memory-on/off A/B 与 S6 跨会话人评都成立。本轮不改代码（属产品语义，需 yj 拍板是否让 `ConfirmDraft` 在同一次提交里写入新章序号）。

## 5. 本轮不证明什么

- 不证明提取质量、幻觉率、文学质量——桩模型对每个请求返回同一份 JSON，且全程无真实密钥。
- 不证明「真实断电/文件系统丢块」的语义：注入是**按 payload 键改写磁盘文件**，模拟的是写序间隙，不是磁盘随机损坏；随机损坏由 §2 的 5b/X1/X2 侧证 fail-closed 行为。
- 不覆盖 `data/`（用户目录未参与任何读写）；所有目录都在 gitignored 的 `output/` 下，端口 8097/8098（8123/8124 属 Windows 系统保留段）。
- 不改 G1–G8 任何判定：G6「机制/原子性」一侧的自动化空白被真实进程证据补上，质量一侧仍 blocked。

## 6. 交付物

| 文件 | 内容 |
|---|---|
| `e2e_g6_crash_matrix.mjs` | 驱动（P1 造崩溃点 → P2 对照 golden → P3 证抖动一次性 → C1…C7 + C6b + X1/X2），可直接 `node` 复跑 |
| `e2e_g6_restart_convergence.mjs` | 四轮重启抖动取证（§1.1 的表） |
| `e2e_g6_storyorder_probe.mjs` | 同会话 vs 重启后的 `memorySection` 逐行差取证（§4.3） |
| `g6-crash-matrix.json` | 全部场景的结构化读数（含每个场景的 `injected`/`api`/`preview`/`jointRecord`/`meta`/`payloadTargetsDifferingFromGolden`/`verdict`） |
| `crash-matrix-run.log` | 同一轮的控制台输出（人读版） |
| `restart-convergence.json` / `storyorder-probe.json` | 两份取证的原始 JSON |

台账：`task/g6-acceptance.md` §5.6 与 §6 第 8 项、`task/e2e-stub-chain-acceptance.md` §9、`task/roadmap.md` S5 与风险表、`docs/issues.md` #66/#67。
