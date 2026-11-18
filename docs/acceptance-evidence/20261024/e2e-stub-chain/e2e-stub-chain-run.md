# 桩模型端到端链路：真实二进制 + 真实 HTTP 第一次跑通「分析 → 提取 → 采纳 → 注入 → 自愈」（2026-10-24）

## 0. 这一轮证明什么、不证明什么

此前 G1/G2 的「模型分析 → 深度提取 → ChangeSet 采纳 → 记忆进 prompt」从来没有在**真实进程**里跑过一次——不是代码不通，而是密钥/网关阻断，证据只停在 Go 单测的 mock provider 层。本轮用**本地 OpenAI 兼容桩端点**替代真实模型（`output/stub_provider.mjs`，`http://127.0.0.1:8098/v1`），第一次让真实二进制把整条链走完，并且把「真正发给模型的 prompt」落盘取证。

- ✅ 本轮证明：契约与链路在真实 HTTP 上成立——`/api/analyze` 会按章调用模型并把结构化结果合并进知识库；`/api/memory/initialize` 只做映射、不调模型；`/api/memory/extract` 会调用模型并产出 `proposed` ChangeSet；`accept` 会原子落盘并递增 `memoryRevision`；`/api/plans/generate` 会调用模型产出计划树；已采纳的记忆与已激活的计划会进入下一次 `/api/continue` 的 prompt；改正文会触发按章复核并使注入收缩。
- ❌ 本轮不证明：任何**模型输出质量**。桩对每个请求返回同一份固定 JSON，所以提取准确率、幻觉率、文学一致性、续写文笔全部未验证。因此 **G1/G2/G3 的判定不变**（G2 仍是「代码完成，零真实数据验证」，本轮把「零链路验证」改成「链路已验证、质量未验证」）。

## 1. 环境与口径

- 二进制：`output/novelagent-e2estub.exe`（本轮 14:08 `go build -o` 单独产物，HEAD `f36025e` + 82 个未提交文件；不复用/不覆盖 `novelagent-ui.exe`，避免与同一工作区并行的 workbuddy 撞产物）。构建前复跑：`go build/vet/test ./...` 全绿（9 包含测试）。
- 数据目录：`output/e2e-stub-data`——**从空目录起步，不使用 `cmd/longeval` 记忆夹具**，因此这一轮不受 issues #59 影响（全轮 `safeState=false`）。这同时反证 #59 是夹具不自洽，而不是产品必然写坏链。
- 模型配置：`PUT /api/config {baseUrl:"http://127.0.0.1:8098/v1", model:"stub-model", apiKey:"stub-key-not-a-secret", contextChars:12000}`。密钥位置填的是**字面占位串**，不是任何真实密钥；深度提取要求 effective key 非空才会发起调用（`internal/analysis/extract.go:54-58`），故必须占位。
- 语料：自撰 8 章 / 约 700 字（合成、不含任何未公开作品），第 3 章埋入三处可定位引文——`握紧青玉符`、`左臂旧伤开始发凉`、`青玉符上的刻痕`，与桩返回的 `quote` 逐字一致，使服务端 `locateQuote` 能定位（引文对不上会落 `evidence_unlocated` 而不进注入）。
- 端口坑：8123/8124 落在 Windows 保留端口段，`bind` 报 EACCES（不是权限问题而是系统排除列表），改用 8097/8098。

## 2. 逐步读数

| 步 | 动作 | 结果 |
|---|---|---|
| 导入 | `POST /api/import` | 200，`importedChapters=8`，自动分章按标题行切分 |
| 分析 | `POST /api/analyze`（异步）+ 轮询 | `analysis={status:"ready", analyzedCount:8, totalCount:8}`；桩共收到 8 次按章调用（system 1164 字 + user 102–295 字）+ 1 次总览合成调用 |
| 记忆初始化 | `POST /api/memory/initialize {}` → 202 | 状态 `ready`，`stats={mappedEvents:8, mappedStates:1, skippedInvalid:0}`，`memoryRevision=1`，counts `{events:8, states:1}`——**此步零模型调用**（与 `ExtractLauncher` 分开） |
| 深度提取 | `POST /api/memory/extract {chapterIds:[ch3]}` → 202 | `status=done`，`stats={chaptersDone:1, changeSetsCreated:1, candidatesCreated:1, opsTotal:4, failedChapters:0}` |
| 变更集 | `GET /api/memory/changesets?status=proposed` | 1 个：`mcs_59fc5e40095f0557d7`，`proposer=deep_extract`，`status=proposed`，`basedOnMemoryRevision=1`，`opCount=4`，`conflictCount=0` |
| 采纳 | `POST /api/memory/changesets/{id}/accept {}` | 200 `{status:"applied", commitId:"commit_fbc52a5e98de804ec0", memoryRevision:2}`；counts → `{events:9, states:2, cognitions:1, threads:1}`（认知与伏笔由本轮 schema 真实产出，非夹具） |
| 预览注入 | `POST /api/continue/preview` | `memorySection` 非空 105 字（状态 + 认知 + 关键事件三档）、`records` 3 条、`seeds` 林砚 10.4 / 沈青禾 7.8 / 县衙 5.8、`budget=3000` |
| 计划生成 | `POST /api/plans/generate {endingTone,volumeCount:1}` | 200，`memoryRevision=3`，产出 global《旧案主线》+ 1 卷，`involvedCharacters` 解析为真实 `c_` ID |
| 计划激活 | `POST /api/plans/{id}/activate {}` | 200 `memoryRevision=4`、`commit_33f03f281ff2823dc4`；`recordVersion 1→2` |
| 预览（含计划） | `POST /api/continue/preview` | `planSection` 出现：`全书《旧案主线》｜目标…｜约束…｜候选发展…`，`planCount=1` |
| 真实续写 | `POST /api/continue`（SSE） | 事件序列 `meta→delta→done`；meta：`memoryEnabled=true, memoryUsedChars=105, planAligned=true, planCount=1, promptSections={memories:105, plans:108, manuscript:361, characters:59, overview:45, world:32}, promptTotalChars=1438, promptOverBudget=false, truncated=false`；`done.chars=67` |
| 改正文 | `PUT /api/chapters/{ch3}`（删掉埋点句） | 章节 `revision 1→2`；`memoryRevision 4→5`，新增一条 `type=reconcile` 提交 |
| 自愈后果 | `POST /api/continue/preview` | `memorySection=""`、`used=0`、`counts` 全 0、`skippedReview=4`、`memoryEnabled=false`；**计划段仍在注入**（`planCount=1`） |
| G7 闸门 | `POST /api/memory/initialize {reinitialize:true}` | `400 {"code":"reinit_not_supported"}`（见 [[g7-reinit-approval-gate]]） |

提交类型序列（`memory/commits.json`）：`initialize → formal → formal → manual_fix → reconcile`，与「映射 / 采纳变更集 / 生成计划 / 激活计划 / 改章复核」一一对应。

## 3. 闭环证据：模型侧确实收到「已采纳记忆 + 已激活计划」

桩端点把每个入站请求的完整 messages 落盘（`stub-requests.jsonl`，摘要见 `stub-calls.json`，续写那次单独导出为 [continue-user-prompt.txt](continue-user-prompt.txt)、[continue-system-prompt.txt](continue-system-prompt.txt)）。续写请求（`stream=true`，共 1 次）的 user prompt 833 字里逐段可读到：

```
【已确认剧情记忆（来自正文抽取，效力低于下方最近正文）】
人物当前状态：
- 林砚｜伤势：左臂旧伤开始发凉
人物认知：
- 林砚｜认定青玉符能打开旧案缺口
关键事件（近因优先）：
- 林砚在县衙外握紧青玉符

【计划对齐（仅约束走向，不得覆盖已确认事实）】
- 全书《旧案主线》｜目标：让林砚沿青玉符追查三年前旧案，不提前揭露真凶。
  约束：不提前收束；不改变已确认事实
  候选发展：在县衙重验卷宗；沈青禾提出伤药线索
```

这四行文本本来就是**桩在上一轮返回给产品的字符串**，现在原样出现在产品为下一次调用组装的 prompt 里——即「模型输出 → 落库为记忆 → 被下一次模型调用消费」这个闭环第一次被直接观测到，而不是靠 `agent.Build` 的代码对称性推断（issues #57 那条推断在此升级为实测，且覆盖采纳路径而非夹具路径）。

程序化判定（`e2e-stub-chain-summary.json` → `promptEvidence.contains`）：事件 ✅、状态 ✅、计划目标 ✅、**伏笔标题 ❌**（见 §5 发现一）。

## 4. G4 自愈在「真实链路的记忆」上的行为

改第 3 章后（`PUT /api/chapters/{id}`，正文里三处埋点句全删）：

| 记录 | 通道 | `reviewReason` | `recordVersion` | status/内容 |
|---|---|---|---|---|
| `evt_941aad80d53ae04ebd` 林砚在县衙外握紧青玉符 | events | `evidence_stale` | +1 | `confirmed` 未变、文本逐字未变 |
| `state_fb69ac1f9b4073d884` 伤势=左臂旧伤开始发凉 | states | `evidence_stale` | +1 | 同上 |
| `cog_77fd8ba4c74e33100b` 认定青玉符能打开旧案缺口 | cognitions | `evidence_stale` | +1 | 同上 |
| `thread_83ec6a9c5dfe6ef919` 青玉符的来历 | threads | `evidence_stale` | 1→2 | `status=open` 未变 |
| `state_8f3a39e6c64bd9a2a6`（映射回填的状态） | states | **`backfill_unverified`** | — | 未变 |

- 与 5M 夹具轮的差别：这次被标的是**由模型产出、经 ChangeSet 正式采纳**的记录，`sourceRefs{chapterId,chapterRevision,quote}` 是服务端 `locateQuote` 写进去的真实偏移（`rev=1` vs 章节现 `rev=2` → 判 `evidence_stale`），于是「改稿 → 已采纳记忆自我标记 → 注入随之清零」第一次在真实采纳路径上闭环。
- **一条接口一致性问题（现场发现）**：同一批记录，`GET /api/memory/characters/{id}` 的 `events` 元素**不透出** `needsReview`/`reviewReason`/`sourceRefs`（读到的是 `needsReview=false`、`refs=[]`），而 `memory/events.json` 里同一 `evt_941aad80d53ae04ebd` 是 `needsReview=true, reviewReason=evidence_stale, recordVersion=2, refs=[{ch…,rev:1,quote:"握紧青玉符"}]`；`states`/`cognitions` 元素则正常透出。即「审阅视图看得到状态/认知失效，看不到事件失效」——入 issues #62。
- **`skippedReview` 的语义据此可读**：它是「检索候选里被待复核挡掉的条数」，不是「库里待复核总数」。续写 meta 在健康态是 1（只有回填状态 `state_8f3a39e6c64bd9a2a6` 命中种子又被挡），自愈后是 4（事件 + 状态 + 认知 + 伏笔）。另外 8 条映射回填事件是 `status=proposed`（未采纳），本就不进检索。
- **计划不被正文改动自动覆盖，也不被停投**：`planCount` 仍是 1。原因是这份计划不绑定章节证据（`createdFromCommit=commit_fbc52a5e98de804ec0`，无 `dependsOnThreads`）。对照 5M 夹具轮（人工写了 `plan_near.dependsOnThreads=[thread_jade]`，删掉被依赖章 → 计划 `dependency_stale` → 停投），可得一条此前没有的判据：**G4 的传播只发生在计划确实声明了对受影响事件/伏笔的依赖时**；无依赖的计划不会因改稿被停投。这是对的语义，但也说明「计划对齐」通道对正文改动天然不敏感，G5 的「计划随改稿复核」目前只能靠显式依赖边。
- 自愈后预览的 `memoryEnabled=false`，而响应里**没有 `memoryDisabledReason` 字段**（空值被省略），只能靠 `skippedReview=4` 反推真实原因是「全部候选都待复核」——见 §5 发现二。

## 5. 三项新发现（已入台账，本轮不改任何代码）

1. **issues #60：经深度提取采纳的伏笔没有进注入，而夹具伏笔进过——差异条件未定位。** 本轮 `thread_83ec6a9c5dfe6ef919`（`青玉符的来历`，`status=open`，引文已被服务端定位、非 `evidence_unlocated`）在采纳后 `counts.threads=1`，但预览与 SSE meta 的 `recalledByKind.threads` 一直是 0，`memorySection` 里没有伏笔档，闭环 prompt 文本里也搜不到伏笔标题；改章后它又能被正常标记 `evidence_stale`（说明它确实在复核与检索候选集合里）。对照 5M 夹具轮：同一份预览里 `thread_jade` 是被注入的 7 条之一（`docs/acceptance-evidence/20261024/g4-5m-heal/g4-5m-heal-run.md` §2.1）。两条通道一条能注一条不能，差别只可能出在记录字段（夹具手写的 `introducedByEvent`/`knownSinceEvent` 等链接 vs 提取产出的字段集）或打分门槛上，本轮没有把它定位到行。**影响面**：`threads` 是「未回收的债」，进不了 prompt 就意味着「吞伏笔」这一类失败模式目前不可能被记忆系统改善——而这正是 G3 的立项初衷之一。
2. **issues #61：`memoryDisabledReason` 缺「全部候选都因待复核被停投」这一档。** 与 #55（低 ctx 静默归零时该字段为空）、#58（计划停投无原因）同族，但这条发生在**默认配置 + 真实采纳记忆**下，最容易在人评里被误读成「记忆失效」。建议枚举 `all_pending_review` 并在预览/SSE 透出（纯观测字段，不改语义）；同时前端文案需要把 `skippedReview` 与 `memoryEnabled` 组合起来判断，而不是只看有没有 reason。
3. **issues #62：角色记忆视图不透出事件级审阅信息。** `GET /api/memory/characters/{id}` 的 `events[]` 缺 `needsReview`/`reviewReason`/`sourceRefs`（读到 `needsReview=false`、`refs=[]`），而 `memory/events.json` 里同一条记录是 `needsReview=true, reviewReason=evidence_stale, refs=[{chapterId, chapterRevision:1, quote}]`；`states[]`/`cognitions[]` 却完整透出。即「审阅界面能看到状态/认知失效，看不到事件失效」。属 DTO 字段遗漏（纯观测，不改语义），但直接影响 G4 的人工复核入口能否数对。

## 6. 边界（不得据此读成什么）

1. 桩对每个请求返回同一份 JSON：**提取质量、幻觉率、schema 遵从度完全未验证**；G1/G2 的真实模型验收仍缺（决策 A/F、issues #43）。
2. 语料只有 8 章自撰文本：本轮是**链路**证据，不是**规模**证据（规模见 `../g4-5m-heal/`、`../g3-5m-joint/`）。
3. 续写正文由桩固定返回 68 字：`truncated=false`、`done.chars=67` 只证截断标志链路，不证任何写作质量。
4. 未跑 G6（草稿 + ChangeSet 一次联合提交）：本轮停在「已采纳记忆 + 注入 + 自愈」。下一步可把 `POST /api/drafts → /extract → /confirm` 接进同一个桩链路。
5. `GET /api/knowledge` 返回 404（路由不存在，issues #52 已记录过同类校正），知识库状态只能从 `/api/analyze` 后的预览段（`overview/characters/world` 字数）间接读出。
6. 深度提取要求 effective API key 非空——这是**刻意的护栏**（避免无配置时静默跑空），本轮用字面占位串满足它；真实密钥始终未出现在命令、配置或证据里。

## 7. 复现

```bash
# 0) 构建本轮专用二进制（不覆盖别人在用的产物）
go build -o output/novelagent-e2estub.exe .
# 1) 起桩（8123/8124 在 Windows 保留段，用 8097/8098）
STUB_PORT=8098 STUB_LOG=output/stub-requests.jsonl node output/stub_provider.mjs &
# 2) 起服务，空数据目录起步
rm -rf output/e2e-stub-data && mkdir -p output/e2e-stub-data
./output/novelagent-e2estub.exe -addr 127.0.0.1:8097 -data output/e2e-stub-data &
# 3) 跑链路（写 docs/acceptance-evidence/20261024/e2e-stub-chain/）
node output/e2e_stub_chain.mjs
# 4) 只读复核细节（角色 ID 从上一步的预览 seeds 里取）
node output/e2e_stub_probe.mjs c_45301ad360063196d5
```

三份脚本随证据留档（`stub_provider.mjs` / `e2e_stub_chain.mjs` / `e2e_stub_probe.mjs`，均在本目录），`output/` 下为同一份可执行副本。本轮无 Go/前端代码改动；仓库 `data/` 未触碰（`git status --porcelain -- data/` 为 0 行）；服务与桩均已停止。语料为自撰合成文本，可公开；`apiKey` 字段是字面占位串 `stub-key-not-a-secret`，不含任何真实凭据。
