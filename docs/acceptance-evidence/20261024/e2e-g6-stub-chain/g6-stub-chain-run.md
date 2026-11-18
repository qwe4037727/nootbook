# G6 桩模型端到端验收：正文草稿 + 记忆变更的一次联合提交（真实进程 + 真实 HTTP）

日期：2026-10-24 ｜ 执行方：Qoder agent（验收侧，零产品代码改动）
台账：[task/e2e-stub-chain-acceptance.md](../../task/e2e-stub-chain-acceptance.md)（上一轮 G1–G5 桩链路）
上一轮证据：[../e2e-stub-chain/e2e-stub-chain-run.md](../e2e-stub-chain/e2e-stub-chain-run.md)

## 0. 这一轮证明什么、不证明什么

**证明**：G6 的核心机制——「一次确认同时落正文与记忆变更」——第一次在真实进程 + 真实 HTTP 上被观察到是**单个提交**，并且它的失败路径（记忆基线过期、草稿版本过期、草稿派生批次被单独采纳）都拒绝且**不留部分副作用**；驳回路径同样零副作用。

**不证明**：提取质量、文笔、以及任何「联合确认比分开提交更好写」的价值判断。桩对每个请求返回同一份 JSON，`conflicts=0` 只说明这条链路跑得通，不说明真实模型不会产出冲突。

**判定不变**：G6 仍是 🚧（代码 + 自动化 + Mock UI + 本轮真实 HTTP 链路），**不因此轮升格为「已交付」**；新发现的语义洞见 §5（issues #63/#64/#65）。

## 1. 环境

| 项 | 值 |
|---|---|
| 二进制 | `go build -o output/novelagent-g6.exe .`（工作区 HEAD + 并行 agent 的未提交改动，构建/回归同轮全绿） |
| 服务 | `./output/novelagent-g6.exe -addr 127.0.0.1:8097 -data output/e2e-g6-data` |
| 模型 | 本地 OpenAI 兼容桩 `node output/stub_provider.mjs`（`STUB_PORT=8098`，请求体全量留档 `output/stub-requests-g6.jsonl`） |
| 配置 | `PUT /api/config {baseUrl:"http://127.0.0.1:8098/v1", model:"stub-model", apiKey:"stub-key-not-a-secret", contextChars:12000}` |
| 语料 | 6 章自撰文本（可公开），导入后先跑 analyze/initialize/extract/accept 建立基线记忆，再进草稿链路 |
| 驱动 | `output/e2e_g6_stub_chain.mjs`（36 步，含护栏阶梯）+ `output/e2e_g6_confirm_probe.mjs`（17 步，正向联合提交 + 自愈 + 复活探测） |

密钥：无。`apiKey` 字段是字面占位串，深度提取要求它非空（刻意的护栏）。仓库 `data/` 未触碰（`git status --porcelain -- data/` 为 0 行）。

## 2. 正向链路：一次 confirm = 一个提交 = 正文 + 记忆

| 步 | 结果 |
|---|---|
| `POST /api/drafts {title,content}`（不给 chapterId） | 201；`newChapter=true`、`targetChapterId=ch_41ed9b9a82c7aa337e`、`baseChapterRevision=0`、`baseMemoryRevision=3`、`draftRevision=1` |
| `POST /api/drafts/{id}/extract` | 200；`mcs_0fa3eae2580c62cfed`，`basedOnMemoryRevision=3`、`draftRevision=1`、**3 个 op**（`add_event`/`add_cognition`/`update_thread`），每条 op 的 `sourceRefs` 由服务端定位（`quote:"握紧青玉符"`，rune 偏移 21–26，`needsReview=false`） |
| `POST /api/drafts/{id}/confirm {draftRevision:1, expectedMemoryRevision:3}` | 200；`commitId=commit_93a191ad13bbb25cfe`、`memoryRevision=4`、`memoryChangeSetId=mcs_0fa3eae2…`、`chapter{id,revision:1}`、`project.chapters` 8 条、`conflicts` 0 |
| 提交链 | **只多一个提交**（`commitCountDelta=1`），`type=formal`、`basedOn=3 → target=4`，payload 同时含 `chapter:ch_41ed9b9a82c7aa337e` 与 `events.json / cognitions.json / threads.json / states.json / changesets.json / drafts.json / project.json` |
| `GET /api/memory` | `memoryRevision 3→4`；counts `events 7→8`、`cognitions 1→2`（`states/threads/plans` 不变） |
| `GET /api/memory/changesets?status=proposed` | `[]`（批次被本次提交消费，无残留） |
| `GET /api/drafts/{id}` | `status=committed`、`memoryChangeSetId` 与 `commitId` 都在、`chapterRevision=1` |

即「正文与记忆变更一次联合确认提交」是**单提交原子落盘**，不是两步写。

## 3. 闭环：G6 提交出来的记忆真的进了下一次模型调用

紧接的 `POST /api/continue`（SSE `meta`，未截断）：

```
memoryUsedChars=137  memoryRevision=4  memoryEnabled=true  skippedReview=1
recalledByKind={states:1, events:2, threads:0, cognitions:2, plans:0}
promptSections={characters:59, manuscript:397, memories:137, overview:45, plans:0, recall:0}
promptTotalChars=1315  contextChars=379  truncated=false
```

- 与上一节前的 `memoryUsedChars=105 / recalledByKind.cognitions=1 / events=2` 对照：**联合提交把记忆段从 105 抬到 137 字**，新增的正是草稿派生的事件与认知（`counts` 也同步 7→8、1→2）。
- 桩侧留档的 user prompt（`continue-user-prompt-after-joint-confirm.txt`）同时含「林砚在县衙外握紧青玉符」（草稿派生事件文本）与新章正文「封条印在灯下…」——**同一轮里既看到草稿正文进了上下文，又看到草稿派生的记忆进了记忆段**。
- `analysisStatus=pending / analyzedChapters=7 of 8`：新章落地后分析自动重新排队（这也是 §5 第三条的诱因）。

## 4. 失败路径与护栏（都是真实 HTTP 上的状态码 + 零副作用核对）

| 探测 | 结果 | 副作用核对 |
|---|---|---|
| 草稿派生批次直接 `POST /api/memory/changesets/{id}/accept` | 409 `draft_managed_changeset` | — |
| 同上 `/rebase` | 409 `draft_managed_changeset` | — |
| `confirm` 带过期 `expectedMemoryRevision`（基线+7） | 409 `revision_conflict` | `memoryRevision`、`counts`、提交数、章节数、草稿 `pending_confirmation` **全部不变** |
| `PATCH` 后仍用旧 `draftRevision` confirm | 409 `draft_conflict`（`draftRevision=1 current=2`） | — |
| 对**已提交**草稿的批次再 `accept` | 409 `changeset_terminal`（重放路径见 §5 第二条） | — |
| `POST /api/drafts/{id}/reject {reason}` | 200，`status=rejected` | `memoryRevision` 不变、提交数不变、目标章节内容与 `revision` 逐字未变、批次置 `rejected` |
| `POST /api/memory/initialize {reinitialize:true}` | 400 `reinit_not_supported` | G7 门槛未被这轮绕开（见 [[g7-reinit-approval-gate]]） |

**G4 × G6**：改掉第 8 章里的埋点句 → 草稿派生的 `evt_048eb577c3ae894d39` 置 `evidence_stale`（`recordVersion` 1→2，文本与 `status` 逐字未变）；提交链只多一个 `reconcile`（payload 仅 `cognitions.json` + `events.json`）；预览 `memorySection` 137→105 字、`counts` 里 events 2→1、`skippedReview` 1→3。与上一轮（夹具/单独采纳路径）一致，这次失效标记发生在**经 G6 提交进来的记录**上。

## 5. 三项新发现（本轮只记录，不改任何 Go/前端代码）

1. **issues #63：`PATCH` 草稿会作废并解绑已提取的 ChangeSet，随后 `confirm` 仍成功但静默退化为「只提交正文」。** 第一轮里：创建草稿 → 提取（`mcs_0c69ff6c1d29990f95`，3 op）→ 改一行正文（`PATCH`，`draftRevision` 1→2）→ `confirm {draftRevision:2, expectedMemoryRevision:2}` **返回 200**。落库核对：`memoryRevision 2→3`、提交 `type=formal` 且 payload 里有 `chapter:…`，但 `counts` 完全没变（`events 7`、`cognitions 1`、`threads 1`），草稿的 `memoryChangeSetId` 已被清空，那个批次永久停在 `rejected`，`proposed` 列表里也不会再出现（再 `accept`/`rebase`/`reject` 一律 409 `changeset_terminal`）。语义上「改了正文就不该带上旧提取」是对的，`revision_conflict`/`draft_conflict` 也证明闸门在；问题在于 **API 层没有任何「本次提交不含记忆变更」的信号**（`memoryChangeSetId` 缺省、`conflicts=0`、`memoryRevision` 照样 +1），前端靠 toast「已提取的记忆变更需重新提取」提醒（`web/app.js:1905`），但脚本调用方与将来的 agent 接口会静默丢记忆。建议：`confirm` 响应带 `memoryOpsApplied` 计数，或在「曾提取后被编辑」时返回 409 让调用方显式决定；作废批次应可追溯（现在没有任何字段说明它为什么消失）。
2. **issues #64：`confirm` 对已提交草稿是幂等重放（200 + 同一 `commitId`，不新增提交），而 `PATCH`/`reject`/`extract` 在终态返回 409 `draft_terminal`。** 重放安全是好事（本轮实测第二次 `confirm` 返回 `commit_5c54771fdcd279868f`，提交数没有变化），但同一族操作里判读口径不一致，验收脚本若只看状态码会把「重放」误读成「第二次提交成功」。
3. **issues #65：章节编辑后分析会自动重排队（`analysisStatus=pending`），此时走 `accept` 会先撞上 409 `analysis_running`，而不是变更集自身的状态错误。** 本轮 D 组探测里 `accept` 报 `analysis_running`，而同一批次的 `rebase`/`reject` 报 `changeset_terminal`——错误码指向的守卫不同，人在界面上容易误读成「分析还在跑，稍后再试就行」，实际该批次已经永久终态。

## 6. 边界（不得据此读成什么）

1. 桩返回固定 JSON：**提取质量、冲突检出率、真实模型的 op 分布全部未验证**；`conflicts=0` 不是「不会冲突」。
2. 6 章自撰语料：本轮是**链路/原子性**证据，不是规模证据（规模见 `../g4-5m-heal/`、`../g3-5m-joint/`）。
3. 崩溃点恢复矩阵（spec §11.3 R4 冻结的六个停点）未做：本轮核对的是「拒绝时不留部分副作用」，不是「写一半崩了也不留」。`-race` 全绿与提交链校验仍是既有护栏。
4. 前端体验未走查本轮路径（浏览器只跑过 Mock 模型的 40 章全流程）；§5 第一条的严重性判断借读了 `web/app.js` 的 toast，不等于实测 UI。
5. 驱动脚本自身有一处缺陷已如实留档：第一轮的 `28_idempotent_accept_applied` 因重新提取失败（`draft_terminal`）而拿到 `undefined` 批次 ID，返回 400；正确的幂等探测在第二轮 D 组。

## 7. 复现

```bash
go build -o output/novelagent-g6.exe .
STUB_PORT=8098 STUB_LOG=output/stub-requests-g6.jsonl node output/stub_provider.mjs &   # 8123/8124 在 Windows 保留段
rm -rf output/e2e-g6-data && mkdir -p output/e2e-g6-data
./output/novelagent-g6.exe -addr 127.0.0.1:8097 -data output/e2e-g6-data &
node output/e2e_g6_stub_chain.mjs        # 36 步：护栏阶梯 + 编辑后退化路径
node output/e2e_g6_confirm_probe.mjs     # 17 步：正向联合提交 + 闭环 prompt + G4 自愈 + 批次复活探测
```

产物：`e2e-g6-stub-chain-summary.json`（第一轮全步）、`g6-confirm-probe.json`（第二轮全步）、`continue-user-prompt-after-joint-confirm.txt`（闭环 prompt）、`stub-calls.json`，脚本副本 `e2e_g6_stub_chain.mjs` / `e2e_g6_confirm_probe.mjs` 随本目录留档。`output/` 下为同一份可执行工作件（gitignored）。服务与桩跑完后停止；`data/` 全程未触碰。
