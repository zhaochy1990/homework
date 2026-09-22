# 调研:STRIDE 认证服务(auth-service)对接契约

> Ticket: 001 · 日期: 2026-09-22
>
> 研究对象:本地仓库 `/Users/zhaochaoyi/workspace/stride`(下文引用路径均相对该根目录)。
> 核心源码:`auth/sources/dev/authentication-go/`;决策记录:`auth/docs/adr/0001–0011`;部署语境:`stride-devops/local/`。
> 语境文档:`auth/CONTEXT.md`(领域词汇表,微信身份 = (appid, openid) 对)。

---

## 1. 微信小程序免密登录:grant 流程

### 1.1 唯一登录路径:`POST /oauth/token` + `grant_type=token_exchange`(RFC 8693)

微信小程序登录以 OAuth2 Token Exchange grant 暴露,而不是自定义端点(ADR 0001,`auth/docs/adr/0001-wechat-login-token-exchange.md`)。请求(支持 `application/x-www-form-urlencoded` 和 `application/json` 两种格式,ADR 0003 `auth/docs/adr/0003-oauth-token-content-type.md`):

```
POST /oauth/token
Content-Type: application/x-www-form-urlencoded   (或 application/json)

grant_type=token_exchange
client_id=app_xxxxxxxxxxxxxxxxxxxxxxxx      ← 公开客户端,body 传 client_id 即可,无需 secret
subject_token=<wx.login() 拿到的 js_code>
subject_token_type=wechat_mini_program
scope=...                                   ← 可选;省略则取 application 的 allowed_scopes
```

依据:`internal/handlers/token_exchange.go`(常量 `wechatSubjectTokenType = "wechat_mini_program"`)、`internal/handlers/oauth2.go`(`tokenRequest` 结构体,`ShouldBind` 双格式)。

服务端流程(`internal/handlers/token_exchange.go` `handleTokenExchange`):

1. 校验 `subject_token` / `subject_token_type` 存在且类型正确,否则 400;
2. 解析调用方 application:有 HTTP Basic(`client_id:client_secret`)则用 Basic 身份,否则读 body 的 `client_id`(公开客户端风格);app 不存在 → 404 `application_not_found`,非活跃 → 403 `application_not_active`;
3. 读该 application 的**微信 provider 配置**(`auth_app_providers` 行,`provider_id='wechat'`,JSON `{"appid":"...","secret":"..."}`);缺失/不完整 → 400 `wechat_not_configured`;
4. 用该 appid/secret 调微信 `code2Session`(`internal/wechat/wechat.go`,端点 `sns/jscode2session`,测试可用 `wechat_code2session_url` 覆盖);
5. 按 openid(键 = 该 provider 配置解析出的 appid + openid)查 `auth_user_wechat_links`(ADR 0002)。

### 1.2 返回

**已绑定身份 → 200 标准令牌响应**(`internal/handlers/oauth2.go` `oauthTokenResponse`):

```json
{
  "access_token": "<JWT, RS256>",
  "refresh_token": "<64 位 hex 随机串>",
  "token_type": "Bearer",
  "expires_in": 3600,
  "scope": "openid profile ..."
}
```

**注意:登录响应不含 user 对象** —— 客户端需再调 `GET /api/users/me` 获取身份(响应含 `wechat_bound` 字段)。这是 ADR 0001 明确的契约(取代了 stride-devops#173 "登录响应带用户对象" 的旧约定)。

**未绑定身份 → 400 `{"error":"wechat_needs_binding"}`**,客户端转入绑定流程(见 1.3)。`needs_binding` 是正式术语(`auth/CONTEXT.md`)。

**微信侧错误映射**(`internal/wechat/wechat.go` `mapWechatError`):`40029`→400 `wechat_invalid_code`;`45011`→429 `wechat_rate_limited`;`40013`→400 `wechat_invalid_appid`;其余→400 `wechat_api_error`。

### 1.3 绑定流程(两种,注意版本差异)

- **email + password 绑定**(已实现):复用同一个 `token_exchange` grant,附加 `email` + `password` 扩展参数。凭据错误 → 401 `invalid_credentials`;身份已被其他账号绑定 → 409 `wechat_already_bound`;成功 → 直接签发令牌(绑定即登录)。
- **`wechat_phone_bind` grant(ADR 0011,`auth/docs/adr/0011-wechat-phone-bind-grant.md`)**:新 grant,`subject_token`(wx.login code)+ `phone` + `code`(`bind_phone` 场景 SMS),把微信身份绑到该手机号对应的账号,手机号无人持有时**自动注册手机号账号**,响应额外带 `registered: true` 作为一次性 onboarding 信号。

> **重要 caveat**:ADR 0011 状态为 accepted,但**本地源码快照中尚未出现该 grant** —— `internal/handlers/oauth2.go` 的 `Token` switch 只有 `authorization_code / client_credentials / refresh_token / password / token_exchange` 五个分支,全仓 grep `wechat_phone_bind` 无命中(快照内 SMS 绑定路径是:已登录会话调 `POST /api/users/me/phone`,`internal/handlers/sms.go` `BindPhone`)。对接前需确认部署版本是否已包含该 grant;若没有,小程序首次绑定的可行路径只有 email+password(手机号账号没有密码,走不通)或先 SMS 注册再在已登录态绑微信(代码中无此端点)。**这是一个需要尽早和 auth 侧对齐的缺口。**

### 1.4 客户端识别细节

- `X-Client-Id` header:`/api/auth/*` 系列端点(如 `/api/auth/sms/*`)用它识别 application(`internal/middleware/middleware.go` `ClientApp`);`/oauth/token` 的 token_exchange 不需要它(body 的 `client_id` 足够),但发 `/api/users/me` 时无所谓(走 Bearer)。
- `client_id` 格式:`app_` + 24 位 hex(`internal/auth/auth.go` `GenerateClientID`)。

---

## 2. JWT:claims、签名算法、公钥分发

### 2.1 用户 access token claims(`internal/auth/auth.go` `AccessClaims`)

```json
{
  "sub": "<user uuid>",
  "aud": "<client_id, 单个字符串,即签发时的 application>",
  "iss": "auth-service",
  "exp": 1758000000,          // unix 秒,签发 + jwt_access_token_expiry_secs
  "iat": 1757996400,
  "scopes": ["openid", "..."], // 数组,来自 application.allowed_scopes 与请求 scope 的交集
  "role": "user",              // "user" | "admin"
  "membership": "regular",     // 会员档位(domain.MembershipTier,缺省按 regular)
  "user_type": "regular",      // 账号用途分类
  "name": "..."                // 可选(omitempty)
}
```

`aud` 是**单字符串**(不是数组),自定义 claims 用 snake_case —— 注释明确这是公开 JWT 契约,不能改。另有 client_credentials 令牌(`AppClaims`):只有 `sub`(app id)、`iss`、`exp`、`iat`、`grant_type:"client_credentials"`,无 `aud`,与用户令牌区分。

### 2.2 签名算法与密钥

- **RS256(RSA-SHA256)**,验证时强制 `jwt.WithValidMethods([]string{"RS256"})`(`internal/auth/auth.go` `VerifyAccessToken`)—— 算法混淆攻击已被服务端防住,我们侧同样必须固定白名单。
- RSA 密钥对从磁盘 PEM 加载:配置键 `jwt_private_key_path` / `jwt_public_key_path`(`internal/config/config.go`;`internal/auth/auth.go` `NewJWTManager`)。
- 默认配置(`auth/sources/dev/authentication-go/config.yml`):`jwt_issuer: "auth-service"`、`jwt_access_token_expiry_secs: 3600`(1 小时)、`jwt_refresh_token_expiry_days: 30`。

### 2.3 公钥分发:**无 JWKS,两个途径**

1. **HTTP 端点**:`GET /api/system/public-key`(公开,无需鉴权),返回 JSON `{"publickey": "<PEM, PKIX 格式, BEGIN PUBLIC KEY>"}`(`internal/handlers/system.go` `GetPublicKey`)。
2. **静态 PEM 文件**:部署时密钥对放在 `stride-devops/local/keys/{private,public}.pem`(由 `local/scripts/bootstrap.sh` 用 openssl 生成),消费方以只读卷挂载。

### 2.4 作业后端本地验签所需配置

| 配置项 | 值 | 来源 |
|---|---|---|
| 公钥 | PEM(PKIX)文件,或启动时从 `GET /api/system/public-key` 拉取缓存 | `system.go`、`PublicKeyPEM()` |
| issuer | `auth-service`(必须校验) | `config.yml` `jwt_issuer` |
| audience | 我们小程序 application 的 `client_id`(**必须自行校验**) | 见下 |

> 关键点:auth-service 自己验签时**不校验 aud 值**("no expected audience is configured",`internal/auth/auth.go` `VerifyAccessToken` 注释),只要求 aud 非空。所以"这个 token 是发给谁的"完全靠**资源服务器侧**校验 aud。同仓库的兄弟 Go 服务就是这么做的 —— `stride-devops/local/docker-compose.yml` 中 `stride-api` 配置:`STRIDE_WORKER_API_AUTH_ISSUER: "auth-service"`、`STRIDE_WORKER_API_AUTH_AUDIENCE: "${AUTH_CLIENT_ID},${WECHAT_MINIPROGRAM_CLIENT_ID}"`、`STRIDE_WORKER_API_AUTH_PUBLIC_KEY_PATH: "/keys/public.pem"`。**这就是作业后端应照抄的三件套**(注意它的 aud 支持逗号分隔多值,因为同一 API 服务多个前端)。

---

## 3. refresh_token 轮换与过期语义

### 3.1 两个等价端点

| 端点 | 客户端认证 | 依据 |
|---|---|---|
| `POST /oauth/token`,`grant_type=refresh_token` | HTTP Basic 必须(`client_id:client_secret`)——非 token_exchange 的 grant 都要求 Basic(`internal/handlers/oauth2.go` `Token`) | `handleRefreshTokenGrant` |
| `POST /api/auth/refresh`,body `{"refresh_token": "..."}` | `X-Client-Id` header(无 secret) | `internal/handlers/auth.go` `Refresh` |

小程序作为公开客户端,适合走 `/api/auth/refresh`。两者都调同一个 `auth.RotateRefreshToken`(`internal/auth/auth.go`)。

### 3.2 轮换语义(`internal/auth/auth.go` `RotateRefreshToken`)

- refresh_token 是 **64 位 hex 随机串**(32 字节 crypto-random),服务端只存 SHA-256 hash(`HashToken`),不存明文;
- 查 hash → 已撤销 → 401 `token_revoked`;app 不匹配 → 401 `invalid_token`;已过期 → 401 `refresh_token_expired`;
- **一次性轮换**:旧 token 立即 revoke,签发全新 token;scopes 原样继承,device_id 透传;
- 过期时间:**每次轮换重新计算 30 天**(从新 token 存入时刻起 `+expiryDays`),即滑动过期,活跃用户不会被迫重新登录;
- 成功响应 = 标准令牌对(新 access + 新 refresh + `expires_in`)。

### 3.3 撤销

- `POST /oauth/revoke`(RFC 7009,Basic 认证,**无论成败恒返回 200**);`POST /api/auth/logout`(X-Client-Id,body `{"refresh_token": ...}`)—— 两者底层同 `RevokeRefreshToken`(`internal/handlers/oauth2.go` `Revoke`、`internal/handlers/auth.go` `Logout`)。
- 客户端策略:小程序端存好 refresh_token,access 过期(401 `invalid_token`)时静默调 refresh;收到 401 `token_revoked` / `refresh_token_expired` 时清空本地会话走重新登录。

---

## 4. 错误响应约定

统一 JSON 形状(`internal/apperror/apperror.go` 包注释 + `internal/middleware/middleware.go` `RespondError`):

```json
{ "error": "<稳定机器码>", "message": "<人类可读信息>" }
```

**不是** OAuth RFC 6749 的 `error`/`error_description` 形状,也没有 `WWW-Authenticate` header —— 对接方按 `error` 字符串分支,不要按 RFC 惯例解析。`error` 码是稳定契约(`apperror.go` 注释:"stable machine-readable code")。非 `*apperror.Error` 的未知错误统一收敛为 500 `internal_error`(不泄露细节)。

与鉴权相关的关键码:

| HTTP | error | 场景 |
|---|---|---|
| 400 | `wechat_needs_binding` | 微信身份无账号 → 走绑定(ADR 0001) |
| 400 | `wechat_not_configured` | application 未配微信 provider(ADR 0004) |
| 400 | `wechat_invalid_code` / `wechat_invalid_appid` | code2Session 失败(`wechat.go`) |
| 400 | `bad_request` | 参数缺失/格式错(message 带细节) |
| 401 | `invalid_token` | JWT 无效/过期/签名错(**本地验签失败返回这个最一致**) |
| 401 | `token_revoked` / `refresh_token_expired` | refresh 轮换被拒 |
| 401 | `invalid_credentials` | Basic 认证失败、密码错 |
| 401 | `unauthorized` | 缺 Bearer |
| 403 | `forbidden` | 已认证但无权限(如非 admin) |
| 403 | `user_disabled` | 账号被禁用(注意:不是 401,客户端不应循环刷新) |
| 403 | `application_not_active` | application 被停用 |
| 404 | `application_not_found` / `user_not_found` | |
| 409 | `wechat_already_bound` / `phone_already_bound` | 绑定冲突(产品明确不做账号合并) |
| 429 | `rate_limited` | IP 滑窗限流(middleware,同形状) |
| 503 | `service_unavailable` | Redis 等依赖不可达,故意 fail-closed |

**401 vs 403 的语义**:401 = 身份未建立(token 缺失/无效/撤销,或凭据错);403 = 身份已建立但不许可(角色、禁用、app 停用)。作业后端中间件应遵循同一划分。

---

## 5. 作业小程序需要注册新 application 吗?—— 需要,两步

auth-service 是 IDaaS:**每个客户端 application 一份自己的凭证与微信配置**(ADR 0004 `auth/docs/adr/0004-wechat-credentials-per-application.md`;app 级 `wechat_app_id`/`wechat_app_secret` 列已在 stride-devops#187 移除,唯一来源是 provider 配置表)。同一微信身份键 = (该 application 的 appid, openid),所以新小程序必须有自己的 application,否则 openid 空间就和现有小程序混在一起。

注册流程(admin API,均需 admin 用户 Bearer —— `AdminAuth` 中间件要求 `role=admin`,`internal/middleware/middleware.go`):

1. **建 application**:`POST /admin/applications`,body `{"name":"homework-miniprogram","redirect_uris":[],"allowed_scopes":[...]}`。响应返回 `client_id`(`app_<24hex>`)和**仅此一次明文**的 `client_secret`(服务端只存 SHA-256 hash,`internal/handlers/admin.go` `CreateApplication`)。小程序是公开客户端,secret 可以不用,但记录在案。
2. **挂微信 provider**:`POST /admin/applications/{id}/providers`,body `{"provider_id":"wechat","config":{"appid":"<小程序 appid>","secret":"<小程序 secret>"}}`(`internal/handlers/admin.go` `AddProvider`;重复挂会 400)。**微信 secret 明文落库**(code2Session 需要原文,是 ADR 0004 记录的已知 trade-off),admin API 永不回显。
3. 需要时 `PATCH /admin/applications/{id}` 调整 `allowed_scopes`(token_exchange 的 scope 是请求 scope 与它的交集,`token_exchange.go` `respondTokenExchange`)。

本地环境的等价做法是直接 SQL 插 `auth_applications` 行(`stride-devops/local/scripts/bootstrap.sh` 对 admin-dashboard 的注册),但走 admin API 是正规路径。**作业侧需要提前准备**:小程序 appid/secret、admin 账号(或让 auth 管理员代配),以及把 `client_id` 作为我们后端 JWT aud 校验的白名单值。

---

## 6. Go 侧推荐验签中间件(golang-jwt/v5)

auth-service 自己就是用 `github.com/golang-jwt/jwt/v5`(`internal/auth/auth.go` import),我们用同一库可以逐字段对齐。推荐做法 = 复刻服务端 `VerifyAccessToken` 的校验集,并**补上它不做的 aud 校验**:

```go
type AccessClaims struct {
    Sub        string   `json:"sub"`
    Aud        string   `json:"aud"`
    Iss        string   `json:"iss"`
    Exp        int64    `json:"exp"`
    Iat        int64    `json:"iat"`
    Scopes     []string `json:"scopes"`
    Role       string   `json:"role"`
    Membership string   `json:"membership"`
    UserType   string   `json:"user_type"`
    Name       string   `json:"name,omitempty"` // 作业侧可自行决定是否解析
}
// 实现 Claims 接口的五个 getter,照抄 internal/auth/auth.go。

func ParseAccessToken(token, pubKeyPEM, expectedAud string) (*AccessClaims, error) {
    key, err := jwt.ParseRSAPublicKeyFromPEM([]byte(pubKeyPEM)) // 启动时解析一次,复用
    if err != nil { return nil, err }
    claims := &AccessClaims{}
    _, err = jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return key, nil },
        jwt.WithValidMethods([]string{"RS256"}),  // 必须:算法白名单,防 alg 混淆
        jwt.WithIssuer("auth-service"),           // 必须:iss 固定
        jwt.WithExpirationRequired(),             // 必须:强制 exp
        jwt.WithAudience(expectedAud),            // auth-service 自己不校验,消费方必须补
    )
    if err != nil { return nil, err }
    if claims.Sub == "" || claims.Iat == 0 { return nil, errors.New("missing required claims") }
    return claims, nil
}
```

中间件形态(参照 auth-service 自身 `internal/middleware/middleware.go` `AuthenticatedUser` 的分层):

1. `Authorization: Bearer <jwt>` 缺失 → 401 `{"error":"unauthorized"}`;解析/校验失败 → 401 `{"error":"invalid_token"}` —— 错误形状与 auth-service 完全一致(第 4 节),前端/网关不用区分两套错误语义;
2. 通过后把 `sub / aud / scopes / role` 放进 request context(等价于它的 `ctxUserID/ctxClientID/ctxScopes`);
3. **纯本地验签,每请求不查库**:auth-service 对普通用户端点也不回查用户(`AuthenticatedUser` 只在 admin 路径回查 isActive);JWT 1 小时过期本身就是撤销边界 —— 我们拿到的 token 由 auth 签发,禁用/撤销状态由 auth 服务自己在用户调 auth 端点时把关,作业 API 侧按无状态 JWT 处理即可(与 `stride-api` 的消费方式一致,见 `stride-devops/local/docker-compose.yml` 注释 "direct-browser tier: RS256 JWT verified against the auth service key");
4. 公钥管理:启动时读 PEM 文件(部署挂卷,如 `/keys/public.pem`),失败即拒绝启动;或启动时拉 `GET /api/system/public-key` 并缓存(注意该端点无 ETag/版本机制,轮换密钥需重启或定时拉取 —— 目前部署是文件挂载为主,`stride-devops/local/keys/`)。

---

## 7. Caveats 与遗留确认项

1. **ADR 0011 `wechat_phone_bind` grant 未见于本地源码快照**(全仓 grep 无命中,`internal/handlers/oauth2.go` 只有五个 grant 分支)——ADR 已 accepted,部署版本可能已含;对接前必须确认,它直接决定小程序"微信未绑定 + 手机号注册"的落地路径。
2. **ADR 0010 scene-scoped SMS codes 同样未见于快照**:`internal/repository/redis/redis.go` 的码键仍是 `sms:code:{phone}` 形态(无 scene 段)。若部署版本已实现,`/api/auth/sms/send` 需要带 scene 参数。
3. **stride-devops#335 spec 未在本地文件中找到**:`stride-devops` 仓库只含代码与部署编排,issue/spec 存于 GitHub 共享 tracker `zhaochy1990/stride-devops`(`auth/docs/agents/issue-tracker.md` 明确说明 issue 不在本地)。本文以源码 + ADR 为准;#335 的原文需要 `gh issue view -R zhaochy1990/stride-devops 335` 获取。
4. 微信 secret 明文落库、无 JWKS、无 token 撤销黑名单(离线 JWT 撤销靠 refresh 轮换 + 30 天过期)——均为上游已知设计,作业侧接受即可,不必自行加固。
