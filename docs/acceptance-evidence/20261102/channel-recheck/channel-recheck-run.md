# F-UPSTREAM-01 阻断层级复查 + 备选出口路径可用性（2026-11-02，无 Key，只读探针）

> 脱敏：本文件中的出口/内网 IP 与地理位置已替换为文档保留值。

> 本轮零代码改动、零配置修改、未触碰 `data/`、未发出任何带密钥的请求。目的是把「网关还能不能用」从猜测变成读数，并且专门核实 roadmap 里写着的缓解手段（`Clash 7897 代理`）今天是否还成立。

## 1. 结论

1. **`ai.laodog.top` 对本机出口 IP 是 Cloudflare 边缘 IP 级封禁，不是源站错误。** `GET /v1/models` 返回 `HTTP/2 403`，`server: cloudflare`，应答体 4545 字节的 CF 拦截页（`<title>Attention Required! | Cloudflare</title>`、`<h1 data-translate="block_headline">Sorry, you have been blocked</h1>`）。
2. **不是 10-24 那种 `1010`（浏览器指纹）规则。** 应答体里 `1010` 出现次数为 **0**，也没有 `error code` 元素；换完整 Chrome UA 结果相同（见 §3）。`1010` 是 Cloudflare 对「非浏览器客户端」的分类拦截，会显示自己的错误码；这里的形状是防火墙/IP 访问规则命中后的通用拦截页。
3. **被拦的出口 IP 与 10-24 是同一个：`198.51.100.10`（出口位置已脱敏）。** 所以这不是「换了网络」，而是 10-24 之后针对这个 IP 的封禁**新加了一层**：10-24 请求还能进到源站拿到网关自己的 `503`（账号池为空），当天 15:31 甚至拿到过真实补全；现在连源站都到不了。
4. **本机出网本身正常**，`https://www.gstatic.com/generate_204` → `204`。所以不能把 403 解读成「网络坏了」，它是目标域名侧的规则。
5. **文档里记的备选缓解路径今天不成立。** `127.0.0.1:7897` 确实在 LISTENING，但占用者是 **`com.vortex.helper.exe`（PID 10896）而不是 Clash**：HTTP 转发返回 `502`，HTTPS `CONNECT` 与 `socks5h` 都在 TLS 阶段失败（`curl exit 35`，`code=000`）。系统 `ProxyEnable=0x0`，环境变量代理全空。→ 「走 Clash 7897」这条已经在 roadmap/详设里写成三选一之一的路径，**在当前机器上是死的**，不能作为等待期间的默认解法。

对目标的影响：**G1 后半段（自动分析出知识库）与一切质量侧判据仍然没有可用通道，且候选解法从三条缩到两条半**——网关侧加白、换出口网络（换机器/换线路，不是开 7897），或直接换 DeepSeek 官方。这三条都需要 yj 侧动作，我不会自己动。
6. **但「换 DeepSeek 官方」这一条今天已证明不需要任何网络侧配合**：`api.deepseek.com` 从本机 `401`（`t≈0.34s`，解析到国内 `114.237.67.202`），两个上游转发站 `wawapii.com`/`api-c.reqtoken.com` 也都 `401` 可达；被拦的只有 `ai.laodog.top` 一个域名，而平台机 `192.0.2.60` 今天从本机**连不上**（`curl err=7`，无内网路由）。详见 §6。

## 2. 复现命令（逐条可直接执行）

```bash
# 2.1 出口 IP（与被拦那次同一台机器）
curl -s https://ipinfo.io/ip            # 198.51.100.10
curl -s https://myip.ipip.net           # 当前 IP：198.51.100.10 来自于：出口位置已脱敏

# 2.2 本机出网健康对照
curl -s -o /dev/null -w '%{http_code}\n' https://www.gstatic.com/generate_204   # 204

# 2.3 被拦目标（不带任何密钥）
curl -s -D headers.txt -o cf-403-body.html https://ai.laodog.top/v1/models       # 403 + CF 拦截页
grep -c 1010 cf-403-body.html                                                    # 0
curl -s -o /dev/null -w '%{http_code}\n' http://ai.laodog.top/v1/models           # 403（明文也一样）

# 2.4 「7897 是 Clash」这条路径的核实
netstat -ano | grep LISTENING | grep ':7897'          # TCP 0.0.0.0:7897 LISTENING 10896
tasklist //FI "PID eq 10896"                          # com.vortex.helper.exe
curl -s -o /dev/null -x http://127.0.0.1:7897 http://example.com      # 502
curl -s -x http://127.0.0.1:7897 https://www.gstatic.com/generate_204 # exit 35
curl -s -x socks5h://127.0.0.1:7897 https://www.gstatic.com/generate_204  # exit 35
curl -s -x http://127.0.0.1:7897 https://ai.laodog.top/v1/models      # exit 35 / code=000
reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings" | grep -i proxy   # ProxyEnable 0x0
```

## 3. 结果表

| 探针 | 结果 | 判读 |
|---|---|---|
| `GET /v1/models`（直连，默认 curl UA） | `HTTP/2 403`，`server: cloudflare`，`cf-ray: a54196b4e87b6704-AMS` | 边缘拦截，未进源站 |
| `GET /v1/models`（直连，完整 Chrome UA） | 同样 `403` 拦截页 | 排除「按 UA/客户端类型拦」 |
| 应答体 `1010` 计数 | `0`（也无 `error code` 元素） | 排除 10-24 的浏览器指纹规则；属 IP/防火墙规则形状 |
| `POST /v1/chat/completions`（无鉴权，直连） | 同一 CF `403` 页 | 与路径/方法无关 |
| `http://`（443 之外）同路径 | `403` | 不是 TLS/端口层问题 |
| 出口 IP | `198.51.100.10`，与 10-24 记录一致 | 封禁是在同一 IP 上新增的 |
| 出网对照 `gstatic` | `204` | 本机网络可用 |
| `127.0.0.1:7897` 归属 | `com.vortex.helper.exe`（PID 10896），**非 Clash** | 文档路径失效 |
| 经 7897 的 HTTP / HTTPS / socks5h | `502` / `exit 35` / `exit 35` | 该端口不能作出口转发 |

## 4. 与 2026-10-24 结论的关系（不推翻，只前移一层）

- 10-24 的上机定论是：**源站** `503`，根因 `openai_chat_completions.account_select_failed (pool=0)`，映射到 `gpt-5.6-sol/terra` 的 4 个账号被 Cloudflare `403 error code: 1010` 自动停用；且 `wawapii` 按出口 IP 轮询内部分组。那一层结论仍然有效，只是**现在从这台机器已经无法观测到它**——请求在到达 sub2api 之前就被 CF 边缘拦下了。
- 因此「把那 4 个账号重新置为可调度」这个动作是否已完成，本轮**无法判定**：要看到源站的 `503`/`200` 才能判。这条要等有可用出口或 yj 在网关侧自查。
- 判读红线追加一条：**以后凡是「网关挂了」的说法，必须先区分是 CF 边缘 `403` 拦截页还是源站 `503`/`404` 业务码**，两者处置方完全不同（前者归 `ai.laodog.top` 运营方/我们的出口，后者归 192.0.2.60 平台管理员）。

## 5. 明确不做

1. **不做任何绕过 Cloudflare 的动作**：不尝试直连回源 IP、不改 TLS/JA3 指纹、不做多 IP 轮换规避。被拦的是这台公司出口 IP，正当解法只有「让运营方加白」或「换合法出口/换供应商」，都归 yj 决策。
2. 不发带密钥的请求（本机也没有密钥：`data/config.json` 只有 `baseUrl/model/contextChars`）；不改 `data/config.json`；不改系统代理；不动 7897 那个进程。
3. 不据此断言上游账号池状态有无变化（见 §4 第二条）。

## 6. 候选通道从本机的可达性盘点（同轮追加，无 Key）

只发未鉴权的 `GET /v1/models`（或根路径），看**服务自己答的码**与网络层码，用来判断「哪条路只差一把 key，哪条路连网络都不通」：

| 候选 | `remote_ip` | 结果 | 判读 |
|---|---|---|---|
| `https://ai.laodog.top/v1/models` | （CF 边缘） | `403` CF 拦截页 | **被 IP 规则拦在边缘，进不了源站** |
| `https://api.deepseek.com/models`、`/v1/models`、根路径 | `114.237.67.202` | `401`，`t≈0.31–0.34s` | **网络与 TLS 全通，只是没带 key**；国内解析、延迟最低 |
| `https://wawapii.com/v1/models` | `104.21.6.133`（CF 前置） | `401`，`t≈1.06s` | 可达且**拿到了源站业务码**（10-24 从本机是 `404 分组不支持`，今天形状不同） |
| `https://api-c.reqtoken.com/v1/models` | `104.21.62.169` | `401`，`t≈2.72s` | 可达，仅无鉴权 |
| `http(s)://192.0.2.60/`、`:3000`、`/v1/models` | — | 全部 `curl err=7`（连不上） | **今天这台机器没有到平台机所在内网的路由**（10-24 能做只读 SSH 说明当时有）→ 「让管理员重启用那 4 个账号」这个动作现在**从本机也够不着** |

**这直接改变了 yj 的决策形状**：被拦的只有 `ai.laodog.top` 这一个域名，DeepSeek 官方与两个上游转发站从本机都是通的。所以候选解法里唯一**不需要任何人配合、只差一把 key** 的是「主通道换成 DeepSeek 官方」；而「走网关 + 平台机重启用账号」这条今天同时踩了两个坑（域名被 CF 边缘拦 + 平台机无路由）。

**必须连着看的边界**：`401` 只证明**可达**，不证明模型可用、价格、配额或分组路由正确。10-24 已实测 `wawapii` 按出口 IP 把同一把 key 轮询到不同内部分组，所以「今天 wawapii 答 401」不等于「带上 key 后能拿到 `gpt-5.6-sol`」；同理 DeepSeek 的 `401` 不等于模型清单与续写质量已验证——那要有 key 之后的事，也正是 S1 门票要量的东西。

## 7. 台账接线

- `docs/issues.md` 新增 **#69**（外部依赖，非产品缺陷）。
- `task/roadmap.md`：风险表 `F-UPSTREAM-01` 行写入「阻断层级前移 + 7897 路径失效」。
- 项目记忆 `model-channel-and-decisions.md`：新增 11-02 复查条目，`description` 与 `MEMORY.md` 索引同步。

## 8. 本目录文件

| 文件 | 内容 |
|---|---|
| `channel-recheck-run.md` | 本文档 |
| `cf-403-headers.txt` | CF `403` 完整响应头（含 `cf-ray`、`server: cloudflare`） |
| `cf-403-body.html` | CF 拦截页原文（4545 字节，无密钥无敏感信息） |
