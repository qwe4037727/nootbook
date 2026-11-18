# G8 回归闸复测：预算硬闸、成本计量与 dry-run 判读盲区（2026-11-02）

口径声明：

- **无模型 Key**：全部 `go run`/二进制调用都是 dry-run，未发起任何模型请求；桩密钥本轮未使用。
- **零代码改动**：产品代码与 `internal/eval`、`cmd/longeval`、`eval/cases` 全部只读。工作区 HEAD `f36025e` + 83 个未提交条目（并行实施方的批次），其中 `internal/eval/`、`cmd/longeval/`、`eval/` 整体仍是 `??` 未跟踪状态。
- **仓库 `data/` 未触碰**；报告落在 gitignore 的 `output/`，本目录只存副本。
- 语料：`eval/cases/longform-synthetic.json`（合成 40 章 / 41,520 非空白字）+ `eval/cases/memory/` 与 `eval/cases/knowledge.json` 夹具（`memoryRevision=12`）。**夹具记忆只证管线，不证提取质量**（issues #56）。
- 二进制：`go build -o output/longeval-g8.exe ./cmd/longeval`（2026-11-02 15:37，本轮新建，专为拿真实退出码）。

## 1. 一句话结论

G8 的「成本/预算可度量、可控」三件事在当前工作区**复测全部成立**（预算硬闸 `exit 3`、`-pricing` 逐停点与合计费用、`-max-cost` 未定价即拒绝）；同时量到两个 dry-run 侧的**判读盲区**并已入库为 issues #68：①回归清单在 dry-run 恒为 `unjudged`，报告里没有任何正文字段可供机械判定；②dry-run 下预计费用超 `-max-cost` 只发 warning、退出码仍 0。因此本轮把「回归闸现在实际 gate 什么」从推断升级为实测：**gate 的是预算与注入量，不是事实一致性**。

## 2. 复现命令（逐字）

前置回归（2026-11-02 15:41 实跑）：`go build ./...` 与 `go vet ./...` 退出码 0；`go test ./... -count=1` 9 个有测试的包全 `ok`（`agent 4.1s / analysis 9.0s / eval 7.9s / llm 15.9s / novel 14.2s / plan 17.9s / recall 13.0s / server 24.0s / store 30.1s`），`novelagent` 与三个 `cmd/` 为 no test files。也就是说本轮结论建立在**当前工作区可构建、可通过全量测试**的前提上，不是 10-23 那批二进制的读数。

```bash
# 常规构建与回归（本轮实跑，全绿）
go build ./... && go vet ./... && go test ./... -count=1
go build -o output/longeval-g8.exe ./cmd/longeval

# ① 12 停点联合复测（early/late 记忆 + recall-on/off 召回 × 4000/12000/30000）
./output/longeval-g8.exe -case eval/cases/longform-synthetic.json -tiers 4000,12000,30000 \
  -out output/r4_gate_recheck.json                      # exit 0

# ② 预算硬闸：1200/1600 应「贴顶但不超」
./output/longeval-g8.exe -case eval/cases/longform-synthetic.json -tiers 1200,1600 \
  -strict-budget -out output/r4_strictbudget.json        # exit 0

# ③ 预算硬闸：400 必须失败
./output/longeval-g8.exe -case eval/cases/longform-synthetic.json -tiers 400 -stops late \
  -strict-budget -out output/r4_tight_built.json         # exit 3（见 §6 go run 只给 1）

# ④ 成本计量（合成单价 2.5/10 USD per M token，非真实账单）
./output/longeval-g8.exe -case eval/cases/longform-synthetic.json -pricing output/pricing-test.json \
  -tiers 12000 -stops early,late -out output/r4_priced.json   # exit 0，priced=true

# ⑤ 成本护栏的两条边界
./output/longeval-g8.exe -case eval/cases/longform-synthetic.json -stops late -max-cost 0.01 \
  -out output/r4_maxcost_unpriced.json                   # exit 1：max cost requires input/output pricing
./output/longeval-g8.exe -case eval/cases/longform-synthetic.json -pricing output/pricing-test.json \
  -tiers 12000 -stops early,late -max-cost 0.04 -out output/r4_maxcost_hit.json   # exit 0 + warning（§5）
```

## 3. 十二停点联合读数（`gate-recheck-12stops.json`）

`totals`：`stops=12`、`inputTokens=149,952`、`outputTokens=8,400`、`maxUserBudgetPct=92.325`、`memoryInjectedStops=6`、`recallInjectedStops=3`、`overBudgetStops=0`、`priced=false`。

| 停点 | user/limit | 占比 | overBudget | 记忆 注入/预算 #条 | 召回 注入/预算 #命中 [章] |
|---|---|---|---|---|---|
| early@ctx4000 | 3645/4000 | 91.125% | false | 242/1000 #7 | 0/0 h0 [] |
| early@ctx12000 | 9635/12000 | 80.292% | false | 242/3000 #7 | 0/0 h0 [] |
| early@ctx30000 | 11287/30000 | 37.623% | false | 242/3000 #7 | 0/0 h0 [] |
| late@ctx4000 | 3693/4000 | 92.325% | false | 242/1000 #7 | 0/0 h0 [] |
| late@ctx12000 | 9656/12000 | 80.467% | false | 242/3000 #7 | 0/0 h0 [] |
| late@ctx30000 | 27660/30000 | 92.200% | false | 242/3000 #7 | 0/0 h0 [] |
| recall-on@ctx4000 | 3391/4000 | 84.775% | false | 0/1000 #0 | **779/800 h1 [3]** |
| recall-on@ctx12000 | 9409/12000 | 78.408% | false | 0/3000 #0 | **779/800 h1 [3]** |
| recall-on@ctx30000 | 27355/30000 | 91.183% | false | 0/3000 #0 | **779/800 h1 [3]** |
| recall-off@ctx4000 | 3431/4000 | 85.775% | false | 0/1000 #0 | 0/0 h0 [] |
| recall-off@ctx12000 | 9394/12000 | 78.283% | false | 0/3000 #0 | 0/0 h0 [] |
| recall-off@ctx30000 | 27398/30000 | 91.327% | false | 0/3000 #0 | 0/0 h0 [] |

- 与 2026-10-24 那轮（`docs/acceptance-evidence/20261024/r4-synthetic-40ch-dryrun.json`，`early` 不启用召回）在规模与口径上一致；本轮把它扩到 3 档 × 4 停点重放，**12/12 无一超预算**，召回注入量三档同为 779/800 且都命中第 3 章埋点。
- `late@ctx12000` 的分段（`manuscript 8958 + memories 242 + characters 89 + summaries 81 + overview 55 + world 36`）合计 ≤ `user 9656`，即 R2 的「全量预算计量」不变式仍成立。
- 记忆停点每条都有 `skippedReview=1`、`droppedByBudget=0`：夹具里那条待复核记录被红线停投，符合 §4.8 的判定。
- **`recall-on/off` 两臂 `mem 0` 是用例设计**（`evalreview -axis recall` 要求两臂 `useMemory` 一致），不是记忆回归。

## 4. 预算硬闸的工作边界（`strict-budget-tiers-1200-1600.json`、`strict-budget-ctx400.json`）

| 档 | 停点 | user/limit | 占比 | overBudget | 记忆 |
|---|---|---|---|---|---|
| 1200 | early | 1143/1200 | 95.250% | false | 0/0 #0 |
| 1600 | early | 1575/1600 | 98.438% | false | 0/0 #0 |
| 1200 | late | 1164/1200 | 97.000% | false | 0/0 #0 |
| 1600 | late | 1578/1600 | 98.625% | false | 0/0 #0 |
| **400** | late | **543/400** | **135.750%** | **true** | 0/0 #0 |

- 收缩顺位到底时，1200/1600 两档**贴顶但不越界**（最高 98.625%），`ctx ≤ 1600` 记忆归零（issues #55 的成因，本轮再量一次）。
- `ctx=400` 时即便把记忆与召回都归零，`overview+plans+角色字段+正文最小窗口` 仍收缩到 543 rune > 400 → `overBudget=true`，`-strict-budget` 让进程以 **3** 退出（`longeval: 1 stop(s) exceed ContextChars after shrink`）。这就是「可控」的硬闸形态：它不承诺任意小的 `ctx` 可用，而是承诺「收缩到底仍越界时必然报警」。
- 400 档报告里 `warning: late: prompt exceeds budget after shrink (543 > 400 runes)`（`runner.go:348-350`）。

## 5. 成本计量与护栏（`priced-2stops.json`、`max-cost-warning-dry-run.json`）

- 合成单价 `{inputPerMillion:2.5, outputPerMillion:10, currency:USD}`（本目录 `pricing-synthetic.json`，与 `eval/pricing.example.json` 同族、**非真实账单**）。
- 逐停点：`early cost=0.033058 USD`、`late cost=0.033125 USD`；`totals={stops:2, inputTokens:20073, outputTokens:1600, totalCost:0.066183, priced:true, maxUserBudgetPct:80.466667, memoryInjectedStops:2, overBudgetStops:0}` → 「可度量」侧成立。
- 护栏边界一：未定价 + `-max-cost` 直接拒绝（`eval: max cost requires input/output pricing`，`runner.go:150`），退出码 1 → 护栏不会被「忘了填单价」静默失效。
- 护栏边界二（**新入库**）：dry-run + `-max-cost 0.04`，预计 `0.066183 USD` 超限时只打印 `warning: dry-run projected cost … exceeds max cost …; no calls were made`，`costLimitHit` 仍为 false、**退出码 0**（`runner.go:209-212` 只在 `!Live` 分支写 warning，`CostLimitHit` 仅在 live 分支 191-202 置真，`main.go:112` 据此才退 2）。

## 6. 退出码陷阱（判读红线）

| 运行方式 | 同一 ctx=400 超预算用例 | 证据 |
|---|---|---|
| `./output/longeval-g8.exe …` | **exit 3** | `strict-budget-ctx400-built-binary.txt` |
| `go run ./cmd/longeval …` | **exit 1**（stderr 文本里才写 `exit status 3`） | `strict-budget-ctx400-go-run.txt` |

`go run` 会把子进程非零码统一压成 1（与 §2 早前记录的 `evalreview` 退出码 4 同一现象）。**任何把 `-strict-budget`/`-max-cost` 接进 CI 的做法都必须先 `go build` 再跑二进制**，否则「3=预算越界、2=成本越界、4=人评不通过」三种失败在流水线上会退化成同一个 1，判据无法分流。

## 7. 判读盲区：dry-run 的清单恒为 `unjudged`（issues #68）

- 现象：本轮 4 份报告、52 个清单项（12 停点 ×4、tight 4、strictbudget 12、priced 6）**全部 `hint:"unjudged"`、`judgment:""`**。
- 机制（代码侧，只读）：dry-run 路径 `report.Checklist = buildChecklist(stop, nil)`（`internal/eval/runner.go:371`），`buildChecklist` 缺省置 `unjudged`（`runner.go:443`），只有 `live != nil && live.Error == ""` 才对 `live.Output` 做子串判定（`runner.go:445-462`）。这与 §3 度量口径第 5 条一致，属设计而非缺陷。
- **更要紧的边界**：dry-run 报告里根本没有可判定的文本。`PromptMetrics.Sections` 是 `map[string]int`（段名→rune 数，`runner.go:302-305`），`MemoryMetrics.Records` 是条数（`runner.go:322`），全文只有字符数。于是「mustLand 事实有没有进 prompt」在 CLI 侧无法机械核对——而这恰是免 Key 场景里唯一能先证的那一半。
- 对照：同一条事实在 HTTP 预览路径是可查的（`POST /api/continue/preview` 返回 `memorySection` 全文，§4.7 已用它实测到 `左臂旧伤未愈`、`青玉符的来历` 进 prompt）。**盲区只属于 eval CLI 一侧，不是产品侧观测缺失。**
- 因此当前阶段的准确表述：**合成回归闸 gate 的是「预算不被越界 + 注入量与召回命中符合预期」，不 gate 事实一致性**；G8 的「质量回归集」一侧仍需 live + 人评（§4.3、issues #43）。

## 8. 免 Key 可补的一步（待 yj 拍板，本轮零代码）

把清单判定拆成「模型之前」与「模型之后」两段，前者不需要 Key：

1. `buildChecklist` 增加第三类判定：`mustLand` 对 `prompt.User` 做子串检查 → `in_prompt` / `absent_from_prompt`（`mustNot` 反向），仍保留 live 侧的 `observed/not_observed`。改动点集中在 `internal/eval/runner.go:436-470`，`runStop` 已把 `prompt` 留在作用域内（`runner.go:291-300`）。
2. 报告增补 `prompt.memoriesText` 或 `prompt.digest`（只放记忆段/召回段文本，或正文哈希），否则判定结果无法复核。
3. **需要显式决策**：`internal/eval/runner_test.go:175` 与 `:376` 目前断言 dry-run 的 hint 必须是 `unjudged`，第 1 步会改这条被测试钉住的行为——属实现方与 yj 拍板范围，本验收轮不改。
4. 附带：dry-run 成本超限是否也该退非零（§5 边界二）同样待拍板；若要，最小改法是 `!Live` 分支把 `CostLimitHit` 置真或新增 `projectedCostOverLimit` 观测字段。

这三条落成后，「回归集」才能在无 Key 环境里回答「记忆里的既定事实有没有被送到模型眼前」，把 G3/G8 的一半判据从人评前移到 CI。

## 9. 本轮变更文件

- 新增 `docs/acceptance-evidence/20261024/g8-gate-recheck/`：本 run doc + `gate-recheck-12stops.json`、`strict-budget-tiers-1200-1600.json`、`strict-budget-ctx400.json`、`strict-budget-ctx400-built-binary.txt`、`strict-budget-ctx400-go-run.txt`、`priced-2stops.json`、`max-cost-warning-dry-run.json`、`pricing-synthetic.json`。
- `docs/issues.md`：新增 #68（dry-run 清单恒 unjudged + 报告无正文可判 + dry-run 成本超限只 warning + 退出码被 `go run` 压平的判读红线）。
- `task/g8-acceptance.md`：新增 §4.9，§4 证据表补一行，§6 未决项同步。
- `task/roadmap.md`：S6/G8 行补复测结论与红线。
- 产品代码、`internal/eval`、`cmd/longeval`、`eval/cases`、仓库 `data/` 零改动。
