# 本轮变更记录

- internal/server/memory.go：可选重初始化 JSON 契约、严格输入和大小校验，先于 Store 拒绝重建。
- internal/server/memory_test.go：请求/状态矩阵、文件哈希无副作用对照及 JSON 首次初始化/续跑回归。
- docs/acceptance.md：修正全部闭环误述，限定辅助函数与 U9/U22/U33 的覆盖边界。
- docs/features.md：补齐本期记忆基础设施与未交付能力边界。
- docs/memory_model_spec.md：同步本期交付范围、请求契约、恢复实现说明及修订 R13。
- docs/issues.md：更新原 #1 裁决，记录 D01/D02 关闭依据。
- docs/acceptance-evidence/20261022/release-api-tests.txt：新增 API 回归执行结果。
- docs/acceptance-evidence/20261022/release-store-tests.txt：保存带标签两轮 store 复测结果。
- docs/acceptance-evidence/20261022/release-all-tests.txt：保存全量回归结果。
- docs/acceptance-evidence/20261022/release-http-smoke.ps1：新增隔离服务的真实 HTTP 冒烟脚本。
- docs/acceptance-evidence/20261022/release-http-smoke.json：保存 HTTP 状态/错误码/落盘哈希证据。
- docs/v1.0_release_acceptance_20261022.md：正式限定范围发布结论、证据和未覆盖项。
- task/release/P1-D01-D02-release.md：记录授权、允许范围、验收标准和完成状态。
- task/release/P1-D01-D02-release-issues.md：记录 D01/D02 的本轮解决方式。
