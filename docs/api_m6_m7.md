# M6 图像链路 / M7 导出接口说明

本文档对应仓库路线图中的 M6（图像生成与素材库）与 M7（整书导出）。两者是
M5 之后新增的功能批次编号，与 `详设_V1.1` 里的「迭代 2/3」不是同一套编号；
功能语义以下文为准。

路由只在 `milestone.yaml` 的 `milestones.m6.enabled` / `milestones.m7.enabled`
为 `true` 时注册，关闭后访问返回 404。项目隔离方式与其他接口一致：通过
`X-Project-ID` 请求头、`projectId` 查询参数或 `project_id` Cookie 选择项目，
未指定时使用默认项目。

> 约束：图像生成只写 `image_asset` 表与项目 `images/` 目录；导出只读取正文、
> 知识库与已完成图像，生成独立快照文件。二者都不会修改正文或删除词条。

## 1. 数据与迁移

| 数据 | 位置 | 说明 |
| --- | --- | --- |
| 图像资产 / 队列 | `agent.db#image_asset` | `pending/running/done/failed`；pending 行即任务 |
| 原图 | `<项目>/images/<assetId>.<ext>` | 后端返回的原始字节 |
| 缩略图 | `<项目>/images/<assetId>_thumb.png` | 最长边 320 px，最近邻缩放 |
| 图像模型配置 | `config.json#image` | 与文本模型配置同文件、独立字段 |
| 导出记录 | `<项目>/exports/<expId>.json` | 导出元数据快照 |
| 导出文件 | `<项目>/exports/<安全标题>_<expId>.<ext>[.zip]` | 可重复下载 |

迁移文件：`internal/store/migrations/0006_m6_image.sql`，随项目 `agent.db`
惰性打开执行，`CREATE TABLE IF NOT EXISTS`，对旧库幂等。

图像任务在项目运行时启动时恢复：`running` 行会转回 `pending` 并重新入队，
避免重启后任务永久卡住。每个项目默认 2 个生成协程，单项目内按入队顺序领取。

## 2. 图像模型配置

`GET /api/config/image-model` → 公开配置（Key 脱敏）：

```json
{
  "backend": "openai",
  "baseUrl": "https://api.example.com/v1",
  "model": "flux-1-schnell",
  "styleTag": "水墨风",
  "negativePrompt": "",
  "width": 1024,
  "height": 1024,
  "steps": 20,
  "timeoutSeconds": 120,
  "hasApiKey": true,
  "apiKeyHint": "sk-…abcd",
  "apiKeyFromEnv": false
}
```

`PUT /api/config/image-model`：字段同上（不含 `hasApiKey/apiKeyHint/apiKeyFromEnv`），
另支持写 Key 的 `apiKey` 与 `clearApiKey`。规则：

- `backend` 缺省或 `openai`：Base URL 填到 `/v1`，客户端追加 `/images/generations`；
  `sdwebui`：Base URL 填根路径，客户端追加 `/sdapi/v1/txt2img`；其他值 400。
- Base URL 非空时必须是 `http://` 或 `https://` 地址。
- 缺省与约束（`ImageConfig.Normalized`）：宽高 256–2048（缺省 1024），
  steps 1–80（缺省 20），timeout 10–600 秒（缺省 120）。
- Key 优先读取环境变量 `IMAGE_API_KEY`，其次使用保存值；图像通道**不**复用文本
  Key，以便文本与图像指向不同服务。
- 当前为双分支适配：真实可用的后端取决于用户自填的服务；仓库不自带模型或 Key。

## 3. 图像生成

### 3.1 资产类型

| `kind` | 别名 | 必填 | 提示词组成 |
| --- | --- | --- | --- |
| `character` | — | `entryId` | 名称 + 外貌字段 + 当前状态 + 摘要 + 半身立绘/干净背景 |
| `scene` | `location`、`setting` | `entryId` | 名称 + 摘要/内容/地域 + 场景全景/电影感构图 |
| `item` | — | `entryId` | 名称 + 摘要/效果 + 物品特写/静物摄影 |
| `illustration` | `chapter` | `chapterId`（可选 `entryId`） | 章节标题 + 末尾 300 字场景摘录 + 叙事插画/宽幅构图 |
| `cover` | — | — | 书名 + 简介 + 书籍封面构图/留白标题区 |

提示词按「画风锚 + 类型内容 + 负面词」组装；请求里的 `styleTag/negative/prompt`
分别覆盖配置、追加负面、整体替换正向提示。负面词始终追加通用负面
（低质量/模糊/文字/水印等）。

### 3.2 预览

`POST /api/images/preview-prompt`

```json
{"kind": "character", "entryId": "kb_xxx", "styleTag": "水墨风"}
```

只组装提示词，不校验模型配置、不入队、不调用模型。响应：

```json
{
  "positive": "水墨风，林越，青衣束发，…，半身立绘，干净背景",
  "negative": "低质量, 模糊, 变形, …",
  "features": ["青衣束发", "…"],
  "coreFeatures": ["青衣束发"],
  "styleTag": "水墨风",
  "shot": "半身立绘"
}
```

### 3.3 提交生成

`POST /api/images/generate`

```json
{
  "kind": "illustration",
  "chapterId": "ch_xxx",
  "width": 1024,
  "height": 576,
  "steps": 24,
  "seed": 12345,
  "styleTag": "可选，覆盖配置",
  "negative": "可选，追加负面词",
  "prompt": "可选，整体覆盖正向提示"
}
```

- 返回 HTTP 200 与 `ImageAsset`，其中 `status` 为 `pending`，表示**已入队**；
  生成由后台协程异步执行。
- 入队前校验：`kind` 合法、对应词条/章节存在、模型配置已填。`openai` 后端要求
  `baseUrl` 与 `model`；`sdwebui` 只要求 `baseUrl`。缺配置返回 400
  `请先在「图像模型」中填写 Base URL 与模型名`。
- 宽高/steps 缺省取配置值并夹到上述范围；`seed=0` 时若存在基准图，沿用基准图 seed。
- 提示词快照持久化在 `image_asset.prompt_json`，后续重试不重新组装。

失败原因写在资产的 `error`；重试使用 `POST /api/images/{id}/retry` 重新入队。

### 3.4 资产查询与文件

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/images/assets?kind=&entryId=&chapterId=&status=&limit=` | `{"assets": [...], "pending": 2}` |
| GET | `/api/images/{id}` | 单条资产 |
| GET | `/api/images/{id}/file` | 原图；未生成时 404 |
| GET | `/api/images/{id}/thumb` | 缩略图（PNG）；未生成时 404 |
| POST | `/api/images/{id}/anchor` | 设为同范围唯一基准图 |
| POST | `/api/images/{id}/retry` | 重置为 pending 并唤醒队列 |
| DELETE | `/api/images/{id}` | 删除记录并清理磁盘文件 |

- 文件响应带 `Cache-Control: private, max-age=300`；资产尚未 `done` 时文件 404。
- 基准图按范围唯一：插图按章节，其他类型按 `kind + entryId`；设为基准会清除同范围
  旧基准。基准图作为后续同一范围生成的参考图（若后端支持图像输入）并优先复用 seed。
- `limit` 缺省 200，上限 2000。

### 3.5 一致性评分与审计

生成完成后计算结构化一致性分数（`consistencyScore`，0–1）：

- 当前实现是**结构化代理**，`visualProxy` 恒为 `true`，说明中固定注明
  「视觉相似度未接入，按结构化特征代理」。评分 = 0.6×核心特征命中 +
  0.3×特征代理 + 0.1×画风一致；有基准图时按与基准图的核心特征重叠计算。
- 未接入视觉嵌入模型，因此该分数**不能**当作视觉相似度结论。
- 每次生成/失败写入 `agent_run`（AgentKey `image`，名称「图像一致性 Agent」，
  权限 `observe`），可在 Agent 中心查看成本与耗时。
- 分数低于 0.75 时写一条 A8 审计报告（`image_consistency`，<0.6 为 critical），
  只报告、不改稿。

## 4. 导出（M7）

### 4.1 创建导出

`POST /api/export`

```json
{"format": "epub", "title": "可选覆盖书名", "includeImages": true}
```

- `format`：`txt` / `md` / `epub`，其他值 400。
- `includeImages` 只对 `txt/md` 生效：为 true 时输出 `<doc>.zip`，内含正文文件与
  `images/<assetId>.<ext>`，正文中以相对路径引用图片。
- EPUB 固定内嵌章节插图、封面与资产画廊（人物/场景/物品），不受
  `includeImages` 影响。
- 没有可导出章节时 400。

响应为导出记录：

```json
{
  "id": "exp_xxx",
  "format": "epub",
  "title": "测试作品",
  "fileName": "测试作品_exp_xxx.epub",
  "sizeBytes": 182734,
  "chapterCount": 3,
  "imageCount": 2,
  "includeImages": true,
  "createdAt": "2026-11-19T08:00:00Z"
}
```

导出是快照：之后修改正文或图像不会改变已生成文件；需要新内容请重新导出。

### 4.2 列表与下载

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/exports` | `{"exports": [Record]}`，按创建时间倒序 |
| GET | `/api/exports/{id}/download` | 下载文件；记录或文件缺失时 404 |

下载响应带 `Content-Disposition: attachment; filename*=UTF-8''<escaped>` 与
`Cache-Control: no-store`。

### 4.3 EPUB3 结构

```text
mimetype                      # 第一个条目，Stored 不压缩
META-INF/container.xml
OEBPS/content.opf             # uuid 书号、dc:title、dcterms:modified
OEBPS/nav.xhtml               # EPUB3 导航
OEBPS/style.css
OEBPS/cover.xhtml             # 基准封面优先，其次任一封面图
OEBPS/chapter-<n>.xhtml       # 每章一节，插图以 <figure> 内嵌
OEBPS/gallery.xhtml           # 人物/场景/物品资产画廊
OEBPS/images/<assetId>.<ext>
```

## 5. 功能开关查询

`GET /api/features` 返回当前项目里程碑与派生能力：

```json
{"m1": true, "m2": true, "m3": true, "m4": true, "m5": true,
 "m6": true, "m7": true, "images": true, "export": true}
```

`images` 要求 M6 打开且项目图像服务已装配；`export` 等于 M7。
前端用它决定是否显示「图像中心」「导出」入口。

## 6. 错误约定

| 状态 | 场景 |
| --- | --- |
| 400 | 请求体格式错误、`kind` 非法、缺 `entryId/chapterId`、模型配置缺失、导出格式非法、无章节 |
| 404 | 资产/词条/章节/导出记录/导出文件不存在，或里程碑关闭导致路由未注册 |
| 500 | 读盘、写库、生成文件等内部错误（响应为通用中文提示，详情写日志） |
| 503 | 里程碑开启但项目未装配对应服务（防御性检查，正常装配下不会出现） |
