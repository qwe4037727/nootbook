# 三个参考项目的借鉴评估

日期：2026-10-23
评估对象：`EthanYoQ/AI-Novel-Writer`（Pagekin）、`TensorHub-ORG/Storydex`、`zenstory-ai/oh-story-claudecode`（Oh Story）
用途：判断哪些机制值得引入 novelagent，以及各自挂到哪个阶段
关联：[roadmap.md](roadmap.md)、[iter3-详设.md](iter3-详设.md)

---

## 0. 一句话结论

三个项目都是"AI 写长篇小说"，但形态不同：Pagekin 是 Electron 桌面应用（重交互），Storydex 是 Tauri + Rust sidecar（重检索），Oh Story 是一套给 Claude Code/Codex 的 skill 包（重流程与文档）。

**它们验证了我们的大方向（按章切片 + 结构化状态卡 + 事务式提交），但没有一个可以直接搬代码。** 真正值得借鉴的是 8 个具体机制（另有 1 个 R5 溯源/事务提交我们已具备），其中 3 个是当前实现的 P0 缺口：

| 优先级 | 机制 | 来源 | 我们缺什么 |
|---|---|---|---|
| P0 | `finish_reason` 截断分类 | Pagekin | 客户端根本不解析 `finish_reason`，截断被当成成功 |
| P0 | 上下文预算按"全段合计"核算 | Oh Story / Pagekin | `ContextChars` 只等于正文长度，其余段是额外开销 |
| P0 | 冻结用例 + 细纲兑现核对 + 交换顺序盲评 | Oh Story | 我们没有任何"续写质量"的可复现评测 |
| S6 | 纯 Go BM25 + 中文 n-gram 老章节召回 | Storydex | `manuscriptTail` 只能看尾部，第 3 章的伏笔在第 40 章不可见 |
| S4 | 作者真相 / 读者已知双轨 | Oh Story | 只到 S4 做 StoryPlan 才需要，现在不引入 |
| S4 | 章节约束卡（must / must-not / stop-at） | Oh Story | `Direction` 是一段自由文本，缺硬约束 |
| P2 | 去 AI 味确定性检测（只提示） | Oh Story / Pagekin | 可选，成本低但优先级低 |
| P2 | 生成后一致性审稿（可选双调用） | 参考项目综合 | 每次续写多一次调用，先由评测证明必要性 |

---

## 1. 三个项目的核心机制（只列与我们相关的）

### Pagekin（`EthanYoQ/AI-Novel-Writer`，TypeScript/Electron/SQLite）

- **分级上下文**：L0 始终注入（项目名/类型/进度/大纲前 300 字，约 500 token）→ L1 编辑器感知（当前 Tab 内容前 500 字）→ L2 按需 Tool 读取。见 `src/services/agent/context-builder.ts`。
- **生成失败分类**：把失败分成 `capacity-preflight / budget-exhausted / provider-length / network / storage / cancelled / unknown`，其中 `finish_reason in {length, max_tokens, max_output_tokens}` 单独归为 `provider-length`。见 `src/services/generation/prompt-budget-failure.ts`。
- **预算报告**：每个上下文段有稳定的 section 名和中文标签（知识库/故事前提/角色身份清单/已有章节蓝图/续写请求…），超预算时按段展示占用。
- **单章"写-审-修-定"闭环**，伏笔按章追踪，蓝图逐章下发。

### Storydex（`TensorHub-ORG/Storydex`，Rust + Vue + Python）

- **先检索再生成**：`vendor/coomi-rs/skills/story-project-retrieval/SKILL.md` 的顺序是"先加载活跃约束 → 再最近完整片段 → 再结构化记忆 → 只有需要消歧时才读更老的原文"。
- **零依赖 BM25**：`apps/backend/services/storydex_retrieval.py` 用正则做中文 2/3-gram 分词 + 标准 BM25（k1=1.5, b=0.75），行级 snippet，完全不依赖分词库。
- **带版本的检索索引**：`retrieval_service.py` 用 SQLite FTS5，chunk 3200 字 / 400 重叠，索引状态 `ok/building/stale/error`，snippet 同时给出字符与字节区间并回原文校验。
- **事实记忆是三元组**：`fact_memory_store.py` 的 `ProjectFact{subject, predicate, object, confidence, evidence}`，注入时明确写两句边界："Only the listed facts are hard constraints for this turn / Unlisted facts remain background truth, not active chapter material."

### Oh Story（`zenstory-ai/oh-story-claudecode`，Claude Code/Codex skills）

- **单一权威 + 派生视图**：`追踪/_tracking-state.json` 是唯一可写真相，`上下文.md`（≤12KB、固定 7 栏）、角色状态、伏笔、双时间线全部由它确定性生成；`check` 会重新渲染并比对，防止派生文件被手改。见 `docs/keep-ai-novel-consistent-over-100-chapters.md`。
- **作者真相 / 读者已知两条时间线**：写作时模型能看到"凶手是谁"，同时知道"主角还不知道"，从而不会让角色说漏。
- **逐章增量有硬上限**：单章记录目标 ≤1536 字节、硬上限 3072，只记影响后续章的变化；历史继续增长，但当前状态卡恒定。
- **预算守卫**：`scripts/check-doc-budget.sh` 读取 `doc-budget.json`，枚举所有"可能叠加"的读取组合，取最坏组合对照 35K 硬上限；漏登记真实读取会被测试抓出来。
- **续写字数系统性偏短**：实测"改提示词说清字数"完全无效（三种写法最终/目标都在 0.71–0.75），换更强模型才到 1.07；且明确拒绝"欠字自动补写"（会注水）。
- **评测协议**：`scripts/bench/judge.py` 的 coverage 逐条判情节点 `landed/summarized/missing` 并列出"细纲未授权的新事实"；pairwise 盲评交换 A/B 顺序各评一次，两次一致才算胜负。评委必须与写手不同家族。

---

## 2. 现在做（P0）

### R1 模型输出截断必须可见

**我们的问题**：`internal/llm/client.go` 的 `completionResponse` 和 `streamEvent` 都没有 `finish_reason` 字段（[client.go:450](file:///D:/go/internal/llm/client.go#L450)、[client.go:459](file:///D:/go/internal/llm/client.go#L459)）。模型被长度上限截断时，我们把半截内容当成功返回：

- 续写：用户看到半句话，没有任何提示；
- 分析：`parseJSONObject` 报 `unexpected EOF`，最终提示"分析结果格式异常，请重试"（[manager.go:169](file:///D:/go/internal/analysis/manager.go#L169)），把"被截断"误报成"格式错"。

**实测证据**：测试 DeepSeek 渠道时，首轮 `/api/analyze` 就出现过一次 `unexpected EOF`，重试后正常。这不是理论风险。

**做法**（对齐 Pagekin 的分类，但只做我们需要的子集）：

1. `completionResponse` / `streamEvent` 增加 `finish_reason`，`StreamChat` 返回最终 `finishReason`。
2. `llm` 包新增 `ErrTruncated`；`finish_reason == "length"`（含 `max_tokens`、`max_output_tokens`）时返回该错误。
3. 分析侧把 `ErrTruncated` 单独映射为"模型输出达到长度上限被截断，请缩短章节或调整模型上限"，与"格式异常"区分开。
4. `StreamChat` 正常结束但 `finishReason == "length"` 时，SSE `done` 事件带 `"truncated": true`，前端给出"续写被模型长度上限截断"的提示。

**验收判据**：

- `httptest` 伪造 `finish_reason:"length"` → `CompleteChat` 返回 `ErrTruncated`，不再返回不完整字符串；
- 分析失败消息按错误类型分叉，测试覆盖两种文案；
- SSE `done` 在 `length` 场景含 `truncated:true`，正常场景不含该字段（保持旧响应兼容）。

**成本**：小。不动 prompt 契约，不动存储。

---

### R2 上下文预算按"全段合计"核算 + 守卫测试

**我们的问题**：`ContextChars` 目前只约束正文。`manuscriptWindow = contextChars - memoryBudget`（[prompt.go:99](file:///D:/go/internal/agent/prompt.go#L99)），而 `Prompt.ContextChars` 上报的是**最近正文的 rune 数**（[prompt.go:171](file:///D:/go/internal/agent/prompt.go#L171)）。核心设定、文风、人物卡、世界观、前情提要都不计入。

现有测试断言的也是这个"正文长度"：`prompt.ContextChars > 400` 即失败（[prompt_test.go:164](file:///D:/go/internal/agent/prompt_test.go#L164)），并不能证明整个 prompt 不超预算。

**量级**：角色 12 条 + 世界 10 条 + 摘要 3 条 + 总览 + 文风，**且 `formatCharacter` / `formatWorld` 对字段没有任何逐项截断**（[prompt.go:217](file:///D:/go/internal/agent/prompt.go#L217)）。分析产出的 personality / appearance / relationships 是不可控长度的自由文本，满配很容易到 5–8K 字。用户设 30000 时，真实 prompt 可能是 32–35K。这正好是 Oh Story 那次预算事故的同一类问题："登记漏了真实会读的文件"。

**做法**：

1. `Prompt` 增加 `TotalChars`（`System + User` 的 rune 数）与 `Sections map[string]int`（逐段字数）。
2. `Build` 内部改成"先算固定开销，再分配正文与记忆"，即 `manuscriptWindow = contextChars - memoryBudget - overhead`。
3. 给人物/世界字段加逐项截断（对齐 Pagekin 的 300/150 字上限），并在 `selectCharacters` 里按"字段总字数"而不是条数做预算。
4. 新增守卫测试：`contextChars ∈ {1600, 4000, 12000, 30000}` × 知识库/记忆满配 × 长短章节，断言 `TotalChars <= contextChars`。
5. SSE `meta` 增加 `promptChars` 与分段占用，便于用户理解"为什么正文只有这么点"。

**注意**：这一项会改变 `ContextChars` 的语义（从"正文窗口"变成"总预算"）。这正是 roadmap 决策点 C 待拍板的内容，**需要先确认再动代码**。

**验收判据**：守卫测试在旧实现上必红、新实现上必绿；`/api/continue` 的 `meta.contextChars` 与 `meta.promptChars` 差值等于非正文段开销。

**成本**：小到中。

---

### R3 冻结评测基线（roadmap 决策点 E）

**我们的问题**：I3 的记忆注入目前只有单元测试（检索选择了哪些记录、预算裁剪、降级），没有任何"续写是否真的更好"的证据。V1.0 与 iter2 的结论也都建立在单元测试 + 空库冒烟上。

**借用 Oh Story 的协议**（不搬代码，只搬验收方法）：

1. **冻结用例**：用现有素材造 3 个用例，全部离线可复现——①《无边悔恨》10 章现状；②177K 语料切 10 章（导入后即"长篇"）；③人工构造的"伏笔在第 2 章、当前第 8 章"用例。
2. **coverage 核对**（替代主观打分）：给每个用例写 3–5 条"本次续写必须发生/必须不发生"，让评委逐条判 `landed / summarized / missing`，并单独列出"没有授权却新增的剧情事实"。可数、不靠 1–10 分。
3. **配对盲评**：同一用例的"记忆开 / 记忆关"两版随机 A/B，交换顺序各评一次，两次一致才计胜负。
4. **评委与写手不同家族**：写手是 DeepSeek 就用 GPT-5.6 评，反之亦然（实测两渠道都能跑通，见上一轮渠道结论）。

**验收判据**（I3 是否成立的判据，不是"跑过"）：

- 3 个用例、每个 2 次交换顺序，记忆开在"与已确认事实冲突数"上不劣于关闭，且至少 2 个用例严格更少；
- 不出现"主线事件凭空新增"（unauthorized）次数上升；
- 字数达成率作为观测项记录，不作为通过条件（Oh Story 已证明提示词改不动它）。

**成本**：中（写用例 + 一个跑批脚本）。收益是后续 S2–S6 每一步都有同一把尺子。

---

## 3. 进 S6（长篇能力）

### R4 纯 Go BM25 + 中文 n-gram 老章节召回

**我们的问题**：`manuscriptTail` 只取最近正文（[prompt.go:360](file:///D:/go/internal/agent/prompt.go#L360)）。第 40 章续写时，第 3 章埋的物件、说过的话、用过的招式名都不在上下文里。I3 的结构化记忆只能覆盖"被抽取成记录的事实"，覆盖不了原文措辞与场景细节。

**Storydex 的做法可以直接移植**（`storydex_retrieval.py`）：

- 分词：中文按 2-gram + 3-gram 展开，ASCII 词 ≥2 字符，去重；
- 打分：标准 BM25，k1=1.5，b=0.75，文档长度归一；
- 片段：选命中词最多的一行，取 800 字窗口，锚在第一个命中位置；
- 返回 `doc_id / score / snippet`，可溯源到章节。

**落地方式**（保持零依赖）：

1. 新增 `internal/recall`，输入章节列表 + 查询串，输出 `RecallHit{ChapterID, Score, Snippet, StartRune, EndRune}`。
2. chunk 3200 字、重叠 400，全量在内存现算；200 章约 60 万字，一次查询应在 20ms 内，不需要索引库。
3. 查询串由最近正文末尾 + `Direction` 组成，取 top 3，注入新段"【前文相关片段】"，预算 800 字，从正文窗口扣减。
4. 默认关闭，用配置项或"章节数 ≥ N"触发；关闭时 prompt 与现在逐字相同（golden test 守住）。
5. 召回失败/超时只降级不阻塞，沿用 I3 的 `DisabledReason` 语义。

**验收判据**：在 177K 语料上查"重瞳"能命中 3 个相关场景且 snippet 可读；关闭时 golden prompt 不变；p95 < 20ms；`chapters=200` 的内存与延迟有记录。

**成本**：中（约 300–400 行 + 测试）。**不要**引入 SQLite/FTS，单机 JSON 项目不值得为召回加依赖。

---

## 4. 推迟到 S4（StoryPlan）

### R6 作者真相 / 读者已知双轨

Oh Story 的双时间线是为"大纲先行、正文慢揭"设计的。我们目前是**线性续写**：没写出来的就是没发生，读者已知 ≈ 全部正文，所以现在引入收益很低。

真正需要它的时点是 S4 上线 StoryPlan 之后：计划里会有"第 12 章才揭露某某是凶手"，此时必须能表达"事实已确定但读者尚未看到"。建议那时再加，形态是给 `StoryEvent` 增加：

```go
ReaderKnown    bool   `json:"readerKnown"`          // 读者是否已在正文中看到
RevealedIn     string `json:"revealedIn,omitempty"` // 首次揭露的章节 ID
```

并配一条系统规则："读者未知的事件不得在续写中直接点明，只能埋线索。"

**现在明确不做**：`novel.Cognition` 已经覆盖"角色知道什么"（known/wrong/forgotten/uncertain），这是续写最常用的一条轴。

### R7 章节约束卡（must / must-not / stop-at）

Oh Story 每章开写前先落六项约束：字数区间、必须发生、禁止发生、时间锚点、停在哪里、新开什么债。我们现在的 `Direction` 是一段自由文本，全靠模型自己理解。

这属于 S4 的产品范围（需要 UI 与 StoryPlan 联动），现在做会变成"给一个输入框加五个字段"的伪需求。**推迟，但在 S4 设计 StoryPlan 时把这六项作为字段蓝本。**

---

## 5. 可选（P2）

### R8 去 AI 味确定性检测（只提示，不改写）

Oh Story 有 64 条核心词项 + 句式检测（`banned-words.md`、`check-ai-patterns.js`），Pagekin 有可导入的"去 AI 味" Skill。我们可以做一个 Go 版轻量检测：破折号密度、排比句、总结式结尾、"眼中闪过一丝"类套话、句长方差过低。

**边界**：只报告，不自动改写（Oh Story 自己也强调检测器只是候选，需语义复核）。可以作为续写结果下方的一行提示。**不做**独立的"去味"按钮和改写环路。

### R9 生成后一致性审稿（可选双调用）

用注入的记忆做一次"审稿"：角色是否知道了不该知道的、状态是否与 `active` 状态矛盾、伏笔是否被提前兑现。只提示不改写。

成本是每次续写多一次模型调用，**建议先由 R3 评测证明基线确实存在这类错误，再决定是否默认开启**。

---

## 6. 明确不借鉴

| 机制 | 出处 | 不借鉴的理由 |
|---|---|---|
| Electron/Tauri 桌面壳 + SQLite | Pagekin / Storydex | 我们是单二进制 + 本地 JSON，桌面化会推翻部署形态 |
| 13 个 skill + 多宿主适配（Claude Code/Codex/OpenCode） | Oh Story | 那是插件生态的分发问题，与我们产品无关 |
| 作者偏好记忆（AP/BP 两级事件库） | Oh Story | 需要交互式"记住/忘了"入口，且与"续写"主链路无关 |
| 全文 FTS5 索引 + 索引版本管理 | Storydex | 为单机中小体量引入索引库不划算，R4 的内存 BM25 足够 |
| 拆解仿写 / 扫榜 / 封面 | Pagekin / Oh Story | 属于选题与包装，不在续写主链路 |
| 欠字自动补写 | Oh Story 明确否决 | 实测会注水；改提示词也无效，不如把达成率展示给用户 |
| 本地 Git 控制 / 可审 diff | Storydex | 我们已有 revision + 快照 + ChangeSet 审阅 |

---

## 7. 与 roadmap 的挂接

> 已同步至 [roadmap.md](roadmap.md) §4/§8 与 [iter3-详设.md](iter3-详设.md) §1/§4.1/§10/§13。

- **S1（真实语料端到端）**：R1（I3-6）作为 S1 第 1 项——截断不再伪装成格式错，否则 S1 的失败无法归因。
- **S2（记忆接入续写）**：R2（I3-7）补全量预算计量，R3（I3-15 改写）做冻结用例评测；记忆注入代码已由并行会话落地（`internal/store/memory_retrieve.go`、`novel/memory_context.go`），但还没有 R3 这样的证据。
- **S3（记忆自维护）**：Oh Story 的"单一权威 + 派生视图 + 漂移检查"可以借来做"记忆与正文是否对得上"的巡检，届时再细化。
- **S4（StoryPlan）**：R6（双轨，需 spec 修订）、R7（约束卡字段蓝本）作为增量引入，不阻塞 S4 现有范围。
- **S6（长篇验收）**：R4 是 S6 的核心能力，没有它，100 章级别的续写只能看到尾部；R8/R9 为可选增强。

---

## 8. 待拍板

- **Q1**：R1 + R2 的排期。当前整合口径：R1 进 S1（I3-6），R2 进 S2（I3-7）。若 yj 要求预算先可信再跑 S1，可把 I3-7 前移。
- **Q2**：`ContextChars` 是否正式改为"整个提示词的总预算"（roadmap 决策点 C）？R2 依赖这个决定。
- **Q3**：R3 的评测用例是否就用《无边悔恨》+ 177K 语料 + 一个构造用例？需要你确认"构造用例"允许人工编写。
- **Q4**：R4 的召回是否接受"默认关闭、章节数超过阈值自动开启"的默认值（roadmap 决策点 F）？这会影响老用户所见即所得的上下文长度。
