---
id: 002
title: "Research: 微信内容安全 API 接入要点"
labels: [wayfinder:research]
status: open
assignee:
blocked-by: []
---

## Question

v1 决定接入微信内容安全 API（文本 `security.msgSecCheck` 同步检测；图片/视频 `mediaCheckAsync` 异步检测）。需要弄清：

1. 两个接口的调用前置条件（小程序认证要求、服务端 access_token 获取）；
2. 频率限制与每日配额的**权威数字**（若网络受限查不到，基于既有知识给出并显著标注"需提审前验证"，禁止编造精确配额）；
3. 异步检测（mediaCheckAsync）的结果回调/轮询模式，检测中与检测不通过时媒体在业务侧应如何表现（我们的方案：检测不通过 → 对成员不可见）；
4. 检测口径：作业文本、班级名、孩子昵称、上传图片、打卡视频各走哪个接口、检测参数（scene/scene 参数取值）；
5. Go 调用要点与 access_token 管理建议（自建 token 缓存 vs 复用云调用）。

输出写入 `docs/research/002-wechat-content-security.md`。
