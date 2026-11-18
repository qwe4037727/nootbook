# 从当前状态到最终目标：阶段路线图

> 目的：把「现在在哪」到「小说续写终态」之间的每一阶段、进入条件、退出判据、明确不做的事一次性列清，作为排序与取舍的唯一基准。
> 依据：[详设.md](../详设.md)（总详设）｜[memory_model_spec.md](../docs/memory_model_spec.md) V1.2 冻结｜[features.md](../docs/features.md) F1–F23｜[acceptance.md](../docs/acceptance.md) U1–U35｜[iter2-详设.md](iter2-详设.md)｜[iter3-详设.md](iter3-%E8%AF%A6%E8%AE%BE.md)。
> 生成：2026-10-23。本文件不含实现承诺，只含阶段与判据；每阶段开工前另出实施级详设。

---

## 1. 最终目标（先定义终点）

**一句话**：导入一本几百万字的书，能持续续写到几十章而不崩人设、不吞伏笔、不失忆；改正文时记忆跟着修正，且每一步都能被审核和回滚。

用户视角的终态清单（逐项可验收，缺一不算到达）：

| # | 终态能力 | 当前 |
|---|---|---|
| G1 | 导入即自动分章、自动分析出知识库 | ✅ 已实现；**2026-11-02 起口径修正为「链路已在真实语料（《无边悔恨》79 章/16.3 万字）上跑到终态（本地桩，`analysis.status=ready` 79/79），质量侧仍无 Key」**——不是笼统的「真实语料未跑通」；另实测分析阶段只带最近 3 条摘要 ⇒ 成本 O(N) 但跨章一致性不靠分析（issues #71/#72） |
| G2 | 正文事实沉淀为可审核的结构化记忆 | 🚧 代码完成；**2026-11-02 真实语料链路终态（桩）：initialize `ready`、extract `done`（11.5 s）→ 79 条 `proposed` 变更集**；提取质量与人工采纳语义仍未在真实模型下验证 |
| G3 | 记忆经确认后**被续写实际消费**（长程一致性可观测提升） | 🚧 代码完成，I3-01~13、16/17 自动化全绿；R3 评测（I3-14/15）待真实模型 |
| G4 | 改/删正文后记忆自我修复并提示冲突，不自动覆盖计划 | 🚧 代码完成，自动化用例全绿；真实模型 E2E 待通道恢复 |
| G5 | 大纲/分卷/近期计划可生成、可确认、可回灌续写 | 🚧 代码完成，自动化 + Mock UI E2E 全绿；真实模型 A/B blocked（issues #34） |
| G6 | 正文草稿 + 记忆变更一次联合确认提交（原子） | 🚧 代码完成，自动化 + Mock UI E2E 全绿；真实模型提取质量 blocked（见 `task/g6-acceptance.md`） |
| G7 | 全量重初始化（换版本/大改后重建记忆） | ❌ 显式拒绝（`400 reinit_not_supported`，spec R13） |
| G8 | 长篇质量回归集与成本/预算可度量、可控 | 🚧 基础设施、人评工具与 R4 召回已交付（`internal/eval` + `cmd/longeval`，盲评 `cmd/evalreview`，合成 40 章/500 万字、真实语料 dry-run、Mock live、成本上限闸、`-race` 全绿、40 章 UI 全流程走查、X/Y 盲评包与聚合闸自测（`-axis memory|recall` 两条对照轴）；R4：`internal/recall` BM25 + 方向加权 + SSE/预览/eval 观测）；真实模型人评、R4 文学质量 A/B、发布报告待做（issues #42/#43/#45/#46/#47/#48/#49）；**2026-11-02 三闸在当前工作区复测成立**（12 停点 `exit 0`、ctx400 越界 → 构建后二进制退 3、ctx1200/1600 贴顶不越界、成本计量 `priced=true`、未定价 + `-max-cost` 拒绝），同时把 dry-run 的两个判读盲区入库 **#68**：**「回归闸绿」只覆盖预算与注入量，不覆盖事实一致性**；dry-run 成本超限只 warning 退 0 |

---

## 2. 当前状态快照（三个维度，诚实口径）

| 维度 | 事实 |
|---|---|
| 代码 | 迭代 1 已发布（V1.0 限定验收通过）；迭代 2/3 工作区 `go build/vet/test ./...` 全绿（8 包，新增 `internal/eval` + `cmd/longeval` + `cmd/evalreview`）；G3/G4/G5/G6 代码完成，G8 度量基础设施与盲评工具已交付，真实模型验证受通道阻断 |
| 数据 | `data/` 内《无边悔恨》10 章已导入；`project.json` `analysis.status=error`、`analyzedCount=0`、message「分析中断，请检查模型设置后重试」（2026-10-23 06:56 UTC）；`knowledge.json` 无实体；**`data/memory/` 目录不存在** |
| 验证 | 迭代 1/2 结论来自单测 + 空库 HTTP 冒烟；迭代 3 增加 G5/G6 Mock 模型 UI E2E（真实 HTTP + 浏览器）。R1 截断可见性与 R2 全量 prompt 预算已由 LLM/分析/agent/server 四层用例覆盖（I3-16/I3-17）。2026-10-24 起，**规模/预算/自愈三类口径已有「真实进程 + 真实 HTTP + 5M 字合成语料（配夹具记忆）」实测**（G1 导入、G3 联合注入与预览-SSE 等价、G4 改稿删章自愈，见 `task/g8-acceptance.md` §4.6–§4.8），夹具只证管线不证提取质量。同日另一轮改用**本地 OpenAI 兼容桩模型**（`task/e2e-stub-chain-acceptance.md`），在真实进程 + 真实 HTTP 上跑通「分析 → 记忆初始化 → 深度提取 → 审阅采纳 → 记忆注入 → 计划生成/激活 → 续写 SSE → 改稿自愈」全链，并首次直接抓到「模型输出被采纳后出现在下一次请求 prompt 里」的闭环证据；代价是这一轮的「提取正确性」由桩保证，只算链路证据。**「真实书 → 分析 → 记忆初始化 → 深度提取 → 审阅采纳 → 续写/草稿确认」仍未在真实模型下跑通**（2026-11-02 补：这一段已在**真实语料**《无边悔恨》79 章上以本地桩跑到终态——G1 `analysis.status=ready` 79/79、G2 initialize `ready` + extract `done` → 79 条 `proposed` 变更集，同时量出 token 规模与官方价 ≈¥1–9；仍属链路/规模证据，不属质量证据，issues #71/#72）；长篇生成质量人评未执行；`-race` 已全绿（2026-10-23）。**读数前置条件（issues #59）**：引用任何注入/记忆读数前先看 `GET /api/memory` 的 `safeState`——eval 记忆夹具缺 `commits.json`，「写一次记忆 + 重启」会 fail-closed 使注入全 0 |

> 结论：项目当前的瓶颈不是功能代码量，而是**真实模型下的端到端从未验证**。链路形状本身已在 2026-10-24 用本地桩模型于真实进程 + 真实 HTTP 上跑通一次（`task/e2e-stub-chain-acceptance.md`），所以剩下的缺口精确地是「模型输出质量」，而不是「管线能不能跑」。任何后续阶段的收益判定都必须建立在 S1 产出之上。

---

## 3. 阶段总览

| 阶段 | 名称 | 核心问题 | 依赖 | 退出判据（证据类型） |
|---|---|---|---|---|
| **S0** | 迭代 2 收口 | 提取/草稿/审核的代码与测试是否配得上「可用」 | 无 | 自动化测试 + 复审报告（代码/测试已达成，待签字） |
| **S1** | 真实语料端到端打通 | 这套记忆到底能不能在真书上产出正确事实 | S0；外部网关可用 | 端到端运行记录 + 提取质量抽检报告 |
| **S2** | 记忆接入续写 | 注入记忆是否真的提升长程一致性 | S1（需真实记忆） | I3-01…14/16/17 用例 + R3 冻结用例盲评 |
| **S3** | 记忆自我维护 | 改正文/删章后记忆会不会失真、会不会误导续写 | S2 | §7 分级用例 + 冲突提示可复现 |
| **S4** | 大纲/计划链路 | 能否「按规划写」而不是顺着手感写（G5） | S2（回灌需上下文预算与消费通道） | F20–F23 验收 + 计划对齐 A/B |
| **S5** | 联合提交与重建 | 改稿工作流原子性（G6/G7） | S3 + S4 | 崩溃点恢复矩阵复测（**矩阵侧 2026-10-24 已复测**）+ reinit 用例（仍空，待 G7 批准） |
| **S6** | 长篇可控性验收与 V2.0 | 几十章连写不崩、成本可控、UI 全流程可用（G1–G8 全绿） | S5 | 长篇回归集 + 全流程 UI 走查 + 发布报告 |

关键路径：**S0 → S1 → S2 → S3 → S5 → S6**；S4 与 S3 可并行（S4 依赖 S2 的上下文通道，不依赖记忆修复）。

```
S0 迭代2收口
 └─ S1 真实语料端到端 ── 阻塞点：上游网关封禁 / 提取质量阈值
      └─ S2 记忆接入续写（收益首次可观测）
           ├─ S3 记忆自我维护 ──┐
           └─ S4 计划链路 ──────┴─ S5 联合提交+重建 ─ S6 长篇验收 / V2.0
```

---

## 4. 各阶段详述

### S0 —— 迭代 2 收口（代码与测试已完成，待复审签字）

- **目标**：把「代码写完」变成「可提交、可信赖的行为」。
- **进入条件**：无（正在进行）。
- **交付**：iter2 复审打回项收口——B1（§10 表 17 项用例落地）、D3 章节溯源结构化、D5 register 查重、D6 Resolution 记实体 ID、D7 事件去重覆盖 pending；补 I2-x 任务 MD 与 `-changes.md`/`-issues.md`。
- **已完成部分**：B1/B2/B3/D1/D2/D3/D4/D5/D6/D7/D8 + EntityHint 原因码（详见 [iter2-acceptance-rework.md](iter2-acceptance-rework.md)）；仅 `w_` 世界条目 register 分类（issues #28）待产品确认。
- **退出判据**：`go test ./... -count=1` 与 `go test -tags acceptance_review ./... -count=2` 双绿且**包含迭代 2 新用例**；复审签字；由 yj 决定提交。**双绿已于 2026-10-23 达成（6 包全 PASS），剩余为复审签字与提交决策。**
- **明确不做**：不扩提取范围、不加新 API、不动 `ContextChars` 语义。
- **风险**：D5 若不修，S1 阶段同一角色记忆会劈成两个 `c_`，直接压低 S2 检索命中率（已修复）。

### S1 —— 真实语料端到端打通（**最高优先级，其余阶段的门票**）

- **目标**：第一次真跑 `导入 → 逐章分析 → 知识库 → 记忆初始化 → 深度提取 → 人工审阅采纳`，并回答「提取出来的事实对不对、值不值得信」。
- **进入条件**：S0 提交；上游模型可用（`F-UPSTREAM-01` Cloudflare 按出口 IP 封禁 → ~~Clash 7897 代理~~ / 网关加白 / 换供应商，三选一由 yj 定。**2026-11-02 修正：第一条已实测失效——`127.0.0.1:7897` 被 `com.vortex.helper.exe` 占用而非 Clash，HTTP `502`、HTTPS CONNECT 与 socks5h 全 `curl exit 35`；且当前拦的是 Cloudflare 边缘 IP 规则（`403` 通用拦截页、无 `1010`），比 10-24 的源站 `503` 更靠前一层。所以实际只剩「加白」与「换出口/换供应商」两条，见 issues #69**）。
- **交付**：
  1. **错误可见性（R1，借鉴 Pagekin 的 truncation 分类）**：`llm` 读取 `finish_reason`，`length` 类截断暴露为 `Truncated`；分析失败文案区分「输出被截断」与「格式异常」；续写 `done` 事件带 `truncated` 标志。**此项先做**——否则 S1 的「跑失败」永远分不清是模型不行还是被截断。
  2. `corpus/无边悔恨.txt` 取前 10–30 章建独立数据目录（不覆盖现有 `data/`）。
  3. 全链手工 + 脚本化运行记录：分析耗时/token 成本、初始化回填条数、逐章提取 ChangeSet 数、采纳/驳回/冲突分布。
  4. **提取质量抽检报告**：人工核对 ≥50 条 confirmed 事实 vs 原文，统计准确率、幻觉率、歧义率；成本与时延基线。
  5. 提示词迭代（`extract_prompt.go`）——质量不达标时的唯一修正面，禁止靠放宽校验「让它通过」。
- **退出判据**：抽检 confirmed 事实准确率达到 yj 设定阈值（建议 ≥90%），且无「未知实体被当既定事实」样本；失败样本至少能区分截断/格式两类原因（上游返回可识别拒绝码时单列）；报告与样本留入 `docs/acceptance-evidence/`。
- **明确不做**：不接续写、不做 StoryPlan、不加前端新板块（现有审阅面板够用）。
- **风险**：阈值定得太松 → S2 注入垃圾并被放大；定得太严 → 记忆几乎为空、S2 无从判定。**这是全项目最容易自欺的一环。**
- **状态追加（2026-10-24：S1 的链路形状已在桩模型下跑通一次，质量仍未跑通）**：用本地 OpenAI 兼容桩（`task/e2e-stub-chain-acceptance.md`、证据 `docs/acceptance-evidence/20261024/e2e-stub-chain/`）把 S1 交付第 3 项的全链在真实进程 + 真实 HTTP 上走了一遍：8 章自撰语料 → `POST /api/analyze`（`status=ready`，8/8）→ `POST /api/memory/initialize`（rev 1，`mappedEvents=8/mappedStates=1`）→ `POST /api/memory/extract`（1 个 ChangeSet、4 个 op）→ `POST /api/memory/changesets/{id}/accept`（rev 2，`events=9/states=2/cognitions=1/threads=1`）。三条对 S1 正式开跑有用的口径：①**深度提取要求 effective apiKey 非空**（`internal/analysis/extract.go` 短路），所以「没有密钥」阻断的是提取这一步本身，而分析/续写只需 `baseUrl`+`model`——桩测时密钥给字面占位串即可，真实密钥始终不入命令与证据；②`baseUrl` 必须自带 `/v1`（客户端只追加 `/chat/completions`），且 `stream:true` 请求若被 `application/json` 应答会自动回落非流式解析，一个万能 JSON 体可同时满足章节分析/总览/提取/计划四套解析器（Go 侧忽略未知键）；③Windows 上 8123/8124 落在系统保留端口段，`bind` 直接 `EACCES`，桩测请换 8097/8098。**S1 退出判据（真实书 + 人工抽检 ≥50 条准确率/幻觉率）未变、未达成**：桩对每个请求返回同一份 JSON，提取质量与幻觉率零信息量。

### S2 —— 记忆接入续写（G3）

- **目标**：让记忆第一次真正影响续写产出。
- **进入条件**：S1 达标并留有可用记忆内容的真实数据目录。
- **交付**：按 [iter3-详设.md](iter3-%E8%AF%A6%E8%AE%BE.md) I3-1…I3-7 与 §13 增补（I3-6/R1 已于 2026-10-23 完成）——
  1. I3-1…I3-5：检索层、`ContextChars` 统一预算（1/4、硬顶 3000、≤1600 退化）、`agent.Build` 注入与 systemPrompt 优先级增补、`POST /api/continue/preview` 只读预览、前端注入开关与召回展示（**代码已落地，I3-01…13 自动化全绿**）。
  2. **R2 全量预算计量（已落地）**：`Prompt` 输出 `TotalChars` 与分段 `Sections`（知识库/记忆/计划/摘要/正文尾窗），`ContextChars` 仍是正文预算但整体 user prompt 受其约束；超预算按冻结顺位收缩（角色字段→世界别名→低分角色/设定→摘要→记忆 P1→正文至 `ctx/2`），四档守卫用例 `ctx ∈ {1600,4000,12000,30000}` 全绿；`OverBudget` 仅在收缩到底仍超限时置真（`TestBuildFullBudgetInvariants/over_budget`）。
  3. **R3 冻结用例评测协议**：抽 3 个停点做冻结用例（landed/summarized/missing 三态覆盖 + 未授权事实检出），开/关记忆各生成一篇，做**交换顺序的盲评 pairwise**（自恋偏差校正），替代原来「I3-15 一次开/关人评」。
- **退出判据**：I3-01…13、I3-16/I3-17 自动化全绿（零回归 golden、预算不变式、`needsReview` 红线、确定性、全量预算守卫）+ **R3 协议下开/关记忆在至少 2/3 停点胜出或打平且无「未授权事实」增加**；I3-14/15 需真实模型；对照正文与评分表留入 `docs/acceptance-evidence/`。
- **明确不做**：不做语义/向量检索、不做计划回灌、不动提交协议；R4 长篇召回延到 S6。
- **风险**：A/B 无收益 → 判定「提取内容不够好」而非「检索没做好」，回 S1 而非继续加功能；`ContextChars` 语义变更影响既有观感（须 UI 说明 + issues 台账）；全量计量会让知识/摘要/记忆在极端配置下让位于正文（issues #40），上线后如出现 `promptOverBudget=true` 需调低 `ContextChars` 或精简知识库。
- **状态（2026-10-23）**：I3-1…I3-7 代码完成，I3-01…13、16/17 自动化全绿；I3-14/15（真实模型优先级人评与开/关质量对照）仍受通道阻断（issues #30）。R1 已同步覆盖分析/总览/草稿提取/计划生成四个调用点与前端提示（issues #41）。
- **状态追加（2026-10-24，规模侧实测）**：把 confirmed 记忆夹具放进 500 章 / 5,020,294 rune 的隔离数据目录并让真实 HTTP 进程读取，`POST /api/continue/preview` 在同一次请求里同时给出记忆 242/3000 与召回 800/800（命中埋点第 3、12 章），`ctx=4000/12000/30000` 联合非零、`ctx=1600` 两档同时归零，同输入逐字可重放（证据 `docs/acceptance-evidence/20261024/g3-5m-joint/`、`task/g8-acceptance.md` §4.7）。**这不改变 G3 未达成的判定**：夹具记忆绕过了 `initialize`/extract/审阅（issues #56），且 R3 盲评所需的真实模型对照仍被通道与密钥阻断。**对 R3 的硬约束（issues #54）**：记忆预算是「预留即扣」，`useMemory=false` 并不把 3000 字还给正文（实测两臂 `manuscriptChars` 均 8179），所以 memory-off 对照臂是「prompt 少 341 字、正文一样长」而不是「读到更多正文」；评审口径若不写明这点，会把「注入变长」与「上下文变多」两种收益混在一起。**观测口径红利（issues #57 已实测为等价）**：同一副本目录、同一 `ctx`、同一方向句下，`/api/continue/preview` 与 `/api/continue` 的 SSE `meta` 在 3 变体 × 15 字段共 45 项对比中 0 处不一致（`docs/acceptance-evidence/20261024/g3-5m-joint/preview-vs-sse.json`），因此预算/召回这类**注入读数**可在无密钥时用预览度量，不必等真实通道；只有「模型输出质量」一侧仍必须有 Key。
- **状态追加（2026-10-24：采纳路径上的消费闭环，非夹具）**：桩模型那一轮把「记忆被续写实际消费」第一次观测到**闭环**而非两端：`/api/continue` 的 SSE `meta` 给出 `memoryUsedChars=105`、`planAligned=true`、`planCount=1`、`promptSections{memories:105, plans:108, manuscript:361, characters:59, overview:45, world:32}`、`promptTotalChars=1438`、`skippedReview=1`、`truncated=false`，而落库的那次真实请求体里能逐字搜到「上一轮模型返回、经 ChangeSet 采纳」的事件摘要/状态值/计划目标原文（`continue-user-prompt.txt` 留档）。issues #57 的「预览可代表真实续写侧注入读数」由推断升级为实测，且覆盖的是采纳路径；`safeState` 全程为假，读数无需按 #59 折扣。**两条新限制**：①issues #60——经深度提取采纳的 `threads` 始终 0 注入（`counts.threads=1` 而 `recalledByKind.threads=0`、prompt 里搜不到伏笔标题），而 5M 夹具轮的 `thread_jade` 是被注入的 7 条之一；**根因已于同日定位（见 §10 of `task/e2e-stub-chain-acceptance.md` 与 `docs/acceptance-evidence/20261024/issue60-thread-gate/`）**：`add_thread` 的记录从不填 `involvedCharacters`/`question`，而检索的相关性门槛只认这三条路（[internal/store/memory_retrieve.go:182](../internal/store/memory_retrieve.go#L182)），概括性标题又不会在正文里逐字出现 → 提取通道的伏笔**结构上不可能被注入**，夹具能进是因为手写带了角色 ID。它意味着「吞伏笔」这类失败模式目前不可能被记忆系统改善，正是 G3 的立项初衷之一；修法三选一待拍板。②issues #61——全部候选都因 `needsReview` 被停投时，预览/SSE 只有 `memoryEnabled=false` 而没有 `memoryDisabledReason`（与 #55/#58 同族），人评里最容易被误读成「记忆失效」。**判定不变**：R3 的文学质量盲评仍要真实模型，S2 退出判据未达成。

### S3 —— 记忆自我维护（G4）

- **目标**：改稿不再让记忆失真。
- **进入条件**：S2 已上线消费通道（否则修好了也没人用）。
- **交付**：详设 §7 正文变化分级（局部改 / 删章 / 重排 / 换书 → 影响面与处置）；`evidence_stale` 复核全量联动与批量入口；U23 父计划取消传播（若 S4 未完则只交付依赖图与标记）；历史依赖修复；`cognitions` 产出方（人物「错误认知」是续写最容易崩的点）。
- **退出判据**：构造「改中段一章 → 下游受影响记录被标记 + 冲突提示，绝不被自动覆盖」的可复现用例；`reconcile` no-op 不递增仍成立；S2 检索红线在新标记状态下不误注入。
- **明确不做**：不做自动重写历史、不做无人审核的批量失效。
- **状态（2026-10-23）**：代码完成、自动化用例全绿（见 `task/g4-acceptance.md`）；真实模型/真实语料 E2E 待模型通道恢复后复核 `cognitions` 抽取质量。逐条 needsReview 复核闭环与 U23 取消传播不在本期范围（issues #33）。
- **状态追加（2026-10-24：5M 字规模实测，见 `task/g8-acceptance.md` §4.8 与 `docs/acceptance-evidence/20261024/g4-5m-heal/`）**：退出判据里「改中段一章 → 受影响记录被标记 + 绝不被自动覆盖」第一次在**真实进程 + 真实 HTTP + 500 章语料**上量到，不再只有单测结论。三条可复用口径：①三种失效原因（`evidence_stale`/`quote_mismatch`/`chapter_deleted`）各自真实触发并沿 事件→伏笔→计划 传播为 `dependency_stale`；②**注入侧后果单调可测**——`memorySection` 242→212→195→102 字，每一步恰好少「刚被标记的那条」，同时计划被停投（`planCount 1→0`）而 `status/title/objective` 逐字未变，即「停投不改稿」是实测行为而非设计意图；③规模成本口径：全量复核 352 ms、单章复核 4 ms、删章 307 ms，`SaveChapter` 内联复核因此不构成续写链路瓶颈。
- **两条新发现（本轮只记录，未改代码）**：issues #58——计划段被停投时接口层没有停用原因字段，`planCount` 突然变 0 只能另跑 `GET /api/plans` 交叉比对 `needsReview` 才能解释；这条影响 S4/S5 的验收判读（记忆档有 `skippedReview`，计划档没有对等字段）。issues #59——`cmd/longeval` 记忆夹具缺 `commits.json`，夹具目录「写一次记忆 + 重启」必然 fail-closed 进 `memory_safe_state`，因此**验收协议红线：引用任何注入读数前先读 `safeState`；「注入突然全 0」不得读成「记忆失效」**（G6 联合提交与跨会话人评最容易踩到）。
- **状态追加（2026-10-24：自愈发生在「真实链路产出的记忆」上，见 `task/e2e-stub-chain-acceptance.md`）**：5M 夹具轮标的是人工写死的记录，桩模型这轮标的是**由模型产出、经 ChangeSet 正式采纳**的记录——改第 3 章（删掉三处埋点句）后 `evt_/state_/cog_/thread_` 四条同时置 `evidence_stale`（服务端 `locateQuote` 写入的 `chapterRevision=1` vs 章节现 `rev=2`），`memoryRevision` 4→5 且只多一个 `reconcile` 提交（提交链全程 `initialize → formal → formal → manual_fix → reconcile`），预览 `memorySection` 105 字→空、`used` 归 0、`skippedReview` 1→4，四条记录的 `status` 与文本逐字未变（不自动覆盖仍是实测行为）。映射回填的那条状态另有原因码 `backfill_unverified`。
- **一条此前没有的判据（本轮对照得出）**：G4 的传播**只发生在计划确实声明了依赖边时**。桩轮的 `plan_near` 无 `dependsOnThreads`，改章后 `planCount` 仍为 1；5M 夹具轮人工写了 `dependsOnThreads=[thread_jade]`，删被依赖章 → `dependency_stale` → 计划停投。语义正确，但说明「计划对齐」通道对正文改动天然不敏感，G5 想让计划随改稿复核只能靠显式依赖边（配套观测缺口是 issues #58：计划档没有停用原因字段）。
- **接口一致性新发现（issues #62）**：`GET /api/memory/characters/{id}` 的 `events[]` 不透出 `needsReview`/`reviewReason`/`sourceRefs`（读到 `false`/空数组），而 `memory/events.json` 里同一条记录是 `needsReview=true, reviewReason=evidence_stale`；`states[]`/`cognitions[]` 正常透出。即「审阅视图看不到事件级失效」，直接影响 G4 人工复核入口能否数对。另：`skippedReview` 的语义是「检索候选里被待复核挡掉的条数」，不是「库里待复核总数」——引用前不要当后者用。
- **仍不构成 S3/G4 完成**：收益判定（真实语料 + 真实模型下「改中段一章 → 续写是否避开失真记忆」）仍需模型通道；本轮记忆与证据链为人工夹具（承接 issues #56 的来源声明要求）。

### S4 —— 大纲/计划链路（G5，F20–F23）

- **目标**：可控走向——立意/分卷/结局先规划，续写按 active 计划推进。
- **进入条件**：S2 的上下文预算与渲染通道已存在（否则计划仍然进不了 prompt）。
- **交付**：`POST /api/plans/generate`（global+volume，`proposed`）、`GET /api/plans` 计划树、确认激活（同作用域旧 active 自动 superseded，补 U22 写入口）、取消传播（补 U23）、续写「对齐计划」开关。
  - **R6 作者真相 / 读者已知双轨（S4 增量，未实现）**：计划上线后需要表达「事实已确定、读者尚未看到」。形态是给 `StoryEvent` 增加 `ReaderKnown`/`RevealedIn`（**需 spec 修订并经 yj 批准**），配系统规则「读者未知的事件不得直接点明，只能埋线索」。
  - **R7 章节约束卡（S4 增量，未实现）**：把 oh-story 的六项约束（字数区间、必须发生、禁止发生、时间锚点、停在哪里、新开什么债）作为 `StoryPlan` 字段蓝本与 UI 联动；不做自动生成细纲。
- **退出判据**：U21/U22/U23 从 ◐/➖ 升为 ✅；计划对齐 A/B 可观测（不跑偏、不提前收束）；正文与计划冲突时按 §6.2 优先级——**计划永不覆盖已确认事实**。
- **明确不做**：不做自动改计划、不做分章细纲（P2 扩展）。
- **状态（2026-10-23）**：代码完成、自动化用例全绿、Mock 模型 UI 端到端跑通（见 `task/g5-acceptance.md`）；U21/U22/U23 已升 ✅，§6.2 事实优先级由渲染过滤 + systemPrompt 3.2 保证；真实模型「计划对齐 A/B」待模型通道恢复后执行（issues #34）。

### S5 —— 联合提交与重建（G6/G7）

- **目标**：改稿工作流原子化——一次确认同时落「新正文 + 由它派生的记忆变更」。
- **进入条件**：S3 的记忆修复语义已定义（联合提交要处理正文失效）。
- **交付**：正文草稿态（`draft_`，详设 §4.1）+ `POST /api/works/:workId/drafts`（§8.2）；正文 + ChangeSet 单次提交（复用 `commitMemoryLocked` 扩展 payload 域，不新增锁序分支）；全量重初始化 `reinitialize`（撤销 spec R13 的 400 拒绝，需修订冻结条款并经 yj 批准）。
- **退出判据**：六崩溃点恢复矩阵对新 payload 全覆盖复测；重建前后哈希/链校验证据；导入隔离与在途任务失效不回退。
- **明确不做**：不做多作品并行（当前恒 `work_001`）。
- **状态（2026-10-23）**：G6 代码完成、自动化用例全绿、Mock 模型 UI 端到端跑通（见 `task/g6-acceptance.md`）；真实模型下的提取质量与人工核对体验待通道恢复（issues #37）。G7 仍按 spec R13 拒绝，需 yj 批准修订冻结条款后实施（issues #38）。
- **S5 开工前置项（2026-10-24 新增，issues #59；同日据实测更正）**：联合提交必然「写记忆 + 进程重启」，而 `cmd/longeval` 的记忆夹具缺 `commits.json`，该组合会让提交链校验 fail-closed（`safeState=true`、注入全 0）。**恢复矩阵不依赖先修夹具**——它已在「真实管线跑出来的自洽目录」上跑完（见下方状态追加与 `task/g6-acceptance.md` §5.6）；但**只要用 `cmd/longeval` 夹具目录做起重/恢复类验收，仍必须先修 #59**。协议红线不变：读任何注入读数前先看 `safeState`。
- **状态追加（2026-10-24：真实 HTTP 上的联合提交第一次被观测为「一个提交」，见 `task/g6-acceptance.md` §5.5、`task/e2e-stub-chain-acceptance.md` §8）**：6 章自撰语料 + 本地桩模型 + 独立二进制/端口/空目录，`POST /api/drafts` → `/extract`（3 个 op，引文由服务端定位）→ `/confirm {draftRevision, expectedMemoryRevision}` 得 `commit_93a191ad13bbb25cfe`，**`commitCountDelta=1`** 且单个 payload 覆盖正文 + `project.json` + 五类记忆 + `changesets.json` + `drafts.json`；`memoryRevision 3→4`、`events 7→8`、`cognitions 1→2`，`proposed` 列表清空。闭环一侧：紧随的 `/api/continue` meta `memoryUsedChars 105→137`，桩侧留档 prompt 同含草稿派生事件与新章正文。失败路径实测：`revision_conflict`/`draft_conflict`/`draft_managed_changeset`（accept 与 rebase）/`draft_terminal` 均拒绝且**零部分副作用**；`reject` 零副作用；G6 提交进来的记录同样会被 G4 自愈标 `evidence_stale`；G7 闸门 `400 reinit_not_supported` 未被绕开。**判定不变**：桩返回固定 JSON，`conflicts=0` 不证明真实模型不产冲突，G6 仍不宣称已交付。（该轮记为缺口的「六崩溃点恢复矩阵真实进程注入版」已于同日补齐，见下一条。）
- **状态追加（2026-10-24：S5 退出判据里「六崩溃点恢复矩阵」一侧已由真实进程证据补齐；重建一侧仍空）**：同一个桩环境里把 G6 联合提交做成提交链**链尾**（`commit_24644d4a0f4fb5dba5`，`formal 8→9`，payload 8 个目标 = 正文 + `project.json` + 五类记忆 + `drafts`/`changesets`），再用 `taskkill /T /F` 硬杀 + 对磁盘改写状态造出 spec §11.3 的六个停点，逐个重启收敛：①prepared 未落盘 → 不被重放，等价于「这次提交从未发生」；②③④ → 重放补齐，**8 个目标与不崩溃对照 0 处差异**（③ 故意留 3 个目标在提交后状态、④ 故意让文件全在提交后，都不产生第三种状态），④ 的新事件在 `events.json` 里恰好 1 次（幂等重放不双重生效）；⑤committed 后 meta 未更新 → **只对齐 meta 不重放文件**（正文里的 8 字探针标记仍在、`manuscriptChars` 463→471 恰为标记本身），meta 对齐到**链尾** 10 而非 +1；⑥一致态重启与对照同形。附带：链尾 `aborted` 补做回滚（新章文件被删、新事件消失、不计链尾）；链断/分叉 → `safeState=true` + `commit_chain_broken`，写侧 500 `memory_safe_state` 而 `GET /api/project` 与章节读仍 200；恢复后 HTTP 重放 `confirm` 返回**同一 `commitId`**、提交数不变。**本轮最有价值的一条**：原子性第一次落在续写侧——②③④⑥ 恢复后的注入与不崩溃对照**逐字相等**（`memoryUsedChars 182`、`recalledByKind events=3/cognitions=3/states=1`），①/aborted 与「从未发生」相等（145，−37 字/召回事件 −1）。方法学三条（复跑必须知道）：基准是「崩溃点目录 + 一次对照重启」，因为联合提交后首次启动必做一轮 `reconcile`（`storyOrder` 重算，4 轮重启取证只有首轮抖动）；文件比对必须按 ISO 时间戳归一化；`commits.json`/`meta.json` 只可做结构断言。**新发现两条**：issues #66（committed 之后目标文件被外部改坏无检测，「正文新、记忆旧」的分裂态静默——非崩溃可达，属设计边界的可观测空白）、issues #67（**联合提交的新事件在同会话内 `storyOrder=0`、重启后才变对，于是同一目录同一方向的注入读数重启前后是 177 vs 182 字**；已升为跨会话验收的前置动作：先重启一次再取读数）。证据 `docs/acceptance-evidence/20261024/e2e-g6-crash-matrix/`、判读 `task/g6-acceptance.md` §5.6、`task/e2e-stub-chain-acceptance.md` §9。**S5 退出判据仍有一项空**：`reinitialize` 重建前后哈希/链校验证据（G7 需 yj 批准修订 spec R13 后才能实施）；G6 质量一侧（真实模型提取、人工核对体验）仍 blocked。本轮零产品代码改动、`data/` 零改动。

### S6 —— 长篇可控性验收与 V2.0（G8）

- **目标**：把「功能齐全」变成「写长篇真能用」。
- **交付**：多作品长篇回归集（含 500 万字级导入）；连写 30–50 章的人评基准（崩人设/吞伏笔/失忆计数）；成本与预算调优（`ContextChars` 分档建议）；`CGO_ENABLED=1 go test -race ./...`（装 mingw-w64，解 issues #4）；UI 全流程走查；盲评与聚合工具（X/Y 盲评包、三态覆盖、未授权事实、通过闸）；发布报告。
  - **R4 长篇召回（借鉴 Storydex 的 BM25 检索，2026-10-24 已交付）**：零依赖实现 BM25 + CJK 2/3-gram 分词，用于超长本书的「按需检索历史章节/远期记忆」；默认关闭，≥20 章自动启用，检索段独立小预算（800 字，实际 `min(800, ctx/4)`，ctx≤1600 或与正文硬下限冲突时为 0）。在 S2 的预算框架内运行（冻结顺位新增「召回」一档），不自建拼接；续写方向按 IDF×3 加权，避免最近正文的表层词淹没旧章节关键名词（issues #47）。实现 `internal/recall` + `internal/agent` 第 8 段，观测与 A/B 见 SSE meta、`POST /api/continue/preview`、eval 停点 `recall` 指标（不存在 `/api/memory/preview` 路由，2026-10-24 校正，issues #52）。前端（issues #46 ②）已交付：续写条「前文召回」三档开关 + 预览面板召回段分组与计量，2026-10-24 浏览器实测走查（`docs/acceptance-evidence/20261024/r4-ui/`、`task/g8-acceptance.md` §4.5），走查只到 SSE `meta`，不含真实模型续写。**同日修复召回质量（issues #50）**：真实语料曾量到「埋点→召回」只有 19%（复审口径 5%–13%）却每档填满 800 字预算，根因是 tf≡1 的存在性打分、几百个 n-gram 全部相加、无注入闸、且「按 df 罕见」把 2 字虚词残片当线索；改为真实 tf 饱和 + 每块取 `ScoreTermLimit=3` + 章节频罕见闸 + `MinClueRunes=3` 长度—唯一性阶梯后，同一探针复测 3 字线索 19%→96%、2 字线索 4%→42%、无线索方向零注入并归还预算（证据 `docs/acceptance-evidence/20261024/r4-recall-quality/`）。判据同日入库为 `TestSearchPlantedClueRateOnSyntheticCorpus`（合成 60 章：命中率 ≥0.90、每条带线索方向过闸章数 == 1、零线索零注入、同输入逐条一致）；反向验证显示只监控命中率看不见闸门放宽，hits 数量才是哨兵。
  - **R8/R9（可选）**：R8 确定性「AI 味」lint（破折号密度、排比、总结式结尾、套话、句长方差）**只提示不改写**；R9 生成后一致性审稿（用记忆检查「知道了不该知道的/状态矛盾/伏笔提前兑现」），每次续写多一次模型调用，**先由 R3 证明基线存在此类错误再决定是否默认开启**。两者都不进生成路径的决策链。
- **退出判据**：G1–G8 逐项 ✅，且每项有 `docs/acceptance-evidence/` 内的可重放证据；R4 开启前后长篇召回命中率与正文一致性人评可量化。
- **状态（2026-10-24）**：G8 基础设施已交付——`internal/eval`（用例 schema、语料准备、prompt/记忆/成本度量、dry-run + live）与 `cmd/longeval` CLI；盲评工具 `internal/eval/review.go` + `cmd/evalreview`（按 seed 分配 X/Y、packet/key 分离、三态覆盖与未授权事实聚合、失败退出码 4）；证据为 40 章合成、500 章/502 万字合成、真实语料《无边悔恨》三类 dry-run 报告，Mock live 覆盖流式输出与截断，40 章 UI 全流程走查覆盖记忆预览→续写→提取→联合提交→提交后召回（`docs/acceptance-evidence/20261023/g8-ui/`），盲评包与聚合闸用 mock live 报告自测（`docs/acceptance-evidence/20261023/g8-review/`，评分为合成数据）。**G8 未完成**：真实模型 30–50 章人评、R4 开/关的文学质量 A/B、发布报告仍待做（issues #43/#45/#46）。R4 召回本体已于 2026-10-24 交付（合成 40 章三档均召回埋点第 3 章，预算与注入量可观测；issues #46/#47/#48，证据 `docs/acceptance-evidence/20261024/`）。盲评工具同日扩到两条对照轴：`cmd/evalreview -axis memory|recall`，召回轴要求「开侧真注入、闭侧未注入、两侧记忆设置一致」，聚合判定文案随轴变化，闸值与退出码不变（issues #49，合成夹具自测见 `docs/acceptance-evidence/20261024/r4-review/`）。R4 前端同日交付并做了浏览器走查（40/10/2 章三档开关与召回段展示、未注入反馈，`docs/acceptance-evidence/20261024/r4-ui/`；因无模型通道，走查止于 SSE `meta`，草稿正文仍报错，不作真实续写证据）。`-race` 已全绿（2026-10-23 与 2026-10-24 复跑，R4 前端收尾当日第三次复跑，召回质量修复收尾第四次复跑，见 `docs/acceptance-evidence/20261023/g8-race.txt`、`docs/acceptance-evidence/20261024/r4-race.txt`；issues #4「本机无 C 编译器」据此结案），live 成本闸（`-max-cost`）与预算闸（`-strict-budget`）已落地并有用例，`ContextChars` 分档建议已产出（默认推荐 12000，并在 2026-10-24 用真实 HTTP + 5M + 夹具记忆补出「记忆与召回两条链路同时可用」的下限档 **ctx≥4000**，1600 档两档同时归零，见 `task/g8-acceptance.md` §4.1 补表与 §4.7）。S6 退出判据里的「R4 召回命中率」一侧已由真实语料度量支撑（issues #50），剩下的「正文一致性人评」一侧仍卡在模型通道与密钥（决策 A/F、issues #43）。度量口径边界见 issues #42 与 `task/g8-acceptance.md`。**2026-10-24 追加（G1 规模口径，合成语料 + 真实 HTTP）**：496.6 万 rune / 500 章经 `POST /api/import` 导入并自动分章（20.3 s、目录 15 MB / 514 文件、覆盖导入不留孤儿文件），超 500 万字上限的请求按预期 `400` 且不损坏既有作品；`POST /api/continue/preview` 在该规模下命中埋点第 7 章（预算 800、实注入 103 字、正文窗口 9000→8200、四档 `overBudget` 均为 false），时延 1.15–1.53 s vs 关召回 0.38–0.44 s；模糊方向强制开召回仍花 1.28–1.34 s 却零注入——**时间成本在建索引，闸门只省窗口**（issues #46②/#53）。证据 `docs/acceptance-evidence/20261024/g1-5m-import/`。这不是 G1 结案：分析与深度提取仍需真实模型（`analysis=error`「分析中断，请检查模型设置后重试」，`memory_absent`）。**同日补齐记忆侧规模口径**：同一 5M 数据目录配上 confirmed 记忆夹具后，真实 HTTP 预览在同一次请求里同时注入记忆 242/3000 与召回 800/800（埋点第 3、12 章），`ctx=1600` 两档同时归零、4000/12000/30000 联合非零、同输入逐字可重放，记忆检索在 5M 上几乎不增加时延（429 ms vs 记忆+召回 1325 ms）；同时量出「记忆预算预留即扣」——实际只注 242 字却仍扣满 3000、且 `useMemory=false` 不还窗口（issues #54/#55/#56，证据 `docs/acceptance-evidence/20261024/g3-5m-joint/`、`task/g8-acceptance.md` §4.7）。夹具记忆绕过提取与审阅，故此项只证管线与计量，不算 G2/G3 达成。**同日再补齐自愈侧规模口径（G4/S3）**：同一 5M 目录（人工补写章节级 `sourceRefs` 与一条 事件→伏笔→计划 依赖链）经真实 HTTP 改第 12 章 / 全量复核 / 删第 3 章，三种失效原因各真实触发（`evidence_stale`、`quote_mismatch`——只有全量扫才抓得到、`chapter_deleted`）并传播为 `dependency_stale`，`memorySection` 单调收缩 242→212→195→102 字、注入记录 7→6→5→2 条，计划被停投（`planCount 1→0`）而 `status/title/objective` 逐字未变，审计为 3 次 +1 的 `reconcile` 提交 + 最小写集；复核成本 5M 上全量 352 ms / 单章 4 ms、删章 307 ms；G7 闸门在改动后的目录上仍返回 `400 reinit_not_supported` 且零落盘（证据 `docs/acceptance-evidence/20261024/g4-5m-heal/`、`task/g8-acceptance.md` §4.8）。两项新发现入库 issues #58（计划段停投无停用原因，接口层无法区分「无激活计划 / 被取消 / 因证据失效停投」）与 #59（**eval 记忆夹具缺 `commits.json`，「写一次记忆 + 重启进程」必然 fail-closed 进 `memory_safe_state` 使注入全 0**）；后者已升为验收协议红线：**读任何注入读数前先看 `safeState`**，S5/S6 的联合提交与跨会话人评最容易踩到。
  - **2026-11-02 三闸复测（当前工作区、无模型 Key、零代码改动）**：`go build/vet/test ./...` 先跑绿（9 包含测试的包全 `ok`），再用构建后的 `output/longeval-g8.exe` 重放——12 停点联合复测 `exit 0`（`overBudgetStops=0`、`maxUserBudgetPct=92.325`、记忆 6 停点各 242 字/7 条、召回 3 停点各 779/800 字命中第 3 章）；预算硬闸形状做实（ctx 1200/1600 贴顶 95.25%–98.625% 仍 `exit 0`，ctx 400 收缩到底 `543>400` → `-strict-budget` 下**二进制退出码 3**）；成本计量 `priced=true`、2 停点合计 0.066183 USD；未定价 + `-max-cost` 直接拒绝。**新入库 issues #68 两条判读红线**：①dry-run 的 `mustLand/mustNot` 恒为 `unjudged` 且报告里没有任何文本可判（`prompt.sections` 只给字符数、`memory.records` 只给条数）→ **「合成回归闸绿」只等于预算与注入量符合预期，不等于事实一致性通过**；②dry-run 下预计费用超 `-max-cost` 只发 warning、退出码 0，硬失败只在 live 分支；另附 CI 陷阱——`go run` 把子进程 2/3/4 压成 1，必须先 `go build` 再跑二进制。免 Key 的最小补法（对 `prompt.User` 做子串判定 + 报告带段文本）会改到被 `internal/eval/runner_test.go:175/:376` 钉住的行为、且该目录属未提交批次，待 yj 拍板。证据 `docs/acceptance-evidence/20261024/g8-gate-recheck/`、`task/g8-acceptance.md` §4.9。

---

## 5. 需要 yj 决策的分叉点（现在不用答，开工到对应阶段前必须答）

| # | 决策 | 我的推荐 | 不决定的后果 |
|---|---|---|---|
| A | S1 走哪条模型通道（代理 / 加白 / 换供应商） | ~~Clash 7897 代理，最小改动~~ → **2026-11-02 更新：7897 实测不是 Clash（被 `com.vortex.helper.exe` 占用），该推荐作废；实际只剩「加白 / 换出口 / 换 DeepSeek 官方」，其中只有换 DeepSeek 不需要任何人配合、只差一把 key（issues #69/#70）。且实测成本已不构成约束：整本 G1+G2 输入 525,732 tokens，官方价 ≈¥1–9（issue #72）→ 按质量与可无人值守来选，不必为省钱选弱通道** | S1 无法启动，S2 之后全部悬空 |
| B | S1 提取质量阈值 | confirmed ≥90%、幻觉 0 容忍（未知实体不得成事实） | 阈值过松 → S2 放大错误 |
| C | 是否接受 `ContextChars` 语义从「正文窗口」变「总预算」 | 接受，配 UI 文案 + issues 记录；R2 落地后进一步变为「整个 prompt 的总预算」 | 记忆段只能无限追加，违反详设 §6.1；预算数字继续偏离实际 |
| D | S4 与 S3 谁先 | 先 S3（改稿不失忆）再 S4（有规划） | 按错方向投入，用户主诉未被解决 |
| E | 是否为 S1/S2 建「评估集」资产（样本 + 评分表） | 建，按 R3 的冻结用例协议落库：3 停点 × 三态覆盖表 + 交换顺序盲评模板 | 每轮验收重新手工核对一遍，且结论不可比 |
| F | 长篇召回（R4）默认开关与预算 | 默认关，≥20 章自动启用，独立 800 字预算，不与记忆段合并 | 长书后期「近期窗口」不够用，人或模型都看不到旧伏笔 |

---

## 6. 跨阶段风险台账

| 风险 | 触发阶段 | 缓解 |
|---|---|---|
| 上游网关按出口 IP 封禁（F-UPSTREAM-01） | S1、S2、S4、S6 | ~~代理/加白~~；重试已备（#22）；预览接口不调模型可绕开。**2026-11-02 复查（issues #69，证据 `docs/acceptance-evidence/20261102/channel-recheck/`）：阻断层级已从源站 `503`（10-24 账号池被 1010 停用）前移为 Cloudflare 边缘 `403` 通用拦截页（本机出口仍是 `198.51.100.10`（已脱敏），应答体零次 `1010`、换 Chrome UA 与走 `http://` 一样被拦）——本机出网正常（`gstatic` 204），是目标侧规则。且「Clash 7897 换出口」这条缓解路径实测失效：7897 被 `com.vortex.helper.exe` 占用，HTTP `502`、CONNECT/socks5h 全 `exit 35`。剩余解法只有加白 / 换机器出口 / 换 DeepSeek；新判读红线：说「网关挂了」要区分 CF 边缘 `403` 与源站 `503/404`，处置方不同。**同轮可达性盘点（#70）：只有这一个域名被拦——`api.deepseek.com` 从本机 `401` 可达（`t≈0.34s`、解析到国内 IP），`wawapii.com`/`api-c.reqtoken.com` 也 `401` 可达，而平台机 `192.0.2.60` 今天 `curl err=7` 连不上（无内网路由，10-24 能 SSH 说明当时有）→ 三条解法里只有「换 DeepSeek 主通道」不需要别人配合、只差一把 key；但 `401` 只证明可达，不证明模型清单/配额/分组路由（`wawapii` 按出口 IP 分组这条仍然有效）** |
| 输出被截断却被当成「成功/格式错误」（R1） | S1、S2、S4、S6 | `finish_reason` 透出 + 失败原因分类；截断样本单独统计 |
| 逐章分析的记忆段窗口只有最近 3 条摘要：成本是 O(N) 但跨章一致性不靠分析（新，2026-11-02 真实语料实测） | S1、S2、S6 | 已量到形状：`analysis.status=ready` 79/79、章文体 79/79 零截断（上限 30000，实测最长约 3,600 字）、摘要段最长 195 字（上限 6000）→ 无人值守跑整本在预算上安全（G1 251,625 + G2 274,107 = 525,732 tokens，官方价 ≈¥1–9）。处置不是加窗口（会把成本推向 O(N²)），而是把长篇一致性交给 G3 注入 + R4 召回；要动窗口需 yj 拍板（issues #71/#72，证据 `docs/acceptance-evidence/20261102/g1-realcorpus-cost/`） |
| 预算只统计正文、知识/记忆段落无上限（R2） | S2 起 | `TotalChars` 全量计量 + 分段守卫用例 + 固定收缩顺位 |
| 评测只看「好不好」不看「该记的有没有记」（R3） | S2、S6 | 冻结用例三态覆盖（landed/summarized/missing）+ 未授权事实检出 + 交换顺序盲评 |
| 提取幻觉被续写放大 | S2 | 只注入 confirmed/active 且非 `needsReview`；S1 先设阈值 |
| `memory` 与 `knowledge` 双来源冲突 | S2 | §6.2 优先级 + 正文最高来源；渲染期同主体去重（iter3 Q1） |
| 测试记录缺失导致验收无法签字（iter2 教训） | 每阶段 | 任务 MD + `-changes.md` + `-issues.md` 与代码同批交付 |
| 无真实模型长篇连写质量证据；UI 全流程只做了 mock 走查 | S6 前持续 | S6 前不宣称「生产可用」 |
| eval 记忆夹具缺 `commits.json`，「写记忆 + 重启」即 fail-closed 使注入全 0（issues #59） | S3 已复现，S6（跨会话人评）最易踩；S5 的恢复矩阵**已绕过**（改用真实管线跑出的自洽目录跑通，2026-10-24） | 夹具目录仍须先修 #59 才能做重启类验收；协议不变：**引用注入读数前先读 `safeState`**，「全 0」不得当成「记忆失效」 |
| **同一目录、同一方向，重启前后的记忆注入不同**（新事件 `storyOrder=0` → 重启 reconcile 才变对；实测 177 vs 182 字，issues #67。**2026-11-02 追加：不限于草稿路径**——全仓 `StoryOrder` 只在 `memory_init.go:344` 与重启对账 `:633/:639` 赋值，深度提取采纳的那条实测同样留 0、起一次进程后 `0→3`） | S2/S3/S6 一切跨轮次与跨会话的 A/B 与人评对照 | 每轮取读数**前先重启一次**（或确认已发生 `reconcile` 提交），并把该轮 `memoryRevision` 记进证据；**该前置动作幂等**（第二次开进程零副作用，不会反复刷修订号）。修法（采纳时写对序号 / 提交后同进程对账）待 yj 拍板，证据 `docs/acceptance-evidence/20261024/issue67-deepextract-storyorder/` |
| `committed` 之后目标文件被外部改坏无检测，「正文新、记忆旧」静默存在（issues #66） | S5 之后所有长跑与故障恢复判读 | 现协议只在 `prepared` 窗口内保证原子性；现场若出现「记忆比正文少一轮」，需手工核对 `commits.json` 链尾状态而不能只信 `safeState`；是否加 payload 摘要校验（有启动开销）待拍板 |
| **提取通道的伏笔恒不进注入**（`add_thread` 不填 `involvedCharacters`/`question`，检索相关性三条路对其恒假；五变体单变量判别实测，issues #60 根因已定位） | S2/S3 的 G3 收益判定、S6 人评「吞伏笔」类判据 | 在修法落地前，**任何验收都不得把「伏笔已被消费」算作覆盖项**（夹具注入不代表提取通道，需写明来源，issues #56 同源）；修法三选一（提取侧补实体解析 / 检索侧加 `knownClues` 或同章事件参与者回退 / 审阅侧加记录编辑入口）待 yj 拍板，见 `docs/acceptance-evidence/20261024/issue60-thread-gate/issue60-thread-gate-run.md` §6 |
| 计划段被停投时无停用原因，注入突然变短无法解释（issues #58） | S3、S4、S5 验收判读 | 交叉跑 `GET /api/plans` 比对 `needsReview`；接口侧补 `planSkippedReview`/`planDisabledReason` 需 yj 拍板（纯观测字段，不改语义） |
| **编辑草稿后 `confirm` 静默退化为「只提交正文」**：旧提取被作废且解绑，`memoryRevision` 照样 +1、响应无「本次记忆操作数」（issues #63） | S5 起（改稿工作流主路径） | 短期：调用方（含 agent）在 `PATCH` 后必须重新 `extract` 才 `confirm`，并把「草稿曾有批次」视为需要显式核对的状态；中期需 yj 拍板——`confirm` 补 `memoryOpsApplied`（纯观测）或恢复 `draft_needs_recheck` 拒绝语义（改语义） |
| 同一情形不同错误码：批次已终态时 `accept` 报 `analysis_running`、`rebase`/`reject` 报 `changeset_terminal`（issues #65） | S5 验收判读、自动化脚本 | 判据一律看提交数 + `commitId` + 批次真实 `status`，不看单次错误码；观测层修法（终态检查前置）待排期 |
| **「合成回归闸绿」被当成「事实一致性通过」**：dry-run 的 `mustLand/mustNot` 恒 `unjudged`，报告又没有任何文本可判（`prompt.sections` 只给字符数、`memory.records` 只给条数）；且 dry-run 下预计费用超 `-max-cost` 只 warning、退 0（issues #68，2026-11-02 实测） | S6 的 G8 判定与一切 CI 化尝试 | 红线：预算/注入量绿 ≠ 质量绿；事实一侧要么走 HTTP 预览查 `memorySection` 全文，要么待 `buildChecklist` 补 `prompt.User` 子串判定（会改到 `internal/eval/runner_test.go:175/:376` 钉住的行为，待 yj 拍板）。CI 必须先 `go build` 再跑二进制，`go run` 会把 2/3/4 压成 1 |

---

## 7. 度量口径（每阶段「算完成」的证据类型）

- S0：自动化测试 + 复审签字。
- S1：真实数据目录 + 运行日志 + 人工抽检表 + 失败样本的截断/格式分类统计。
- S2：用例矩阵 + **R3 冻结用例报告**（三态覆盖表 + 未授权事实计数 + 交换顺序盲评胜负）。
- S3：改稿场景可复现用例（改中段 → 影响面标记 → 不自动覆盖）。
- S4：U22/U23 升级 + 计划对齐 A/B；R6/R7（若做）另附约束卡违规计数与揭示节奏人评。
- S5：崩溃点恢复矩阵复测（**2026-10-24 已按此口径交付**：真实进程硬杀 + 逐停点注入，判据同时取「payload 目标文件按时间戳归一的字节一致」与「恢复后注入读数与不崩溃对照逐字相等」两路）+ 链校验哈希证据（重建一侧仍空）。
- S6：G1–G8 全绿 + 长篇连写人评 + `-race` + R4 召回命中率。

**统一红线**：任何阶段不得用「空库冒烟通过」「单测通过」替代「真实语料端到端通过」的结论。

---

## 8. 参考项目借鉴落点（R1–R9）

> 来源与逐条取舍见 [reference-repos-review.md](reference-repos-review.md)。这里只登记「落到哪个阶段、以什么做验收」，避免借鉴项悬空。
> 三个参考项目（AI-Novel-Writer / Storydex / oh-story-claudecode）都没有可直接移植的代码，可借的是机制；其中 R1/R2/R3 属于**当前实现的缺口**，不是锦上添花。

| # | 机制 | 来源 | 落点 | 验收方式 |
|---|---|---|---|---|
| R1 | `finish_reason` 截断分类 | Pagekin | **S1 第 1 项**（I3-6，先做） | I3-16：截断样本被标记 `Truncated`，分析文案可区分原因 |
| R2 | 全 prompt 预算计量 + 分段守卫 | Pagekin | **S2**（I3-7） | I3-17：`ctx ∈ {1600,4000,12000,30000}` 用例 + 总字数断言 |
| R3 | 冻结用例覆盖判据 + 交换顺序盲评 | oh-story judge/metrics | **S2**（I3-15 改写） | 三态覆盖表 + 未授权事实计数 + 盲评胜负 |
| R4 | 零依赖 BM25 + CJK 2/3-gram 召回 | Storydex | **S6** | ≥20 章数据上召回命中率 + 一致性人评 |
| R5 | 溯源/事务提交（已具备） | Storydex + oh-story | 已落地（iter1/2） | 现有 spec 用例 |
| R6 | 作者真相 / 读者已知双轨（`ReaderKnown`/`RevealedIn`） | oh-story | **S4**（需 spec 修订） | 信息揭示节奏人评；读者未知事件不被直接点明 |
| R7 | 章节约束卡（must / must-not / stop-at 等六项） | oh-story | **S4** | 约束卡违规计数 |
| R8 | 确定性去 AI 味 lint | oh-story deslop | **S6 可选** | 只作提示，不计入生成链 |
| R9 | 生成后一致性审稿（可选双调用） | 参考项目综合 | **S6 可选** | 由 R3 先证明基线存在此类错误，再决定默认开关 |

**明确不借鉴**（已评估后放弃）：Electron/Tauri 桌面壳 + SQLite、多宿主 skill 分发、作者偏好记忆、全文 FTS5 索引、拆解仿写/扫榜/封面、欠字自动补写、本地 Git 控制。
