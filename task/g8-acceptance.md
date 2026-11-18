# G8：长篇质量回归集与成本/预算度量（基础设施与人评工具）

状态：**基础设施、人评工具与 R4 长篇召回已交付；G8 未完成**（2026-10-24）。2026-11-02 在当前工作区复测：预算/成本/护栏三闸成立，同时把 dry-run 的两个判读盲区入库为 issues #68（§4.9）。

- 已跑通：合成 40 章、合成 500 万字（500 章）、真实 511KB 语料（79 章 / 163,423 非空白字）的 dry-run；496.6 万字合成语料走**真实进程 + 真实 HTTP**（导入分章、上限拒绝、覆盖导入无残留、召回预览、四档预算计量，§4.6）；同一 5M 数据目录配上 confirmed 记忆夹具后的**记忆+召回联合注入**实测（§4.7）；live 通道用 mock provider 验证输出、截断、清单提示与费用口径；40 章 UI 全流程走查（章节列表、记忆预览、续写、草稿提取、联合提交、提交后召回）；记忆开/关 live 报告 → X/Y 盲评包 → 评分聚合的人评工具链（工具自测，非真实人评）；R4 历史召回在合成 40 章上把第 3 章埋设的「青玉符」召回并注入 prompt（离线断言，非真实模型质量结论）；同一 5M 目录上「改中段一章 / 删章 → 记忆标记失效 → 续写注入随之收缩」的自愈实测（§4.8）；本地 OpenAI 兼容桩模型下的 8 章真实链路 E2E（分析 → 深度提取 → 采纳 → 记忆注入 → 计划生成/激活 → 续写 SSE → 改稿自愈，含「模型输出被采纳后出现在下一次请求 prompt 里」的闭环留档，见 `task/e2e-stub-chain-acceptance.md`）——只证链路，不证质量。
- 未完成：真实模型下的 30–50 章人评、真实模型的质量对照、R4 开启/关闭的**文学质量** A/B、发布报告。真实模型仍受上游网关阻断（F-UPSTREAM-01）。
- 因此本文件只能证明「预算与成本可度量、可重放，召回链路可观测」，不能证明「长篇质量提升」。

授权：用户在 G7 决策中明确「暂不批准，先做 G8 基础设施」。本任务不实现 G7，不修改冻结条款（spec R13），不触碰用户 `data/`，不自动提交。

## 1. 交付物

1. **度量包 `internal/eval`**
   - [token.go](../internal/eval/token.go)：CJK/ASCII 启发式 token 估算与按百万 token 单价估算费用。口径是保守上界，不是账单真值。
   - [case.go](../internal/eval/case.go)：回归用例 schema（数据源、停点、mustLand/mustNot、方向与预算），严格 JSON 解码与校验。
   - [corpus.go](../internal/eval/corpus.go)：`dataDir` 复制隔离、`corpus` 导入分章、确定性合成长篇、knowledge/memory 夹具注入。
   - [runner.go](../internal/eval/runner.go)：停点切片 → `agent.ContinuationWindow` → `store.RetrieveContinuationMemory` → `agent.Build` → 分段预算/成本报告；可选 live 调用（与 `/api/continue` 同走流式路径）。
2. **CLI `cmd/longeval`**
   - dry-run 不调用模型，可在无模型通道时复现「注入多少记忆、prompt 多长、多少 token/费用」。
   - `-tiers` 覆盖 `ContextChars` 分档；`-live` 调用真实模型并记录输出、`finish_reason`、截断、时延与覆盖清单提示。
3. **可重放资产 `eval/cases`**
   - `longform-synthetic.json`：40 章合成长篇 + 知识/记忆夹具；`early`/`late` 两个常规停点，加 `recall-on`/`recall-off` 一对 R4 A/B 停点（同方向、同预算，仅差 `useRecall`）。
   - `longform-5m.json`：500 章 × 10000 字，验证 500 万字级导入与预算计量。
   - `corpus-wubian.json`：本地真实语料导入（511,497 字节）。
   - `pricing.example.json`：示例价，必须替换为实际单价。
4. **人评工具 `internal/eval/review.go` + `cmd/evalreview`**
   - 将同一用例两版 live 报告配对，按种子随机分配 X/Y 并分离 key；生成 JSON 盲评包与人读 Markdown。
   - 对照轴 `-axis memory|recall`：`memory` 比较记忆注入开关（R3），`recall` 比较 R4「前文相关片段」开关（此时校验两侧 `useMemory` 必须一致、开侧确实注入了召回、闭侧没有）。缺省仍是 memory，旧包/旧 key 无 `axis` 字段时按 memory 处理。
   - 评审填写 `mustLand` 三态、`mustNot` 二态、人设崩坏/吞伏笔/失忆次数、未授权事实与偏好；聚合输出覆盖、胜负、平局与 R3 判定（≥2/3 停点胜或平、未授权事实不增加），不通过时 CLI 退出码 4；判定文案按轴标注 `memory-on`/`recall-on`。
5. **自动化用例**
   - `internal/eval/token_test.go`：token 明细、定价/未定价成本。
   - `internal/eval/runner_test.go`：dry-run 预算与记忆注入/开关、ctx=1600 零记忆、报告确定性、真实语料/语料前缀停点、mock live（截断/清单/实际输出 token）、`dataDir` 源目录零修改（SHA-256 全树比对）、用例校验。
   - `internal/eval/review_test.go`：盲评包确定性、X/Y 不泄漏开关、key 映射、聚合胜负/覆盖/未授权事实、缺评分与输入不合法拒绝；记忆轴与召回轴各自的配对校验（含两侧记忆开关不一致、双侧都开/都关、未知轴拒绝）与聚合标注。
6. **R4 长篇历史召回（2026-10-24）**
   - [recall.go](../internal/recall/recall.go)：零依赖 BM25（k1=1.5、b=0.75，**真实词频饱和**，2026-10-24 修）；中文按 2-gram + 3-gram、ASCII 按整词切分；章节正文按 3200 rune 分块、400 rune 重叠；`Search` 返回最多 3 个不同章节的命中，每块只取贡献最高的 `ScoreTermLimit=3` 个词参与求和，片段按「词权重（IDF × 意图权重）」选行并锚定，长度硬上限 = 传入预算。注入前置闸：块内必须命中一条「方向里的罕见线索」（章节频 `df ≤ ceil(5%×章数)`，且线索 ≥3 个连续汉字或全书唯一），否则整段不注入、预算归还正文尾窗。同一输入结果确定，排序同分时按章节新旧（越近越前）与章节 ID 字典序。
   - 意图加权：`Options.Intent`（续写方向）分出的词按 `defaultIntentWeight=3` 放大。查询主体是最近正文尾窗，词量大而信号弱，不加权时随机性强的旧章节会因为表层 n-gram 重叠盖掉远处埋设的关键名词。
   - [prompt.go](../internal/agent/prompt.go)：新增第 8 段「前文相关片段」`SectionRecall`；只在正文尾窗起点之前的旧章节里检索，检索失败/无命中一律降级为不注入，不阻断续写；关闭召回时 golden prompt 逐字不变。
   - 预算策略（决策 F）：`RecallBudgetChars(ctx)=min(800, ctx/4)`，`ctx ≤ 1600` 或剩余正文窗口低于 `ctx/2` 时为 0；召回预算从正文尾窗里扣，冻结收缩顺位新增「召回」一档（知识字段 → 知识条目 → 摘要 → 召回 → 记忆 P1 → 正文尾窗），收缩时先丢排在末尾的命中。
   - 启用条件：默认关闭，`≥20 章`（`novel.RecallMinChapters`）自动启用；`useRecall` 显式 true/false 覆盖自动判定，用于 A/B（显式 `true` 会绕过 20 章门槛，只剩预算不变式与「窗口外有旧章节」两个约束；10 章副本实测仍能注入，见 §4.5）。
   - server 观测：`/api/continue` 接受 `useRecall`，SSE `meta` 增加 `recallEnabled/recallBudgetChars/recallUsedChars/recallChapters`；`POST /api/continue/preview` 同步返回 `recallSection/recallEnabled/recallBudgetChars/recallUsedChars/recallHits/recallChapters`，且预览仍为只读（不推进记忆修订、不改数据目录）。**不存在 `/api/memory/preview` 路由**（本文与 roadmap/issues 旧引用已于 2026-10-24 校正，见 issues #52）。
   - eval 度量：停点级 `recall{requested,enabled,autoEligible,budgetChars,injectedChars,hits,chapters}` 与 `totals.recallInjectedStops`；显式要求召回却没注入时写 warning（`recall requested but not injected`），避免 A/B 里出现「看起来开了其实没生效」的假对照。
   - 前端（issues #46 ②）：续写条新增「前文召回」三档下拉（自动/开/关 → `useRecall` 不下发/true/false）；「预览本次注入」面板把召回段渲染成独立分组，标题带命中章节、正文用强调色实线左边框与虚线的计划段区分、超高内部滚动；页脚计量 `前文召回 N 段 · 已用/预算 字`，关档显示「前文召回已关闭」，显式开却无注入时显示原因；`meta` 到达后在草稿状态行追加 `前文召回 N 段 · X 字`，显式开而未注入会弹错误提示。走查见 §4.5。
   - 自动化：`internal/recall/recall_test.go`（分词、埋点章命中、TopK 去重与确定性、片段长度上限、空输入、方向加权 vs 不加权、200 章/500 万 rune 基准）；`internal/agent/recall_test.go`（预算不变式、无命中回收预算、关闭召回 golden prompt 一致）；`internal/server/recall_test.go`（预览契约 + 只读、SSE meta 透出、mock provider 实际收到「前文相关片段」与规则行、短篇不启用）；`internal/eval/recall_test.go`（auto/on/off 三态、预算与 `sections.recall` 一致、ctx=1600 降级、短篇不启用、显式开而无命中告警）。

## 2. 用法

```bash
# dry-run：预算与 token/费用，不调用模型
go run ./cmd/longeval \
  -case eval/cases/longform-synthetic.json \
  -pricing eval/pricing.example.json \
  -tiers 1600,4000,12000,30000 \
  -out report.json

# 真实模型（密钥只从环境变量读取，不进命令行/报告）
OPENAI_API_KEY=... go run ./cmd/longeval \
  -case eval/cases/longform-synthetic.json -live \
  -base-url https://api.example.com/v1 -model <model> \
  -stops late -tiers 12000 -max-cost 0.50 -out live-report.json
```

人评盲评：

```bash
# R3 记忆轴：同一用例分别跑记忆开/关的 live 报告后，生成 X/Y 盲评包
go run ./cmd/evalreview -axis memory -on on.json -off off.json -seed 20261023 \
  -packet packet.json -key key.json -markdown packet.md

# R4 召回轴：同一用例分别跑 -stops recall-on / -stops recall-off 的 live 报告
go run ./cmd/evalreview -axis recall -on recall-on.json -off recall-off.json -seed 20261024 \
  -packet packet.json -key key.json -markdown packet.md

# 评审只拿 packet.md/packet.json，填完 score 后聚合；key 不交给评审
go build -o evalreview ./cmd/evalreview
./evalreview -score packet.json -key key.json -summary summary.json
```

`packet.json` 不含开关映射，只标明对照轴（`axis`）与该轴的说明文字；`key.json` 记录每个停点的「开启侧」落在 X 还是 Y。聚合要求 ≥3 个停点、每个事实已评、偏好为 x/y/tie；开启侧胜或平 ≥2/3 且未授权事实不增加才 `pass=true`，否则二进制退出码 4。用 `go run` 时 Go 会把非零退出码统一显示为 1，CI 应构建后直接运行。

停点字段：

```json
{
  "name": "late",
  "afterChapter": 40,
  "direction": "让门外的人开口。",
  "targetChars": 800,
  "contextChars": 12000,
  "useMemory": true,
  "mustLand": ["左臂旧伤"],
  "mustNot": ["林砚的左臂已经完全痊愈"]
}
```

`afterChapter<=0` 表示使用全部章节；`-tiers` 非空时覆盖停点/用例/数据目录中的 `ContextChars`。`useMemory=false` 会跳过记忆读取与注入。`-max-cost` 只约束 live 调用：每次调用前用「输入 token + 目标输出 token」预测，超过剩余额度即跳过该停点（`liveSkipped`），后续更便宜的停点仍可运行；只要有跳过或实际花费超限，报告 `costLimitHit=true`，CLI 以退出码 2 结束。dry-run 不花钱，只在总预计费用超限时给 warning。`-strict-budget` 用于 CI：任一停点 `overBudget=true`（收缩后仍超 `ContextChars`）时退出码 3。

## 3. 度量口径

1. **token**：汉字 1 token/字；ASCII 字母数字每 4 字 1 token；全角标点与符号 1 token；连续空白 1 token。保守上界，供横向比较与预算预警，不用于对账。
2. **费用**：`inputTokens × 输入单价 + outputTokens × 输出单价`。dry-run 的输出 token 以 `targetChars` 估算；live 使用实际输出。仅估算一次成功调用，**不包含重试、截断后重生成、评审模型等额外调用**。
3. **预算**：`limitChars=ContextChars` 只约束 user prompt；`totalChars` 含 system，可大于 `limitChars`。`overBudget` 只在按冻结顺位收缩到底仍超限时置真；`manuscriptChars` 是正文尾窗。
4. **记忆**：`injectedChars` 是实际进入 prompt 的记忆段 rune 数（与 `sections.memories` 一致）；`retrievalUsedChars` 是检索层裁剪后的 DTO 计量（全量收缩前）。`disabledReason` 记录 `safe_state/read_error/absent`。
5. **清单**：`mustLand/mustNot` 在 live 模式只给机械提示（精确子串「observed/not_observed/flagged/clear」）；三态结论（`landed/summarized/missing`）与「未授权事实」必须人工填写 `judgment`，dry-run 全部为 `unjudged`。
6. **成本上限**：`-max-cost` 是调用前预测闸，不依赖供应商侧限额；预测按目标输出长度计算，实际输出更长时仍可能单次超限，runner 会在调用后复核并把 `costLimitHit` 置真、写 warning。失败/超时调用不计入已花费（供应商可能仍计费）。

## 4. 本次证据（2026-10-23）

| 用例 | 规模 | 结果 | 证据 |
|---|---|---|---|
| 合成 40 章 | 40 章 / 41,520 非空白字 | 8 个「停点×档位」；ctx≥4000 注入 7 条 / 242 字记忆，ctx=1600 零记忆；总输入 71,767 token，示例价合计 0.084567 USD | [g8-dryrun-40ch.json](../docs/acceptance-evidence/20261023/g8-dryrun-40ch.json) |
| 合成 500 万字 | 500 章 / 5,020,294 非空白字 | 3 档全部完成；ctx=12000 时 user=9,613/12,000；总输入 41,875 token | [g8-scale-5m-dryrun.json](../docs/acceptance-evidence/20261023/g8-scale-5m-dryrun.json) |
| 真实语料《无边悔恨》 | 79 章 / 163,423 非空白字 | 导入分章成功；ctx=4000/12000/30000 均未超预算；无分析与记忆，故记忆注入为 0 | [g8-realcorpus-wubian-dryrun.json](../docs/acceptance-evidence/20261023/g8-realcorpus-wubian-dryrun.json) |
| 合成 500 万字走**真实 HTTP 应用** | 500 章 / 4,966,490 rune | 导入 20.3 s、超上限 400、覆盖导入无残留；预览开召回命中埋点第 7 章（103/800 字）耗时 1.15–1.53 s vs 关召回 0.38–0.44 s | [g1-5m-import-run.md](../docs/acceptance-evidence/20261024/g1-5m-import/g1-5m-import-run.md)｜[g1-5m-summary.json](../docs/acceptance-evidence/20261024/g1-5m-import/g1-5m-summary.json) |
| mock live | 3 章合成 | 输出正确落报告；`finish_reason=length` → `truncated=true`；mustLand=observed、mustNot=clear；费用按实际输出 token 重算 | 自动化 `TestRunLiveWithMockProvider` |
| 合成 500 万字 + **confirmed 记忆夹具**走真实 HTTP | 500 章 / 5,020,294 rune / `memoryRevision=12` | 同一次预览里记忆 242/3000 与召回 800/800 **同时非零**（命中第 3、12 章）；ctx=4000/12000/30000 联合非零、1600 两档归零；同输入逐字可重放；关记忆不还正文窗口（两臂 `manuscriptChars` 均 8179，issues #54） | [g3-5m-joint-run.md](../docs/acceptance-evidence/20261024/g3-5m-joint/g3-5m-joint-run.md)｜[g3-5m-joint-summary.json](../docs/acceptance-evidence/20261024/g3-5m-joint/g3-5m-joint-summary.json) |
| 5M 字上「改稿/删章 → 记忆自愈 → 注入收缩」真实 HTTP 走查 | 500 章 / 5,020,294 rune / `memoryRevision` 12→15 | 三种失效原因各真实触发一次并沿依赖链传播（事件→伏笔→计划）；`memorySection` 242→212→195→102 字，每次收缩恰好对应刚被标记的那条记录；计划被停投但 `status/title/objective` 逐字未变；最小写集 + 每次 +1 `reconcile` 提交；成本口径：全量复核 352 ms / 单章 4 ms / 删章 307 ms | [g4-5m-heal-run.md](../docs/acceptance-evidence/20261024/g4-5m-heal/g4-5m-heal-run.md)｜[g4-5m-heal-summary.json](../docs/acceptance-evidence/20261024/g4-5m-heal/g4-5m-heal-summary.json) |
| race 全量 | 8 个包 | `CGO_ENABLED=1 go test -race ./... -count=1` 全绿（gcc 15.2.0），无数据竞争报告 | [g8-race.txt](../docs/acceptance-evidence/20261023/g8-race.txt) |
| UI 全流程（40 章 + mock） | 40 章 / 7→8 条记忆 / revision 12→13 | 40 行章节；预览 7 条 / 242 字；生成 32 字；提取基线 revision 12；联合提交后 revision 13、待确认草稿 0；提交后预览召回新事件 8 条；0 控制台错误 | [结果 JSON](../docs/acceptance-evidence/20261023/g8-ui/g8-ui-40ch-result.json)｜截图 [预览](../docs/acceptance-evidence/20261023/g8-ui/g8-ui-2-memory-preview.png)｜[提交](../docs/acceptance-evidence/20261023/g8-ui/g8-ui-6-committed.png)｜[提交后召回](../docs/acceptance-evidence/20261023/g8-ui/g8-ui-7-preview-after-commit.png) |
| 人评工具自测（mock） | 40 章 / 3 停点 / 开-关各 3 次 | 生成 X/Y 盲评包且不泄漏开关；合成评分聚合 `pass=true`（on 2 胜 1 平、覆盖 1.00 vs 0.00、未授权 0 vs 1）；反向评分 `pass=false`、二进制退出码 4。**评分为合成，不是人评结论** | [盲评包](../docs/acceptance-evidence/20261023/g8-review/g8-review-selftest-packet.md)｜[开报告](../docs/acceptance-evidence/20261023/g8-review/g8-review-selftest-on-report.json)｜[关报告](../docs/acceptance-evidence/20261023/g8-review/g8-review-selftest-off-report.json)｜[汇总](../docs/acceptance-evidence/20261023/g8-review/g8-review-selftest-summary.json) |
| 成本闸（mock） | 2 停点 | 第一个停点 live 成功后，第二个停点因预计费用超剩余额度被跳过；provider 只收到 1 次请求，`costLimitHit=true`；实际输出超目标长度时也会在调用后置真 | 自动化 `TestRunLiveCostLimitSkipsRemainingCalls` / `TestRunLiveCostLimitFlagsActualOverrun` |
| 预算闸（CLI） | ctx=50 | 2 个停点 `overBudget=true`，加 `-strict-budget` 后二进制退出码 3；正常 ctx=12000 退出码 0 | 手工复跑（2026-10-23） |
| 分档矩阵 | 40ch / 5M / 真实语料 | 每个用例覆盖 1600/4000/12000/30000 四档；记忆在 4000 已饱和（7 条/242 字），1600 为 0；12000 成本约为 4000 的 2 倍、30000 的 1/2.5 | [40ch](../docs/acceptance-evidence/20261023/g8-tier-matrix-40ch.json)｜[5M](../docs/acceptance-evidence/20261023/g8-tier-matrix-5m.json)｜[真实语料](../docs/acceptance-evidence/20261023/g8-tier-matrix-wubian.json) |
| **三闸在当前工作区复测**（2026-11-02，无 Key） | 合成 40 章 + 夹具记忆，12 停点 | 预算硬闸（1200/1600 贴顶不越界、400 必退 3）、成本计量（`priced=true`、0.066183 USD）、护栏拒绝未定价**三项成立**；同时量到 dry-run 的两个判读盲区（清单恒 `unjudged` 且报告无文本可判、成本超限只 warning 退 0），入库 issues #68 | [g8-gate-recheck-run.md](../docs/acceptance-evidence/20261024/g8-gate-recheck/g8-gate-recheck-run.md)｜§4.9 全部 JSON |

真实语料导入只证明「分章 + 预算」链路，不代表 G1 的真实模型分析验收完成；真实模型下的质量与成本仍需通道恢复后按 R3 协议执行。

### 4.1 ContextChars 分档建议（dry-run，示例价）

| 档位 | 记忆预算 | 实际注入（40ch） | user 占比（40ch 后期 / 5M） | 输入 token（40ch / 5M） | 相对成本 | 结论 |
|---|---|---|---|---|---|---|
| 1600 | 0 | 0 条 | 98.6% / 95.5% | 1,975 / 1,924 | ~0.30x | 仅纯续写；记忆功能实际关闭 |
| 4000 | 1000 | 7 条 / 242 字 | 92.3% / 91.1% | 4,162 / 4,112 | ~0.49x | 成本敏感档，仍保留记忆 |
| 12000 | 3000 | 7 条 / 242 字 | 80.5% / 80.1% | 10,050 / 10,004 | 1.00x | **默认推荐**，记忆与正文平衡 |
| 30000 | 3000 | 7 条 / 242 字 | 92.2% / 92.0% | 27,821 / 27,759 | ~2.5x | 只在需要长正文窗口时使用；记忆不再增加 |

- 记忆注入量在 4000 已饱和（7 条 / 242 字），继续加大档位只增加正文窗口和费用；要提升「远期记忆」应做 R4，而不是继续加 `ContextChars`。
- 1600 时 `MemoryBudgetChars=0`，记忆段完全不注入；若产品要求「记忆可用」，不应作为默认档。
- 以上为合成语料与示例单价，真实分词器/单价不同；上线默认值仍需真实模型 A/B 与成本账单校准。

**分档补充（2026-10-24：真实 HTTP + 5M 字 + confirmed 记忆夹具，见 §4.7）**——上一表来自离线 dry-run，只覆盖记忆一档；下面是「记忆 + 前文召回」两档同时开启时的实测形态（方向句带埋点线索）：

| ctx | 记忆 预留/实注 | 召回 预算/实注 | 正文窗口（实测） | 预留空置 | 两条链路同时可用 |
|---|---|---|---|---|---|
| 1600 | 0 / 0 | 0 / 0 | 1258 | 0 | **否**（同时归零，等价纯续写） |
| 4000 | 1000 / 242 | 800 / 800 | 2153 | 758 | 是 |
| 12000 | 3000 / 242 | 800 / 800 | 8179 | 2758 | 是 |
| 30000 | 3000 / 242 | 800 / 800 | 26166 | 2758 | 是 |

- **分档结论因此收紧**：只要产品要求「记忆可用 + 长篇召回可用」，`ctx` 下限就是 **4000**（1600 档两档同时归零，且接口不回传原因，issues #55）；`12000` 仍为默认推荐（`user` 实测 10420/12000，`overBudget=false`）。
- **预留空置是真实成本**：记忆库小（实注 242 字）时，`ctx=12000` 有 2758 字（23% 总预算）正文窗口被预留却没被任何内容使用，`ctx=30000` 同样空置 2758 字但相对占比降到 10.5%。在 issues #54 的回补口径改掉之前，「加大 `ContextChars`」能把空置稀释，而「保持 12000 且记忆条目少」是纯浪费；反之记忆条目多到顶满 3000 时该空置消失。
- 表中「实注」与命中（第 3、12 章）来自**夹具记忆 + 合成语料**，只用于预算与窗口的算术，不作为文学质量或提取质量的结论。

### 4.2 UI 全流程走查（40 章，mock 模型）

- 环境：服务指向 `-keep` 保留的 40 章合成数据副本，模型配置指向本地 mock provider；Playwright Chromium headless。未调用真实模型，未修改 `data/`。
- 断言：40 行章节、默认打开第 40 章；记忆预览 7 条 / 242 字 / revision 12、待复核跳过 1 条；续写输出 32 字；存入草稿台后提取 1 个 `add_event`、0 冲突，基线 revision 12；联合确认后第 40 章 1,090→1,122 字、记忆 revision 13、待确认草稿归零；再次预览召回 8 条，包含引用 `林砚按住左臂旧伤` 的新事件；控制台 0 错误。
- 走查发现并修复：草稿 ChangeSet 前端读取不存在的 `basedOnRevision`，基线被显示为 0；已改为 `basedOnMemoryRevision`（[web/app.js:1785](../web/app.js)）。重跑证据显示 revision 12。
- 边界：mock 输出只验证链路与契约，不代表真实模型质量；Playwright 脚本与 mock provider 是临时资产，未纳入仓库测试。

### 4.3 人评工具链自测（mock，非真实人评）

- 用 40 章合成数据、3 个停点（第 10/25/40 章）分别跑记忆开/关的 mock live 报告；每侧各 3 次调用，输出 32 字。
- `evalreview` 生成 3 停点盲评包；X/Y 映射记在 key，包内不含 `memoryOn`/`useMemory`；本包实际映射 early=y、middle=y、late=x。
- 为验证聚合器，脚本填入了**合成评分**（memory-on 侧 landed/clear、memory-off 侧 missing/flagged，early/middle 偏好 on、late 平局，off 侧记 1 条未授权事实）。聚合结果 `pass=true`、onWins=2/ties=1、覆盖率 1.00 vs 0.00、未授权 0 vs 1；将偏好全部反转后 `pass=false`，构建后的二进制退出码 4。
- **这些评分不是人评结论**，只证明盲评包、映射分离、聚合与硬闸可执行；真实人评仍待模型通道恢复后由两位评审按 R3 交换顺序完成。

### 4.4 R4 召回证据（2026-10-24，dry-run 与基准，非真实模型质量）

| 项 | 结果 | 证据 |
|---|---|---|
| 合成 40 章 × 3 档（含 R4 A/B 停点） | 12 个「停点×档位」；`recall-on` 三档均召回埋点第 3 章，注入 763–800/800 字，`sections.recall == injectedChars`，`Σsections ≤ user`；`recall-off` 同方向同预算下正文尾窗 +764～822 字、示例价成本增量 +0.00005～0.00009 USD/停点；`totals.recallInjectedStops=6/12`；`early`（第 10 章）不启用，注入 0 | [r4-synthetic-40ch-dryrun.json](../docs/acceptance-evidence/20261024/r4-synthetic-40ch-dryrun.json) |
| 检索规模基准 | 200 章（约 68 万 rune）：121 ms、32.1 MB、965k allocs／次；500 章（约 530 万 rune）：781 ms、257 MB、7.52M allocs／次（i7-7700，`-benchtime=1x` 单样本，复跑同机测得 98/690 ms，抖动约 ±20%）。每次续写重建索引，未做跨调用缓存 | [r4-recall-benchmark.txt](../docs/acceptance-evidence/20261024/r4-recall-benchmark.txt) |
| 召回正确性 | 埋点章命中、TopK 去重与确定性、片段长度硬上限、方向加权 vs 不加权、预算不变式、无命中回收预算、关闭召回 golden prompt 逐字不变、SSE meta 与预览契约、mock provider 实际收到「前文相关片段」 | `internal/recall`、`internal/agent`、`internal/server/recall_test.go`、`internal/eval/recall_test.go` |
| 召回轴盲评工具自测（合成夹具，非人评） | `evalreview -axis recall` 用 3 停点合成 on/off 报告生成盲评包：包内不含开关映射与两侧设置差异，key 记录开启侧（early=x/middle=x/late=y）；合成评分聚合 `pass=true`（on 2 胜 1 平、覆盖 1.00 vs 0.00、未授权 0 vs 1），反转评分 `pass=false` 且退出码 4，判定文案为 `recall-on …`；`-axis memory` 拒绝该配对、未知轴报错退出码 1 | [运行记录](../docs/acceptance-evidence/20261024/r4-review/r4-review-run.txt)｜[盲评包](../docs/acceptance-evidence/20261024/r4-review/r4-review-packet.md)｜[开报告](../docs/acceptance-evidence/20261024/r4-review/r4-review-on-report.json)｜[关报告](../docs/acceptance-evidence/20261024/r4-review/r4-review-off-report.json)｜[汇总](../docs/acceptance-evidence/20261024/r4-review/r4-review-summary.json) |

- 合成语料的埋点命中只证明「检索 + 预算 + 观测」链路正确，不证明真实长篇的召回质量；R4 的质量判定要靠 `recall-on`/`recall-off` 两个停点跑 live 后进盲评（§2 用法：`-stops recall-on,recall-off` 各跑一次，再 `evalreview -axis recall`）。
- **真实语料一度证伪了这条链路（2026-10-24 补记）**：独立复审 [r4-realcorpus-review.md](r4-realcorpus-review.md) 在真实长篇上量到埋点命中率 5%–13%，本轮自建探针复现 5/26（19%），且 79 章每一档都无条件填满 800 字正文窗口（`ctx=4000` 时正文 −26.4%）。根因是内核并非标准 BM25：分词返回集合使 tf≡1、查询几百个 n-gram 全部相加、且没有注入闸；「按 df 罕见」也不等于「是线索」——空洞方向的 33 个 n-gram 里 27 个全书不出现，独自决定 800 字注入的只是一个 2 字虚词残片。修复（真实 tf + `ScoreTermLimit=3` + 章节频罕见闸 + `MinClueRunes=3` 长度—唯一性阶梯）后复测：**3 字埋点 19%→96%（`ctx=4000` 档 100%）、2 字埋点 4%→42%、无线索方向注入 0 字并归还预算、中性方向 5 次排序一致**。详见 [r4-recall-quality-run.md](../docs/acceptance-evidence/20261024/r4-recall-quality/r4-recall-quality-run.md) 与 issues #50。
- 上述命中率是「埋点—召回」的检索度量，仍不等于文学收益；两字线索 38%–42% 是无分词 n-gram 的已知上限，这块收益应由 G2/G3 的结构化实体记忆承担。
- 5M rune 级每次续写约 0.7–0.8 s 的检索开销发生在模型调用之前，用户体感是「开始生成」变慢；若通道恢复后判定不可接受，方案是加章节级倒排缓存或限制扫描范围（需产品确认，见 issues #46）。

### 4.5 R4 前端走查（2026-10-24，浏览器实测；只到 SSE `meta`，非真实模型续写）

数据仍是隔离副本（合成 40 章 / 删到 10 章 / 删到 2 章），`baseUrl` 指向本机桩端点（延迟 6 s 返回 502），目的是让 `meta` 之后的模型调用失败也不影响对注入观测的验证。三次 Playwright 走查退出码均为 0、`console_errors` 为空。

| 判据 | 结果 | 证据 |
|---|---|---|
| 预览面板出现召回段并标命中章节 | 通过：`前文相关片段（来自第 25、3、31 章）`，段内 763 字，页脚 `前文召回 3 段 · 763/800 字` | [走查记录](../docs/acceptance-evidence/20261024/r4-ui/r4-ui-run.md)｜[自动档截图](../docs/acceptance-evidence/20261024/r4-ui/r4-ui-1-preview-auto.png)｜[r4-ui-walk-result.json](../docs/acceptance-evidence/20261024/r4-ui/r4-ui-walk-result.json) |
| 与计划段视觉可区分 | 通过：召回段为强调色 `2px solid` 左边框 + `max-height:220px` 内部滚动，计划段仍虚线 | [只开召回截图](../docs/acceptance-evidence/20261024/r4-ui/r4-ui-4-recall-only.png) |
| 三档开关改变请求体与面板 | 通过：`useRecall` 依次 `null/true/false`；关档无召回段且页脚「前文召回已关闭」+ toast | [关档截图](../docs/acceptance-evidence/20261024/r4-ui/r4-ui-3-preview-recall-off.png) |
| 续写 `meta` 注入观测 | 通过：开 → `… · 计划 1 条 · 前文召回 3 段 · 763 字`；关 → 无召回字样 | [开](../docs/acceptance-evidence/20261024/r4-ui/r4-ui-6-continue-recall-on.png)｜[关](../docs/acceptance-evidence/20261024/r4-ui/r4-ui-5-continue-recall-off.png) |
| 自动档 20 章门槛 / 强制开绕过门槛 | 通过：10 章副本「自动」不注入，「开」注入 2 段 582 字（来自第 2、1 章） | [r4-ui-walk-short.txt](../docs/acceptance-evidence/20261024/r4-ui/r4-ui-walk-short.txt)｜[截图](../docs/acceptance-evidence/20261024/r4-ui/r4-ui-7-short-forced-on.png) |
| 强制开却无命中的告警 | 通过：2 章副本页脚/空态给出「已要求前文召回但无注入：旧章节没有命中片段，或正文窗口不让位」，续写时另发 error toast | [页脚](../docs/acceptance-evidence/20261024/r4-ui/r4-ui-10-tiny-foot-warning.png)｜[空态](../docs/acceptance-evidence/20261024/r4-ui/r4-ui-11-tiny-empty-warning.png)｜[toast](../docs/acceptance-evidence/20261024/r4-ui/r4-ui-12-tiny-continue-warning.png) |

- 走查当场修掉两处：①显式开却无命中时界面无反馈（补页脚/空态说明，且只在显式「开」时出现，避免把「自动」档的策略性关闭误报成异常）；②原文案「正文满 20 章才会生效」与实现不符（显式 `true` 绕过门槛，10 章实测注入 582 字），改为「不满 20 章也会检索」。
- 前端没有自动化测试框架，以上为一次性脚本走查（脚本随证据存档于 `docs/acceptance-evidence/20261024/r4-ui/`），复跑命令见 [r4-ui-run.md](../docs/acceptance-evidence/20261024/r4-ui/r4-ui-run.md) §2。
- 修改后回归：`go build ./...`、`go vet ./...`、`go test ./... -count=1` 全绿（本轮只动 `web/` 静态资源与台账，无 Go 侧改动），走查用的三个服务与桩端点已停止，见 [r4-ui-run.md](../docs/acceptance-evidence/20261024/r4-ui/r4-ui-run.md) §6。

### 4.6 500 万字级真实 HTTP 链路度量（2026-10-24，合成语料、无模型 Key）

此前 5M 规模只有两条口径：`eval/cases/longform-5m.json` 的离线 dry-run（不进应用）和 `internal/recall` 里的 200 章/500 万 rune 检索基准。本轮补上**真实进程 + 真实 HTTP**的口径：合成 4,966,490 rune / 500 章（第 7 章埋全书唯一三字线索）经 `POST /api/import` 落进一个全新数据目录，再用 `POST /api/continue/preview` 走读。完整口径与复现见 [g1-5m-import-run.md](../docs/acceptance-evidence/20261024/g1-5m-import/g1-5m-import-run.md)。

| 判据 | 结果 |
|---|---|
| 导入与自动分章 | 200，20.3 s（≈24.5 万 rune/s），500 章 / `charCount` 合计 4,959,708，目录 14,975,582 B / 514 文件 |
| 上限拒绝（5,000,001 字） | `400 文稿太大，请控制在 500 万字以内`，223 ms；读回仍是原 500 章、标题未变 |
| 覆盖导入不留残留 | 再导 3 章小稿后章节文件 500→3、目录 14.98 MB→2,868 B，`analysis` 重置 pending |
| 召回在 5M 规模仍命中埋点章 | `recallHits=1`、`recallChapters=[7]`、实注入 103/800 字、`recallSection` 含原句；正文窗口 9,000→8,200，`promptTotalChars` 9,574→8,973，四档均 `promptOverBudget=false` |
| 分档（1600/4000/12000/30000） | 召回预算 0/800/800/800，正文窗口 1,453/2,200/8,200/26,200 —— 与 §4.1 离线档一致 |
| 时延 | 开召回 1.15–1.53 s、关召回 0.38–0.44 s；**强制开却零注入的模糊方向仍 1.28–1.34 s** → 时间成本在建索引，闸门只省窗口（#46②/#53） |
| 无模型时的分析失败路径 | 导入即 `analysis=error`「请先完成模型设置」；`PUT /api/config` 触发一次自动重试 → 拨号被拒 → `error`「分析中断，请检查模型设置后重试」，`analyzedCount` 恒 0、无在途残留 |

边界：本轮**不**构成 G1 结案（分析与深度提取仍需真实模型），也**不**构成 G3 证据（`memory_absent`，记忆 3000 + 召回 800 的联合预算未同时非零地跑过）；内存读数因采样污染不可用，勿与早前 257 MB 离线估算对比。

### 4.7 记忆注入在 5M 字上的联合预算观测（2026-10-24，夹具记忆、无模型 Key）

§4.6 的缺口是「无分析即无记忆」（`memory_absent`），只能给召回口径。本轮用 `cmd/longeval` 的隔离目录把 **confirmed 记忆夹具**（`eval/cases/memory/` + `eval/cases/knowledge.json`）与 500 章合成语料放在同一个数据目录里，再让真实 HTTP 进程读它，从而第一次量到「记忆段与召回段同时非零」：

| 判据 | 结果 | 证据 |
|---|---|---|
| 5M 字上记忆段真进 prompt | 通过：ctx=12000 注入 242 字（预留 3000），记录含第 3/12 章溯源事件、`左臂旧伤未愈` 状态、`青玉符的来历` 伏笔、`追查青玉符` 计划；种子 `林砚 7.45 / 沈青禾 6.90 / 陆行舟 6.80`（`reason=recent_mention`） | [run doc](../docs/acceptance-evidence/20261024/g3-5m-joint/g3-5m-joint-run.md) §2.1｜[summary.json](../docs/acceptance-evidence/20261024/g3-5m-joint/g3-5m-joint-summary.json) `cases.cluedAuto/steps.sectionText` |
| 记忆段与召回段共存（联合预算） | 通过：同一次请求 `memories 242 + recall 800 + manuscript 8197`，`recallChapters=[3,12]`，`promptOverBudget=false` | 同上 `sections` |
| 分档联合形态 | ctx=4000/12000/30000 均联合非零（`242/1000`、`242/3000`、`242/3000` + 召回 800）；**ctx=1600 两档同时归零**（记忆退化 + 召回与正文硬下限冲突），只剩 `overview+plans+manuscript 1276` | run doc §2.2 |
| 同输入可重放 | 通过：`memorySection`/`recallSection`/`sections` 两次逐字一致 | `steps.determinism` |
| 记忆/召回互不依赖 | 通过：关召回→记忆仍 242 字且窗口 +752；无线索方向→记忆仍注入、召回零注入并整额还窗口；关记忆→召回仍 800 | run doc §2.1 |
| 记忆检索的时延成本 | 可忽略：只开记忆 429 ms vs 记忆+召回 1325 ms vs 只开召回 1260 ms——5M 上的 0.9 s 级开销主体仍是召回建索引（与 §4.6 无记忆口径同量级） | run doc §3 |
| 预览读数可代表真实续写侧 | 通过：用不可达桩端点 `http://127.0.0.1:9/v1`（SSE `meta` 在建 prompt 后、模型调用前发出）对同一副本目录跑 `/api/continue` 与 `/api/continue/preview`，三个变体（都开/关召回/关记忆）× 15 字段共 45 项对比 **0 处不一致**（两侧同为 `memories 242 + recall 800 + manuscript 8197`、`recallChapters=[3,12]`）→ 无密钥时预览是合法观测点 | run doc §2.3｜[preview-vs-sse.json](../docs/acceptance-evidence/20261024/g3-5m-joint/preview-vs-sse.json) |

- **新记录的口径问题（issues #54/#55/#56，本轮只度量不改代码）**：①记忆预算是「预留即扣」——正文窗口恒为 `ctx − MemoryBudgetChars(ctx) − 召回预算`（[internal/agent/prompt.go:152-154](file:///D:/go/internal/agent/prompt.go#L152-L154)），既不随实际注入 242 字回补（5M 实测空置 2758 字），也不随 `useMemory=false` 释放（两臂 `manuscriptChars` 都是 8179）；召回却有「零命中整额归还」的对称规则。这直接影响 G3 的 A/B 解释：**memory-off 臂不是「把 3000 字还给正文」，而是「少 341 字 prompt、正文一样长」**。②`ctx ≤ 1600` 时记忆静默归零且 `memoryDisabledReason` 是空串，界面无法区分「配置太小」与「库里没记忆」。③夹具记忆可绕过 `initialize`/extract/审阅直接落盘（`initStatus=not_started` 却已能注入），因此任何「记忆已进 prompt」的证据都必须声明来源，夹具只证管线不证提取。
- **不得据此判定 G3 达成**：本轮无模型调用（`analysis.status=error`、无密钥），记忆为人工夹具，语料为合成。它把 G3 的「链路在长篇规模上可用且可观测」从推断变成实测，但「长程一致性可观测提升」一侧仍需真实模型 memory-on/off 盲评（issues #43）。
- 复现：`go run ./cmd/longeval -case eval/cases/longform-5m.json -temp output -keep` → 复制该目录 → `./output/novelagent-ui.exe -addr 127.0.0.1:8097 -data <副本>` → `node output/g3_5m_joint.mjs`（联合预算）→ `node output/g3_5m_preview_vs_sse.mjs`（预览 vs SSE 等价性；该脚本会把**副本**的 `baseUrl/model` 留在桩值，不可指向仓库 `data/`）。二进制与 §4.6 同一个（13:05 构建，HEAD `f36025e` + 82 个未提交文件），本轮无 Go/前端改动，仓库 `data/` 未触碰，探针服务与桩端点已停止。

### 4.8 G4 自愈链在 5M 字上的规模与成本口径（2026-10-24，夹具记忆与人工证据链、无模型 Key）

§4.7 量的是「静态注入」，本轮量的是「正文改动之后注入如何随之变化」——即 G4 的自愈链路是否只在单测里成立。同一份 5M 隔离目录副本（人工补写章节级 `sourceRefs` 与一条 事件→伏笔→计划 依赖链），全程真实 HTTP，`contextChars=12000`：

| 判据 | 结果 | 证据 |
|---|---|---|
| 三种失效原因可分别触发 | 通过：改第 12 章→`evidence_stale`；全量复核抓到故意写坏的引文→`quote_mismatch`（只有全量扫才抓得到）；删第 3 章→`chapter_deleted`，并沿依赖链传播 `thread_jade→plan_near` 为 `dependency_stale` | [run doc](../docs/acceptance-evidence/20261024/g4-5m-heal/g4-5m-heal-run.md) §2.1 |
| 注入侧后果单调可测 | 通过：`memorySection` 242→212→195→102 字，注入记录 7→6→5→2 条，每一步都恰好少「刚被标记待复核」的那条 → 红线「`needsReview` 一律不注入」在 5M、在三种原因下都生效 | run doc §2.1｜[summary.json](../docs/acceptance-evidence/20261024/g4-5m-heal/g4-5m-heal-summary.json) `steps` |
| 计划不被自动覆盖 | 通过：删章后 `GET /api/plans` 里计划仍 `status=active`、`title/objective/dependsOnThreads` 逐字未变，仅 `needsReview=true`、`recordVersion 0→1`；同时续写侧停投（`planCount 1→0`） | run doc §2.1 |
| 审计与最小写集 | 通过：3 条 `type=reconcile` 提交，`basedOn→target` 12→13→14→15 每次恰好 +1；13 只重写 `states.json`、14 只重写 `cognitions.json`、15 重写 `events+plans+threads` | run doc §2.2 |
| 复核成本（5M / 500 章） | 全量 `POST /api/memory/reconcile {}` **352 ms**（`marked=1`）；单章 `{chapterIds:[…]}` **4 ms**（`marked=0`）；`DELETE /api/chapters/{id}` 307 ms。`SaveChapter` 内联的单章复核因此是毫秒级，不构成续写链路瓶颈 | run doc §2.1 |
| 待复核停投的可观测性 | 部分通过：记忆档有 `skippedReview`（健康链下实测 6）；**计划档没有停用原因字段**（issues #58） | [review-visibility.json](../docs/acceptance-evidence/20261024/g4-5m-heal/review-visibility.json) |
| G7 闸门在改动后的目录上仍有效 | 通过：`{"reinitialize":true}` → `400 reinit_not_supported`，`memoryRevision` 保持 15、零落盘 | run doc §2.3 |

- **成本/预算侧的再印证（issues #54）**：删章后记忆只剩 102 字，正文窗口仍是 `ctx − 3000 − 召回`，空置 2898 字；召回无命中时整额归还（8179→8931）。即「记忆越失效、预留越空转」——失效期越长，正文窗口被空置越多，这是 #54 在 G4 场景下的具体代价。
- **发现二（issues #59，属验收基础设施缺陷）**：`cmd/longeval` 的记忆夹具带 `meta.json{memoryRevision:12}` 但没有 `commits.json`，于是「写入一次记忆 + 重启进程」必然触发提交链校验失败并 fail-closed（`safeState=true`、预览 `memoryDisabledReason="safe_state"`、记忆/计划/召回注入同时归零、窗口回补）。本轮用脚本手工重编号提交链后重启即恢复，证明是夹具不自洽而非产品写坏数据。**G8 验收协议红线**：引用任何注入读数前先看 `safeState`；「注入突然全 0」不得读成「记忆失效」。现场见 [review-visibility-safe-state.json](../docs/acceptance-evidence/20261024/g4-5m-heal/review-visibility-safe-state.json)。
- **不得据此判定 G4 达成**：G4 的**收益判定**（真实语料 + 真实模型下「改中段一章 → 续写是否避开失真记忆」）仍需模型通道，`task/g4-acceptance.md` §8 的 1/4 两条不变。本轮把「自愈链路与注入侧后果在长篇规模上成立」从单测结论升级为真实 HTTP 实测结论。
- 复现：见 run doc §5 五步（`cmd/longeval` 生成 → `output/g4_5m_seed_refs.mjs` 补证据 → `output/g4_5m_heal.mjs` 走查 → 重启复现 safe_state → `output/g4_5m_chain_repair.mjs` 修链后恢复）。探针目录 `output/g4-5m-httpdata`、`output/g4-5m-chainfixed` 均在 gitignore 的 `output/` 下；本轮无 Go/前端代码改动，仓库 `data/` 未触碰，服务已停止。

### 4.9 G8 三闸复测与 dry-run 判读盲区（2026-11-02，合成 40 章 + 夹具记忆、无模型 Key）

§4/§4.1 的读数出自 2026-10-23 那批二进制；本轮在**当前工作区**（HEAD `f36025e` + 83 个未提交条目，`internal/eval/`、`cmd/longeval/`、`eval/` 整体仍未跟踪）先把 `go build ./...`、`go vet ./...`、`go test ./... -count=1` 跑绿，再用 `go build -o output/longeval-g8.exe ./cmd/longeval` 重放，确认「成本/预算可度量、可控」这一侧不是过期结论。完整命令与逐停点表见 [g8-gate-recheck-run.md](../docs/acceptance-evidence/20261024/g8-gate-recheck/g8-gate-recheck-run.md)。

| 判据 | 结果 | 证据 |
|---|---|---|
| 12 停点联合复测（`early/late` 记忆 × `recall-on/off` 召回 × ctx 4000/12000/30000） | 通过：`exit 0`；`overBudgetStops=0`、`maxUserBudgetPct=92.325`、`memoryInjectedStops=6`（三档恒 242 字 / 7 条 / `skippedReview=1` / `droppedByBudget=0`）、`recallInjectedStops=3`（三档同 779/800 字、命中第 3 章埋点）；分段不变式在 `late@ctx12000` 实测（`manuscript 8958 + memories 242 + characters 89 + summaries 81 + overview 55 + world 36` ≤ `user 9656`） | [gate-recheck-12stops.json](../docs/acceptance-evidence/20261024/g8-gate-recheck/gate-recheck-12stops.json) |
| 预算硬闸的边界形状 | 通过：ctx=1200/1600 四停点贴顶不越界（95.250%–98.625%）且 `exit 0`；ctx=400 收缩到底仍 `543 > 400 rune`（135.75%）→ `overBudget=true`、`-strict-budget` 下**构建后的二进制退出码 3** | [tiers-1200-1600.json](../docs/acceptance-evidence/20261024/g8-gate-recheck/strict-budget-tiers-1200-1600.json)｜[ctx400.json](../docs/acceptance-evidence/20261024/g8-gate-recheck/strict-budget-ctx400.json)｜[stdout](../docs/acceptance-evidence/20261024/g8-gate-recheck/strict-budget-ctx400-built-binary.txt) |
| 成本可计量 | 通过：合成单价 2.5/10 USD·M⁻¹ 下逐停点 `0.033058 / 0.033125 USD`、`totals.priced=true`、`totalCost=0.066183`、`inputTokens=20073` | [priced-2stops.json](../docs/acceptance-evidence/20261024/g8-gate-recheck/priced-2stops.json)｜[pricing-synthetic.json](../docs/acceptance-evidence/20261024/g8-gate-recheck/pricing-synthetic.json) |
| 护栏不会被「忘了填单价」静默失效 | 通过：未定价 + `-max-cost` → `eval: max cost requires input/output pricing`、退出码 1 | run doc §5 |
| **护栏在 dry-run 侧不硬失败（新，issues #68）** | 实测：`-max-cost 0.04` 而预计 `0.066183 USD` 时只打印 `warning: dry-run projected cost … exceeds max cost …; no calls were made`，`costLimitHit=false`、**退出码 0**（`internal/eval/runner.go:209-212` 只在 `!Live` 分支写 warning，`CostLimitHit` 仅 live 分支置真，`cmd/longeval/main.go:112` 据此才退 2） | [max-cost-warning-dry-run.json](../docs/acceptance-evidence/20261024/g8-gate-recheck/max-cost-warning-dry-run.json) |
| **回归清单在 dry-run 恒不可判（新，issues #68）** | 实测：4 份报告 **52 个清单项全部 `hint:"unjudged"`**；且报告没有任何文本可判——`prompt.sections` 是段名→rune 数的 map、`memory.records` 是条数（`runner.go:302-305`、`:322`），于是「mustLand 事实有没有进 prompt」在 CLI 侧无法机械核对。同一条事实在 HTTP 预览路径可查（`memorySection` 全文，§4.7 已实测 `左臂旧伤未愈`、`青玉符的来历`）→ **盲区属 eval CLI，不属产品观测** | run doc §7｜`internal/eval/runner.go:371,443,445-462` |
| 退出码在 CI 里会不会塌成一个 | 实测陷阱：同一 ctx=400 超预算用例，构建后二进制 `exit 3`，`go run` 只给 `exit 1`（子进程码仅出现在 stderr 文本 `exit status 3`）→ 接 CI 必须先 `go build`，否则 2/3/4 三类失败无法分流 | [go-run stdout](../docs/acceptance-evidence/20261024/g8-gate-recheck/strict-budget-ctx400-go-run.txt)｜[二进制 stdout](../docs/acceptance-evidence/20261024/g8-gate-recheck/strict-budget-ctx400-built-binary.txt) |

- **因此 G8 的准确口径收紧为**：合成回归闸当前 gate 的是「预算不变式 + 注入量与召回命中符合预期」，**不 gate 事实一致性**；`-strict-budget` 承诺的不是「任意小 `ctx` 可用」，而是「收缩到底仍越界时必然以 3 退出」。
- **免 Key 可补的一步（待 yj 拍板，本轮零代码）**：`buildChecklist` 增加对 `prompt.User` 的子串判定（`in_prompt/absent_from_prompt`），并让报告带上记忆/召回段文本；`runStop` 已把 `prompt` 留在作用域（`runner.go:291-300`），改动集中在 `:436-470`。但这会改掉被测试钉住的行为（`internal/eval/runner_test.go:175`、`:376` 断言 dry-run hint 必须是 `unjudged`），且这些文件属并行实施方的未提交批次，故不在验收轮内动。详见 run doc §8。
- 与 G8 退出判据的关系不变：本轮只把已有基础设施的**当前有效性**与**判读边界**做实，质量一侧（真实模型 30–50 章连写人评、R3/R4 A/B）仍空白。

## 5. 自动化

```bash
go test ./internal/eval -count=1
go vet ./internal/eval ./cmd/longeval
go build ./... && go vet ./... && go test ./... -count=1
go test -tags acceptance_review ./... -count=2
export PATH="/d/tools/mingw64/bin:$PATH"; export CGO_ENABLED=1
go test -race ./... -count=1
```

全量回归（2026-10-23）：上述四组命令均通过，8 个包无失败；`acceptance_review` 标签下 `store` 29.5s、`server` 8.6s 等全部 `ok`。

全量回归（2026-10-24，R4 与盲评轴落地后）：`go build ./...`、`go vet ./...`、`go test ./... -count=1`（9 包）与 `CGO_ENABLED=1 go test -race ./... -count=1` 全部通过，退出码 0；普通跑 `internal/eval` 2.8s、`internal/server` 5.5s、`internal/store` 12.0s，`-race` 下分别 8.6s、12.2s、17.3s（[r4-race.txt](../docs/acceptance-evidence/20261024/r4-race.txt)）。`gofmt -l internal cmd web` 仍列出 5 个既有文件（`internal/analysis/extract_prompt.go`、`internal/analysis/merge_test.go`、`internal/novel/memory_test.go`、`internal/novel/project.go`、`internal/store/memory_init_test.go`），它们是 HEAD 既有格式偏差、与本次改动无关，未顺手重排以免与并行改动冲突。

覆盖点：

- 报告确定性：同一用例、同一固定时间跑两次，`Report` 深度相等。
- 预算不变式：ctx=1600 记忆预算为 0；ctx=12000 时 `user≤limit`、`sections.memories==injectedChars`、费用为正。
- 记忆开关：`useMemory=false` 时检索与注入都为 0。
- 隔离性：`dataDir` 模式运行后源目录全树 SHA-256 不变。
- 停点切片：`afterChapter=1` 的正文尾窗小于全书停点。
- live：mock provider 的流式/非流式路径、截断、清单提示、实际输出 token 与费用。
- 成本闸：未定价时拒绝 `maxCost`；第二个停点超限跳过且不产生调用；实际输出超目标长度时调用后仍能标记超限。
- 人评工具：盲评包同种子可重放、包内不泄漏开关、key 映射正确；聚合覆盖/胜负/未授权事实、缺评分与非法状态拒绝；≥3 停点且 2/3 胜或平才通过；记忆轴与召回轴各自的配对合法性（召回轴要求两侧记忆设置一致、开侧真注入、闭侧未注入）与未知轴拒绝。
- 语料：合成长篇确定性、埋点必现、`novel.SplitChapters` 章数正确。
- R4 召回：埋点章命中与 TopK 去重/确定性；片段长度不超过传入预算（含省略号）；方向加权 vs 不加权的排序差；预算不变式（`sections.recall == injectedChars`、`Σsections ≤ user`、ctx=1600 召回为 0、正文窗口不让位）；无命中回收预算；关闭召回 golden prompt 逐字不变；SSE meta/预览透出与预览只读；显式开而无命中的 warning。

## 6. 未决与边界

1. **真实模型人评未执行**：G8 的核心「30–50 章连写质量」需要真实模型与人工评分；当前只有 mock 与 dry-run。I3-14/I3-15 同样未执行（issues #30）。盲评包/聚合工具已就绪并同时支持记忆轴与召回轴（§4.3、§4.4），但当前 `data/config.json` 不含密钥、`OPENAI_API_KEY` 未设置，live 通道即使网关恢复也无法启动；需要 yj 提供密钥/代理后再执行。**2026-10-24 桩模型轮的收窄结论（`task/e2e-stub-chain-acceptance.md`）**：把 `baseUrl` 指向本地 OpenAI 兼容桩（返回一份对四套解析器都合法的 JSON + 固定流式正文），真实进程已跑通「分析 → 初始化 → 深度提取 → 采纳 → 记忆注入 → 计划生成/激活 → 续写 SSE → 改稿自愈」，并把每次请求体留档做成 prompt 捕获仪表。因此本节缺的东西可以精确成一句：**只有「模型输出质量」一侧需要密钥，链路/预算/注入/自愈这一侧不需要**。（复现注意两条环境坑：`baseUrl` 必须自带 `/v1`；Windows 上 8123/8124 属系统保留端口段，`bind` 会 `EACCES`，用 8097/8098。）
2. **合成语料不评价文笔**：它只提供确定性的规模/预算基准；`mustLand` 命中也只能作为提示。
3. **记忆夹具是当前快照**：停点只截断正文，不回放历史记忆；早期停点看到的是同一份记忆。若要评估「第 2 章伏笔、第 8 章当前」，应使用 `dataDir` 模式并准备对应时点的数据副本。
4. **成本是启发式估算**：tokenizer 差异、重试、评审调用、缓存折扣均未计入；单价需人工维护。`-max-cost` 是本地预测闸，不能替代供应商账户限额；首次真实长篇运行仍建议同时设置供应商侧预算。报告可能含真实正文与模型输出，**不要提交含未公开作品的报告**。
5. **数据目录复制成本**：`dataDir` 模式会完整复制目录后再运行（保证源目录只读）；超大作品需要预留磁盘与时间。
6. **仍未交付的 S6 项**：真实多作品长篇回归集、500 万字级**真实**作品导入（合成分量级已于 2026-10-24 走通真实 HTTP 链路，见 §4.6；缺的是真实长篇语料的导入+分析，卡在通道与 Key）、30–50 章人评、`ContextChars` 分档建议定稿、R4 开启/关闭的文学质量 A/B、发布报告。R4 召回本体已交付（§1.6、§4.4），但其收益判定依赖真实模型 live；UI 全流程走查已在 40 章 + mock 模型下完成（§4.2），真实模型下的 UI 走查并入人评。`CGO_ENABLED=1 go test -race ./...` 已于 2026-10-23 全绿，2026-10-24 复跑（含 R4）见 §5。`-max-cost`/`-strict-budget` 只提供本地闸，不替代真实模型质量评估。
7. **R4 的已知边界**：①BM25 + n-gram 是词面检索，没有语义召回，别名/换词指代会漏召；②每次续写重建索引，500 万 rune 级约 0.7–0.8 s、257 MB 瞬时分配（§4.4），未做跨调用缓存；③方向加权（×3）是启发式，方向写得很长或含大量表层词时会稀释；④前端已展示召回段与 `useRecall` 三档开关（§4.5 走查），但走查只覆盖到 SSE `meta`，真实模型下的开/关质量对照仍未做；⑤真实长篇的「埋点—召回」命中率已在 2026-10-24 修复后量到 3 字线索 96%、2 字线索 42%（§4.4 补记、issues #50），但这是检索度量，不是文学质量度量，且 2 字线索的天花板来自无分词 n-gram 本身；⑥回归闸已于同日收尾入库（`TestSearchPlantedClueRateOnSyntheticCorpus`，合成 60 章/约 3.5 万 rune：3 字埋点命中率 ≥0.90、每条带线索方向过闸章数 == 1、无线索方向注入 0 字、同输入两轮逐条一致）；反向验证表明**只看命中率看不见闸门放宽**（`MinClueRunes` 降到 1 时命中率仍 1.0、但 20 条探针全部 `hits=2`），故 hits 数量是主要哨兵。2 字线索 ≥0.40 仍只在真实语料探针脚本里量（脚本含未公开正文，不进版本库）。

8. **`cmd/longeval` 记忆夹具的提交链不自洽（issues #59，S6 之前必须修）**：`eval/cases/memory/` 带 `meta.json{memoryRevision:12}` 却没有 `commits.json`，因此用该夹具的隔离目录只要「写入一次记忆（含 G4 的 reconcile、G6 的联合提交）+ 重启进程」，提交链校验就必然 fail-closed——`safeState=true`、`memoryDisabledReason="safe_state"`、记忆/计划/召回注入同时归零、正文窗口回补。**验收协议红线**：引用任何注入读数前先读 `GET /api/memory` 的 `safeState`；「注入突然全 0」不得读成「记忆失效」。修法（①补一份 `basedOn=0` 起链的合法 `commits.json`，或 ②`meta.memoryRevision` 归 0 让首轮自然递增）属 eval 夹具，不改产品代码；§4.6/§4.7/§4.8 三轮未受影响（各自会话内未重启，且 §4.6/§4.7 只读不写记忆）。

9. **dry-run 侧的两个判读盲区（issues #68，2026-11-02 实测）**：①不带 `-live` 时清单恒 `unjudged`，且报告里没有任何正文/段文本可判（`prompt.sections` 只给字符数、`memory.records` 只给条数），所以「mustLand 事实是否进了 prompt」在 CLI 侧无法机械核对——「回归闸绿」只能读成「预算与注入量符合预期」，不能读成「事实一致性通过」。②dry-run 下预计费用超 `-max-cost` 只发 warning、退出码 0，硬失败只发生在 live 分支。③`go run` 会把子进程 2/3/4 压成 1，CI 必须用构建后的二进制。**免 Key 的最小补法**（对 `prompt.User` 做子串判定 + 报告带段文本）会改掉 `internal/eval/runner_test.go:175/:376` 钉住的行为，且文件属并行实施方的未提交批次，需 yj 拍板后由实现方动；本轮零代码改动。详见 §4.9 与 [run doc](../docs/acceptance-evidence/20261024/g8-gate-recheck/g8-gate-recheck-run.md) §7/§8。

## 7. 与 G8 退出判据的关系

roadmap S6 的退出判据保持不变：G1–G8 逐项 ✅、每项有可重放证据、R4 开启前后召回命中率与一致性可量化。本次交付把「预算/成本可度量、可重放」从零变成可执行基础设施，补齐 `-race`、40 章 UI 全流程与人评盲评工具证据，并在 2026-10-24 落地 R4 长篇召回（可检索、可注入、可观测、可在 eval 里做 on/off 对照），但**不构成 G8 完成**：召回命中率已有真实长篇度量（3 字线索 96%、2 字线索 42%，无线索方向零注入），而真实模型下的开/关一致性对照仍未执行——「注入更相关的前文是否让几十章连写不崩」没有证据；G8 仍为 🚧。
