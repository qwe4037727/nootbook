# novelagent 功能点清单（全量）

> 用途：开发前对齐范围。状态图例：✅ 已实现（代码可证）｜🚧 规划中（本次大纲模块主线）｜❌ 缺口（用户诉求，将由 🚧 补上）
> 生成：2026-10-22

---

## 一、已有功能（✅ 代码已实现）

### A. 作品与文稿
| 编号 | 功能 | 说明 | 对应实现 |
|---|---|---|---|
| F1 | 导入文稿 | 粘贴文本或选文件，按「第X章/回/节」正则自动分章；识别前言、清洗「本章字数/更新时间」等元数据行与导出站水印；上限 500 万字 | `server.handleImport` → `novel.SplitChapters(text,"正文")` |
| F2 | 设置/修改作品标题 | 编辑书名 | `server.handlePutProject` → `store.UpdateTitle` |
| F3 | 章节 CRUD | 新增/编辑/删除单章内容 | `handleCreateChapter` / `handleUpdateChapter` / `handleDeleteChapter` / `handleGetChapter` |
| F4 | 自动触发分析 | 导入或编辑章节后，分析状态置 `pending`，后台自动启动分析 | 各写接口调用 `notifyAnalysis()` |

### B. 模型配置
| 编号 | 功能 | 说明 | 对应实现 |
|---|---|---|---|
| F5 | 模型设置 | 配置 BaseURL / Model / APIKey / ContextChars；APIKey 缺省时回退环境变量 `OPENAI_API_KEY`；BaseURL 做 http/https 校验 | `server.handleGetConfig` / `handlePutConfig`；`store.Config.EffectiveAPIKey` |

### C. 知识库分析（核心）
| 编号 | 功能 | 说明 | 对应实现 |
|---|---|---|---|
| F6 | 逐章分析 | LLM 抽取每章的 人物 / 世界观 / 文风 / 章节摘要 | `analysis.buildAnalystMessages` + `llm.CompleteChat`(temp 0.2) |
| F7 | 增量合并 | 按别名/称谓相交匹配同一实体；仅当本章序号≥已记录 LastSeen 才覆盖字段；confidence 取 max；evidence 去重限 6 条 | `analysis.merge.go` `applyChapter` |
| F8 | 总览合成 | 分析尾声生成 `overview`（核心设定与当前进展） | `buildSynthesisMessages`(temp 0.3) → `applySynthesis` |
| F9 | 分析状态机 | idle / pending / analyzing / ready / error；失败可重试 | `analysis.Manager` `step()` / `Retry()` |
| F10 | 知识库持久化 | `knowledge.json`：characters / world / style / summaries / overview | `store.LoadKnowledge` / `CommitAnalysis` / `FinishAnalysis` |

### D. 续写（核心）
| 编号 | 功能 | 说明 | 对应实现 |
|---|---|---|---|
| F11 | 流式续写 | SSE 逐字输出续写正文 | `server.handleContinue` → `llm.StreamChat` |
| F12 | 上下文组装 | 取最近正文（默认 12000 字窗口，截断到句边界）；智能选角色(前12)/设定(前10)/前情(前3章) | `agent.Build` `selectCharacters/selectWorld/selectSummaries` |
| F13 | 续写参数 | 剧情方向 direction(≤2000字) / 目标字数 targetChars(200–4000) / 温度(≤1.5) | `continueRequest` |
| F14 | 续写约束 | 从最近正文末接、不回改前文、以正文为最高事实来源、不草率收尾 | `agent.systemPrompt` |
| F15 | 续写元信息 | 返回上下文统计（字符数/是否截断/选中角色与设定数等） | SSE `meta` 事件 |

### E. 工程与安全
| 编号 | 功能 | 说明 | 对应实现 |
|---|---|---|---|
| F16 | 安全响应头 | CSP(默认 self) / X-Frame-Options / nosniff / Referrer-Policy | `server.withHeaders` |
| F17 | 严格输入校验 | 体大小限制 + `DisallowUnknownFields` | `decodeJSON` |
| F18 | 原子写 + 乐观锁 | 临时文件+rename；`revision` 防并发覆盖 | `store.writeJSONFile`；`CommitAnalysis` 等带 revision |
| F19 | v1 数据迁移 | 旧单文件格式自动拆分为章节结构并备份 | `store.migrateLegacy` |

---

## 二、规划中功能（🚧 本次开发主线：大纲规划模块）

> 前置：必须有知识库 `knowledge.json`（走 F1+F6 链路）。新书旧书通用。

| 编号 | 功能 | 说明 | 计划实现 |
|---|---|---|---|
| F20 | 生成大纲 | 基于 knowledge + 可选参数 `endingTone`(结局基调)/`avoidTropes`(规避套路)/`volumeCount`(建议卷数)/`extra`(自由补充) → 产出结构化 `Outline` → 存 `outline.json` | `POST /api/outline`；新建 `internal/outline` 包（prompt + 容错 JSON 解析 + `Generate`） |
| F21 | 查看大纲 | 读取已存大纲，无则返回空 | `GET /api/outline`；`store.LoadOutline/SaveOutline` |
| F22 | 前端大纲面板 | 「生成大纲」按钮(loading 态) + 展示 立意/分卷分章大纲/大结局/主题 | `web/` 新面板 |
| F23 | 大纲回灌续写 | 续写开关「对齐大纲」→ `agent.Build` 注入 `Outline`，续写遵循规划 | `agent.Build` 增加 `Outline` 参数；续写 prompt 加「大纲对齐」段 |

**Outline 数据结构（草案）**
```
premise      核心立意（一句话故事内核）
structure[]  { arc卷名, summary, chapters:[{title, summary, keyBeats[]}] }
ending       大结局规划（文字）
endingBeats[] 关键收束点（伏笔回收/人物弧光）
themes[]     主题线索
notes        写作提醒
```

---

## 三、用户诉求中的缺口与说明

- ❌ **规划中间大纲（分卷/分章）** —— 当前没有，由 **F20** 补上
- ❌ **规划大结局** —— 当前没有，由 **F20** 补上
- ℹ️ **「改掉现有结局继续写」不是独立功能**：系统续写只从最近正文末尾往后接，不回改前文。要做「改结局」= **F3（手动截改/删除末章）** + **F11（续写）** 的组合用法；保存后会自动重分析(F4/F9)使知识库跟随新正文。

---

## 四、潜在扩展（P2，本次不做）
- 大纲修订迭代（基于已有大纲 + 用户反馈重新生成）
- 分章细纲（把某卷展开成逐章细纲）
- 进度对齐（标记哪些章已写、哪些待写）
- 流式大纲生成（SSE 边生成边显示）
