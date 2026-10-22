# novelagent 项目概览

> 生成时间：2026-10-22 | 用途：项目熟悉 / 上手地图

## 1. 项目定位

**novelagent** 是一个**本地运行的「小说智能分析与续写助手」Web 服务**（Go 单二进制，零第三方依赖）。

一句话流程：把整本小说按「第X章/回/节」切分成章节 → 用 LLM 逐章抽取**人物 / 世界观 / 文风 / 章节摘要**构建「知识库」→ 后续基于知识库做**可控续写**（SSE 流式输出）。

- 入口：`main.go`，监听 `127.0.0.1:8080`（可 `-addr` / `-data` 覆盖）
- 前端：`web/`（index.html + app.js + style.css，约 75KB，内嵌进二进制）
- 数据：落盘在 `-data` 目录（默认 `data/`），纯 JSON 文件存储

## 2. 技术栈

- **Go 1.25.4**，模块名 `novelagent`
- **纯标准库**：HTTP（`net/http` + `http.ServeMux` 的 method+path 路由）、JSON、SSE、正则、`embed`
- **无 go.mod 依赖**（require 为空）——所有能力自研
- LLM 接入：OpenAI 兼容的 `/chat/completions`，支持流式（SSE）与非流式两种响应

## 3. 目录结构

```
novelagent/
├── main.go                 # 入口：embed web、建 Store、起 analysis.Manager、起 HTTP
├── go.mod                 # go 1.25.4，无第三方依赖
├── internal/
│   ├── novel/             # 数据模型 + 文本处理
│   │   ├── project.go      #   Project/Knowledge/Character/WorldEntry/StyleProfile 模型 + Normalize/合并
│   │   └── chapter.go      #   SplitChapters 分章算法（正则 + 元数据/水印清洗）
│   ├── store/             # JSON 文件持久化（原子写 + 乐观锁 revision）
│   │   └── store.go
│   ├── llm/               # OpenAI 兼容客户端（CompleteChat / StreamChat）
│   │   └── client.go
│   ├── analysis/          # 后台分析引擎（状态机）
│   │   ├── manager.go      #   Manager 循环 + step() 驱动 pending→running→synthesis→ready
│   │   ├── prompt.go       #   分析/综合 prompt 构造 + 知识摘要裁剪
│   │   ├── merge.go        #   applyChapter/applySynthesis 增量合并 + 别名匹配
│   │   └── synthesis_probe.go  ⚠️ package main，与本目录 analysis 冲突（见末尾问题）
│   ├── agent/             # 续写 prompt 构建（Build）
│   │   └── prompt.go
│   └── server/            # HTTP API + 静态托管 + 安全头
│       └── server.go
├── web/                   # 前端单页（embed）
├── data/                  # 运行数据：project.json / knowledge.json / chapters/*.json / config.json
├── corpus/                # 测试语料：无边悔恨.txt（511KB）
└── tools/corpus_check.py  # 语料检查脚本
```

## 4. 核心数据模型（novel 包）

- `Chapter` / `ChapterMeta`：章节（含 CountChars 精确字数）
- `Project`：标题 + 章节列表 + `AnalysisState`（status / analyzedCount / totalCount / message）
- `Knowledge`（知识库）：
  - `Overview` 总览（300~500 字核心设定）
  - `Characters []Character`：name/aliases/role/personality/appearance/relationships/state/notes/evidence/confidence/LastSeen
  - `World []WorldEntry`：title/aliases/category/content/evidence/confidence/LastSeen
  - `Style StyleProfile`：perspective/tone/sentence/dialogue/pacing/samples（文风指纹）
  - `Summaries []ChapterSummary`：每章 summary + endsAt 停点
- `AnalysisStatus`：idle / pending / analyzing / ready / error

## 5. 两条核心流水线

### 5.1 分析流水线（analysis.Manager 后台状态机）

```
导入/编辑章节 → Store 把 Analysis 置 pending → Manager.Notify()
→ loop() 收到 notify → step():
  1. 校验 config（BaseURL/Model 必须有），否则 markFailed("请先完成模型设置")
  2. status=pending: CommitAnalysis → running（乐观锁 revision 防并发覆盖）
  3. 逐章：buildAnalystMessages(作品, 本章, 已有知识摘要≤6000字, 正文前30000字)
          → LLM(temperature=0.2) → parseJSONObject(容错 ```json 包裹)
          → applyChapter 增量合并进 knowledge → CommitAnalysis(本章 index)
  4. 全部章节后：buildSynthesisMessages → LLM(temperature=0.3)
          → applySynthesis(写 Overview + 合并 style) → FinishAnalysis → ready
  5. 任意一步失败 → FailAnalysis → error（可 Retry 重跑）
```
- 单章调用超时 **5 分钟**
- 合并策略（`merge.go`）：按 **名字/别名相交**匹配同一实体；仅当 `本章Index >= 已记录LastSeen` 才用新值覆盖字段（避免旧章节回写过期信息）；confidence 取 max；evidence 去重且限 6 条。

### 5.2 续写流水线（POST /api/continue，SSE 流式）

```
handleContinue:
  → 校验正文非空、config 已配置、知识库可读
  → agent.Build(Input):
       manuscriptTail: 取最近正文（默认 12000 字符窗口，截断到换行边界）
       selectCharacters: 按 confidence+最近正文提及+LastSeen 打分，取前 12
       selectWorld: 取前 10；selectSummaries: 前情提要 3 条
       组装 system(续写规则) + user(设定/文风/人物/最近正文/剧情方向/目标字数)
  → SSE: 先发 meta(上下文统计) → 逐块 delta → done
```
- 续写 system 规则强调：**「最近正文」为最高事实来源**，知识库与正文冲突时服从正文；不重复揭示伏笔、不草率收尾。

## 6. 关键设计亮点

- **乐观锁 revision**：`CommitAnalysis/FinishAnalysis/FailAnalysis` 都带 `revision`，若与当前 `project.Revision` 不符则跳过（避免并发/重试覆盖）。
- **字符级截断与字数统计**：`CountChars` 去空白/零宽字符，对中文小说精确计费；续写/分析均有 rune 级截断保护。
- **知识库增量一致**：「newer 才覆盖」+ 别名并集 + evidence 去重，避免章节乱序导致的状态回退。
- **安全头**：`withHeaders` 统一加 CSP（default-src 'self'）、X-Frame-Options、nosniff 等。
- **输入严格校验**：`MaxBytesReader` + `DisallowUnknownFields` + 各接口长度上限（导入 500 万字、续写要求 2000 字等）。
- **v1 数据迁移**：`store.migrateLegacy` 兼容旧版单文件结构（manuscript/style/characters/world），自动拆分章节并备份为 `project.v1.backup.json`。

## 7. 当前磁盘状态

- `data/config.json`：已配置 `baseUrl=https://ai.laodog.top/v1`、`model=gpt-5.6-sol`、`contextChars=30000`、已填 apiKey（**明文**，见问题）。
- `data/project.json`：当前是 **v1 legacy 格式**（《无边悔恨》作者"作手"，manuscript 为整段带「本章字数/更新时间」标记的文本，`characters/world` 为空）——尚未迁移成新章节结构。
- `corpus/无边悔恨.txt`：511KB，疑似上述 manuscript 的源文本。
- 4 个 `*.exe`：历史构建产物（novelagent / e2e / e2e2 / next），因当前源码无法编译，已**不是最新**。

## 8. 发现的问题（需关注）

1. ⚠️ **编译阻断（最高优先级）**：`internal/analysis/synthesis_probe.go` 是 `package main`，与同目录 `package analysis` 冲突，`go build ./...` 与 `go build .` 均失败。该文件是一个硬编码 `llm.New("http://127.0.0.1:7897", "gpt-5.6-sol")` 的**游离探针/对比脚本**（不在 main 包引用、用另一套 overview 提示词），应移出 internal/（如 `cmd/probe/` 或 `tools/`）、加 build tag，或删除。
2. 🔑 **明文 API Key**：`data/config.json` 内含明文 key。建议加入 `.gitignore`、改用环境变量 `OPENAI_API_KEY`（代码已支持），避免泄露。
3. 📦 **legacy 数据未迁移**：`data/project.json` 仍是 v1 结构，`store.New` 理论上会在首次启动时迁移，但当前文件未迁移——需确认是样例数据还是迁移逻辑未触发。
4. 🧪 **`tools/corpus_check.py` 与 `synthesis_probe.go`**：均为辅助/实验脚本，与主流程无关，长期应归类管理。

## 9. 建议的下一步（待你确认）

- [ ] 修复编译阻断（移动或删除 `synthesis_probe.go`）
- [ ] 跑通主程序：配好模型后启动，`curl` 触发分析，验证知识库生成
- [ ] 把 `config.json` 的 key 改为环境变量、加 `.gitignore`
- [ ] 补一份 README / 开发文档
- [ ] 确认 `data/project.json` 迁移行为是否符合预期
