---
id: 015
title: "Task: 上线前置资质与配置清单（HITL）"
labels: [wayfinder:task]
status: open
assignee:
blocked-by: []
---

## Question

不阻塞决策、但阻塞上线的人工配置项，需要用户执行（agent 出精确清单）：

1. 微信小程序后台：确认小程序**类目**是否覆盖本产品（教育/工具类，涉及提审）；配置 request/downloadFile/uploadFile 合法域名（备案域名 + COS 域名 `<bucket>.cos.<region>.myqcloud.com`）；确认内容安全接口可用（需小程序已认证）；
2. 腾讯云：创建 COS 桶（私有读写、地域与服务器同地域）、CAM 子账号/角色最小权限策略；
3. DeepSeek：创建 API Key，确认账户余额与限流；
4. STRIDE auth-service：为作业小程序注册 application（见 research/001 的结论）与微信凭证。

产出：wizard 式 checklist 交给用户逐项执行，完成情况记录在 Resolution（含最终域名、桶名、region 等后续 ticket 依赖的事实）。
