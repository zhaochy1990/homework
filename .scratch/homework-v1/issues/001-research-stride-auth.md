---
id: 001
title: "Research: STRIDE 认证服务对接契约"
labels: [wayfinder:research]
status: closed
assignee: claude
blocked-by: []
---

## Question

作业后端（Go）要对接 STRIDE auth-service（本地仓库 `/Users/zhaochaoyi/workspace/stride/auth`，源码在 `sources/dev/authentication-go`，决策记录在 `docs/adr/0001–0011`，另有 stride-devops#335 spec 的本地语境）。需要产出一份**对接契约摘要**，回答：

1. 微信小程序免密登录的精确 grant 流程：小程序端调哪些端点、传什么（wx.login code → token exchange？），拿到什么（access_token / refresh_token 结构）；
2. 签发的 JWT 的 claims 有哪些（`sub`、`aud`、自定义 claims）、签名算法、公钥如何分发（JWKS 端点？静态公钥文件？）——作业后端本地验签需要哪些配置；
3. refresh_token 轮换流程与过期语义；
4. 401/403 的错误响应约定；
5. 认证服务是否区分 application/client（ADR 0004 wechat-credentials-per-application）——作业小程序需要注册新的 application 吗，流程是什么；
6. Go 侧推荐的验签中间件做法（golang-jwt/v5）。

输出写入 `docs/research/001-stride-auth.md`，ticket 关闭时在 Resolution 里给指针与三行以内结论。

## Resolution

调研文档:`docs/research/001-stride-auth.md`(全部结论均引 stride 仓库文件路径)。

- 登录走 `POST /oauth/token` 的 `grant_type=token_exchange`(subject_token=wx.login code, subject_token_type=wechat_mini_program, body 传 client_id 即可);JWT 为 RS256,claims sub/aud(单字符串=client_id)/iss("auth-service")/exp/iat/scopes/role/membership/user_type,公钥无 JWKS,经 `GET /api/system/public-key`(JSON `{"publickey": PEM}`)或挂载的 `local/keys/public.pem` 分发,消费方须自校验 aud。
- refresh_token 一次性轮换(旧 revoke、新发 30 天滑动),小程序走 `POST /api/auth/refresh` + `X-Client-Id`;错误统一 `{"error","message"}`(401 invalid_token / 403 user_disabled 等)。作业小程序需注册新 application(admin API 建 app + `POST /admin/applications/{id}/providers` 挂 wechat appid/secret)。
- ⚠️ ADR 0011 的 `wechat_phone_bind` grant 与 ADR 0010 的 scene-scoped SMS 在本地源码快照中均未实现,对接前需确认部署版本;stride-devops#335 spec 不在本地(issue 在 GitHub tracker)。Go 侧用 golang-jwt/v5:RS256 白名单 + WithIssuer + WithExpirationRequired + 补 WithAudience,纯本地验签不查库。
