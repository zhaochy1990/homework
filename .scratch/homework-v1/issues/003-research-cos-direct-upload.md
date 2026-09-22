---
id: 003
title: "Research: 小程序媒体上传与 COS 直传/播放"
labels: [wayfinder:research]
status: open
assignee:
blocked-by: []
---

## Question

媒体方案已定：腾讯云 COS 私有桶 + 后端签发凭证、小程序直传直连（ADR 0005）。需要研究并给出**可落地的技术选型**：

1. 直传机制选型：预签名 URL（PUT）vs STS 临时凭证（cos-js-sdk / 自封装签名）在**微信小程序环境**下的可行性——`wx.uploadFile` 能否携带预签名所需的头（Content-Type、host 签名）？小程序端常见做法是什么？
2. `wx.chooseMedia` / `wx.uploadFile` 的能力边界：视频时长/大小限制、并发上传、大文件分片（打卡视频预期 ≤ 数百 MB）；
3. 私有桶对象的播放与查看：`<video>`/`<image>` 用带签名的临时 URL，URL 有效期与缓存策略；Range 拖动是否需要额外配置；
4. 上传后回调/校验模式（如何确认直传成功并落库：客户端回执 + 后端 HEAD 校验？COS 回调？）；
5. 视频封面/首帧获取的可行方案（不做转码的前提下）；
6. Go 侧 SDK 选型（tencentyun/cos-go-sdk-v5）与桶/CAM 角色最小权限设计。

输出写入 `docs/research/003-cos-direct-upload.md`，结论要给出明确推荐（选型 A/B + 理由），供 API 契约 ticket 引用。
