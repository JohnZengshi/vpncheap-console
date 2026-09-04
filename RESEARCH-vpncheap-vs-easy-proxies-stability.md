# VPNCheap vs easy_proxies：代理逻辑链路与稳定性对比

> 目的：分析两者代理链路实现差异，解释"VPNCheap 稳定 / easy_proxies 不稳定"的根因，
> 为"是否自己写内核"决策提供依据。
> 方法：一手资料对比——VPNCheap 运行配置 + easy_proxies 源码 + sing-box 源码。

## 结论先行

VPNCheap 稳定的根因是**它直接用 sing-box 原生的出站协议栈 + 原生 urltest/selector**，
连接建立、TLS 握手、多路复用、会话保持全部由 sing-box 内核处理——这是久经考验的
C/Go 级实现。而 easy_proxies 的不稳定来自**它在 sing-box 之上又自己造了一层
"池调度 + 健康检查 + 连接包装"**（`pool.go`），这层用纯 Go 的 net.Conn 包装实现
retry/blacklist/probe，引入了 sing-box 原生不存在的故障面。

两者差异可以用一句话概括：
- **VPNCheap = sing-box 原生能力 + 订阅驱动配置**（稳定来自内核成熟）
- **easy_proxies = sing-box 节点 + 自研池调度层**（不稳定来自自研层）

---

## 一、连接建立链路对比

### VPNCheap（稳定）

VPNCheap 的每个节点是 sing-box 的**原生 outbound**，连接建立完全由 sing-box 内核处理：

anytls 节点样本（占 46/64，主力协议）：
```json
{"type":"anytls","tag":"node_0","server":"...","server_port":16278,
 "password":"...","domain_resolver":"local",
 "tls":{"enabled":true,"insecure":false,"alpn":["h2","http/1.1"],
        "server_name":"zhengshu12.fengxingxunjie.com"}}
```

关键稳定性来源：
1. **anytls 协议自带会话复用**：sing-box 的 anytls outbound（`protocol/anytls/outbound.go`）
   用 `anytls.Client`，配置了 `IdleSessionCheckInterval`/`IdleSessionTimeout`/`MinIdleSession`——
   即**空闲会话保持 + 最小空闲会话数**，连接复用是协议级内置的，不是外挂。
2. **TLS 由 sing-box tls 库处理**：`tls.NewClient` 用的是 sing-box 成熟的 TLS 栈
   （含 ALPN h2/http1.1 协商），不是手写 `crypto/tls.Dial`。
3. **出站选择用原生 urltest**：`{"tag":"auto","type":"urltest","outbounds":[...62节点]}`
   ——sing-box 原生的 urltest 自动测速选优，测速逻辑、超时、间隔全由内核处理。
4. **全局 selector**：`{"tag":"proxy","type":"selector","outbounds":["auto","node_0",...]}`
   把 urltest(auto) 作为 selector 的第一个成员，默认走 auto（自动选优）。

VPNCheap 的连接路径（无中间层）：
```
TUN 入站 → route.rules 匹配 → selector(proxy) → urltest(auto) → anytls outbound → sing-box 内核建连
```

### easy_proxies（不稳定）

easy_proxies 不是直接用 sing-box 的 selector/urltest，而是**自己注册了一个
`pool` 类型的自定义 outbound**（`internal/outbound/pool/pool.go`）：

```go
const Type = "pool"  // 自定义 outbound 类型
func Register(registry *outbound.Registry) {
    outbound.Register[Options](registry, Type, newPool)
}
```

它的连接路径（多一层自研）：
```
mixed 入站 → route.rules → proxy-pool(pool自定义类型) → 自研 pickMember → sing-box outbound → sing-box 内核建连
                        ↑ 自研层：retry/blacklist/wrapConn
```

自研层做了什么（`DialContext`，pool.go:280）：
```go
func (p *poolOutbound) DialContext(...) (net.Conn, error) {
    maxAttempts := p.maxAttempts()
    for attempt := 1; attempt <= maxAttempts; attempt++ {
        member, err := p.pickMemberFiltered(...)  // 自研选节点
        conn, dialErr := member.outbound.DialContext(...)  // 调 sing-box outbound
        if dialErr != nil {
            p.recordFailure(member, dialErr)  // 自研故障计数
            continue  // retry 下一个
        }
        p.recordSuccess(member)
        return p.wrapConn(conn, member), nil  // ← 包装连接
    }
}
```

**不稳定的三个故障面全在这一层：**

#### 故障面1：连接包装 wrapConn（pool.go:638）
```go
func (p *poolOutbound) wrapConn(conn net.Conn, member *memberState) net.Conn {
    return &trackedConn{Conn: conn, ...}  // 包装成 trackedConn
}
```
sing-box 原生 outbound 返回的 `net.Conn` 被包装成 `trackedConn`，用于跟踪 active 计数。
这个包装层在 `Close()` 时做 `decActive`——**如果包装层的 Close 逻辑有 bug 或时序问题，
连接复用、会话保持都会受影响**。而 sing-box 原生 selector 不做这层包装，连接生命周期
完全由内核管理。

#### 故障面2：自研健康检查 probe（pool.go:682+）
```go
func httpProbe(conn net.Conn, host string) (time.Duration, error) {
    req := fmt.Sprintf("GET /generate_204 HTTP/1.1\r\nHost: %s\r\n...", host)
    conn.Write([]byte(req))
    reader.ReadByte()  // 等 TTFB
}
```
easy_proxies 的健康检查是**自己手写 HTTP probe**（`GET /generate_204`，手动 ReadByte），
而不是用 sing-box 原生的 urltest 测速机制。这引入：
- 手写 HTTP 不如 sing-box 内核的 HTTP 客户端健壮（连接复用、超时处理差异）
- probe 用的是**新建连接**（`member.outbound.DialContext`），不等于真实流量质量
- probe 的 watchdog（`probeMember` 里的 ctx watchdog）额外复杂度，处理"节点接受TCP但不HTTP响应"
  的边界情况——这种边界情况本身就是自研层才需要操心的

对比 sing-box 原生 urltest：测速逻辑在内核里，用真实 HTTP 客户端，连接管理成熟。

#### 故障面3：自研 blacklist + retry
```go
func (p *poolOutbound) recordFailure(member *memberState, cause error) {
    failures, blacklisted, until, transient :=
        member.shared.recordFailure(cause, p.options.FailureThreshold, ...)
    // 连续失败N次 → 拉黑 24h
}
```
- **黑名单 24h 过长**：节点临时抖动（如 429 限流）可能导致连续失败被拉黑 24h，
  期间该节点完全不可用。VPNCheap 的 urltest 无黑名单机制，节点恢复后立即可选。
- retry 是"拨号失败换节点"，但**在包装层换节点会中断当前连接的会话**——
  而 sing-box 原生 selector 切换是 selector 级，不破坏已建立的连接复用。

---

## 二、DNS 与分流链路对比

### VPNCheat（精准且成熟）

```json
"servers": [
  {"tag":"default","detour":"proxy","type":"udp","server":"1.1.1.1"},
  {"tag":"local","type":"udp","server":"223.5.5.5"},
  {"tag":"remote","type":"fakeip","inet4_range":"198.18.0.0/15"}
],
"rules": [
  {"clash_mode":"global","server":"default"},
  {"clash_mode":"direct","server":"local"},
  {"rule_set":"geosite-cn","server":"local"},
  {"query_type":["AAAA"],"action":"reject"},
  {"query_type":["A"],"server":"remote"}  // A记录走fakeip
]
```

稳定性来源：
1. **fakeip**：域名解析成 198.18.x.x，路由用 fakeip 地址匹配，**避免域名规则的歧义**
   （但这也让域名路由规则匹配不到真实域名——见 FINDING 系列的副作用）
2. **detour: proxy**：default DNS（1.1.1.1）经 proxy 出站解析，**DNS 本身加密且稳定**
3. **rule_set 分流**：geosite-cn/geolocation-cn/!cn 用 MetaCubeX 规则集（.srs 二进制），
   精准且自动更新（24h）
4. **AAAA reject**：纯 IPv4 策略避免 IPv6 的不稳定性

### easy_proxies

easy_proxies 配置（`runtime/config.yaml`）：
```yaml
dns:
  server: 223.5.5.5  # 单一国内 DNS
  fallback_servers: [8.8.8.8, 1.1.1.1]
```
- **无 fakeip**：域名解析走真实 DNS，路由用 domain_suffix 匹配——
  但真实 DNS 可能被污染，domain_suffix 匹配不如 fakeip 精准
- **无 detour**：DNS 解析不经代理出站，国内直连 DNS 对国外域名解析质量不稳
- **无 rule_set**：分流靠 china_direct_enabled（内置 geosite-cn 快照），
  规则不如 VPNCheap 的远程 rule_set 实时

---

## 三、协议层对比

### VPNCheap 用什么协议（为什么稳）

64 个节点：anytls 46 / hysteria2 2 / shadowsocks 14
- **anytls**（占 72%）：新协议，**TLS 内多路复用 + 空闲会话保持**，
  `IdleSessionCheckInterval`/`MinIdleSession` 配置让会话复用是协议级内置。
  这意味着**频繁请求不重建连接**，延迟低、抗抖动。
- **hysteria2**：QUIC-based，抗丢包好，但只有 2 个（非主力）
- **shadowsocks**：aes-128-gcm，成熟稳定

### easy_proxies 节点来源

easy_proxies 节点来自 VPNCheap 的订阅（`runtime/easy_proxies-config.yaml`）：
```yaml
subscriptions:
  - "https://siusn-sisjxl.top/v7mK2pQ9xL4n/..."  # 同一个订阅
```
**节点本身和 VPNCheap 是同一批**（同一订阅）。所以"easy_proxies 网络质量差"
不是节点差，而是**easy_proxies 处理这些节点的链路差**——自研池层破坏了
sing-box 原生的会话复用和连接管理。

---

## 四、架构差异的本质：深度对比

| 维度 | VPNCheap | easy_proxies |
|---|---|---|
| 出站选择 | sing-box 原生 urltest（内核） | 自研 pool outbound（外挂） |
| 连接建立 | sing-box outbound 直连 | 经 wrapConn 包装层 |
| 会话复用 | anytls 协议级内置 | 受 wrapConn Close 逻辑影响 |
| 健康检查 | urltest 原生测速 | 自研 httpProbe（手写 HTTP） |
| 故障处理 | 无黑名单，自然恢复 | blacklist 24h + retry |
| DNS | fakeip + detour + rule_set | 真实 DNS + domain_suffix |
| 节点来源 | 订阅直驱 | 同一订阅（节点相同） |
| 深度 | 浅（内核原生） | 深（自研层堆叠） |

**用 codebase-design 的词汇**：VPNCheap 的代理模块是**浅模块**——大量行为
（建连/TLS/复用/测速/选优）藏在 sing-box 内核这个小接口背后。easy_proxies 是
**深模块**——把池调度、健康检查、连接包装这些本可由内核处理的行为自己实现了一遍，
增加了故障面。

---

## 五、对"自己写内核"决策的含义

这是这份分析要服务的决策。关键推论：

### 5.1 easy_proxies 的不稳定是"自研层"造成的，不是"独立 sing-box"路线本身的问题

节点同源（同一订阅），但 easy_proxies 在 sing-box 之上加了 pool.go 这层自研调度，
破坏了原生会话复用。**如果自己写内核时不造这个轮子，直接用 sing-box 原生的
urltest + selector + 原生 outbound，稳定性应该接近 VPNCheap。**

### 5.2 "自己写内核"的正确做法（如果走这条路）

不要复刻 easy_proxies 的 pool.go。正确架构是：
1. **直接用 sing-box 原生 outbound**（anytls/hysteria2/ss），不包装
2. **用 sing-box 原生 urltest 做自动选优**，不自研 probe
3. **用 sing-box 原生 selector 做手动选择**，不自研 retry
4. **配置 route.rules 做域名路由**（sing-box 原生能力，见 easy_proxies builder.go:290）
5. **每节点开一个 mixed inbound**（sing-box 原生，easy_proxies builder.go:330 已示范）
   ——但这要用 sing-box 的 multi-port inbound，不是自研包装
6. DNS 复用 VPNCheap 的 fakeip + detour + rule_set 模式

即：**把自己写的代码降到"配置生成器 + sing-box 生命周期管理"，不碰连接/调度/测速**。
sing-box 原生已经提供了 urltest(选优) + selector(选择) + route.rules(域名路由) +
mixed inbound(每节点端口) 全部能力。自己写内核 = 写一个正确的 sing-box 配置 + 托管它，
而不是重写 pool.go。

### 5.3 风险点

即便不复刻 pool.go，"自己写内核"仍有风险：
1. **anytls 会话复用的配置参数**（IdleSessionTimeout 等）需正确设置，
   否则可能不如 VPNCheap 调得好
2. **DNS fakeip + 域名路由的交互**：fakeip 会让 domain_suffix 规则匹配不到
   （见 FINDING-0004 排查2），需用 sniff 或 domain_matcher 解决
3. **NetworkExtension vs 普通 sing-box 进程**：VPNCheap 的稳定部分来自
   macOS NE 的 TUN 实现（gvisor 栈）；普通 sing-box 进程的 TUN 需要权限，
   可能不如 NE 稳定
4. **订阅刷新时机**：VPNCheap 有 app 内置的订阅验证/刷新；自己要复刻这套

### 5.4 最小可行验证（决策前）

在投入"自己写内核"之前，可以用一个**最小实验**验证可行性：
取 VPNCheap 的订阅，生成一份只含 anytls 节点 + urltest + fakeip DNS 的
sing-box 配置，直接跑一个独立 sing-box 进程（不用 easy_proxies，不用 pool.go），
测稳定性和延迟。如果这个"裸 sing-box + 正确配置"稳定，说明路线可行；
如果不稳，说明问题在节点本身或 NE 之外不可复现的稳定性。

这个实验就是 /prototype 该做的事——回答"裸 sing-box + VPNCheap 节点能多稳"。
