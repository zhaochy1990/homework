---
id: 011
title: "Grilling: API 契约设计"
labels: [wayfinder:grilling]
status: open
assignee:
blocked-by: ["010", "003"]
---

## Question

基于敲定的数据模型与媒体直传方案，设计后端 REST API 契约：

- 端点清单：认证透传（token 刷新策略）、班级/成员/邀请、孩子/监护、教材库/班级选教材/资料上传下载签发、作业 Session CRUD/LLM 解析触发与确认、勾选/打卡/视频上传签发/卡片获取、日历/无作业日、打卡日历与 streak 查询；
- 鉴权中间件约定（JWT 本地验签，见 research/001）、错误码体系、分页与时间语义（时区：家长在哪个时区看"今天"）；
- 上传/下载 URL 签发端点的请求响应形状（依赖 research/003 的选型结论）。

产出：`docs/design/api-contract.md`，经用户确认后关闭。这是实施拆 ticket 的直接输入。
