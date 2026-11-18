# G5 大纲/计划链路（S4）验收记录

> 日期：2026-10-23
> 规格依据：`docs/features.md` F20–F23、`docs/memory_model_spec.md` §4.5/§6.2、`task/roadmap.md` S4。
> 结论：**代码完成、自动化用例全绿、Mock 模型 UI 端到端跑通**；真实模型下「计划对齐 A/B（不跑偏、不提前收束）」未执行（与 G1–G4 同因：上游模型通道阻断）。
> G5 退出判据中「U21/U22/U23 ✅」「计划永不覆盖已确认事实」「计划回灌续写」已由自动化用例与 Mock UI E2E 覆盖；质量收益判定待真实通道恢复。

## 1. F20–F23 交付映射

| 编号 | 交付 | 实现 |
|---|---|---|
| F20 生成大纲 | `POST /api/plans/generate`：知识库/正文/既有伏笔与计划 → LLM 产出 global + volume 级 `proposed` 计划，整批提交（`memoryRevision+1`） | `internal/plan`（prompt/容错 JSON/组装）、`store.AddProposedPlans`、`server/plans.go` |
| F21 查看大纲 | `GET /api/plans`：按 `(level, parentPlanID)` 返回计划树；空库返回空树 | `store.LoadPlanTree` |
| F22 前端大纲面板 | 顶栏「大纲规划」→ 生成表单 + 计划树（全书/分卷/近期徽标、状态徽标、待复核提示）+ 激活/暂停/完成/取消操作 | `web/index.html` `planDialog`、`web/app.js` 计划模块、`web/style.css` |
| F23 计划回灌续写 | 续写开关「对齐计划」→ 仅注入 `active && !needsReview` 的 global/volume/near_term 计划；`/api/continue/preview` 同步展示；SSE `meta.planAligned/planCount` 可观测 | `novel.RenderPlanSection`、`agent.Build`、`server.handleContinue`/`memory_preview` |

## 2. API 契约

### 2.1 `POST /api/plans/generate`

请求（严格单 JSON 对象，允许空体；未知字段 400）：

```json
{"endingTone":"…≤200","avoidTropes":"…≤500","volumeCount":3,"extra":"…≤1000"}
```

- `volumeCount` 缺省 3、上限 12；模型多返回时截断，少返回时按实际数量。
- 成功：`{"plans":[...],"memoryRevision":N,"commitId":"..."}`，所有计划 `status=proposed`，ID/父子关系服务端分配。
- 错误码：`409 config_missing`（未配置模型）、`409 knowledge_not_ready`（知识库为空）、`422 invalid_plan_output`（模型输出不可解析/无分卷）、`409 analysis_running`、`422 invalid_plan`、`409 optimistic_conflict`、`500 memory_safe_state`、`502 model_error`。
- 单次模型调用超时 8 分钟；温度 0.6；重复生成不覆盖旧草案，仅追加新的 proposed 备选。

### 2.2 `GET /api/plans`

`{"memoryRevision":N,"count":N,"roots":[{"plan":{...},"children":[...]}]}`；父计划缺失的孤儿记录按根节点返回，不丢记录。

### 2.3 状态变更

- `POST /api/plans/{id}/activate`（空体或 `{}`）→ `active`。
- `POST /api/plans/{id}/status`，`status ∈ active|paused|completed|cancelled`。
- 规则：`proposed → active → paused/completed/cancelled`，`paused → active/completed/cancelled`；非法迁移 `409 plan_transition`，ID 非 `plan_` 前缀 `400 invalid_plan_id`，不存在 `404 plan_not_found`。
- 成功返回 `{"memoryRevision":N,"commitId":"...","plans":[受影响记录]}`。

### 2.4 续写/预览

- `POST /api/continue` 与 `/api/continue/preview` 新增 `usePlan`（缺省 `true`；显式 `false` 关闭）。
- 预览响应新增 `planSection`、`planEnabled`、`planCount`；续写 SSE meta 新增 `planAligned`、`planCount`。
- 计划读取失败只记日志并降级为「不注入计划」，**绝不阻断续写**。

## 3. U21/U22/U23 验收（对应 `docs/acceptance.md`）

| 编号 | 判据 | 实现与用例 |
|---|---|---|
| U21 多作用域并行 active | 不同 `(level, parentPlanID)` 作用域可各有一个 active；同作用域重复 active 拒绝 | `store.ValidateStoryPlanScopes` + `SetPlanStatus`/`AddProposedPlans` 守卫；`TestValidateStoryPlanScopes`、`TestPlanActivation...`（激活不同作用域并存） |
| U22 同作用域激活替代 | 激活新计划时，同作用域旧 active 在同一次提交内置 `superseded`，提交失败整体回滚 | `store.SetPlanStatus` → `PlanApplyResult`；`TestActivatePlanSupersedesSameScopeAndPausesChildren` |
| U23 父计划取消传播 | 取消父计划 → 其 active 后代递归 `paused`；已完成父计划不改子计划，由 reconcile 标记 `needsReview=parent_completed` | `store.SetPlanStatus` + `markCompletedParentHints`；`TestCancelPlanPausesActiveDescendants`、`TestCompletedPlanKeepsChildrenAndReconcileHints` |

补充：计划状态变更与生成均走 `CommitFormal` 式提交（既有提交协议），不新增锁序分支（仍为 `store.mu → memoryMu`）；冲突/安全态/分析中均拒绝写入。

## 4. 计划注入与事实优先级（spec §6.2）

- 渲染过滤器：仅 `Status==active && !NeedsReview`；`proposed/superseded/cancelled/paused` 与待复核计划一律不注入。
- 渲染顺序：全局 → 分卷 → 近期；预算 1200 rune、最多 8 条；超出预算截断而不是挤占记忆/正文。
- 段落头：`【计划对齐（仅约束走向，不得覆盖已确认事实）】`；每条含标题、目标、约束（≤4）、候选发展（≤5）。
- system prompt 规则 `3.2 【计划对齐】`：计划只约束未来走向，与已确认事实冲突时**以事实为准**；计划永不改写/覆盖记忆记录与已确认正文。
- 无计划时渲染为空串：`Input.Plans = nil` 与空切片输出**逐字节一致**（`TestBuildWithoutPlansKeepsBaseline`），零回归。
- 计划段位于记忆段之后、最近正文/摘要之前（`TestBuildPlanAndMemoryRulesCompose`）。

## 5. 自动化用例

| 包 | 用例 | 覆盖点 | 结果 |
|---|---|---|---|
| `internal/store` | `TestLoadPlanTreeEmpty` | 空库返回空树 | PASS |
| `internal/store` | `TestAddProposedPlansCommitsBatch` | 整批提交、revision+1、父子作用域、去重 | PASS |
| `internal/store` | `TestAddProposedPlansRejectsInvalidBatch` | 非法批次整体拒绝、零落盘 | PASS |
| `internal/store` | `TestActivatePlanSupersedesSameScopeAndPausesChildren` | 同作用域替代 + 后代暂停（U22/U23） | PASS |
| `internal/store` | `TestCancelPlanPausesActiveDescendants` | 取消递归传播（U23） | PASS |
| `internal/store` | `TestCompletedPlanKeepsChildrenAndReconcileHints` | 完成不自动改子计划，reconcile 提示 | PASS |
| `internal/store` | `TestPlanStatusTransitionsRejected` | 非法迁移、跨作用域冲突 | PASS |
| `internal/store` | `TestLoadActivePlansSkipsReview` | active 且待复核不参与注入 | PASS |
| `internal/plan` | `TestGenerateCommitsGlobalAndVolumes` | 生成 → submitted 计划、ID/父子/角色解析、revision | PASS |
| `internal/plan` | `TestGenerateRejectsMissingConfig` | `ErrConfigMissing` | PASS |
| `internal/plan` | `TestGenerateRejectsEmptyKnowledge` | `ErrKnowledgeEmpty` | PASS |
| `internal/plan` | `TestGenerateRejectsInvalidOutput` | `ErrOutputInvalid` | PASS |
| `internal/plan` | `TestBuildPlansTruncatesAndFallsBack` | 分卷截断、缺省标题、flexibility 兜底 | PASS |
| `internal/plan` | `TestParsePlanJSONToleratesFences` | 代码围栏/BOM/前后噪声容错 | PASS |
| `internal/server` | `TestPlanTreeEmpty` | 空树 HTTP 契约 | PASS |
| `internal/server` | `TestPlanGenerateActivateAndContinueAlignment` | 生成→树→激活→替代→预览→续写注入→`usePlan:false` 关闭→SSE meta | PASS |
| `internal/server` | `TestPlanGenerateErrors` | 配置缺失/知识为空/输出非法/未知字段 | PASS |
| `internal/server` | `TestPlanStatusContract` | 404/400/409 状态机与成功迁移 | PASS |
| `internal/agent` | `TestBuildInjectsPlanSectionAndRule` | 段落头/层级排序/排除 proposed 与待复核/规则 3.2 | PASS |
| `internal/agent` | `TestBuildWithoutPlansKeepsBaseline` | 无计划零回归（逐字节） | PASS |
| `internal/agent` | `TestBuildPlanAndMemoryRulesCompose` | 3.1 记忆 + 3.2 计划共存、顺序正确 | PASS |

## 6. Mock 模型 UI 端到端证据（2026-10-23）

由于上游网关阻断，使用本地 Node Mock Provider（OpenAI 兼容：`CompleteChat` 返回计划 JSON、`StreamChat` 返回 SSE 正文）走真实 HTTP + 前端路径：

1. 导入 2 章测试书 + 分析知识库（林渊/苏青/铜牌）→ 生成 2 卷规划。
2. 结果：计划树 3 条（1 全书 + 2 分卷），均「草案」，`revision 1`；重复生成追加新草案，不覆盖。
3. 激活全书计划 → 状态「生效」，卡片 `is-active`，操作变为 暂停/完成/取消；再次生成并激活新全书 → 旧全书「已取代」，旧 active 分卷「暂停」（U22/U23 联动）。
4. 关闭面板 → 「预览本次注入」：显示 `已激活计划（1 条，仅约束走向）` 与完整 `【计划对齐…】` 段；底部 `计划 1 条`。
5. 取消勾选「对齐计划」→ 预览无计划段；重新勾选 → 「开始续写」成功产出草稿正文，状态 `已生成 13 字`（Mock SSE）。
6. 未配置模型/知识库为空时，前端分别提示 `config_missing` / `knowledge_not_ready` 文案。

证据目录（本地运行产物，未入库）：`.tmp_e2e/g5ui-provider.js`、`.tmp_e2e/g5ui-provider.log`、`.tmp_e2e/g5ui-server.log`、`.tmp_e2e/g5ui-data-2/`。

## 7. 自测命令与结果

```text
go build ./...                                      # PASS
go vet ./...                                        # PASS
go test ./internal/store ./internal/plan ./internal/agent ./internal/server -count=1   # PASS
node --check web/app.js                             # PASS
```

## 8. 未完成边界（须在真实通道恢复后复核）

1. **真实模型质量未验证**：计划文本质量、A/B「不跑偏、不提前收束」需在同一停点、同一 direction 下开/关「对齐计划」各生成若干章并人工盲评；本期只有管线正确性证据。
2. **near_term 生成未开放**：存储/渲染/API 已支持 `near_term`，生成器本期仅产出 global+volume（与 F20 一致）；分章细纲按 roadmap 明确不做（P2）。
3. **无自动改计划**：正文与计划冲突时不会自动修订计划，也不自动让计划覆盖事实；用户需手动调整/暂停计划。
4. **重复生成累积草案**：每次生成追加新 proposed 备选，旧草案需手动取消；暂无「一键清理旧草案」入口。
5. **计划对齐仅影响续写 prompt**：不做硬约束校验（生成后不检测是否偏离计划），A/B 只能靠人评观测。
6. **并发编辑**：多标签页同时操作计划依靠 `optimistic_conflict` 拒绝，无实时刷新。
