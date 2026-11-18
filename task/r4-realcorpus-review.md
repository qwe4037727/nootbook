# R4 历史召回：真实语料命中率复核（2026-10-24）

授权：用户要求对并行实施方的进度做独立测试与判定。本轮**不修改任何产品代码**，不改 `task/` 既有文档，不触碰 `D:\go\data\`；数据目录与报告全部在 `%TEMP%\r4harness\`（含未公开作品正文，**不要提交**）。

结论一句话：**R4 的「检索—预算—观测」链路成立，但在真实长篇语料上「埋设→召回」几乎不成立（命中率 5%–13%），而每档都在为它支付正文窗口成本。**`task/g8-acceptance.md` §4.4 的合成 40 章证据不能外推到真实作品；roadmap S6 的「R4 开启前后召回命中率可量化」现在有了第一个真实数字，而它不合格。

## 1. 复现步骤

```bash
go build -o "$TEMP/r4harness/novelagent.exe" .
"$TEMP/r4harness/novelagent.exe" -addr 127.0.0.1:18091 -data "$TEMP/r4harness/data"
# 导入真实语料 corpus/无边悔恨.txt（79 章 / 163,423 非空白字），只走 HTTP API：
#   POST /api/import → PUT /api/config{contextChars} → POST /api/continue/preview{useRecall}
```

脚本（均在 `%TEMP%\r4harness\`，未入库）：`r4_real.py`（分档 × 开关对照）、`recall_hitrate.py`（2-gram 埋点命中率）、`recall_recentbody.py`、`recall_diagnosis.py`（四条件诊断）、`recall_3gram.py`（3-gram 公平口径 + 控制组）。

**方法**：从全书中挑选「只在一个章节里出现」的 n-gram（df=1）作为埋点词，把它放进①续写方向，或②最近正文（第 79 章末尾），再看召回注入的三个章节里是否包含该词的原章节，以及注入文本里是否真的含该词。这与合成用例 `Planted: [{Text:"青玉符", Chapter:2}]` 是同一口径，唯一差别是语料为真实文本。3-gram 口径是刻意与合成用例对齐的（「青玉符」= 2 个 2-gram + 1 个 3-gram），避免用长度差冒充结论。

> 首轮曾用 `/api/continue/preview` 的 `afterChapter` 当中盘/后期停点，**该接口不切分章节**，九次探测其实都是同一全书状态；这既不是缺陷也不是结论，正式的停点切片要用 `cmd/longeval -case`（`afterChapter` 生效）。本轮命中率不受影响：所有条件都在同一状态下比较。

## 2. 实测数据

### 2.1 埋点→召回命中率（ctx=12000，除注明外）

| 口径 | 埋点位置 | 样本 | 目标章被召回 | 埋点词出现在注入文本 |
|---|---|---|---|---|
| 2-gram | 仅方向 | 40 | 3（7.5%） | 1（2.5%） |
| 2-gram | 仅最近正文 | 40 | 2（5.0%） | 0（0.0%） |
| 2-gram 重复 8 次 | 最近正文 | 30 | 2（6.7%） | 0 |
| 2-gram | 方向，ctx=4000 | 30 | 0（0.0%） | 0 |
| 2-gram | 方向，ctx=30000 | 30 | 3（10.0%） | 0 |
| 3-gram | 仅方向 | 30 | 4（13.3%） | 3 |
| 3-gram | 仅最近正文 | 30 | 2（6.7%） | 0 |
| 3-gram | 方向 + 正文 | 30 | 3（10.0%） | 2 |

方向加权（`defaultIntentWeight=3`）确实有用——它是唯一能把命中率抬到 13% 的杠杆——但 13% 仍是「基本召不回」。重复正文里的用词没有任何帮助（8 次 ≈ 2 次），见 §3.1。

### 2.2 排序对查询几乎不敏感（控制组）

用不含埋点词的中性方向连跑 5 次预览，**返回的章节集合 5/5 完全相同：`[58, 75, 19]`**（distinct_rankings=1）。同一状态下换三条不同方向时命中的是 `[78,77,58] / [58,19,1] / [12,75,58]`，说明方向能改变排序，但改变幅度受 §3.1 的分母支配：真实排序的主成分是「哪些旧章与最近窗口表层词重叠最多」。

### 2.3 成本与不变式（真实语料，79 章）

| ctx | 召回注入 | `sections.recall` | 正文窗口 开/关 | 正文窗口损失 | `Σsections ≤ user` | `overBudget` | 预览耗时 |
|---|---|---|---|---|---|---|---|
| 4000 | 800/800 | 800 | 2198 / 2988 | −26.4% | 成立（3016） | false | 113–132 ms |
| 12000 | 800/800 | 800 | 8181 / 8973 | −8.8% | 成立（8999） | false | 97–120 ms |
| 30000 | 800/800 | 800 | 26188 / 26994 | −3.0% | 成立（27006） | false | 97–122 ms |

预算/计量侧全部合格：注入量恒等于 800、`sections.recall == recallUsedChars`、收缩未触发 `overBudget`、召回失败不阻断。性能也远低于 `task/g8-acceptance.md` §4.4 记的 5M rune / 0.7 s 担忧（163K rune 时 ~0.12 s）。

**问题不在预算会计，在于买到的东西没有价值**：召回无最低分闸，即使「没有有用命中」也照样塞满 800 字并扣掉正文窗口——ctx=4000 档直接损失四分之一正文。

### 2.4 合成对照（复核实施方证据，可复现）

```
go run ./cmd/longeval -case eval/cases/longform-synthetic.json -pricing eval/pricing.example.json \
  -tiers 12000 -stops recall-on,recall-off -out …
recall-on   hits=3 [25 3 31] 注入 763/800     recall-off hits=0 正文窗口 +764 字
```

埋点第 3 章**确实被召回**（排名第 2），`task/g8-acceptance.md` §4.4 的记录为真。合成通过而真实失败，与实现无关的变量是语料规模与词汇重叠度：合成 40 章 × 600 字模板文本，候选块少、重叠可控；真实 79 章 × 2200 字，与最近窗口重叠最多的章节总是赢。

## 3. 根因（代码级，可核对）

1. **词频被丢弃，`k1` 只剩长度惩罚**。[recall.go:257](../internal/recall/recall.go) 的 `tokenizeRunes` 返回 `map[string]struct{}`，[recall.go:160-162](../internal/recall/recall.go) 打分时 `tf≡1`。所以「一个章节反复使用某个关键名词」这件事对得分毫无加成——同一 ctx=12000、同一口径下，正文里重复 8 次得到 6.7%（2/30），重复 2 次得到 5.0%（2/40），差别在噪声范围内。文档口径需修正：[g8-acceptance.md §1.6](g8-acceptance.md) 与包注释写「标准 BM25（k1=1.5、b=0.75）」，实际是**存在性打分**，不是标准 BM25。
2. **查询过长、打分无归一化**。查询是最近正文尾 1200 rune（[prompt.go:349](../internal/agent/prompt.go)），约上千个 2/3-gram；单块得分 = Σ(所有命中词的 idf，方向词 ×3)。真实罕见词 df=1/约 75 块 → `idf≈3.9`，方向加权后 ≈11.7；而与之主题相近的旧章能命中数百个低 idf 词（每个 0.6–1.5）。加和不除以命中词数，也不看最强匹配，于是「表层重叠最广」必然压过「唯一致命线索」。这正是 `Options.Intent` 注释里承诺要避免的结果（[recall.go:39-42](../internal/recall/recall.go)），`defaultIntentWeight=3` 在这个量级差面前不够。
3. **没有最低分闸**。`Search` 只要有任何一块命中就出 TopK=3，`renderRecallSection` 一路填满 800 字（[prompt.go:354](../internal/agent/prompt.go)），无「无有用命中就整段不注入」的判定。

## 4. 建议（供实施方拍板，不在本轮改动）

按性价比排序：

1. **`tokenizeRunes` 改 `map[string]int` 并接入真实 tf**：`tf*(k1+1)/(tf+k1*(1-b+b*dl/avgdl))`。这是让「反复出现的线索」可被召回的前提，也才配得上「标准 BM25」的说法。
2. **查询只留判别性词**：按 df 过滤（如 df > 5% 块数即丢弃）后再打分，或改为「每块取贡献最高的 N 个词」；配合把 `IntentWeight` 提到与判别性词同一量级（8–16），而不是与数百个噪声词相加竞争。
3. **最低分/判别闸**：最佳块的词权中位数不足时整段不注入，把 800 字还给正文窗口；`meta.recallUsedChars=0` 已是可观测点，A/B 时不会被误读成「开了没生效」（现有 warning 只覆盖显式 `useRecall:true` 无命中的情形）。
4. **把本轮方法固化为回归资产**：`eval` 里真实语料的「df=1 埋点 → 目标章召回率」阈值用例（建议先定 ≥50%，两位评审口径另说）。现在只有合成埋点的确定性断言，改了打分算法也不会告警。
5. 文档修正：`g8-acceptance.md` §4.4 的结论行建议改为「合成链路通过；真实语料命中率实测 5%–13%，不满足 S6 判据」，避免被读成 R4 可用。

## 5. 顺带核实与遗留

- `CGO_ENABLED=1 go test -race ./... -count=1`（gcc 15.2.0，`D:\tools\mingw64`）**今日复跑全绿**：agent/analysis/eval/llm/novel/plan/recall/server/store 9 包 ok，无竞争报告；比 `docs/acceptance-evidence/20261023/g8-race.txt` 多了新 `internal/recall`。
- `go build ./...`、`go vet ./...` 无诊断。
- `task/roadmap.md` 的 G6/G8 行已随实施方更新，我此前指出的「行陈旧」两处已消失。
- 仍存在的文档缺陷：[iter3-acceptance.md:16](iter3-acceptance.md) 「`ContextChars` 仍是正文预算，语义不变」与实现相反——[prompt.go:193](../internal/agent/prompt.go) 用 `ContextChars` 约束整个 user prompt（`docs/issues.md` #40 也按此记录）；同句的 `Sections` 列表已漏掉新增的 `recall` 段。
- 模型通道复测（同日 11:40 后追加）：
  - `http://192.0.2.60:18080/v1` 仍不可用，且**与请求形状无关已再次证实**：`/v1/models` 200（注册表在两次调用间从 6 个 id 变成 4 个，`gpt-5.6-terra` 还变成了带空格的 `gpt-5.6 terra`），但 4 个在册模型 + 伪造模型名 + 空/全空格模型名之外的所有形状（minimal、`max_tokens`、`temperature`、`reasoning_effort`、`stream`、`/v1/responses`、`/v1/embeddings`）**20/20 次都是 503，83–110 ms**。空模型名返回 400 `model is required`、`/v1/messages` 返回 403「This group does not allow /v1/messages dispatch」——说明请求被网关的鉴权/派发层接住，从未出网。
  - 中途出现的一次 **502「Upstream access forbidden, please contact administrator」（2193 ms）** 是唯一线索：那次确实打到了上游并被拒，属上游侧凭据/权限问题，不是我们的载荷。
  - 另一条通道 `https://wawapii.com/v1` **现在可达**：用本项目的 Go 客户端（`cmd/probe`，`User-Agent: novel-agent/0.1`）得到干净的 `401 API_KEY_REQUIRED`，没有 Cloudflare 拦截。也就是说 F-UPSTREAM-01 记录的「上游网关阻断」对这条通道当前不成立，只差一个有效密钥。
  - 真实模型侧的 S1、I3-14/15、G5 A/B、G8 live 仍全部 blocked，但阻塞原因是「无可用密钥」，不再是「代码或链路未准备好」——本轮已把链路验到可直接起跑。

## 6. 实施方整改回执（2026-10-24 追加，上文原样未改）

整改不信任「复审结论」本身：命中率、成本与注入闸都用自建探针在 `corpus/无边悔恨.txt`（79 章 / 177,295 rune，UTF-8）上重跑一遍，复审的 5%–13% 在 3-gram 口径下复现为 5/26（19%）。逐条对账：

| 复审建议 | 处置 | 落点与实测 |
|---|---|---|
| 1 `tokenizeRunes` 返回词频、接入真实 tf | **采纳** | `tokenCounts(runes) map[string]int` + `tf*(k1+1)/(tf+norm)`；包注释与 `g8-acceptance.md` §1.6 的「标准 BM25」口径同时改准（此前确实是存在性打分）。`TestSearchRepeatedClueOutranksSingleMention` 守住「重复 8 次 > 提及 1 次」 |
| 2 查询只留判别性词 / 每块取贡献最高 N 词 / `IntentWeight` 提到 8–16 | **部分采纳** | 采纳 `ScoreTermLimit=3`（每块只加最高 3 条贡献，表层重叠不再靠命中面广取胜）+ 判别性过滤（罕见度按**章节频** `df ≤ ceil(5%×章数)`；复审按块频，400 rune 重叠窗口会把长章节同一个词数两遍，块频当稀有度会误杀埋在长章节里的线索）。**未采纳** `IntentWeight=8–16`：注入闸改成「必须是方向里的罕见线索」后，竞争量级问题被结构性消除，再抬权重等于给方向里每个空洞词发乘数，可解释性差；实测 19%→96% 不靠它 |
| 3 最低分/判别闸，无有用命中整段不注入并把 800 字还给正文 | **采纳（换了形态）** | 不用「词权中位数」阈值（要按语料调参、不可解释），改为前置可追溯闸：块内须命中**方向里**的罕见线索，且线索须 ≥3 个连续汉字，或（2 字词时）全书唯一。`MinClueRunes` 的必要性由真实语料审计钉死：空洞方向的 33 个 n-gram 里 28 个「算罕见」、27 个全书不出现，独自决定注入的只有 2 字虚词残片「现有」（2 章出现）。零命中时 `recallBudget=0`，实测无线索方向注入 0 字、正文窗口回到 8973 字 |
| 4 把本轮方法固化为回归资产（阈值用例） | **采纳，已入库** | `internal/recall/recall_test.go::TestSearchPlantedClueRateOnSyntheticCorpus`：合成 60 章 / 约 3.5 万 rune（跨章通用句 + 中频母题 + 末 5 章独有正文当查询窗），20 条全书唯一三字线索，断言命中率 ≥0.90（实测 20/20）、每条带线索方向**只有 1 章过闸**、片段含原句、两轮逐条一致、模糊方向与无共享词方向各 0 注入，并自检语料前提（模糊方向与正文只共享「现有」，章节频 2）。阈值取 0.90 而非建议的 ≥50%，因修复后实测 0.96/1.00。**反向验证**（临时 `MinClueRunes` 3→1 后还原）：命中率仍是 1.0（排序没塌），但 20 条探针全部 `hits=2` —— 同族线索前两字重复到 df=2 就够开闸。结论：**只监控命中率看不见闸门放宽**，故 `hits==1` 是主要哨兵，这条判据也写进建议冻结清单 |
| 5 文档修正（§4.4 结论别被读成 R4 可用） | **采纳** | `task/g8-acceptance.md` §1.6 重写内核描述、§4.4 增补真实语料否证与修复后数字、§6 第 7 条划清边界（检索指标 ≠ 文学质量；17.7 万字 ≠ 500 万字）、§7 结论改为「召回命中率一侧已有真实语料支撑，文学一致性对照仍无证据」；`task/roadmap.md` S6 同步 |
| §5 遗留的文档缺陷（`iter3-acceptance.md:16`） | **采纳** | `docs/issues.md` #51：加「2026-10-24 更正」引注，写明 `ContextChars` 先扣记忆预算再扣召回预算 `min(800, ctx/4)`、`Sections` 含 `recall` 档、零命中还预算 |

整改后的全量回归（2026-10-24）：`gofmt -l internal/recall` 无输出、`go vet ./...` 通过、`go test ./... -count=1` 9 包全绿、`CGO_ENABLED=1 go test -race ./... -count=1` 9 包全绿（第 5 次复跑，见 `docs/acceptance-evidence/20261024/r4-race.txt` 第 6 节）。数字、脚本副本与口径见 `docs/acceptance-evidence/20261024/r4-recall-quality/`（探针脚本本体在 `output/`，含未公开作品正文，不进版本库）。

仍然不做的两件事，与复审判断一致：两字线索 38%–42% 是无分词 n-gram 的天花板，收益应交给 G2/G3 的结构化实体记忆而不是继续调 n-gram；「注入更相关的前文是否提升续写文学质量」仍无证据，需要密钥跑 `longeval -stops recall-on,recall-off` + `evalreview -axis recall`，当前 `wawapii.com/v1` 只差一个有效密钥（`401 API_KEY_REQUIRED`）、`192.0.2.60:18080` 20/20 次 503。
