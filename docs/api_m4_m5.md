# M4 续写与草稿增强 / M5 Agent、任务与审计中心接口说明

本文档对应里程碑 M4 与 M5 的后端接口。除特别说明外，路由只有在
`milestone.yaml` 的 `milestones.m4.enabled` / `milestones.m5.enabled` 为 `true`
时才注册；关闭后访问返回 404。所有接口按项目隔离：项目通过
`X-Project-ID` 请求头、`projectId` 查询参数或 `project_id` Cookie 选择，
未指定时使用默认项目。

> 约束：Agent 与任务队列都不会自动修改正文或删除词条。续写、批量续写与 Agent
> 提案只写草稿、审计表或审阅队列；正文变更必须由人工提交草稿或调用章节接口。

## 1. 数据与迁移

| 数据 | 位置 | 说明 |
| --- | --- | --- |
| 章节历史版本 | `agent.db#chapter_version` | `(chapter_id, revision)` 唯一，每章保留最近 50 个版本 |
| 全书快照 | `agent.db#book_snapshot` / `book_snapshot_chapter` | 快照含章节正文与 `knowledge.json` 副本 |
| 续写候选组 | `agent.db#continue_group` / `continue_candidate` | 1~3 个候选独立保存，含 token 成本 |
| 任务队列 | `agent.db#task` | `pending/running/paused/completed/failed/cancelled` |
| 审计报告 | `agent.db#agent_report` | findings 内的 `entryId` 用于人物筛选 |
| Agent 运行成本 | `agent.db#agent_run` | 每次 Agent 调用记录 token/耗时/状态 |
| 改编成本 | `agent.db#adapt_run` | M3 改编调用 |
| 续写候选成本 | `agent.db#continue_candidate` | 每个候选的 token |

迁移文件：`internal/store/migrations/0004_m4_manuscript.sql`、`0005_m5_task.sql`，
随项目 `agent.db` 惰性打开执行，均为 `CREATE TABLE IF NOT EXISTS`，对旧库幂等。

## 2. 追加导入与章节增强

### 2.1 导入（覆盖 / 追加）

`POST /api/import`

```json
{
  "title": "可选书名",
  "text": "第一章 启程\n正文……\n第二章 入山\n正文……",
  "mode": "overwrite"
}
```

- `mode` 缺省或 `overwrite`：覆盖导入，重置作品章节与派生状态（既有行为）。
- `mode=append`（M4）：只在全书末尾追加新章节，保留既有章节、记忆与知识库；
  空作品时才采用请求标题。响应中的 `importedChapters/importedChars` 只统计本次追加，
  `totalChars` 为追加后全书字数。

响应：`{ "project": publicProject, "importedChapters": 2, "importedChars": 1200, "totalChars": 5600 }`

### 2.2 排序 / 合并 / 拆分

- `POST /api/chapters/reorder`

```json
{"chapterIds": ["ch_b", "ch_a", "ch_c"]}
```

必须包含**全部章节且不重复**，否则 400。成功后章节序号按数组顺序重排。

- `POST /api/chapters/merge`

```json
{"chapterIds": ["ch_a", "ch_b"], "title": "合章"}
```

要求至少两个**连续**章节；正文按顺序拼接，标题可选。返回合并后的章节与作品。

- `POST /api/chapters/{id}/split`

```json
{"offset": 1200, "firstTitle": "上", "secondTitle": "下"}
```

`offset` 为章节正文的 **rune 偏移**，必须满足 `0 < offset < 章节长度`。
原章节保留前半内容（Revision +1），后半成为紧随其后的新章节。

### 2.3 章节版本与回滚

- `GET /api/chapters/{id}/versions?limit=50`

返回 `{"versions": [ChapterVersion]}`，按 Revision 倒序。版本在章节保存、
拆分、草稿联合提交等正文变更时记录。

- `POST /api/chapters/{id}/versions/{revision}/restore`

将章节恢复为指定历史版本。执行前自动创建全书快照（`reason=auto_before_chapter_rollback`）。
响应：

```json
{"chapter": {...}, "project": {...}, "autoSnapshotId": "snap_xxx"}
```

## 3. 续写候选与批量续写

### 3.1 多候选续写（1~3 个并排版本）

`POST /api/continue/candidates`

```json
{
  "startMode": "chapter_offset",
  "chapterId": "ch_xxx",
  "startOffset": 1200,
  "direction": "让主角进入秘境",
  "targetChars": 800,
  "candidates": 2,
  "temperature": 0.85,
  "useMemory": true,
  "usePlan": true,
  "useRecall": false
}
```

- `startMode=full`（缺省）：从全书末尾继续。
- `startMode=chapter_offset`：从指定章节的 rune 偏移处继续；只把该章前
  `startOffset` 个字符作为上下文。
- `candidates` 归一化为 1~3；各候选并发调用模型，温度按 `+0.05*i` 递增。
- 结果只写 `continue_group/continue_candidate`，不写正文、不写记忆。

响应：`{"group": ContinuationGroup, "candidates": [ContinuationCandidate]}`。
单个候选失败会以 `status:"failed"` 记录；全部失败返回 502，但候选组仍保留。

- `GET /api/continuations?limit=50` → `{"groups": [...]}`
- `GET /api/continuations/{id}` → `{"group": {...}, "candidates": [...]}`

### 3.2 任务中心：自然语言批量续写

`POST /api/tasks`（M5）

```json
{"description": "续写3章，让主角突破瓶颈", "priority": "high"}
```

自然语言解析：

- 含“续写/批量写”或“写 N 章” → `continue_batch`；数量支持 `3`、`三`、`十二`、`二十`。
- 含“校对/润色/错别字”等 → `agent_run`，默认 Agent `proofread`；
  另支持 `plot/style/relation/task/fuse/memory/continuity`。
- 出现“紧急/尽快/高优先”提升为 high，“不急/有空”降为 low。

也可显式指定：

```json
{
  "kind": "continue_batch",
  "priority": "normal",
  "description": "批量续写",
  "params": {
    "chapterCount": 3,
    "direction": "让主角突破瓶颈",
    "targetChars": 800,
    "temperature": 0.85,
    "startChapterId": "ch_last",
    "startOffset": 500
  }
}
```

`continue_batch` 参数约束：

- `chapterCount` 1~20，`targetChars` 200~4000，`temperature` ≤ 1.5。
- `startChapterId` 必须是**最后一章**，`startOffset` 必须位于 `(0, 章节长度]`；
  批量续写从该偏移生成下一章草稿，后续章节依次引用尚未提交的草稿正文。
- 每章生成后通过 `POST /api/drafts` 的存储层落为**待确认草稿**，正文不变；
  任务结果 `TaskContinueResult` 记录 `draftIds/groupIds/chapterIds/tokens`。

`agent_run` 参数：

```json
{"kind": "agent_run", "description": "校对", "params": {"agent": "proofread", "chapterId": "ch_xxx"}}
```

- 未指定 `chapterId` 时取最后一章；事件类型为 `chapter_saved`。
- Agent 必须已在当前项目注册且启用；失败受熔断器保护，不影响其他任务。

任务接口：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/tasks` | 创建任务，返回 `Task` |
| GET | `/api/tasks?status=&limit=` | 列表，默认 50 |
| GET | `/api/tasks/{id}` | 详情（进度、结果、错误） |
| POST | `/api/tasks/{id}/pause` | 暂停 pending/running，执行中任务在下一章检查点退出 |
| POST | `/api/tasks/{id}/resume` | 暂停任务重新入队 |
| POST | `/api/tasks/{id}/cancel` | 取消未结束任务 |

调度规则：单项目单工作协程；按优先级 high → normal → low、同优先级按入队时间领取。
服务重启时 `running` 任务自动转为 `paused` 并提示恢复，避免无人监督继续消耗模型调用。

恢复语义：`resume` 从任务已持久化的进度继续，已生成的草稿按顺序作为后续章节的上下文，
不会重复生成已完成章节；恢复会递增该任务的执行代次，暂停/取消时仍在收尾的旧协程
在下一个检查点发现代次过期后直接退出，不再落草稿。取消任务时，取消瞬间仍在途的
模型调用结果同样不会写入草稿。

任务状态变更会发布 `task.created/completed/failed/cancelled` 事件，供 A9 任务执行 Agent 巡检。

## 4. 全书快照

- `POST /api/snapshots`

```json
{"name": "突破前", "reason": "manual"}
```

保存全部章节正文与知识库副本。空作品返回 400。

- `GET /api/snapshots?limit=50` → `{"snapshots": [BookSnapshot]}`
- `GET /api/snapshots/{id}` → `SnapshotDetail`（含每章正文与 Revision）
- `POST /api/snapshots/{id}/restore`

恢复前自动创建当前状态快照（`reason=auto_before_book_rollback`），然后恢复章节集合与
`knowledge.json`，删除快照之后新增的章节文件。响应：

```json
{"project": {...}, "autoSnapshotId": "snap_xxx"}
```

## 5. M5 Agent

| Key | 名称 | 触发事件 | 产出 |
| --- | --- | --- | --- |
| `plot` | A2 剧情推演 | created/saved/imported/appended | 停滞伏笔、无冲突长章、计划桥段；可调用模型给 `plot_next_beats` |
| `style` | A5 文风 | created/saved/imported/appended | 视角/句长/对白/画像缺失漂移 |
| `relation` | A6 关系情绪 | created/saved/imported/appended | 过期关系、未登记同场、长期未确认情绪 |
| `proofread` | A7 校对润色 | created/saved/imported/appended | 重复标点、引号不配对、空对白、英文直引号、长句、非合法叠词 |
| `task` | A9 任务执行 | task.created/completed/failed/cancelled | 失败/停滞/暂停/积压任务与当前任务摘要 |

- 注册位置：`internal/project/manager.go`，仅 `M5Enabled` 时注册；Agent 本身通过
  `agentruntime.Agent` 接口插件化，不在调度器内写分支。
- 权限：`observe` 只报告；`propose/autonomous` 可把低风险建议送入
  `agent_proposal` 审阅队列。所有 Agent 均无正文写入/词条删除路径。
- 失败隔离：单个 Agent panic 被 recover；连续 3 次失败打开熔断 2 分钟，
  窗口结束仅放行一次半开探测，成功后清零。其他 Agent 与主流程不受影响。
- 开关与权限：`GET/PUT /api/agents/config...`（M1 既有接口）可单独停用每个 Agent。

## 6. 审计与成本

### 6.1 审计报告筛选

`GET /api/audit/reports?agent=&chapterId=&severity=&entryId=&chapterIndexMin=&chapterIndexMax=&limit=`

- `entryId`：匹配 findings JSON 中的人物/词条 ID，用于按人物筛选。
- `chapterId` / `chapterIndexMin..Max`：按章节或序区间筛选。
- `severity`：`info|warning|critical`。
- 响应：`{"reports": [AgentReportRecord], "filter": {...}}`。

### 6.2 项目成本看板

`GET /api/audit/cost?days=7` 或 `?since=2026-11-01T00:00:00Z`

```json
{
  "buckets": [
    {"source": "agent_run", "calls": 12, "failed": 1, "promptTokens": 12000, "completionTokens": 3000, "totalTokens": 15000},
    {"source": "adapt_run", "calls": 2, "failed": 0, "promptTokens": 2000, "completionTokens": 800, "totalTokens": 2800},
    {"source": "continue_candidate", "calls": 6, "failed": 0, "promptTokens": 9000, "completionTokens": 2400, "totalTokens": 11400}
  ],
  "total": {"source": "", "calls": 20, "failed": 1, "totalTokens": 29200},
  "taskCounts": {"pending": 2, "completed": 5, "failed": 1}
}
```

### 6.3 全局成本看板（全部项目）

`GET /api/cost/global?days=7`（应用级路由，不需要项目头；M5 关闭时 404）

返回 `{"projects": [{"projectId","name","summary"}], "total": CostBucket, "taskCounts": {...}}`。
只返回聚合 token 与任务计数，不返回正文或词条内容。

## 7. 错误约定

- 请求体使用 `MaxBytesReader` 与 `DisallowUnknownFields`。
- 章节/版本/快照不存在：404；状态冲突（任务暂停已完成等）：409；
  参数越界（排序缺章、合并不连续、拆分偏移越界、起点非法）：400；
  全部候选生成失败：502；服务未启用或任务中心未注入：503。
- 审计与成本接口只读，不触发分析或 Agent。

## 8. 验收对应

| 验收项 | 实现位置 |
| --- | --- |
| 覆盖导入 vs 追加导入 | `POST /api/import#mode`、`store.AppendImport` |
| 排序/合并/拆分 API | `POST /api/chapters/reorder|merge|{id}/split` |
| 全书末尾或章节内偏移续写 | `POST /api/continue/candidates#startMode/startOffset` |
| 1~3 候选独立保存 | `continue_group/continue_candidate` |
| 连续多章队列 + 暂停/取消 | `POST /api/tasks` + `task.Service` |
| 快照 + 单章版本 + 回滚 | `/api/snapshots`、`/api/chapters/{id}/versions` |
| A2/A5/A6/A7/A9 插件接入 | `internal/agents/*`、`internal/project/manager.go` |
| 自然语言任务中心 | `POST /api/tasks`、`internal/task` |
| 审计筛选 + 成本看板 | `/api/audit/reports`、`/api/audit/cost`、`/api/cost/global` |
| Agent 异常隔离与熔断 | `agentruntime` 熔断器与 panic recover |
