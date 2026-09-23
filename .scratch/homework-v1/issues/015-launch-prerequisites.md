---
id: 015
title: "Task: 上线前置资质与配置清单（HITL）"
labels: [wayfinder:task]
status: open
assignee: pi
blocked-by: []
---

## Question

不阻塞决策、但阻塞上线的人工配置项，需要用户执行（agent 出精确清单）：

1. 微信小程序后台：确认小程序**类目**是否覆盖本产品（教育/工具类，涉及提审）；配置 request/downloadFile/uploadFile 合法域名（备案域名 + COS 域名 `<bucket>.cos.<region>.myqcloud.com`）；确认内容安全接口可用（需小程序已认证）；
2. 腾讯云：创建 COS 桶（私有读写、地域与服务器同地域）、CAM 子账号/角色最小权限策略；
3. DeepSeek：创建 API Key，确认账户余额与限流；
4. STRIDE auth-service：为作业小程序注册 application（见 research/001 的结论）与微信凭证。

产出：wizard 式 checklist 交给用户逐项执行，完成情况记录在 Resolution（含最终域名、桶名、region 等后续 ticket 依赖的事实）。

## Progress

- 清单已产出：`docs/launch-checklist.md`（含 `.env` 模板，值填仓库根 `.env`，已在 `.gitignore`）。用 markdown 而非 bash wizard——一次性上线配置，交互脚本属过度工程；先前的 `scripts/launch-prerequisites.sh` 已删。
- 腾讯云简化为**一个子账号 + 预设策略 `QcloudCOSFullAccess`**：后端调 STS `GetFederationToken` 时用 session policy 把上传临时密钥收口到 `uploads/*`，控制台不建角色、不写自定义策略。
- 已固定的值：`WECHAT_APPID=wx5bdb4b2269d80ce7`（`miniprogram/project.config.json`）、`DEEPSEEK_BASE_URL=https://api.deepseek.com`、`DEEPSEEK_MODEL=deepseek-flash`、`AUTH_JWT_ISSUER=auth-service`、`COS_DOMAIN` 由桶名+region 推导。
- 待用户执行后回填：`API_DOMAIN`、`COS_BUCKET`/`COS_REGION`/`COS_APPID`、CAM 密钥、`WECHAT_APPSECRET`、`WX_MSGPUSH_TOKEN`/`WX_MSGPUSH_AESKEY`、`DEEPSEEK_API_KEY`、`AUTH_SERVICE_BASE_URL`/`AUTH_CLIENT_ID`/`AUTH_CLIENT_SECRET`。
