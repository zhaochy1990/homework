---
id: 001
title: "Research: STRIDE 认证服务对接契约"
labels: [wayfinder:research]
status: open
assignee:
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
