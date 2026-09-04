# VPNCheap Console

小型本地控制台，驱动 VPNCheap 的 Clash 兼容 API（sing-box 内核暴露的 clash_api）。列出节点、测延迟、切节点/模式、流式流量、列连接、启停 macOS VPN 隧道。只跟 localhost 说话。

## Language

**Console**: 这个 Go 程序本身，长驻 loopback HTTP 服务。
_Avoid_: 控制台（歧义）

**Kernel**: VPNCheap 内嵌的 sing-box 进程，随隧道连接而启动，暴露 Clash API 于 127.0.0.1:9090。隧道断开时不存在。
_Avoid_: 内核（口语可，文档用 Kernel）

**Tunnel**: macOS NetworkExtension VPN，由 scutil --nc 控制。连上后 Kernel 才启动、Clash API 才可达。
_Avoid_: 代理（歧义，见下）

**Proxy**: Clash 的 selector 节点名（如 proxy、auto）。是路由选择器，不是 VPN 连接本身。"关闭代理" 在本语境指断开 Tunnel，不是改 selector。
_Avoid_: 节点（指具体 server 时用 Node）

**Clash Name**: VPNCheap 生成的代理 ID（形如 xboard_96b35930b860b2e5），是 Clash API 调用的 key，不可改、不可删。
_Avoid_: 节点名（歧义：指 ID 还是显示名？）

**Display Label**: 给用户看的节点标签，从 server:port 派生（如 jp04.kozmsl-szims.top:16278）。找不到 server 时回退到 Clash Name。只换显示，不动 Clash Name。

**Node**: 一台具体 server，由 Clash Name 标识、Display Label 展示。
