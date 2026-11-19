# M2 大世界 / M3 改编工作台接口说明

本文档对应里程碑 M2（大世界）与 M3（独立改编工作台）的后端接口，路由仅在
`milestone.yaml` 对应开关为 true 时注册；关闭后访问返回 404。

```yaml
milestones:
  m2:
    enabled: true
  m3:
    enabled: true
```

## 1. 数据模型与持久化

| 数据 | 位置 | 说明 |
| --- | --- | --- |
| 角色 / 设定（真相源） | `<project>/knowledge.json` | 沿用既有分析与续写链路，人工编辑不重跑分析 |
| 势力 / 地点 / 物品 | `<project>/agent.db#kb_entry` | 扩展实体，ID 前缀 `f_` / `loc_` / `it_` |
| 字段锁 | `agent.db#kb_field_lock` | `entry_id + field`，分析/重抽取跳过锁定字段 |
| 关系 | `agent.db#kb_relation` | `rel_`，以 entryID 绑定，可跨真相源 |
| 时间线 | `agent.db#kb_timeline_event` | `tl_`，人工补录；记忆事件在读取时合并 |
| 改编审计 | `agent.db#adapt_run` | `adpt_`，记录 diff 参数、影响清单、token 成本与草稿 ID |

- 迁移：`internal/store/migrations/0002_m2_world.sql`、`0003_m3_adapt.sql`，随
  `agent.db` 惰性打开时执行，全部 `CREATE TABLE IF NOT EXISTS`，对旧库幂等。
- 隔离：每个小说项目独立 `Store` 与 `agent.db`；路由按 `X-Project-ID` /
  `projectId` / cookie 选择运行时，跨项目不共享词条、关系、锁与改编记录。
- 字段锁的读取时机：分析器每个 step 读取一次锁集，写入 `knowledge.json` 前跳过锁定字段；
  锁定字段仅允许人工通过 `PUT /api/world/entries/{id}` 修改。

## 2. M2 大世界接口

所有请求体使用 `MaxBytesReader` + `DisallowUnknownFields`；错误响应为
`{"code":"...","error":"..."}`。

### 2.1 词条列表 / 检索

`GET /api/world/entries?kind=&q=&chapter=`

- `kind`：`character|faction|location|item|setting`
- `q`：按名称、别名、摘要、字段值/键匹配
- `chapter`：只返回该章之前已出现的词条（证据引文定位，定位不到视为开场已知）

响应：`[]KBEntry`。`KBEntry` 关键字段：

```json
{
  "id": "c_xxx",
  "kind": "character",
  "name": "林砚",
  "aliases": ["小砚"],
  "summary": "备注",
  "fields": {"role": "捕快", "state": "左肩受伤"},
  "sourceRefs": [{"chapterId": "ch_xxx", "startOffset": 10, "endOffset": 20, "offsetUnit": "rune"}],
  "evidence": ["林砚走进雨巷"],
  "lockedFields": ["role"],
  "confidence": 0.9,
  "lastSeen": 2
}
```

`GET /api/world/search?q=&kind=` 等价于带 `q` 的词条检索，响应
`{"query":"...","entries":[...]}`。

### 2.2 词条详情 / 新建 / 编辑 / 删除

- `GET /api/world/entries/{id}` → `{"entry": KBEntry, "relations": [Relation]}`
- `POST /api/world/entries`（仅扩展类型）

```json
{"kind":"faction","name":"六扇门","summary":"朝廷缉查机构",
 "aliases":["六扇门衙门"],
 "fields":{"leader":"沈大人","region":"京城"},
 "sourceRefs":[{"chapterId":"ch_x","chapterVersion":1,"startOffset":0,"endOffset":6,"offsetUnit":"rune"}]}
```

- `PUT /api/world/entries/{id}`：`{"name":"可选","fields":{"role":"典史"}}`
  - 角色可编辑字段：`role/personality/appearance/relationships/state/notes`
  - 设定可编辑字段：`category/content`
  - 扩展词条允许任意业务字段
- `DELETE /api/world/entries/{id}`：人工删除；扩展词条同时清理其字段锁与关系边。
  任何 Agent 都不会调用该接口。

### 2.3 字段锁

`POST /api/world/entries/{id}/locks`

```json
{"fields":["role","state"],"lock":true}
```

- `lock=true` 加锁，`false` 解锁；返回全量 `{"id":"...","lockedFields":[...]}`。
- 字段名必须是该词条允许的字段，否则 400 `invalid_world_request`。
- 分析管道在 `analysis.merge` 中调用 `setUnlessLocked`：锁定字段不会被新分析覆盖。

### 2.4 证据跳转

`GET /api/world/entries/{id}/evidence`

```json
[{"chapterId":"ch_x","chapterIndex":1,"chapterTitle":"第一章",
  "startOffset":0,"endOffset":6,"quote":"林砚走进雨巷","source":"evidence"}]
```

- 扩展词条优先使用 `sourceRefs` 的 rune 偏移；角色/设定使用 `evidence` 引文在章节全文定位。
- `source`：`sourceRef` | `evidence`。

### 2.5 关系

- `GET /api/world/relations?entryId=&status=active|invalidated`
- `POST /api/world/relations`

```json
{"fromEntryId":"c_x","toEntryId":"loc_y","type":"located_in","label":"驻守",
 "description":"","status":"active","startChapterIndex":1,"endChapterIndex":0,
 "sourceRefs":[]}
```

- `PUT /api/world/relations/{id}`：同 POST，ID 必须在 `rel_` 前缀。
- `DELETE /api/world/relations/{id}`
- 校验：两端词条必须存在、禁止自连、`type` 必填。

### 2.6 时间线

- `GET /api/world/timeline`：合并记忆事件（`source=memory`，只读派生）与人工事件
  （`source=manual`），按章序 / `storyOrder` / ID 稳定排序。
- `POST /api/world/timeline`

```json
{"chapterId":"ch_x","title":"林砚入城","summary":"雨夜抵达县城。",
 "eventType":"arrival","participants":["c_x"],"entities":["loc_y"],"storyOrder":1}
```

- `PUT /api/world/timeline/{id}`、`DELETE /api/world/timeline/{id}`：仅人工事件可写/删。

### 2.7 第 N 章快照

`GET /api/world/snapshot?chapter=1`

```json
{"chapter":1,"entries":[...],"relations":[...],"timeline":[...],"summaries":[...]}
```

- 词条按“首次出现章”过滤；关系要求两端词条在该章已知且 `ActiveAt(chapter)`；
  时间线只含 `chapterIndex <= N` 的事件；章节摘要只含 `index <= N`。
- 首次出现章由证据引文定位近似（`introducedAt`），定位不到按第 1 章处理。

## 3. M3 改编工作台接口

### 3.1 执行选区改编

`POST /api/adapt/rewrite`

```json
{
  "chapterId": "ch_x",
  "startOffset": 100,
  "endOffset": 160,
  "instruction": "更克制一些，删去解释性语句",
  "instructionType": "polish",
  "mode": "B",
  "style": "冷峻",
  "perspective": "第三人称限知",
  "temperature": 0.7,
  "targetChars": 300
}
```

- `startOffset/endOffset`：章节正文的 **rune** 区间，左闭右开。
- `mode`：`A`（默认，不改世界状态）/ `B`（推演影响）。
- `instructionType`：`rewrite|polish|expand|shorten|style|perspective`，可空。
- 模型配置缺失 → 409 `config_missing`；输出截断 → 422 `model_truncated`；
  选区非法 → 400 `invalid_adapt_request`。

响应：

```json
{
  "run": {"id":"adpt_x","status":"done","promptTokens":9,"completionTokens":5,"totalTokens":14},
  "draft": {"id":"draft_x","targetChapterId":"ch_x","status":"pending_confirmation"},
  "chapterId": "ch_x",
  "chapterIndex": 1,
  "original": "...",
  "rewritten": "...",
  "fullContent": "...",
  "diff": "…【删】…【增】…",
  "diffParts": [{"op":"equal","text":"…"},{"op":"delete","text":"…"},{"op":"insert","text":"…"}],
  "impact": {"mode":"B","changedEntities":[],"affectedChapters":[],"foreshadows":[],"memoryEvents":[],"warnings":[]},
  "usage": {"promptTokens":9,"completionTokens":5,"totalTokens":14},
  "memoryUntouched": true,
  "memoryModeNote": "模式B：已推演影响清单，但记忆变更仍不自动提交；需经草稿确认链路"
}
```

- 结果永远写入草稿区（`drafts.json`），不自动改正文、不自动提交记忆变更。
- 模式 A 完全跳过影响推演；模式 B 影响清单包含：
  - `changedEntities`：新旧选区提及变化的知识库词条；
  - `affectedChapters`：后续章节中提及受影响词条的章节（最多 20 条）；
  - `foreshadows`：选区删除后不再包含原文的伏笔；
  - `memoryEvents`：本章既有记忆事件（提示可能需重抽取）。
- `adapt_run` 只做审计与 token 成本记录，不写正式记忆。

### 3.2 diff 预览

`POST /api/adapt/diff`

```json
{"original": "雨落到青石板上。", "rewritten": "细雨落在青石板上。"}
```

- 纯函数预览，不写草稿、不写审计、不调用模型；`rewritten` 为空 → 400 `invalid_adapt_request`。
- 响应：`{"diff":"…【删】…【增】…","diffParts":[{"op":"delete","text":"…"},…]}`。

### 3.3 改编记录

- `GET /api/adapt/runs?limit=100`：倒序返回 `adapt_run`。
- `GET /api/adapt/runs/{id}`：单条记录（ID 必须 `adpt_` 前缀）。

## 4. 错误码

| HTTP | code | 场景 |
| --- | --- | --- |
| 400 | `invalid_entry_id` / `invalid_relation_id` / `invalid_timeline_id` / `invalid_run_id` | ID 前缀或格式错误 |
| 400 | `invalid_kind` / `invalid_status` / `invalid_chapter` / `invalid_query` | 查询参数错误 |
| 400 | `invalid_world_request` / `invalid_adapt_request` | 字段、选区或模式不合法 |
| 404 | `entry_not_found` / `relation_not_found` / `timeline_not_found` / `chapter_not_found` / `run_not_found` | 目标不存在 |
| 409 | `config_missing` | 模型设置缺失（改编） |
| 422 | `model_truncated` | 模型输出被截断 |
| 502 | `model_call_failed` | 上游模型调用失败 |

## 5. 验收映射

| 验收项 | 实现位置 |
| --- | --- |
| 角色/势力/地点/物品 + 关系绑定 | `kb_entry` + `kb_relation`；`POST /api/world/relations` |
| 字段锁跳过重分析覆盖 | `store/kb.go` + `analysis/merge.go:setUnlessLocked` |
| 时间线事件绑定章节 + 第 N 章快照 | `kb_timeline_event` + `GET /api/world/snapshot` |
| 章节ID + 偏移 + 指令 + 模式 | `POST /api/adapt/rewrite` |
| 模式 B 影响清单 | `adapt/impact.go` |
| diff + 写入草稿 | `adapt/diff.go` + `store.CreateDraft` |
| 项目隔离 | 每项目独立 `agent.db`；`internal/app` 隔离测试 |
