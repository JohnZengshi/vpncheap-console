# 可行性分析：vpncheap-console 能否实现 easy_proxies 式代理池（核心仍是 VPNCheap）

> 生成方式：只读调查，未改任何代码、未停止任何运行中进程。
> 调查时间：2026-09-04
> 对照项目：`/Users/john/MY_PROJECT_2026/easy_proxies`

## 一、结论（先行）

**不可行。** 在「不改代码、不停运行中进程、代理核心进程仍是 VPNCheap」这三个约束下，
vpncheap-console **无法**实现类似 easy_proxies 的「无账号密码、负载均衡、健康检查、失败重试」代理池。

根因有三条，任何一条都足以阻断：

1. **VPNCheap 的 sing-box 没有开本地代理入站端口**（TUN 模式，`mixed-port:0`），
   客户端无处接入——而 easy_proxies 的池子本质就是一个 `127.0.0.1:2323` 入站端点。
2. **vpncheap-console 本身不开代理端口**，它只是 VPNCheap Clash API（9090）的遥控器 + Web UI，
   监听的是 `127.0.0.1:18090`（控制台 HTTP，非代理）。
3. **easy_proxies 的「VPNCheap 直启」模式并不复用 VPNCheap 的内核**，
   它是另起一个 `easy_proxies` 二进制（自带 sing-box）。所以「像 easy_proxies」与「核心仍是 vpncheap」
   在 easy_proxies 自身的设计里就是互斥的。

vpncheap-console 现在能做的，只是「**手动/一键切换到最快单节点**」（`/best` 端点），
这是**单节点选择**，不是 easy_proxies 那种**每连接轮转 + 黑名单 + 失败重试**的池子。

---

## 二、两个项目的架构对照

### 2.1 VPNCheap 的 sing-box（真正的"代理核心"）

证据（均为只读探活，未停止任何进程）：

```
$ curl http://127.0.0.1:9090/version
{"meta":true,"premium":true,"version":"sing-box 1.12.25"}

$ curl http://127.0.0.1:9090/configs
{"port":0,"socks-port":0,"redir-port":0,"tproxy-port":0,"mixed-port":0,
 "allow-lan":false,...,"tun":null,"mode":"Rule","mode-list":["Rule","direct","global"]}

$ scutil --nc list | grep vpncheap
* (Connected) 1B4CD633-... VPN (com.vpncheap.macnative) "VPNCheap VPN"
```

要点：
- VPNCheap 跑的是 **sing-box 1.12.25**，Clash API 开在 `127.0.0.1:9090`（无认证）。
- **所有代理入站端口全为 0**（`mixed-port:0`、`port:0`、`socks-port:0`、`allow-lan:false`）。
- 流量接管走的是 **macOS NetworkExtension / TUN 系统级 VPN 隧道**（`scutil --nc` 显示 Connected），
  系统 Web 代理未设置（`networksetup -getwebproxy` 无 Server/Port）。
- `proxy` 出站是 `Selector` 类型（**单选**，一次只走一个节点），当前选 `xboard_f3c602e0f5a5c084`，
  成员约 17+ 个 `xboard_*` 节点。

端口扫描佐证（只读，未用作出网代理）：

```
port 7890: http=000 socks5=000   # 无
port 1080: http=000 socks5=000   # 无
port 2323: http=000 socks5=000   # 无（easy_proxies 默认口，未开）
port 9090: http=200              # 只有 Clash API
```

→ **VPNCheap 内核没有提供任何本地 HTTP/SOCKS 代理端点供"池子"挂接。**

### 2.2 vpncheap-console（当前项目）

证据（源码 `cmd/vpncheap-console/main.go`、`best.go`、`lifecycle.go`）：

- `main.go`：把 `/api/*` 反向代理到 Clash API（9090），另加 `/best`、`/labels`、`/exit`、`/tunnel`、`/health`，
  静态文件走内嵌 `web/`。监听 `127.0.0.1:18090`（**强制 loopback**）。
- `best.go`：`bestHandler` 拉取 `proxy` selector 的全部成员，并发调用 Clash 的
  `/proxies/{name}/delay` 探测，**选延迟最低的一个**，`PUT /proxies/proxy` 切过去。**单节点，固定。**
- `lifecycle.go`：autostart VPNCheap、scutil 起/停隧道、watchClash 重连。**不含任何代理入站逻辑。**

运行中进程（只读 `pgrep`，未停止）：

```
30148 ./vpncheap-console -addr 127.0.0.1:18090 -clash http://127.0.0.1:9090
30152 /Applications/VPNCheap.app/Contents/MacOS/VPNCheap
```

→ vpncheap-console 是**控制面**，不是**数据面**。它能切节点、测延迟、看连接，**但不能当代理用**。

### 2.3 easy_proxies（参考项目）

证据（`README.md`、`runtime/proxy-chain-macos.sh`、`runtime/easy_proxies-config.yaml`、`config.example.yaml`）：

- 它是**独立的 sing-box 代理池管理器**，自己起 sing-box，开 HTTP/SOCKS 入站：
  - `listener: 127.0.0.1:2323`（池入口，支持 HTTP/SOCKS5，可无认证：`username/password` 留空）
  - `multi_port: 127.0.0.1:24000+`（每节点一端口）
  - `management: 127.0.0.1:9091`（WebUI/API）
- 池调度：`sequential/random/balance/latency`，带 `failure_threshold`、`blacklist_duration`、
  `retry_enabled/retry_attempts`、可选 `sticky`（按来源 IP 固定出口）。
- 「**VPNCheap Direct Startup**」模式（`runtime/proxy-chain-macos.sh` 的 `start_direct`）：
  读 VPNCheap 的 plist 订阅状态（`com.vpncheap.macnative.plist`），
  用 VPNCheap 的订阅 URL 重新生成一份 `easy_proxies-config.yaml`，
  然后**启动 `easy_proxies` 二进制**（它自己就是个 sing-box）。
  - `runtime/easy_proxies-config.yaml` 里直接用了 VPNCheap 的订阅：
    `subscriptions: ["https://siusn-sisjxl.top/v7mK2pQ9xL4n/..."]`
  - README 原文（§5）："mode starts only `easy_proxies`, **never `proxypool`** ...
    launcher reads installed VPNCheap client state, writes ... `runtime/easy_proxies-config.yaml`,
    runs in `hybrid` mode: `127.0.0.1:2323` for proxy pool ..."
  - README 又强调："All listeners bind `127.0.0.1` with no proxy authentication."

→ **关键：easy_proxies 的"直启"是借 VPNCheap 的订阅，不是借 VPNCheap 的内核。**
它跑的是自己的 sing-box 进程。所以"代理核心进程依然是 vpncheap"这条约束，
和 easy_proxies 自身的实现方式是直接冲突的。

---

## 三、逐条分析：为什么三个约束下不可行

| easy_proxies 能力 | vpncheap-console 现状（不改代码） | 差距 |
|---|---|---|
| 本地无认证代理入站 `127.0.0.1:2323`（HTTP/SOCKS） | **无任何代理入站**；VPNCheap `mixed-port:0`，console 18090 是控制台非代理 | 致命缺失 |
| 每连接负载均衡（sequential/random/balance/latency） | `/best` 只选一个最快节点并固定；Clash `Selector` 本身是单选 | 无轮转机制 |
| 健康检查 + 黑名单 + 失败重试切下一节点 | Clash API 只能测单节点延迟；无黑名单、无拨号失败自动重试 | 无 |
| 粘性会话（按来源 IP 固定出口） | 无 | 无 |
| GeoIP 地域分流出口 | 无（仅有 `Rule/direct/global` 三模式） | 无 |
| 订阅自动刷新热重载 | 无（依赖 VPNCheap 自身刷新，不可控） | 无 |
| 多端口/混合模式 | 无 | 无 |

**逐条阻断点：**

1. **没有代理端口可接入**（致命）。easy_proxies 池子的价值就是"一个稳定端点背后轮转多个上游"。
   VPNCheap 走 TUN，`mixed-port:0`；vpncheap-console 监听 18090 是 HTTP 控制台。
   客户端 `curl -x http://127.0.0.1:???/` 没有任何可用端口。这无法靠"不改代码"补上。

2. **`Selector` ≠ 池**。VPNCheap 的 `proxy` 出站是 `Selector`（一次一个），
   不是 `URLTest`（自动测速选优）也不是 easy_proxies 那种**每请求轮转**的池。
   Clash API 能 `PUT` 切选哪个，但这是"手动切节点"，不是"透明负载均衡池"。

3. **easy_proxies 自身也不复用 VPNCheap 内核**。它的"直启"模式是另起 `easy_proxies` 进程。
   所以"核心仍是 vpncheap" + "像 easy_proxies"在 easy_proxies 的设计里就是互斥目标——
   它要么自己当核心，要么不当池子。

4. **VPNCheap 的 sing-box 配置不可运行时改**。配置在 app bundle / NetworkExtension 内，
   由订阅重新生成。即便想加一个 `mixed` 入站或把 `Selector` 改 `URLTest`，
   也属于改 VPNCheap 的配置/代码，违反"不改代码"且会被 app 覆盖。

---

## 四、vpncheap-console 现有能力的真实定位

vpncheap-console **不是代理池**，它是 VPNCheap 的**单节点控制台 + 一键测速**：

- `/best`：并发测全部节点延迟，切到最低——**一次性选优，固定单节点**，之后不会自动轮转。
- `/api/proxies/{name}/delay`：测单个节点延迟（透传 Clash）。
- `/tunnel`：起/停 macOS VPN 隧道。
- `/exit`：查当前出口 IP 归属。
- `/labels`：把 `xboard_*` 映射成人话标签。

它解决的是"VPNCheap 没有好用的节点切换 UI"这个问题，**不解决"要一个本地代理端口"这个问题**。

---

## 五、若要实现"类 easy_proxies 池子"，需要什么（仅供规划，本次不执行）

按从轻到重列出，**均超出本次"不改代码"约束**：

1. **用 easy_proxies 本体**（最快，但核心不是 vpncheap）：
   直接跑 easy_proxies 的 `proxy-chain-macos.sh start`，它读 VPNCheap 订阅、起自己的 sing-box，
   池口 `127.0.0.1:2323` 无认证。代价：**代理核心变成 easy_proxies，不再是 VPNCheap**——违反约束。

2. **给 VPNCheap 加 mixed 入站**（核心仍是 vpncheap，但要改 VPNCheap 配置/代码）：
   改 VPNCheap 的 sing-box 配置加 `mixed` 入站 + 把 `Selector` 改 `URLTest`，
   可得到"本地端口 + 自动选优"。但无 per-connection 轮转、无黑名单重试，且配置会被 VPNCheap 重生成覆盖。

3. **在 vpncheap-console 里加一个代理入站 + 调度器**（改本项目代码）：
   console 起一个 `mixed` 入站，每来一个连接，按策略（latency/round-robin）通过 Clash API
   切 `proxy` selector 的当前选择，再让流量走 TUN 出去。
   - 致命难点：TUN 是**系统全局**的，无法把"某条入站连接"精准路由到"某个上游节点"，
     因为出站选择是 selector 级、全系统共享，不是 per-connection。
   - 这正是 easy_proxies 不复用宿主 sing-box、而是自己起一份的根本原因。

**结论不变**：在不改代码、不停进程、核心仍是 VPNCheap 的前提下，无法得到 easy_proxies 式代理池。
最接近的现有能力是 vpncheap-console 的 `/best`（一键切最快单节点），但那是"选优"不是"池"。

---

## 六、证据索引

| 事实 | 来源 |
|---|---|
| VPNCheap=sing-box 1.12.25, Clash API 9090 | `curl http://127.0.0.1:9090/version` |
| 代理入站全 0, TUN 模式 | `curl http://127.0.0.1:9090/configs` |
| 隧道 Connected, 走 NetworkExtension | `scutil --nc list` |
| 无本地代理端口 | 端口扫描 7890/1080/2323/24000 全 000 |
| `proxy` 是 Selector, 17+ xboard 节点 | `curl http://127.0.0.1:9090/proxies` |
| console 进程在跑 | `pgrep -fl vpncheap-console` → PID 30148 |
| console 是反代+UI, 无代理入站 | `cmd/vpncheap-console/main.go` |
| `/best` 是单节点选优 | `cmd/vpncheap-console/best.go` |
| easy_proxies 直启另起 sing-box | `runtime/proxy-chain-macos.sh` (start_direct) |
| easy_proxies 用 VPNCheap 订阅 URL | `runtime/easy_proxies-config.yaml` |
| easy_proxies 池口 2323 无认证 | `README.md` §5 + `runtime/config.yaml` |

---

## 七、补充调查（2026-09-04 追加，因 easy_proxies 核心质量差这条反馈触发）

在第一轮结论后，用户反馈"easy_proxies 核心太差，网络质量不好"，否定了"方案1：
直接套用 easy_proxies"。为此深挖 VPNCheap 实际 sing-box 配置，找到一个此前
误判的关键证据，但实测后结论性质未变。

### 7.1 关键新证据：VPNCheap 配置里确实写过 mixed 入站

配置文件 `~/Library/Group Containers/group.com.novamindllc.vpncheap/macos_vpn_config.json`
（6 月 27 日，20284 字节）的 inbounds 段：

```json
[
  {"type":"tun","tag":"tun-in","auto_route":true,"address":["172.19.0.1/30"],"stack":"gvisor"},
  {"type":"mixed","tag":"mixed-in","listen":"0.0.0.0","listen_port":10081}
]
```

且 outbounds 结构远优于预期：
- 65 个出站：anytls 46 / hysteria2 2 / shadowsocks 14 / direct 1
- `urltest`（tag=`auto`，**自动测速选优**，含 node_0..N）+ `selector`（tag=`proxy`，手动选）
- `route.final = proxy`，`experimental.clash_api.external_controller = 127.0.0.1:9090`
- `default_mode = "Enhanced"`

→ **第一轮把"无代理入站"归为根因一，是被 Clash API `/configs` 返回的 `mixed-port:0`
  误导了**——那是 Clash 风格映射，不反映 sing-box 实际 inbounds。配置文件证明
  VPNCheap *设计上*是开了 `mixed:10081` 的。

### 7.2 但实测：10081 当前不监听 → 代理端口仍不可用

```
$ lsof -nP -iTCP:10081 -sTCP:LISTEN   # 空，无监听
$ curl -x http://127.0.0.1:10081 http://cp.cloudflare.com/generate_204
http_code=000  time=0.000178s          # 立即拒绝，无端口
```

当前实际监听（只读 lsof，排除系统级无关服务）：

```
vpncheap-console  30148  127.0.0.1:18090 (LISTEN)   # 控制台
# 系统扩展 com.vpncheap.macnative.tunnel (PID 1093, root) 跑 sing-box
# 但 9090(由它开)可达, 10081 不可达
```

系统扩展二进制 `com.vpncheap.macnative.tunnel`（65.2MB）内嵌完整 sing-box + Tailscale，
strings 可见 `external_controller`/`default_mode`/`clash_mode`/`InboundRegistry`，
**证明它运行时确实加载 sing-box 配置**，但当前这份运行配置**没有把 10081 起起来**。

### 7.3 为什么 10081 在配置里却没监听？两种可能（均未证伪）

1. **配置已过时**：该文件 6 月 27 日，VPNCheap 此后多次启动/订阅刷新可能用新配置，
   未必保留 mixed-in。用户最近用的节点已变（plist 显示当前 `xboard_f3c602e0f5a5c084`
   TW-台湾1-UDP，与配置文件 node 列表未必同步）。
2. **NetworkExtension 沙盒限制**：system extension 以 root 跑，但 NEProvider
   通常只跑 tun 入站；mixed 入站可能被 NE 框架在运行时过滤掉，或 VPNCheap 客户端
   在生成实际配置时按"是否开系统代理"动态决定要不要写 mixed-in。

**无论哪种，结果一样：当前运行中的 VPNCheap 没有可用本地代理端口。**
第一轮的"根因一（无代理入站）"结论方向正确，只是依据应修正为
"运行时未启用 mixed-in"而非"配置里没有"。

### 7.4 对用户真实意图的重新理解

用户否掉 easy_proxies 后，真实诉求是：**保留 VPNCheap 的好核心（sing-box 1.12.25
+ 65 出站 + urltest 自动选优），只想要一个本地无认证代理端口 + 池化调度**。

这在"不改代码"约束下仍不可行，但**性质从"架构不可行"降级为"配置/运行时未启用"**：

- VPNCheap 配置里**已经具备 mixed 入站 + urltest 自动选优**这两块拼图（见 7.1），
  说明能力在配置层面是存在的，只是当前运行实例没激活。
- 若能让运行中的 VPNCheap 启用 `mixed-in`（哪怕端口换一个），就能得到：
  `curl -x http://127.0.0.1:????/` 一个本地无认证代理口，背后是 `urltest` 自动选优。
- 但"启用"本身需要 VPNCheap 重新加载配置/重启 → 违反"不停运行中进程"；
  且该配置文件由 VPNCheap app 生成覆盖，外部改写不可靠 → 需改 VPNCheap 行为。

### 7.5 修正后的结论

| | 第一轮（误） | 修正后 |
|---|---|---|
| 根因一依据 | 配置无 mixed 入站 | 配置**有** mixed:10081，但**当前运行实例未监听** |
| 结论性质 | 架构层不可行 | 仍是不可行（受三约束），但瓶颈降为"运行时未启用"+无法外部触发 |
| easy_proxies 方案 | 列为方案1 | **作废**（用户否决：核心差） |

**最终判断（三约束不变）：仍不可行，但接近可行了。** 唯一缺的拼图是"让运行中的
VPNCheap 启用 mixed-in 入站"，而这件事在"不改代码/不停进程"下做不到——
它需要 VPNCheap app 重新加载配置。一旦放宽"不停进程"这一条（允许重启 VPNCheap），
且 VPNCheap 版本/配置支持显式开启 mixed 入站，本地无认证代理口即可用，
背后自动挂上 65 出站的 urltest 选优——这就是 VPNCheap 原生的"选优池"，
虽仍不等同 easy_proxies 的"每连接轮转+黑名单"，但已能满足"无账号密码本地代理口 + 自动选好节点"。
