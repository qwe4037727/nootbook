# The World（novelagent）

以世界记忆为核心的长篇续写工作台。本地单用户、无账户，Go 单二进制 + 内嵌前端；
所有作品数据保存在本地目录，除模型接口外不依赖任何外部服务。

模块名与仓库名仍为 `novelagent`，产品名为 **The World**。

## 当前状态

| 里程碑 | 内容 | 状态 |
| --- | --- | --- |
| M0 | 导入分章、后台分析、知识库、流式续写、记忆注入、计划、草稿台 | 已交付 |
| M1 | Agent 插件框架；A1 世界监修 / A3 伏笔管家 / A4 记忆治理；Agent 中心、运行日志、成本统计、审计报告、伏笔台账 | 已交付 |
| M2 | 大世界：统一词条、关系、时间线、字段锁、章节快照 | 后端已交付 |
| M3 | 独立改编工作台：选区改编、diff 预览、影响清单、改编审计 | 后端已交付 |
| M4 | 追加导入、章节排序/合并/拆分、章节版本回滚、多候选续写、全书快照 | 后端已交付 |
| M5 | A2/A5/A6/A7/A9 Agent、自然语言任务队列、审计筛选、项目/全局成本看板 | 后端已交付 |
| M6 | 图像：OpenAI 兼容 / SD WebUI 双后端、提示词组装、基准图、缩略图与资产队列 | 已交付（结构化一致性代理，视觉相似度未接入） |
| M7 | 导出：TXT / Markdown / EPUB3，可选图片素材包 | 已交付 |

前端当前覆盖：导入、章节编辑、续写、记忆审阅、大纲计划、草稿台、Agent 中心、
图像中心与导出。M2–M5 的能力仍以 HTTP API 为主，界面尚未全部接入。

M6 不自带模型或 Key：需在「图像模型」中填写 OpenAI 兼容或 SD WebUI 服务后方可生成；
图像一致性分数是基于提示词特征的结构化代理，不代表视觉相似度。

## 快速开始

要求 Go 1.25.4。

```bash
go run . -addr 127.0.0.1:8080 -data data
# 或
go build -o novelagent.exe . && ./novelagent.exe -data data
```

打开 <http://127.0.0.1:8080>，先在「模型设置」中填写 OpenAI 兼容接口：

- Base URL（如 `https://api.deepseek.com/v1`）
- 模型名
- API Key

支持流式与非流式 `/chat/completions`；流式会请求 `stream_options.include_usage`，
以便记录每次调用的 token 成本。

## 里程碑与 Agent 配置

仓库根目录的 [`milestone.yaml`](milestone.yaml) 是默认配置。复制到 `-data` 目录可覆盖全局默认；
单个 Agent 的开关与权限还可在项目内「Agent 中心」或 `PUT /api/agents/config/{key}` 再覆盖。

```yaml
milestones:
  m1:
    enabled: true
    agents:
      continuity: { enabled: true, level: observe }
      fuse:       { enabled: true, level: observe }
      memory:     { enabled: true, level: observe }
  m2: { enabled: true }
  m3: { enabled: true }
  m4: { enabled: true }
  m5:
    enabled: true
    agents:
      plot:      { enabled: true, level: observe }
      style:     { enabled: true, level: observe }
      relation:  { enabled: true, level: observe }
      proofread: { enabled: true, level: observe }
      task:      { enabled: true, level: observe }
  m6: { enabled: true }
  m7: { enabled: true }
```

- 权限三档：`observe`（只报告）、`propose`（提案送审）、`autonomous`（仅低风险自动更新）。
- 所有 Agent 默认 `observe`；**任何 Agent 都禁止自动修改正文、禁止自动删除词条**。
- 里程碑关闭时对应路由不注册（返回 404），Agent 不装配。
- 插件化：Agent 实现 `agentruntime.Agent`（`Key/Name/Triggers/Run`），由里程碑装配，调度器内没有按 Agent 的分支。

### 内置 Agent

| Key | 名称 | 职责 |
| --- | --- | --- |
| `continuity` | A1 世界监修 | 低置信实体、待复核状态、同主体同字段冲突；只出报告与提案 |
| `fuse` | A3 伏笔管家 | 扫描章节识别伏笔候选，维护台账，并注入后续续写上下文 |
| `memory` | A4 记忆治理 | 记忆合并/过期、知识库健康度、久未确认状态；只入审阅队列 |
| `plot` | A2 剧情推演 | 停滞伏笔、无冲突长章、计划桥段 |
| `style` | A5 文风 | 视角、句长、对白、文风画像漂移 |
| `relation` | A6 关系情绪 | 过期关系、未登记同场、长期未确认情绪 |
| `proofread` | A7 校对润色 | 标点、引号、空对白、长句、叠词等问题 |
| `task` | A9 任务执行 | 失败/停滞/暂停/积压任务与任务摘要 |

## 数据与目录

按项目隔离：每个作品一个数据目录、一个 SQLite 库和一个 Agent 运行库。
项目选择通过 `X-Project-ID` 请求头、`?projectId=` 或 `project_id` Cookie；缺省为 `default`。

```text
<data>/
├── projects.db                 # 项目索引（默认项目即 <data> 本身）
├── projects/<id>/              # 其他作品
├── project.json                # 作品元数据与章节列表
├── chapters/<chapterId>.json   # 章节正文
├── knowledge.json              # 知识库（人物/设定/文风/摘要）
├── memory/                     # 记忆事件、状态、认知、线程、版本
├── drafts.json                 # 草稿台
├── config.json                 # 模型配置（文本与图像，可能含 API Key）
├── images/                     # M6 图像资产与缩略图
├── exports/                    # M7 导出文件与记录
├── agent.db                    # M1–M6 的 Agent/审计/大世界/任务/图像数据库
└── agent/runs.jsonl            # Agent 运行日志（兼容 M0）
```

- `agent.db` 惰性创建，迁移位于 [`internal/store/migrations/`](internal/store/migrations/)，
  随库打开执行且幂等。
- 正文与记忆变更通过「草稿确认」一次提交；#63 起，草稿正文在记忆同步后被修改时，
  提交必须显式选择「重新提取记忆」或「仅提交正文（记忆不更新）」，不会静默跳过。

## 代码结构

```text
main.go                     # 入口：内嵌 web/、启动项目管理器与 HTTP
internal/
├── app/                    # 按项目路由与 Handler 缓存
├── project/                # 多项目 Manager、运行时装配
├── server/                 # HTTP API、静态资源、错误约定
├── agentruntime/           # Agent 契约、事件总线、运行记录、熔断
├── agents/                 # M1/M5 内置 Agent 实现
├── milestone/              # milestone.yaml 加载与默认值
├── store/                  # JSON 原子写 + SQLite（agent.db）、迁移、记忆与草稿提交
├── novel/                  # 数据模型、分章、记忆上下文
├── agent/                  # 续写提示词与上下文窗口
├── analysis/               # 后台分析状态机与知识合并
├── llm/                    # OpenAI 兼容客户端（流式/非流式、token 用量、重试）
├── plan/  recall/          # 大纲计划、长篇召回
├── world/ adapt/ task/     # M2 大世界、M3 改编、M5 任务队列
├── continuation/           # M4 多候选续写
├── imagegen/               # M6 图像后端适配、提示词、一致性评分与队列
├── export/                 # M7 TXT/Markdown/EPUB3 导出
└── eval/                   # 评测与离线检查
web/                        # 单页前端（embed 进二进制）
docs/                       # 设计与接口文档
```

## 主要接口

完整清单见 [docs/api_m2_m3.md](docs/api_m2_m3.md)、[docs/api_m4_m5.md](docs/api_m4_m5.md)、
[docs/api_m6_m7.md](docs/api_m6_m7.md)，
M1 Agent 接口清单写在 [`internal/server/agents.go`](internal/server/agents.go) 顶部注释。

- 作品与章节：`GET/PUT /api/project`、`POST /api/import`、章节 CRUD、`POST /api/analyze`
- 续写与预览：`POST /api/continue`（SSE）、`POST /api/continue/preview`、
  `POST /api/continue/candidates`（M4）
- 记忆与草稿：`/api/memory/*`、`/api/drafts/*`、`/api/memory/changesets/*`
- 计划与回忆：`/api/plans/*`、召回由续写参数控制
- M1 Agent：`/api/agents/{config,runs,stats,reports,proposals}`、`/api/foreshadows`
- M2 大世界：`/api/world/entries|relations|timeline|snapshot`
- M3 改编：`/api/adapt/*`
- M4 章节增强：`/api/chapters/reorder|merge`、`/api/chapters/{id}/split`、
  `/api/chapters/{id}/versions[/{revision}/restore]`、`/api/snapshots/*`、
  `/api/continue/candidates`、`/api/continuations`
- M5 任务与审计：`/api/tasks/*`、`/api/audit/reports`、`/api/audit/cost`、`/api/cost/global`
- M6 图像：`/api/images/*`、`/api/config/image-model`
- M7 导出：`POST /api/export`、`/api/exports`、`/api/exports/{id}/download`
- 功能开关：`GET /api/features`

统一约定：请求体严格 JSON（未知字段拒绝）、错误为 `{"code":"...","error":"..."}`，
所有写操作按项目隔离，并对并发修改使用 revision/expected 校验。

## 开发与测试

```bash
go build ./...
go vet ./...
go test ./... -count=1
```

测试使用 `t.TempDir()` 与 `httptest`，不访问外网、不读写仓库内 `data/`。
在 Windows 上，测试会在 `t.Cleanup` 中关闭 `agent.db` 连接，避免临时目录清理失败。

## 文档

- [docs/project_overview.md](docs/project_overview.md)：M0 时期的项目地图（基线快照，部分内容已过时）
- [docs/features.md](docs/features.md)：功能清单
- [docs/api_m2_m3.md](docs/api_m2_m3.md)、[docs/api_m4_m5.md](docs/api_m4_m5.md)、
  [docs/api_m6_m7.md](docs/api_m6_m7.md)：接口契约
- [详设.md](详设.md)、[docs/详设_V1.1_核实与决策基线.md](docs/详设_V1.1_核实与决策基线.md)：设计基线
- [docs/issues.md](docs/issues.md)：问题与决策记录

## 安全与提交约定

- 本服务面向本机使用，默认只监听 `127.0.0.1`，没有账户体系；不要直接暴露到公网。
- `data/` 可能包含未发表正文与 API Key，已被 `.gitignore` 排除。
- 不提交 `data/`、`corpus/`、`output/`、临时目录、二进制、日志或任何真实密钥。
- 不修改全局 git config；提交与推送分别授权。
