# G3 规模口径：5M 字上「记忆注入 + 前文召回」联合预算的真实 HTTP 观测（2026-10-24）

## 1. 环境与口径

- 二进制：`output/novelagent-ui.exe`（2026-10-24 13:05 构建，HEAD `f36025e` + 82 个未提交文件；与本目录 `../g1-5m-import/` 同一二进制，便于横向比较）。
- 进程与接口：真实 `novelagent` 进程 `127.0.0.1:8097 -data output/g3-5m-httpdata`，只走 HTTP（`POST /api/continue/preview`、`GET /api/memory`、`GET /api/project`、`PUT /api/config`）。
- 语料与记忆来源：`go run ./cmd/longeval -case eval/cases/longform-5m.json -temp output -keep` 生成的隔离数据目录（500 章 / 5,020,294 rune），其 `memory/*.json` 与 `knowledge.json` 来自 `eval/cases/memory/`、`eval/cases/knowledge.json` **人工夹具**，`memoryRevision=12`、`initStatus=not_started`、`counts={events:3,states:3,cognitions:2,threads:2,plans:1}`。
- **这不是模型提取的记忆**：本轮无任何模型调用（无密钥），`GET /api/project` 的 `analysis.status = error`。因此本节证明的是「已确认记忆在 5M 字规模下确实进入 prompt、且与召回段共存」，不证明 G2 的提取质量，也不证明文学一致性收益（G3 的正向收益判定仍需要真实模型 A/B 与人评）。
- 埋点：合成语料第 3 章插入 `青玉符`、第 12 章插入 `左臂旧伤在阴雨天反复发作。`，全库只有第 3 章含「青玉符」（`grep` 确认 1/500 文件命中），方向句显式带上这些线索；对照组方向句无线索。
- 数据目录是夹具目录的副本（`output/g3-5m-httpdata`），原始 `output/longeval-963485058` 保持生成后状态；仓库内 `data/` 未被触碰。

## 2. 结果

### 2.1 联合预算（ctx=12000，同一请求内两档都非零）

| 用例 | 记忆注入/预留 | 召回注入/预算 | 正文窗口 | prompt 总字 | overBudget | 命中章节 | 时延 |
|---|---|---|---|---|---|---|---|
| 埋点方向 · 记忆开 · 召回自动 | **242 / 3000** | **800 / 800** | 8179 | 10420 | false | 3、12 | 1325 ms |
| 埋点方向 · 记忆开 · 召回关 | 242 / 3000 | 0 / 0 | 8931 | 10276 | false | — | 429 ms |
| 埋点方向 · 记忆关 · 召回开 | 0 / 0 | 800 / 800 | **8179** | 10079 | false | 3、12 | 1260 ms |
| 无线索方向 · 记忆开 · 召回自动 | 242 / 3000 | 0 / 0 | 8931 | 10244 | false | — | 1485 ms |

- 判据「记忆段与召回段可同时非零」在 5M 字上成立：`sections={characters:89, manuscript:8197, memories:242, overview:55, plans:55, recall:800, summaries:81, world:0}`。
- 记忆段内容确为远期事实（事件条带 `第3章`/`第12章` 溯源、状态条 `左臂旧伤未愈，阴雨天会复发`、伏笔条 `青玉符的来历｜已知线索：半枚青玉符`、计划条 `追查青玉符：在离城前找到青玉符的另一半`），种子为 `林砚 7.45 / 沈青禾 6.90 / 陆行舟 6.80`，`reason=recent_mention`（来源是最近正文窗口，不是方向句）。
- 同一请求重复两次：`memorySection`、`recallSection`、`sections` 逐字一致（`determinism` 三项 true）。

### 2.2 分档（`PUT /api/config` 只改 `contextChars`，方向句带线索）

| ctx | 记忆 注入/预留 | 召回 注入/预算 | 正文窗口 | prompt 总字 | overBudget | 联合非零 |
|---|---|---|---|---|---|---|
| 1600 | 0 / 0 | 0 / 0 | 1258 | 2092 | false | **false** |
| 4000 | 242 / 1000 | 800 / 800 | 2153 | 4394 | false | true |
| 12000 | 242 / 3000 | 800 / 800 | 8179 | 10420 | false | true |
| 30000 | 242 / 3000 | 800 / 800 | 26166 | 28407 | false | true |

- ctx=1600 时两档同时归零（记忆 `≤1600 → 0` 的冻结退化 + 召回与正文硬下限冲突归零），此时只剩 `overview 55 + plans 55 + manuscript 1276`；`memoryEnabled=false` 且 `memoryDisabledReason` 为**空串**，接口层面无法区分「配置太小」与「库里没记忆」。
- ctx=4000 时预留 1000 而实注 242，正文窗口 2153 = 4000−1000−800−行首对齐余量；此时未用预留 758 字相当于窗口名义值的 35%。

### 2.3 预览与真实续写请求的注入读数等价（2026-10-24 补测）

上面所有数字都取自 `POST /api/continue/preview`。要让这些读数能代表「真实续写时模型看到的 prompt」，需要证明预览与 `POST /api/continue` 的注入路径没有系统性差异。本轮用**不可达桩端点** `baseUrl=http://127.0.0.1:9/v1`（`meta` 事件在建 prompt 之后、模型调用之前发出，因此无需密钥即可拿到真实续写侧的注入读数）对同一数据目录、同一 `ctx=12000`、同一方向句各跑一次两接口：

| 变体 | 请求 | 对比字段数 | 不一致 | `contextChars` | `promptTotalChars` | 分段（memories/recall/manuscript） | 命中章节 |
|---|---|---|---|---|---|---|---|
| 都开 | 埋点方向 · 默认开关 | 15 | **0** | 8179 = 8179 | 10420 = 10420 | 242 / 800 / 8197 | `[3,12]` |
| 关召回 | `useRecall=false` | 15 | **0** | 8931 = 8931 | 10276 = 10276 | 242 / 0 / 8949 | 空 = 空 |
| 关记忆 | `useMemory=false` | 15 | **0** | 8179 = 8179 | 10079 = 10079 | 0 / 800 / 8197 | `[3,12]` |

- 对比的 15 个字段：`contextChars`、`promptTotalChars`、`promptOverBudget`、`promptSections`、`memoryEnabled`、`memoryBudgetChars`、`memoryUsedChars`、`memoryRevision`、`seeds`、`recalledByKind`、`recallEnabled`、`recallBudgetChars`、`recallUsedChars`、`recallChapters`、`planCount`；合计 45 项对比全部相等。SSE 侧在 `meta` 之后收到 `error`（桩端点拒绝连接），属预期。
- 代码侧同构可解释该结果：`handleContinuePreview`（`internal/server/memory_preview.go:160-170`）与 `handleContinue`（`internal/server/server.go:496-506`）以同一组入参调用 `agent.Build`，检索层子预算与 I3-7 全量收缩顺位因此天然对称（入库为 issues #57，状态「已实测为等价」）。
- **意义与边界**：没有模型密钥时，预览是本规模注入读数的合法观测点，不必等真实通道才能做预算/召回度量。但等价性仅在「同一数据目录、同一 `ctx`、同一 `direction/targetChars`」下成立；`/api/continue` 会把 `targetChars` 夹到 `[200,4000]` 而预览原样透传，超界时两处的指令段长度会不同；且这仍不等于模型真收到了该 prompt（由 mock provider 用例 `internal/server/recall_test.go` 覆盖）。
- 证据：[preview-vs-sse.json](preview-vs-sse.json)（脚本 `output/g3_5m_preview_vs_sse.mjs`，已 gitignore）。

## 3. 两项口径级发现（已入库为 issues）

- **记忆预算「预留即扣」**：正文窗口恒为 `ctx − MemoryBudgetChars(ctx) − 召回预算`（[internal/agent/prompt.go:152-154](file:///D:/go/internal/agent/prompt.go#L152-L154)），既不随实际注入量回补，也不随 `useMemory=false` 释放——实测关记忆后 `manuscriptChars` 仍为 8179（差值 0），而召回零命中时整额归还（差值 752，名义 800）。5M 实测浪费 2758 字正文窗口（记忆只注 242 字）。
- **时延主体仍是召回索引**：只开记忆 429 ms，记忆+召回 1325 ms（+896 ms），只开召回 1260 ms；与 `../g1-5m-import/` 的「无记忆」口径（关 380–440 / 开 1150–1530）同量级 → 记忆检索在 5M 上几乎不额外耗时。

## 4. 边界与后续

1. 本节是**夹具记忆**：记录由人工 JSON 落盘，未经 `POST /api/memory/initialize`/extract/审阅（`initStatus=not_started`）。它闭合的是「G3 链路在 5M 规模下是否真的把确认记忆送进 prompt、且与召回共存」，不闭合 G2/G3 的真实模型侧证据。
2. 「记忆是否改善长程一致性」仍未证：需要同一停点的 memory-on/off 真实模型 A/B + 盲评（`cmd/evalreview -axis memory`），卡在模型通道与密钥。
3. A/B 设计约束（本轮实测得出）：关掉记忆**不会**把 3000 字还给正文，因此 memory-off 对照臂的 prompt 比 on 臂短 341 字（10420→10079），两臂正文一样长。这对「只看记忆段差异」是干净的，但**不等于**「没有记忆时模型本可以读到更多正文」——若要后者，需先改预留回补口径。
4. 未覆盖：真实语料（《无边悔恨》17.7 万字）的记忆初始化仍需模型；5M 真实作品导入仍未做；`schemaVersion` 在检索侧不校验（既有记录，未在本轮复现）。

## 5. 复现

```bash
# 1) 生成含夹具记忆的 5M 隔离目录（dry-run，无需密钥）
go run ./cmd/longeval -case eval/cases/longform-5m.json -temp output -keep
# 2) 复制为 HTTP 探针目录并启动服务
cp -r output/longeval-<随机后缀> output/g3-5m-httpdata
./output/novelagent-ui.exe -addr 127.0.0.1:8097 -data output/g3-5m-httpdata
# 3) 跑探针（写回 docs/acceptance-evidence/20261024/g3-5m-joint/g3-5m-joint-summary.json）
node output/g3_5m_joint.mjs
# 4) 预览 vs 真实续写 SSE 的等价性走查（写同目录 preview-vs-sse.json；
#    脚本先把副本配置置为不可达桩端点 baseUrl=http://127.0.0.1:9/v1、model=stub-model、ctx=12000）
node output/g3_5m_preview_vs_sse.mjs
```

脚本随证据留在 `output/`（已 gitignore），本轮无 Go/前端代码改动；探针服务与桩端点已停止。步骤 4 会 `PUT /api/config` 且**不恢复**为桩前的值，因此必须在数据目录**副本**上跑，不可指向仓库 `data/`（本轮全程未触碰 `data/`，副本目录用完即弃）。
