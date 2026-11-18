# issues #67 未测边界实测：深度提取采纳路径同样留 `storyOrder=0`（2026-11-02）

口径声明：**零产品代码改动**（HEAD `f36025e` + 未提交批次，本轮只读代码、只跑真实进程）；**无模型 Key**；仓库 `data/` 未触碰；探针目录与端口全隔离（`output/storyorder-probe-data`、127.0.0.1:8097）；源目录 `output/e2e-stub-data` 只做复制、逐字未改。二进制 `output/novelagent-storyorder.exe` 由当前工作区构建（07:46 UTC）。

## 1. 一句话结论

`#67` 留下的那条未测边界（「本轮只覆盖草稿联合提交路径，深度提取采纳路径是否同样留 0 未测」）**答案是「同样留 0」**，而且是机制性的：全仓 `StoryOrder` 只在两处赋值——`initialize` 回填（`internal/store/memory_init.go:344`）与重启对账重算（`:633`、`:639`），**任何「采纳/提交」写入路径都不填**（`grantEventStatus`（`internal/store/memory_changeset.go:538-559`）定状态、定复核原因，却从不碰该字段）。实测该事件在重启对账后由 `0 → 3`（= 它来源章在 8 章里的序号），并留下一条 `reconcile` 提交、`memoryRevision 5 → 6`；**第二次开进程完全幂等**（revision/提交数/`storyOrder` 均不变）。

## 2. 方法

1. **代码侧穷尽赋值点**：`grep -rn "StoryOrder\s*[:=]|storyOrder" internal/` → 非测试文件里只有 `memory_init.go:344/633/639` 三处写入，另有 `memory_query.go:95/170`（只读视图）与 `novel/memory.go:241`（字段定义）。采纳路径 `memory_changeset.go`、草稿路径 `memory_drafts.go` 均无赋值。
2. **磁盘取证**：桩链路目录 `output/e2e-stub-data/memory/events.json` 里 9 条事件中，8 条 `initialize` 回填的是 `1..8`，唯独 `origin.type=deep_extract` 的 `evt_941aad80d53ae04ebd` 是 `storyOrder: 0`（`status=confirmed`、`needsReview=true`、`reviewReason=evidence_stale`、`recordVersion 2`）。
3. **真实进程判别**：复制成新目录 → 起真实 HTTP 进程（store 打开即触发启动对账）→ 读 `GET /api/memory`、`GET /api/memory/characters/{id}`、`POST /api/continue/preview` → 硬杀 → 再读磁盘。
4. **幂等性再验**：对同一（已修复）目录第二次开进程，比对 `revision / 提交数 / 链尾 commitId / storyOrder`。

## 3. 结果

| 项 | 修复前（复制目录，采纳路径写完后未重启过） | 一次开进程后 | 第二次开进程 |
|---|---|---|---|
| `evt_941aad80d53ae04ebd.storyOrder` | **0** | **3**（= 来源章 `ch_c54e47d84c77d96ee9` 在 8 章中的序号） | 3（不变） |
| `memoryRevision` | 5 | 6（+1） | 6（不变） |
| 提交数 | 5 | 6（新增 1 条 `type=reconcile`、`status=committed`） | 6（不变，链尾同一 `commitId`） |
| `safeState` | — | `false`（提交链完好） | `false` |
| 接口是否透出 `storyOrder` | — | `GET /api/memory` **不透出**（只有 `counts/memoryRevision/lastCommitId/initStatus/safeState`）；只有 `GET /api/memory/characters/c_45301ad360063196d5` 的事件视图给出 `storyOrder=3` | 同左，`200` |

- 该事件因 `needsReview=true` 被停投，本轮预览 `records=0`（**不影响本判据**：判据是字段与提交形态，不是注入量）。
- 证据：[storyorder-summary.json](storyorder-summary.json)（第一次开进程前后 + 接口读数）、[storyorder-second-open.json](storyorder-second-open.json)（幂等）、[storyorder-api-memory.json](storyorder-api-memory.json)（`GET /api/memory` 原始响应形状）；脚本 [e2e_67_deepextract_storyorder.mjs](e2e_67_deepextract_storyorder.mjs)、[e2e_67_second_open.mjs](e2e_67_second_open.mjs) 可重放。

## 4. 对既有结论的影响

1. **`#67` 的红线不是草稿路径专属**，而是「除 `initialize` 外的所有采纳/提交写入」通用：**memory-on/off 的 A/B、跨会话人评、任何两轮注入对照，取读数前都要先让对账发生过一次**。本轮实测顺带确认这条前置动作**很便宜**：只在真有需要修的记录时 +1 revision，第二次开进程零副作用（不会因为反复重启把修订号刷上去）。
2. **修法候选因此可以更精确**（供 yj 拍板）：
   - 若选「提交/采纳时就把新章序号写进记录」——注意 `chapterSnap`（`memory_changeset.go:40-43`）只有 `Content/Revision`、**没有序号**，且 `snapshotChaptersForEvidence`（`:1019-1029`）是按 ID 逐个 `LoadChapter` 的无序 map；要补序号得引入「章节在作品中的次序」查询，属新增依赖。
   - 若选「提交后同进程触发一次对账」——机制与重启完全同源（同一个 `reconcileStoryOrderLocked`），本轮实测证明它的磁盘后果就是「+1 `reconcile` 提交、`storyOrder` 归位」且幂等；代价是同一次采纳会多出一次修订号递增，需要在证据里说明。
   - 观测侧建议：`GET /api/memory` 目前不透出 `storyOrder`，只能经角色事件视图或磁盘核对——把「本次采纳写入的事件序号是否已归位」透出成一个观测字段（纯只读），可省掉验收时的读盘动作。
3. **不改判定**：这属于「同一目录跨轮次注入会抖动」的成因扩展，G3/G6 的既有证据与判定不变；恢复矩阵的对照仍然成立（每个场景都经历了同一次对照重启）。

## 5. 明确不做

- 不改产品代码、不改夹具、不动 `data/`；不新增接口字段（观测建议只登记待拍板）。
- 不据此宣称任何质量侧结论。

## 6. 本轮变更文件

- 新增 `docs/acceptance-evidence/20261024/issue67-deepextract-storyorder/`：本 run doc + 3 份 JSON + 2 个可重放脚本。
- `docs/issues.md`：#67 状态由「未测边界」改为「边界已实测闭合（同样留 0，机制定位到赋值点全集）」，并补幂等与接口透出两条。
- `task/e2e-stub-chain-acceptance.md` §6 第 4 项、`task/roadmap.md` 风险表 #67 行同步。
- 产品代码、`internal/eval`、`eval/cases`、仓库 `data/` 零改动。
