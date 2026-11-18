# G4 规模口径：5M 字上「改稿/删章 → 记忆自愈 → 续写注入随之收缩」的真实 HTTP 实测（2026-10-24）

## 1. 环境与口径

- 二进制：`output/novelagent-ui.exe`（2026-10-24 13:05 构建，HEAD `f36025e` + 82 个未提交文件），与 `../g3-5m-joint/`、`../g1-5m-import/` 同一个，便于横向比较。
- 进程与接口：真实进程 `127.0.0.1:8097 -data output/g4-5m-httpdata`，只走 HTTP（`PUT /api/config`、`GET /api/project`、`GET|PUT|DELETE /api/chapters/{id}`、`POST /api/memory/reconcile`、`GET /api/memory`、`GET /api/memory/characters/{id}`、`GET /api/plans`、`POST /api/continue/preview`、`POST /api/memory/initialize`）。
- 数据目录：`cmd/longeval -case eval/cases/longform-5m.json` 生成的 500 章 / 5,020,294 rune 隔离目录的**副本**，再用 `output/g4_5m_seed_refs.mjs` 人工补写章节级证据与依赖链接：
  - `evt_1.sourceRefs = [第3章, revision=1, quote="青玉符"@59..62]`
  - `state_lin_wound.sourceRefs = [第12章, revision=1, quote="左臂旧伤在阴雨天反复发作。"@77..90]`
  - `cog_1.sourceRefs = [第5章, revision=1, quote="这段引文故意与正文不符"@10..19]`（修订号对得上、引文对不上，用来触发 `quote_mismatch`）
  - `thread_jade.introducedByEvent = evt_1`、`plan_near.dependsOnThreads = [thread_jade]`
- **夹具口径**：记录内容与依赖链是人工构造的（`initStatus=not_started`、零模型调用、`analysis.status=error`）。本轮证明的是「自愈链路与注入侧后果在 5M 规模的真实 HTTP 上成立」，不证明 G2 的提取质量，也不证明文学一致性收益（承接 issues #56 的声明要求）。
- 全轮 `contextChars=12000`，未配置模型（预览不需要，见 issues #57 的等价性实测）。

## 2. 结果

### 2.1 三种失效原因与注入收缩（同一次会话，逐步推进）

| 步骤 | 动作 | `memoryRevision` | 注入记录 | 记忆段字数 | 计划段 | 召回命中章 | 正文窗口 | prompt 总字 |
|---|---|---|---|---|---|---|---|---|
| 基线 | — | 12 | `state_lin_wound, cog_2, cog_1, thread_jade, evt_2, evt_1, plan_near`（7 条） | 242 | 1 条 / 55 字 | 3、12 | 8179 | 10420 |
| A 改第 12 章 | `PUT /api/chapters/{ch12}`，删掉埋点句（PUT 280 ms，章节 revision 1→2） | **13** | 少 `state_lin_wound` → 6 条 | 212 | 1 条 / 55 字 | 3 | 8179 | 10318 |
| B 全量复核 | `POST /api/memory/reconcile {}`（**352 ms**，`marked=1`） | **14** | 少 `cog_1` → 5 条 | 195 | 1 条 / 55 字 | 3 | 8179 | 10301 |
| B' 单章复核 | `POST /api/memory/reconcile {chapterIds:[ch12]}`（**4 ms**，`marked=0`） | 14 | 同上 | — | — | — | — | — |
| C 删第 3 章 | `DELETE /api/chapters/{ch3}`（307 ms，全书 500→499 章） | **15** | 少 `evt_1`、`thread_jade`、`plan_near` → 2 条 | **102** | **0 条 / 0 字** | 无 | 8931 | 10002 |

- 三条失效原因都被真实触发并可读（`GET /api/memory/characters/c_lin` 透出 `needsReview` + `reviewReason` + 原 `sourceRefs`）：A→`evidence_stale`（章节 revision 不匹配）、B→`quote_mismatch`（修订号对得上但引文对不上，只有全量扫才抓得到）、C→`chapter_deleted`（证据章节被删），并沿依赖链传播 `thread_jade → plan_near` 为 `dependency_stale`。
- **注入侧后果是单调可测的**：`memorySection` 242 → 212 → 195 → 102 字，每一次收缩都恰好对应「刚被标记为待复核的那条记录」从注入列表里消失，即迭代 3 冻结的「`needsReview` 一律不注入」红线在 5M 上、在三种原因下都生效。
- **计划不被自动覆盖（G4 红线）**：删章后 `GET /api/plans` 里 `plan_near` 仍是 `status=active`、`title/objective/dependsOnThreads` 逐字未变，只有 `needsReview=true`、`reviewReason=dependency_stale`、`recordVersion 0→1`；同时续写侧的**停投**发生了（`planCount 1→0`），因为 `LoadActivePlans` 过滤 `Status==active && !NeedsReview`。语义是「停投不改稿」，不是把计划改掉或取消。
- 召回侧同向收缩：改/删埋点章后命中从 `[3,12]` → `[3]` → 无命中，无命中时整额归还窗口（8179→8931），与 issues #54 记录的「召回会还、记忆不还」不对称完全一致；记忆段只占 102/3000 时正文窗口仍是 `ctx − 3000 − 召回` → 空置 2898 字，再次印证 #54。

### 2.2 审计与原子性

- `memory/commits.json` 新增 3 条，`type` 全为 `reconcile`，`basedOn→target` 为 12→13、13→14、14→15，每次自愈恰好 +1。
- **最小写集**成立：13 只提交 `states.json`、14 只提交 `cognitions.json`、15 提交 `events.json + plans.json + threads.json`，未受影响的记录文件没有被重写。
- 章节侧：`chapters/` 从 500 个文件变为 499 个（删除即移除文件），`GET /api/project` 的 `chapters` 长度同步为 499。

### 2.3 G7 闸门在变更后的目录上仍然有效

- `POST /api/memory/initialize {"reinitialize":true}` → `400 {"code":"reinit_not_supported"}`，且 `memoryRevision` 保持 15 不变（检查先于状态判定、零落盘）。`GET /api/memory/initialize/status` 仍为 `not_started`。见 [[g7-reinit-approval-gate]]。

## 3. 一项正向结论与一项新发现

- **正向**：冲突提示的记忆侧确实可观测——恢复健康链后（见 3.2），同一预览响应里 `skippedReview=6`，即 5M 上「有 6 条记忆因待复核未注入」是能直接读到的数字（G4 §4 的界面口径有数据源）。
- **发现一（issues #58）**：**计划段被停投时没有停用原因**。`planCount` 从 1 变 0、`planEnabled=false`，但预览/SSE 没有任何字段说明「是因为证据失效被暂时停投」还是「本来就没有激活计划」；只能另跑 `GET /api/plans` 交叉比对 `needsReview`。与 #55 同族（`ctx≤1600` 时 `memoryDisabledReason` 也是空串），但这条影响 G4/G5 的验收判读：注入收缩的可观测性只覆盖记忆档，不覆盖计划档。
- **发现二（issues #59）**：**`cmd/longeval` 夹具的数据目录重启即进入 `memory_safe_state`**。夹具 `eval/cases/memory/` 带 `meta.json{memoryRevision:12}` 但**没有 `commits.json`**；`seedMemory` 只拷存在的文件。首轮写入（本轮是 3 次 reconcile 提交）后重启进程，提交链校验要求第一条非 aborted 提交 `basedOn=0`，实测报 `commit chain broken at commit_0e456fdd…: basedOn=12 want=0`，于是整个记忆子系统 fail-closed：`GET /api/memory` 的 `safeState=true` + `safeStateError` 说明原因，预览 `memoryDisabledReason="safe_state"`，**记忆/计划/召回注入同时归零、正文窗口回补到 8931**。
  - 用 `output/g4_5m_chain_repair.mjs` 把那 3 条提交重编号成 `0→1→2→3` 并把 `meta.memoryRevision` 修成 3 之后重启：`safeState=false`、注入恢复（`skippedReview=6`）——证明这是**夹具不一致**而不是数据损坏，也证明 safe 态是可修复可恢复的。
  - 影响面：任何「写入记忆 + 重启进程」的验收或长跑（尤其 G6 联合提交、跨会话人评）都会踩到；本轮的 5M 自愈测量之所以没受影响，是因为它整个会话没有重启。**不得把这类「注入读数突然全 0」读成「记忆失效」**。
- 证据：`g4-5m-heal-summary.json`（逐步读数与 `memorySection` 原文）、`review-visibility-safe-state.json`（fail-closed 现场）、`review-visibility.json`（修复链后的健康读数）。

## 4. 边界

1. 依赖链只构造了一条（事件→伏笔→计划），多层计划树、`parent_completed`、`evidence_unlocated` 等原因码仍只有单测覆盖，本轮未在 5M 上复现。
2. `quote_mismatch` 是靠人工写坏引文触发的；真实来源（提取时定位偏移）下的发生率仍取决于模型通道（issues #43）。
3. 修复动作是「人工重编号提交链」，产品侧尚无「链重建」入口；全量重建即 G7，仍被 spec R13 冻结。
4. 删除中间章会让后续章节 `index` 整体前移，本轮只验证了记忆侧标记与注入读数，没有把「重排后 `storyOrder` 是否仍与正文一致」列进判据（该点由既有 `TestStoryOrderReconcile*` 覆盖）。
5. G4 的**收益判定**仍未做：需要真实语料 + 真实模型跑「改中段一章 → 续写是否避开失真记忆」（`task/g4-acceptance.md` §8.4）。本轮把「链路在 5M 上成立且可观测」从单测结论升级为真实 HTTP 实测结论。

## 5. 复现

```bash
# 1) 生成 5M 隔离目录（dry-run，无需密钥）
go run ./cmd/longeval -case eval/cases/longform-5m.json -temp output -keep
# 2) 复制并补写章节级证据与依赖链
cp -r output/longeval-<随机后缀> output/g4-5m-httpdata
node output/g4_5m_seed_refs.mjs output/g4-5m-httpdata
# 3) 启动服务并跑自愈走查（写 g4-5m-heal-summary.json）
./output/novelagent-ui.exe -addr 127.0.0.1:8097 -data output/g4-5m-httpdata
node output/g4_5m_heal.mjs
# 4) 重启服务 → 复现 safe_state 现场；再跑可见性探针
./output/novelagent-ui.exe -addr 127.0.0.1:8097 -data output/g4-5m-httpdata
node output/g4_5m_review_visibility.mjs        # 写 review-visibility-safe-state.json 前先改名保留
# 5) 修复提交链后重启 → 证明可恢复
node output/g4_5m_chain_repair.mjs
./output/novelagent-ui.exe -addr 127.0.0.1:8097 -data output/g4-5m-chainfixed
node output/g4_5m_review_visibility.mjs
```

脚本随证据留在 `output/`（已 gitignore），本轮无 Go/前端代码改动；仓库 `data/` 未触碰；探针服务已停止。
