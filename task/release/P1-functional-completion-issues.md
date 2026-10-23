# 问题记录

- F-BOUND-01: 四类列表（knownClues/resolutionCriteria/constraints/candidateBeats）单条文本在超长截断后由于省略号追加导致长度达到 101 rune，违反规格 <=100 rune 要求。
- F-HEAL-01: 章节删除回调失败后，单纯重启服务或新增章节不会自动补偿修复 events.json 中的旧事件；只有在后续发生另一次章节删除时才会通过最新映射完成收敛。
- F-UPSTREAM-01: 外部大模型网关与本地代理出现响应头超时、EOF 以及连接被重置，导致全书分析未能稳定完成全量汇总，续写功能已在可用连接期完成实测取证。
# 问题记录

- F-BOUND-01: 四类列表（knownClues/resolutionCriteria/constraints/candidateBeats）单条文本在超长截断后由于省略号追加导致长度达到 101 rune，违反规格 <=100 rune 要求。
- F-HEAL-01: 章节删除回调失败后，单纯重启服务或新增章节不会自动补偿修复 events.json 中的旧事件；只有在后续发生另一次章节删除时才会通过最新映射完成收敛。
- F-UPSTREAM-01: 外部大模型网关与本地代理出现响应头超时、EOF 以及连接被重置，导致全书分析未能稳定完成全量汇总，续写功能已在可用连接期完成实测取证。
