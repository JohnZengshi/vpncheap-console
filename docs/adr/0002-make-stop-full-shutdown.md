# make stop = 全套停（tunnel + app + console）

`make stop` 是 `make run` 的镜像：断 Tunnel（scutil --nc stop）→ 退 VPNCheap.app（osascript quit）→ 停 Console（SIGTERM via pidfile）。不拆成 stop-console/stop-vpn，也不跳过任何一步。

理由：Console 的 SIGTERM 上下文必须先跑清理（断 tunnel + 退 app）再自退，否则会留 tunnel 连着 / VPNCheap 残留。拆开会让用户只停一半留残局。Console 无脑退 VPNCheap（不记 startedByUs）——跟「关闭代理=断 Tunnel」的 CONTEXT.md 语义一致。
