# 上线前置清单（v1）

> Ticket 015。在控制台逐项执行，值填进仓库根目录 `.env`（已在 `.gitignore`）。全部完成后用它关闭 015，并把最终域名 / 桶名 / region 记进 Resolution。

## `.env` 模板

复制到仓库根目录 `.env`，边做边填。`[固定]` 的已填好，其余待填。

```dotenv
# 后端服务
ENV=dev
HTTP_ADDR=:8080
INVITE_TOKEN_SECRET=                        # 监护人邀请 token 的 HMAC 密钥，随机长串

# MySQL（本地开发 docker compose up -d mysql；生产填实际库）
DB_HOST=127.0.0.1
DB_PORT=3309
DB_USER=homework
DB_PASSWORD=homework
DB_NAME=homework
DB_ROOT_PASSWORD=root

# 腾讯云 COS
COS_BUCKET=               # 完整桶名，形如 homework-1250000000
COS_REGION=               # 例 ap-guangzhou
COS_APPID=                # 桶名后缀那串数字
COS_DOMAIN=               # 推导：<bucket>.cos.<region>.myqcloud.com
TENCENTCLOUD_SECRET_ID=
TENCENTCLOUD_SECRET_KEY=

# 微信小程序
WECHAT_APPID=wx5bdb4b2269d80ce7      # [固定] 见 project.config.json
WECHAT_APPSECRET=
API_DOMAIN=               # 后端备案域名，例 https://homework.example.com
WX_MSGPUSH_TOKEN=
WX_MSGPUSH_AESKEY=

# DeepSeek
DEEPSEEK_API_KEY=
DEEPSEEK_BASE_URL=https://api.deepseek.com   # [固定]
DEEPSEEK_MODEL=deepseek-flash                # [固定] 实测本账号可用模型：deepseek-flash / deepseek-v4-pro

# STRIDE auth-service
AUTH_SERVICE_BASE_URL=
AUTH_CLIENT_ID=
AUTH_CLIENT_SECRET=
AUTH_JWT_ISSUER=auth-service                 # [固定]
AUTH_JWT_AUDIENCE=                           # = AUTH_CLIENT_ID
AUTH_JWT_PUBLIC_KEY_URL=                     # = <AUTH_SERVICE_BASE_URL>/api/system/public-key
AUTH_JWT_PUBLIC_KEY_FILE=                    # 可选：URL 不可达时的本地 PEM fallback
```

## 1. 腾讯云 COS（私有桶 + 一个子账号）

- [ ] COS 控制台 → 创建存储桶：名称自取（如 `homework`），地域与后端同地域，权限「私有读写」。复制完整桶名 → `COS_BUCKET` / `COS_REGION` / `COS_APPID`。
- [ ] CAM 控制台 → 新建子用户（编程访问），命名 `homework`，附加预设策略 `QcloudCOSFullAccess`。
- [ ] 该子用户「API 密钥」页创建密钥 → `TENCENTCLOUD_SECRET_ID` / `TENCENTCLOUD_SECRET_KEY`。

后端的 HEAD 校验、下载签名、清理都用这个子账号；给小程序的上传临时密钥由后端调 STS `GetFederationToken` + session policy 收口到 `uploads/*`。**已验证**：这个子账号挂 `QcloudCOSFullAccess` 就能直接调 `GetFederationToken`（实测成功签发临时密钥），控制台无需再加策略、也不用建角色。

## 2. 微信小程序（认证 / 类目 / 域名 / 消息推送）

- [ ] mp 后台确认主体已认证；服务类目覆盖产品（建议「教育 > 教育信息服务」）。
- [ ] 开发管理 → 开发设置：复制 AppSecret → `WECHAT_APPSECRET`（AppID 已固定）。
- [ ] 服务器域名：request = `API_DOMAIN` 与 `https://<COS_DOMAIN>`；uploadFile、downloadFile = `https://<COS_DOMAIN>`。
- [ ] 开发管理 → 消息推送：启用，URL = `<API_DOMAIN>/api/v1/wx/callback`，JSON 明文模式；Token / EncodingAESKey 随机生成 → `WX_MSGPUSH_TOKEN` / `WX_MSGPUSH_AESKEY`。回调 handler 上线前无法通过校验，可留到最后配。

## 3. DeepSeek

- [ ] 拿 API Key → `DEEPSEEK_API_KEY`；确认余额与限流。
- [ ] `/models` 实测本账号只有 `deepseek-flash` 与 `deepseek-v4-pro`（没有 `deepseek-chat` / `deepseek-reasoner`）——默认解析用 `deepseek-flash`，难例用 `deepseek-v4-pro`。research/004 与 `docs/design/homework-parsing.md` 已同步。

## 4. STRIDE auth-service

- [ ] 用 admin Bearer 调 `POST <base>/admin/applications`，body `{"name":"homework-miniprogram","redirect_uris":[],"allowed_scopes":["openid","profile"]}` → `AUTH_CLIENT_ID` / `AUTH_CLIENT_SECRET`。
- [ ] `POST <base>/admin/applications/{id}/providers`，body `{"provider_id":"wechat","config":{"appid":"<WECHAT_APPID>","secret":"<WECHAT_APPSECRET>"}}`。
- [ ] `AUTH_JWT_AUDIENCE` = `AUTH_CLIENT_ID`；`AUTH_JWT_PUBLIC_KEY_URL` = `<base>/api/system/public-key`。
- ⚠️ research/001：`wechat_phone_bind` grant 可能未部署；微信绑定走不通先与 auth 侧对齐。
