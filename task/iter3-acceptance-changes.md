# 迭代 3 改动清单

- task/iter3-acceptance.md：记录实现范围、自测结果、I3-01~I3-15 用例映射与未执行项
- task/iter3-acceptance-changes.md：本改动清单
- task/iter3-acceptance-issues.md：配套问题记录
- internal/novel/memory_context.go：续写记忆 DTO、预算/正文窗口计算、记忆段渲染与 rune 统计
- internal/novel/mention.go：角色/世界条目提及打分与歧义判定（检索与候选解析共享）
- internal/store/memory_retrieve.go：五类记忆只读检索、P0/近因排序、红线过滤、按预算裁剪、降级原因
- internal/store/memory_retrieve_test.go：检索层用例（P0/红线、事件截断与排序、歧义、伏笔/认知/计划、预算与确定性、三种降级）
- internal/agent/prompt.go：正文窗口扣除记忆预算、插入记忆段、条件系统规则 3.1
- internal/agent/memory_render_test.go：零记忆 golden、注入内容/顺序/规则、预算窗口、零预算退化
- internal/agent/testdata/prompt_without_memory.golden：迭代 2 零记忆提示词基线
- internal/server/server.go：`/api/continue/preview` 路由、`useMemory` 请求字段、SSE meta 记忆字段、检索失败只记日志
- internal/server/memory_preview.go：只读预览响应、召回计数、meta 载荷、严格 JSON 预览处理器
- internal/server/continue_test.go：预览契约/只读哈希、五类文件损坏、安全态续写、SSE meta 与提示词注入/开关
- web/index.html：记忆注入开关、预览面板与关闭按钮
- web/app.js：预览请求/分组渲染/统计、生成前静默刷新、SSE meta 摘要与降级提示、开关联动
- web/style.css：记忆开关、预览面板、分组/条目/标记/底部统计及窄屏适配
- docs/issues.md：追加迭代 3 台账 #29（预算切分行为变更）、#30（真实模型验收阻塞）
