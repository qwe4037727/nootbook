# 真实语料《无边悔恨》G1+G2 链路终态 + S1/G8 成本投影（2026-11-02，零模型 Key）

> 本轮把 **G1 与 G2 的链路形状第一次在真实语料上跑到终态**（桩模型，非真实模型输出），并第一次给出**有实测输入支撑的成本区间**。两件事都必须按各自的口径读：链路≠质量，成本区间≠账单。

## 1. 结论

1. **真实语料的「导入→自动分章→自动分析知识库」在产品里跑到了终态**：`corpus/无边悔恨.txt`（511,497 字节）经 `POST /api/import` 分成 **79 章 / 163,423 字**，`POST /api/analyze` 后 `analysis.status=ready`、`analyzedCount=79/79`，全程 10 秒内（真实网络等待为零，模型侧是本地桩）。分章结果带真实章名（已脱敏），说明切分器对这本真实出版文本是可用的。
   - **这不等于 G1 已验收**：模型返回的是桩里的固定 JSON，知识库内容没有任何真实性可言。G1 的「自动分析**出**知识库」的质量侧仍是零验证，仍卡在没有可用通道（见 issues #69/#70）。这条证据买到的是**链路形状、调用次数、请求体量**在真实语料上成立。
2. **分析请求的输入量是实测的，不是估的**：80 次调用（79 次逐章分析 + 1 次总览合成），合计 **251,625 tokens**（analyst 249,948 / synthesis 1,677），单次 analyst 输入 **1,988–3,833 tokens**；system 提示固定 1,164 字。桩服务把每一次真实请求体都落了盘（元数据见 `wubian-stub-requests.meta.jsonl`，为免把整本书再抄一份进版本库，只保留每次调用的计量字段）。
3. **真实语料上没有一章被 30,000 字裁剪命中**（`clipped=0/79`）。`clipWithNote(chapter.Content, 30000, …)` 的保护在本本书上从未触发——这是**这本书的特征**，不是产品的保证；长篇网文单章超 3 万字不常见，但合集/无章号文本会触发。
4. **分析阶段的「长程上下文」比看上去小得多，而且成本是线性的**：`knowledgeDigest(knowledge, 6000)` 里只取**最近 3 条章节摘要**（`internal/analysis/prompt.go:199`），实测摘要段最大 **195 字**，离 6000 字上限差两个数量级。两个后果必须一起记：
   - **成本侧**：分析不是 O(N²)，是 O(N)（输入几乎只由当章正文决定），所以「书越长越跑不动/越跑越贵」在这条链路上不成立——这正是 S1 敢无人值守跑整本的根据。
   - **质量侧**：逐章分析时模型**看不到全书**，跨章一致性完全依赖 `applyChapter` 的合并去重与最后那一次总览合成。对「长程一致性」这个 G3 目标来说，这是知识库的结构性天花板，值得单独记一笔（issues #71）。
5. **成本根本不是通道决策的约束**（§4）：整本《无边悔恨》跑一遍 G1 分析，按 DeepSeek 官方现价的**输入实测 + 输出三档敏感性**区间是
   - `deepseek-flash` 离峰 **$0.062 – $0.133**（约合 ¥0.44 – ¥0.94），峰时最高 **$0.266**；
   - `deepseek-v4-pro` 峰时最坏 **$0.96**（≈¥6.8）。
   G8 的 30–50 章连写（含每章深度提取）在 flash 离峰 **$0.09 – $0.15**，峰时 ≤$0.30，v4-pro 峰时 ≤$1.21。
   → **「S1 跑真实语料会不会很贵」这个担忧可以划掉**；反过来说，一旦有 Key，先用最便宜的通道把 79 章整本跑通是**近乎零风险**的动作，`-max-cost`/`-strict-budget` 已有硬闸（live 下超限退 2，见 #68）。
6. **G2 的记忆链路同样在真实语料规模上跑到了终态**：同一目录上 `memory/initialize` → `ready`（`events=79`、`states=1`、`cognitions=0`、`threads=0`、`safeState=false`），`memory/extract` 覆盖全部 79 章 → `done`（11.5s，产生 79 个 `proposed` ChangeSet，未采纳所以库计数不变），实测输入 **274,107 tokens**。整本 G1+G2 的输入合计 **525,732 tokens**，flash 离峰约 **$0.165（¥1.17）**、v4-pro 峰时约 **$1.26（¥8.9）** —— 见 §4.1。

## 2. 方法（可复现）
```bash
# 一次性：构建产品二进制（不改动任何产品代码）
go build -o output/novelagent-wubian.exe .

# 真实语料进隔离目录、桩模型跑完整分析链路、抓每次请求体
node output/g1_wubian_cost.mjs      # -> output/wubian-cost-summary.json
node output/g1_wubian_meta.mjs      # -> 压缩成每次调用一行的计量元数据
node output/g1_wubian_price.mjs     # -> output/wubian-cost-pricing.json（按官方现价计价）

# G2：同目录继续跑 记忆初始化 + 全书 79 章深度提取
node output/g2_wubian_memory_cost.mjs   # -> output/wubian-g2-summary.json + wubian-g2-stub-requests.jsonl
node output/g1_wubian_meta.mjs output/wubian-g2-stub-requests.jsonl output/wubian-g2-stub-requests.meta.jsonl
node output/g2_wubian_price.mjs         # -> output/wubian-g2-pricing.json（含整本 G1+G2 合计）

# 真实语料的续写侧单停点（eval CLI，已有用例）
go run ./cmd/longeval -case eval/cases/corpus-wubian.json -pricing eval/pricing.example.json -out output/wubian-probe.json
#   -> chapters=79 chars=163423  input=8629  user=8903/12000(74.2%)  memory=0/3000 records=0  recall=550/800 hits=2 [47 52]
```

隔离口径：数据目录 `output/wubian-cost-data`（全新空目录，由 store 自建），端口 `127.0.0.1:8094`（服务）与 `:8093`（桩），**未触碰 `data/`**；`apiKey` 用字面占位串 `stub-key-not-a-secret`，`baseUrl` 指向本机桩；`corpus/无边悔恨.txt` 只读。零产品代码改动。

## 3. 实测数据

| 项 | 实测值 |
|---|---|
| 导入 | `importedChapters=79`，`importedChars=163,423`，源文件 511,497 字节 |
| 分析终态 | `analysis.status=ready`，`analyzedCount=79`，`totalCount=79`，等待 <10s（桩） |
| 模型调用次数 | 80（79 analyst + 1 synthesis），`temperature` analyst=0.2 / synthesis=0.3 |
| 输入 tokens（产品自己的估算口径） | 合计 251,625；analyst 249,948（单次 1,988–3,833）；synthesis 1,677 |
| system 提示 | 1,164 字（analyst） |
| 「已有知识摘要」段 | 最大 **195** 字（上限 6000，从未接近）；第 1 章为 0（尚无摘要） |
| 30,000 字裁剪命中 | **0 / 79** |
| 续写侧（真实语料尾停点） | 输入 8,629 tokens，`user=8,903/12,000`（74.2%），`memory=0/3000 records=0`（未做记忆初始化），`recall=550/800 hits=2`（命中第 47、52 章） |

## 4. 计价（DeepSeek 官方现价，逐格取自定价页表格）

价格（USD / 1M tokens，2026-11-02 从 `api-docs.deepseek.com/quick_start/pricing` 抓取，原文存 `ds_pricing.html`）：

| | deepseek-flash | deepseek-v4-pro |
|---|---|---|
| 输入（缓存未命中）离峰 / 峰时 | $0.15 / $0.30 | $0.66 / $1.32 |
| 输入（缓存命中）离峰 / 峰时 | $0.003 / $0.006 | $0.022 / $0.044 |
| 输出 离峰 / 峰时 | $0.60 / $1.20 | $1.98 / $3.96 |
| 上下文 / 并发上限 | 1M / 2500 | 1M / 500 |

**S1 整本《无边悔恨》分析（输入按实测 251,625 tokens，输出按每章 500/1000/2000 tokens 三档）：**

| 模型 | 时段 | 每章输出 | 输入 | 输出 | 合计 | ≈CNY |
|---|---|---|---|---|---|---|
| flash | 离峰 | 500 | $0.0377 | $0.0242 | **$0.0619** | ¥0.44 |
| flash | 离峰 | 1000 | $0.0377 | $0.0479 | **$0.0856** | ¥0.61 |
| flash | 离峰 | 2000 | $0.0377 | $0.0953 | **$0.1330** | ¥0.94 |
| flash | 峰时 | 2000 | $0.0755 | $0.1906 | **$0.2660** | ¥1.89 |
| v4-pro | 离峰 | 1000 | $0.1661 | $0.1580 | **$0.3241** | ¥2.30 |
| v4-pro | 峰时 | 2000 | $0.3321 | $0.6288 | **$0.9610** | ¥6.82 |

**G8 长篇连写（续写单次输入用真实语料实测 8,629 tokens；深度提取输入用实测单章均值 3,164+1,500；输出续写 800 + 提取 900）：**

| 模型 | 时段 | 章数 | 输入 tokens | 合计 | ≈CNY |
|---|---|---|---|---|---|
| flash | 离峰 | 30 | 398,790 | **$0.0904** | ¥0.64 |
| flash | 离峰 | 50 | 664,650 | **$0.1507** | ¥1.07 |
| flash | 峰时 | 50 | 664,650 | **$0.3014** | ¥2.14 |
| v4-pro | 峰时 | 50 | 664,650 | **$1.2139** | ¥8.62 |

**计价口径必读**：
- 全部按**缓存未命中**计价，这是有意的保守选择：分析请求里可复用的前缀只有 system 提示（1,164 字）+ 抬头 + ≤195 字摘要段，正文各不相同，缓存命中率极低，所以「缓存命中」那一列对 S1 不适用（对续写倒是有用，因为知识/记忆段会重复）。
- **输入是实测值，输出不是**：无 Key 环境下没有任何真实模型输出可测，输出按 500/1000/2000 三档给敏感性区间；每章分析要吐的 JSON（summary/endsAt/characters[]/world[]/style）现实落点大概在这区间内。账单真值以供应商为准。
- token 估算用产品自己的口径（`internal/eval/token.go`：汉字 1 rune≈1 token，ASCII 4 rune≈1 token），文档明确它「用于预算与成本对比，不用于对账」。
- CNY 只按 7.1 折算给直觉，DeepSeek 以美元计费。

## 4.1 G2 记忆链路同批实测（真实语料 79 章规模）

同一条隔离目录上继续跑 `POST /api/memory/initialize`（纯映射，不调模型）与 `POST /api/memory/extract {chapterIds: 全部 79 章}`：

| 项 | 实测 |
|---|---|
| 初始化 | `status=ready`，`counts.events=79`、`states=1`、`cognitions=0`、`threads=0`，`memoryRevision=1`，**`safeState=false`** |
| 深度提取 | `status=done`，作用域 79 章，耗时 **11.5s**（桩），产生 **79 个 `proposed` ChangeSet**；未采纳，所以 `counts` 与 `memoryRevision` 保持不变（提取只提案、不写库） |
| 模型调用 | 79 次（每章一次），`system` 提示 1,487 字，单次输入 **2,481–4,136 tokens**，合计 **274,107 tokens** |

**整本 G1+G2 的实测输入合计 525,732 tokens**（分析 251,625 + 提取 274,107），计价：

| 模型 | 时段 | 输入 USD | 输出 USD（每章 400/800/1500 档） | G2 合计 | 整本 G1+G2（输出中档 1000/800） | ≈CNY |
|---|---|---|---|---|---|---|
| flash | 离峰 | $0.0411 | $0.019 – $0.071 | **$0.060 – $0.112** | **$0.165** | ¥1.17 |
| flash | 峰时 | $0.0822 | $0.038 – $0.142 | $0.120 – $0.224 | $0.329 | ¥2.34 |
| v4-pro | 离峰 | $0.1809 | $0.063 – $0.235 | $0.244 – $0.416 | $0.630 | ¥4.47 |
| v4-pro | 峰时 | $0.3618 | $0.125 – $0.469 | $0.487 – $0.831 | **$1.260** | ¥8.95 |

→ **一本 16.3 万字的真实书，从知识库分析到全书深度提取提案，全跑一遍是 ¥1–9 量级**（模型与时段决定，成本不是约束）。G2 的提取输入比分析还大（274k vs 250k），因为它每章带的是「已有事件/状态」段而不只是 3 条摘要。

**两个必须连着记的判读**：①`safeState=false` 出现在刚初始化完、尚未采纳任何提案的时刻——按既有红线（`novelagent-stage-roadmap`），此后任何注入读数都要先看这个字段；②`events=79` 全部来自**知识库映射**而不是模型提取，`cognitions/threads=0`，这与 8 章桩链路里「threads 走真实提取通道时 0 注入」（issues #60）是同一族现象，本轮没有推进它，只是确认它在真实语料规模上同样成立。

## 5. 这改变/不改变什么

**改变**：
- roadmap 里 G1 的口径应写成「**链路已在真实语料上跑到终态（桩），质量侧仍无 Key**」，而不是笼统的「真实语料未跑通」——两者对下一步动作的含义不同：前者只差模型质量验证，后者听起来像连流程都会炸。
- S1 的成本风险可以判定为**低**（美元/人民币都是零钱级），通道决策应当按**质量与可复现性**来选，而不是按钱。
- 「跑整本会不会越来越贵/越来越慢」在分析链路上被证伪（线性 + 摘要段只用最近 3 条）。
- roadmap 里 G2 的口径可从「零真实数据验证」细化为「真实语料规模上链路终态已测（initialize `ready` / extract `done` / 79 条 `proposed`），**提取内容与人工采纳语义仍为零**」；`events=79` 全部来自知识映射而非正文提取，`cognitions=0/threads=0` 与 #60 同一形状，读任何注入数前仍要先看 `safeState`（本轮 initialize 时它实测就是 `false`）。

**不改变**：
- G1/G2 的**质量**判据仍是零：知识库条目的正确性、`confirmed` 抽检 ≥90%、未知实体 0 容忍，全都还没测过。
- 深度提取、采纳、注入、计划、改稿自愈的链路形状此前已在 8 章规模上验证过（`e2e-stub-chain`），本轮补的是**真实语料规模**那一侧，两者不能互相顶替。
- 通道本身仍然被 #69 卡着，且只有 yj 能解。

## 6. 明确不做

1. 不调真实模型、不写任何密钥（本机也没有密钥，`data/config.json` 不含 key 字段）。
2. 不改产品代码、不改 `data/`、不改系统代理；`go build` 只产出 `output/` 下的临时二进制。
3. 不把桩输出当成质量证据——文档里每一处「跑通」都标注了是链路意义上的。
4. 不把成本投影写成预算承诺：输出 token 未知，故只给区间。

## 7. 变更文件

- 新增：本目录 14 个文件。G1 侧 9 个（本文档、`wubian-cost-summary.json`、`wubian-cost-pricing.json`、`wubian-stub-requests.meta.jsonl`、`wubian-probe.json`、`g1_wubian_cost.mjs`、`g1_wubian_meta.mjs`、`g1_wubian_price.mjs`、`ds_pricing.html`），G2 侧 5 个（`wubian-g2-summary.json`、`wubian-g2-pricing.json`、`wubian-g2-stub-requests.meta.jsonl`、`g2_wubian_memory_cost.mjs`、`g2_wubian_price.mjs`）。`.mjs` 全部随档留档以便复放（`output/` 本身 gitignore）；两份 `.meta.jsonl` 只含度量字段（角色/长度/段长），不含正文，避免把语料本身写进版本库。
- 台账：`docs/issues.md` 新增「真实语料链路终态与长程上下文上限（G1/G2，2026-11-02）」一节，**#71**（分析阶段长程上下文实测只有最近 3 条摘要 ⇒ 成本 O(N)、跨章一致性不靠分析；含 G1/G2 真实语料终态与 `safeState` 读数红线）与 **#72**（整本 G1+G2 = 525,732 tokens、官方价 ≈¥1–9 ⇒ 成本不是约束，但仍是估算口径不是对账）；`task/roadmap.md` 改 G1 行、G2 行、「验证」行的真实书口径、S1 决策表 A 行（作废 7897 推荐并附成本）、风险表新增「逐章分析的记忆段窗口只有最近 3 条摘要」一行；项目记忆 `novelagent-stage-roadmap.md`、`model-channel-and-decisions.md` 与两份 `MEMORY.md` 索引同步。
- 产品代码：**无**。临时脚本在 gitignore 的 `output/` 下。
