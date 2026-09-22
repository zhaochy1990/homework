---
id: 011
title: "Grilling: API 契约设计"
labels: [wayfinder:grilling]
status: closed
assignee: claude
blocked-by: ["010", "003"]
---

## Question

基于敲定的数据模型与媒体直传方案，设计后端 REST API 契约：

- 端点清单：认证透传（token 刷新策略）、班级/成员/邀请、孩子/监护、教材库/班级选教材/资料上传下载签发、作业 Session CRUD/LLM 解析触发与确认、勾选/打卡/视频上传签发/卡片获取、日历/无作业日、打卡日历与 streak 查询；
- 鉴权中间件约定（JWT 本地验签，见 research/001）、错误码体系、分页与时间语义（时区：家长在哪个时区看"今天"）；
- 上传/下载 URL 签发端点的请求响应形状（依赖 research/003 的选型结论）。

产出：`docs/design/api-contract.md`，经用户确认后关闭。这是实施拆 ticket 的直接输入。

## Resolution

产出 `docs/design/api-contract.md`：通用约定（/api/v1、Bearer 本地验签含 aud 自校验、错误形状对齐 auth-service、offset 分页、CST 日期）、18 个稳定错误码、9 组端点清单（身份/班级成员/入班/教材库/媒体上传/资料/作业/勾选打卡/streak 无作业日日历）、三条关键时序（发作业/打卡/资料上传）。

grilling 敲定：
- Q1 加入流程：班级带 `join_approval` 开关——公开班无审批直接加入（自动 approved）、有审批走 `class_join_requests`（admin 在成员页审批）；私密班凭邀请码直接加入（邀请码即授权）。端点统一为 `POST /join-requests {classId, inviteCode?}` + `POST /join-requests/{id}/approve|reject`。数据模型已回填（classes.join_approval + class_join_requests 表）。
- Q2 LLM 解析**同步**（POST /homework/parse 阻塞返回草稿，超时 502/422，降级手动录入）。
- Q3 成员生命周期：禁踢有孩子在班成员；admin 互踢禁止（创建者可 demote 后再处理）；自主退出；解散仅限班内无孩子。
- 其余采纳设计默认：/api/v1、错误形状 {"error","message"}、offset 分页、孩子上下文入路径、上传走 upload-ticket+confirm（research/003 选型 A）、无作业日由任意成员标记、homework/parse 按上学日历自动判定 day/holiday。
