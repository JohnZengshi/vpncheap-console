# Display Label 只换显示，不换 Clash Name

节点表里 `xboard_96b35930b860b2e5` 这类 ID 是 VPNCheap 从 xboard 面板生成的代理 ID，是 Clash API 调用（selectNode/testOne/probe）的 key，不可改不可删。我们给用户看的 Display Label 从 `server:port` 派生，只换显示层，不动 API 调用层。

替代方案「直接改节点名」会让 PUT/GET 调用全部 404，因为 API 用 Clash Name 当 key。
