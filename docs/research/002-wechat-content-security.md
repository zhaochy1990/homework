# 002 · 微信内容安全 API 接入要点

> Ticket: `.scratch/homework-v1/issues/002-research-wechat-content-security.md`
>
> **调研方式声明**：撰写本文时本环境无法访问外网（`developers.weixin.qq.com` 抓取与搜索均被阻断），全部内容基于既有知识整理。**文中所有具体数字（配额、频率、时长、参数取值）均标注"需提审前验证"——接入与提审前必须逐条对照官方文档（小程序 OpenApiDoc「安全检测」章节）核实**，禁止直接引用本文数字作为实施依据。

---

## TL;DR（结论速览）

| 内容类型 | 接口 | 异步/同步 | 关键参数（需提审前验证） |
|---|---|---|---|
| 作业文本 | `security.msgSecCheck` | 同步 | `scene=3`（论坛）+ `openid`，`version=2` |
| 班级名 / 孩子昵称 | `security.msgSecCheck` | 同步 | `scene=1`（资料）+ `openid`，`version=2` |
| 上传图片 | `security.mediaCheckAsync` | 异步 | `media_type=2`（图片）+ `scene` + `openid` |
| 打卡视频 | `security.mediaCheckAsync` | 异步 | `media_type=3`（视频）+ `scene` + `openid` |

- 两个接口都要求：**已认证小程序 + 服务端 access_token**；`msgSecCheck` v2 与 `mediaCheckAsync` 均要求传用户 `openid`。
- 异步结果**主通道是微信消息推送**（`wxa_media_check` 事件），新文档另提供了按 `trace_id` 查询结果的补充接口（是否存在/参数需提审前验证）；业务侧应以 `trace_id` 落库 + 状态机为主。
- **我们的策略**：`检测不通过 → 对成员不可见`；`检测中` 建议同样不可见（UGC 先审后显是审核 safest practice），图片/视频只有落 `pass` 后才对班级成员可见。

---

## 1. 调用前置条件

### 1.1 小程序侧

- **小程序须已认证**（个人主体能否调用内容安全接口的历史口径多次变化，需提审前验证：在 mp.weixin.qq.com 后台看这两个接口是否出现在可用 API 列表，或直接用一个测试号试调看 `errcode`）。若未认证/无权限，典型错误码为 `43104`（app 无权限使用该 api，需提审前验证）或 `87016`（需提审前验证）。
- 本项目主体是班级/学校场景（含孩子信息），v1 大概率已是企业/组织主体小程序——认证这条通常已满足。
- **建议在提审前跑通一遍"违规样本"自测**：官方提供检测能力，但没有公开的测试样本集；用明显违规词、图片各试一次，确认 `suggest`/`label` 返回符合预期。

### 1.2 服务端 access_token

- 两个接口都是**服务端接口**，走 `https://api.weixin.qq.com`，凭 access_token 调用（query 或 header，需提审前验证）。
- 获取 access_token 的前置条件：
  1. 已知 AppID + AppSecret；
  2. **调用方服务器出口 IP 必须在小程序后台「开发 → 开发管理 → 开发设置 → 服务器域名/IP 白名单」的 IP 白名单里**，否则返回 `errcode 40164`（invalid ip，非白名单；此码较稳定但仍建议验证）。生产/测试环境的出口 IP 都要加。
- 获取端点（两个选择，见 §5）：
  - 普通 token：`GET /cgi-bin/token?grant_type=client_credential&appid=&secret=`，有效期 7200 秒（需提审前验证）；
  - **stable_token**：`POST /cgi-bin/stable_token`，`grant_type=client_credential`，**推荐**——不强制刷新、重复获取不互相踢、配额友好（详见 §5.2）。

### 1.3 openid 从哪来

- `msgSecCheck`（version=2）与 `mediaCheckAsync` 都要求传**该次内容产生者的 openid**，用于微信做用户维度的风险画像/连坐判定。
- 我们的登录链路（STRIDE 换取会话后拿到的微信侧身份）需要在 `code2session` 时把 openid 落库到用户表，内容检测时随请求带上。**若用户从未微信登录（例如家长用手机号登录）则无 openid**——需提审前验证这种场景下接口是否允许不传 openid（历史文档里 v2 将 openid 列为必填；若必填，手机号-only 账号提交的内容要走兜底方案：要么补一次微信静默登录拿 openid，要么这类内容先默认不可见）。

---

## 2. 频率限制与每日配额（全部需提审前验证）

> ⚠️ 本节**没有任何一个数字可作为事实引用**。微信对这类接口的公开口径是"单个 appId 调用接口有频率限制"，且配额会随类目/认证状态动态调整。以下是既有知识中的常见说法量级，仅供容量规划参考。

| 项 | 既有知识中的说法（量级） | 置信度 |
|---|---|---|
| `msgSecCheck` 单 appId | 历史文档见过 2000 次/分钟、100 万次/天一类的量级 | 低，需提审前验证 |
| `mediaCheckAsync` 单 appId | 同量级或更低 | 低，需提审前验证 |
| 超限错误码 | `45009`（api freq limit out of limit）较常见 | 中，需提审前验证 |

**结论对本项目的影响**：作业打卡场景下，文本检测 QPS 的自然峰值 = 打卡峰值 QPS（v1 用户量级下远不构成瓶颈），配额大概率不是风险点。真正要做的防御是：

1. 客户端/服务端对 `45009` 做**指数退避重试**（限流时内容默认"检测中=不可见"，重试成功再放行）；
2. **不要**为同一个内容重复检测（用 `sec_status` 幂等，见 §3.3）；
3. 提审前在 mp 后台/官方文档确认当期配额，写入运维文档。

---

## 3. 异步检测（mediaCheckAsync）的结果获取与业务表现

### 3.1 调用 → 结果的两条路径

```
业务后端                          微信
  │ POST /wxa/media_check_async   │
  │ (media_url, media_type, scene, openid)
  │──────────────────────────────▶│
  │◀──── {trace_id, errcode:0} ───│   同步应答只给 trace_id，不给结论
  │                               │
  │  路径A（主）：消息推送事件       │
  │◀═ XML/JSON POST 到消息服务器 ══│   Event=wxa_media_check，含 trace_id + result
  │  路径B（辅）：按 trace_id 查询   │   （OpenApiDoc 有 mediaCheckAsyncResult /
  │──────────────────────────────▶│    等价查询接口的说法，需提审前验证）
  │◀──── 检测结果 ────────────────│
```

- **主通道是"小程序消息推送"**：需在 mp 后台「开发 → 开发管理 → 消息推送」配置 URL / Token / EncodingAESKey / 数据格式（建议 JSON + 明文或安全模式，按团队能力定）。配置后微信会把异步检测结果以 `MsgType=event`、`Event=wxa_media_check` 的消息 POST 到该 URL。
- 首次配置时微信会 GET 校验 `echostr` 签名——这个校验端点和业务回调**必须是同一个 URL**（同一台服务器上的同一个 handler 区分 GET/POST）。
- 消息签名校验：按 `sha1(sort(token, timestamp, nonce))` 校验 `signature`（加密模式则用 AES 解密后再验），**验签失败要丢弃**，防伪造回调把违规内容放行。
- 路径 B（轮询）：新文档提供了按 `trace_id` 主动查询结果的接口（`mediaCheckAsyncResult`，端点与参数需提审前验证）。即使存在，也建议只作为**回调丢失的兜底**（例如结果 5 分钟未到则主动查一次），不作为主路径。

### 3.2 结果字段（需提审前验证）

- `trace_id`：与调用时返回/自记的 id 对应；
- `result.suggest`：`pass`（通过）/ `racy`（疑似）/ `risky`（确定性违规）三类口径（需提审前验证）；
- `result.label`：风险类目标签（色情/暴恐/政治等，数值枚举需提审前验证）；
- 新版本还有 `detail` 数组，给出分段命中信息（需提审前验证）。

### 3.3 业务侧状态机（与我们的"不通过→不可见"策略对齐）

媒体表加两列：`sec_status`（`pending / pass / blocked`）+ `sec_trace_id`。

```
上传完成(COS 直传回调) 
  → 后端拿到可公网访问的 media_url（临时签名 URL 或公开 URL）
  → 调 mediaCheckAsync，落库 sec_status=pending, sec_trace_id=trace_id
  → 【pending 期间对班级成员不可见】（先审后显）
  → 回调 arrive:
       suggest=pass  → sec_status=pass   → 对成员可见
       suggest=racy  → v1 暂按 blocked 处理（宁可错杀）；
                       预留 admin_review 字段，v2 做人工复核队列
       suggest=risky → sec_status=blocked → 永久不可见
  → 兜底：pending 超 5~10 分钟未回调查一次（§3.1 路径B）；
    查询接口也不可用/微信侧超时 → 保持 pending=不可见，并告警
```

设计要点：

- **不可见的实现是"查询过滤"而非删除**：`WHERE sec_status = 'pass'` 才出现在班级信息流。上传者本人是否能看到自己"检测中/被拒"的内容，v1 建议：本人可见自己刚上传的 pending 项并显示"审核中"，blocked 则提示"内容未通过安全检测"。这样微信审核员体验良好（合规姿态明确）。
- **幂等**：回调可能重复推送，handler 需按 `trace_id` 幂等（`UPDATE ... WHERE sec_trace_id=? AND sec_status='pending'`）。
- **pending 卡死的运营兜底**：告警 + 人工用查询接口/重新提交检测。
- **拒绝与隐私的关系**：blocked 内容文件仍在 COS（未删除）。v1 从成员信息流隐藏即可；是否物理删除/转存隔离桶，属于隐私合规 ticket（见地图 "Not yet specified"）的范围。
- **视频注意事项**：`media_url` 必须是微信服务器能拉到的 URL——COS 直传后的对象要么公开读、要么生成较长的临时签名 URL（有效期需覆盖微信取回媒体的时间，具体取多长需提审前验证；建议 ≥ 1 小时）。视频文件大小上限、是否支持分段抽帧，需提审前验证——若视频过大导致检测失败，考虑只送"封面首帧图"做兜底检测（需提审前验证这种降级是否满足审核要求，**不建议在提审材料里主动写降级方案**）。

---

## 4. 检测口径：各内容类型走哪个接口、什么参数

> scene 取值依据既有知识：`msgSecCheck` v2 的 `scene`：1=资料、2=评论、3=论坛、4=社交日志（需提审前验证）；`mediaCheckAsync` 的 `scene` 取值口径与其一致或相近（需提审前验证）。`media_type`：1=音频、2=图片、3=视频（需提审前验证）。

| # | 内容类型 | 接口 | 参数建议 | 备注 |
|---|---|---|---|---|
| 1 | **作业文本**（打卡文字、作业描述） | `msgSecCheck`（同步） | `scene=3`（论坛）+ `openid` + `version=2` | 作业文本最接近"论坛帖子"。同步接口可直接拦截发布，无需状态机 |
| 2 | **班级名** | `msgSecCheck`（同步） | `scene=1`（资料）+ `openid` | 班级名是资料类字段。创建/改名时同步校验，失败即拒 |
| 3 | **孩子昵称** | `msgSecCheck`（同步） | `scene=1`（资料）+ `openid` | 同上。注意昵称可能很短，短文本误杀率相对高——被误杀时给用户"换个昵称试试"的提示即可，v1 不做申诉 |
| 4 | **上传图片**（作业照片、头像类图片） | `mediaCheckAsync`（异步） | `media_type=2` + `scene=3`（按 UGC 帖子场景；需提审前验证）+ `openid` | 走 §3.3 状态机 |
| 5 | **打卡视频** | `mediaCheckAsync`（异步） | `media_type=3` + `scene` 同上 + `openid` | 走 §3.3 状态机；注意 `media_url` 可拉取性 |

补充口径问题：

- **打卡的"文字+图片/视频"是分别检还是合检**：分别检。文本走同步、媒体走异步，各自有独立结论；文本 pass 但视频 pending 时，打卡文字可先显示（或整条 pending，v1 建议整条 pending=不可见，避免"文字可见但媒体点开是空的"的怪状态）。
- **`title` / `nickname` 辅助参数**：v2 `msgSecCheck` 支持传 `title`（发布内容的标题）、`nickname`（用户昵称）等辅助字段提升检出率（需提审前验证）。打卡文本可把"班级名"作为 `title` 传入；昵称检测场景本身已检昵称。
- **编辑场景**：已通过的内容被编辑后视为新内容重新检测（同一字段被改才重检，未被改的字段沿用旧结论，减少调用量）。
- **小字段防绕过**：班级名/昵称属于低频但高杠杆字段（被塞广告/二维码的风险高），同步接口拒绝时提示用户"包含不允许的内容，请修改"。

---

## 5. Go 集成与 access_token 管理

### 5.1 依赖与客户端

- **不必引入重型 SDK**。这两个接口就是普通 HTTPS + JSON，标准库 `net/http` + `encoding/json` 即可；若想省事可用社区库（如 `silenceper/wechat`、`chanxuehong/snowfire` 系），但 v1 建议自写薄封装：接口少、错误码处理自定义程度高，且 token 管理无论如何要自己做。
- 统一 client 要点：
  - 超时：文本同步接口建议 2~3s 超时（在用户发布路径上）；媒体提交接口 5s；
  - 统一解析 `errcode/errmsg`，非 0 一律走错误分支；
  - **token 过期重试一次**：收到 `40001/42001`（access_token 无效/过期，需提审前验证）时强制刷新 token 后重试一次，仍失败才报错；
  - 限流 `45009` 退避重试（§2）。

### 5.2 access_token 管理建议（自建 token 缓存 vs 云调用）

**推荐：自建 central token provider + 优先使用 stable_token。**

- **用 `stable_token` 而非普通 `token` 端点**（需提审前验证其可用性与参数）：普通端点在多实例下各自刷新会互相覆盖，且微信对刷新频率有限制；stable_token 设计上就是给服务端长期缓存用的——`force_refresh=false` 拿到的 token 在有效期内稳定复用，即使重复请求也返回同一个。本地缓存（内存 + 单飞 singleflight）+ 到期前主动刷新即可。
- **缓存位置**：多实例部署下，内存缓存 + singleflight 已足够（stable_token 保证了实例间拿到同一个 token）；若要更稳（重启后立即可用、避免重启风暴），可落 Redis，key 带过期时间。**禁止**每个请求都实时取 token。
- **IP 白名单是部署侧的活**：生产/预发/本地联调的出口 IP 分别登记；云上出口 IP 漂移的话要用 NAT 固定出口。这条应在部署 ticket 里落实。
- **云调用（微信云托管 CloudBase Run）**：云托管内可通过免鉴权方式调用开放接口（不管理 token）。**不推荐为内容安全单独引入云托管**——我们后端已在腾讯云自建 Go 服务，为两个接口引入一套云托管部署/回调链路得不偿失；只有当整体后端迁到云托管时才值得切云调用。此决策如需变更记 ADR。
- **密钥管理**：AppSecret 不进代码库，走环境变量/密钥管理；泄漏后果是任何人可拿 token 调服务端接口。

### 5.3 消息推送回调的 Go 实现要点

- 回调 handler 与业务 API 同域同端口，路由区分：
  - `GET`：验签并回显 `echostr`（配置校验用）；
  - `POST`：验签 → 解析 JSON/XML → 按 `Event=wxa_media_check` 分发到内容安全结果处理（§3.3 的状态机），按 `trace_id` 幂等更新；
- **必须快速返回**（微信对响应时间有要求，超时会重试推送）：handler 只做落库/置状态，重的后续动作（通知、清理）丢队列。返回体按微信要求返回 `success`/空串（需提审前验证确切口径），异常时返回非 200 触发微信重推。
- 验签失败一律 403 并记日志（防伪造回调放行违规内容，这是安全边界）。
- 推送格式在后台配置为 JSON 的话，回调体是 `{ToUserName, FromUserName, CreateTime, MsgType, Event, trace_id, result:{suggest,label}, ...}`（字段名以官方文档为准，需提审前验证）。

### 5.4 建议的 Go 代码结构（供实施 ticket 参考）

```
backend/internal/wx/
  token.go        // stable_token 获取 + 缓存 + singleflight + 40001 重试
  seccheck.go     // MsgSecCheck(ctx, content, scene, openid, opts) → (suggest, label, err)
  mediacheck.go   // MediaCheckAsync(ctx, mediaURL, mediaType, scene, openid) → (traceID, err)
  callback.go     // HTTP handler：验签 + echostr + wxa_media_check 事件分发
backend/internal/contentsec/
  service.go      // 状态机：pending/pass/blocked、trace_id 幂等、超时兜底查询
```

文本同步检测的调用点（班级创建/改名、昵称保存、打卡发布）在业务 service 层内联调用即可，失败策略 = 拒绝发布并提示；媒体异步检测挂在 COS 上传完成回调之后。

---

## 6. 提审前必须逐条验证的清单

1. 小程序当前主体是否已在后台看到这两个接口可用（或调用后无权限错误码）；
2. `msgSecCheck` v2 / `mediaCheckAsync` 的当前参数表（scene、media_type、openid、title、nickname 是否必填）；
3. 频率限制与每日配额的确切数字；
4. 异步结果查询接口（mediaCheckAsyncResult 或等价物）是否存在及参数；
5. 消息推送事件的字段名与应答格式要求；
6. 视频作为 `media_type=3` 的文件大小上限与 `media_url` 拉取超时；
7. 无 openid 用户（手机号登录）提交内容时的处理口径；
8. stable_token 端点可用性。

来源：官方文档入口 `developers.weixin.qq.com/miniprogram/dev/OpenApiDoc/sec-center/sec-check/`（本环境无法抓取，以上 8 条以该页当期内容为准）。
