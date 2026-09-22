# Research 003：小程序媒体直传 COS 与私有桶播放

> 关联：ADR 0005（COS 私有桶 + 后端签发凭证、小程序直传直连）、ticket 011（API 契约）。
> 说明：撰写时环境无法访问外网（cloud.tencent.com / WebSearch 均被拦截），本文基于对腾讯云 COS、微信小程序 API 的既有知识整理；所有标 **【需验证】** 的条目在实现前须对照官方文档/真机实测确认。

---

## 1. 直传机制选型：预签名 PUT URL vs STS 临时凭证

### 两种候选

| | 方案 A：STS 临时密钥 + cos-wx-sdk-v5 | 方案 B：后端签发预签名 URL + wx.uploadFile |
|---|---|---|
| 原理 | 后端调 STS `GetFederationToken`/`AssumeRole` 换取限定 policy 的临时密钥，下发给小程序；SDK 在端上自行计算 COS 请求签名，内部用 `wx.request`/`wx.uploadFile`/`FileSystemManager` 完成简单上传与**分片上传** | 后端用永久密钥按 COS 签名算法生成带 `q-sign-algorithm...&q-signature=...` 的 URL；客户端对该 URL 直接发起请求 |
| 分片/断点 | SDK 内置 `sliceUploadFile`（分片、并发、失败重试、断点续传），几百 MB 视频开箱即用 | 单次 `wx.uploadFile` 无法分片；自己拼分片上传（InitiateMultipartUpload/UploadPart/CompleteMultipartUpload）等于重写半个 SDK |
| 签名与 header | 签名由 SDK 处理，Content-Type/host 头不出错 | 预签名 URL 的签名通常**绑定 method + pathname + 部分 query/headers**；`wx.uploadFile` 强制 multipart/form-data（POST），与预签名 **PUT** 语义不匹配——这是小程序直传预签名 URL 的经典坑。规避手段是改用 COS **PostObject 表单上传**（policy + 签名放进 formData 字段），但 policy/条件字段拼写繁琐、报错信息差 **【需验证：PostObject 的 policy 字段名与 wx.uploadFile formData 兼容细节】** |
| 权限粒度 | 临时密钥的 CAM policy 可精确到 action + 资源前缀（如只允许 `PutObject`/`UploadPart` 到 `videos/{uid}/*`），到期自动失效，泄漏面小 | 预签名 URL 本身也是一次性/限时授权，粒度相当；但客户端拿到 URL 后可完整重放一次 PUT，且 URL 里明文可见签名参数 |
| 后端实现量 | 只需一个 STS 换取端点 + policy 构造 | 需实现/引入 COS URL 签名；若要支持大文件还得补 PostObject 或分片签发 |

### 小程序端常见做法（行业惯例）

微信小程序社区的主流路径就是 **方案 A**：后端用 CAM 子账号/角色调 STS 换临时密钥 → 小程序用腾讯官方 `cos-wx-sdk-v5`（cos-js-sdk-v5 的小程序适配版，`getAuthorization` 回调里返回 `{ TmpSecretId, TmpSecretKey, SessionToken, StartTime, ExpiredTime }`）直传。预签名 URL 方案在 H5/App 端常见，在小程序端主要用于**下载/播放**而非上传。

### 结论

**上传选方案 A（STS 临时密钥 + cos-wx-sdk-v5）**。决定性理由：打卡视频 ≤ 数百 MB 必须分片上传，cos-wx-sdk-v5 内置该能力，而 wx.uploadFile + 预签名 PUT 在小程序里根本凑不上（multipart POST vs PUT 语义不匹配）。临时密钥也可用 policy 限定到"每个用户/每次上传只写特定前缀"，安全性不低于预签名 URL。**下载/播放侧仍用后端签发的预签名 GET URL**（见 §3），两者并不互斥。

SDK 细节 **【需验证】**：cos-wx-sdk-v5 的最新包名/版本与 `getAuthorization` 回调字段（v5 与 v6/new SDK 字段有差异）、小程序分包体积影响。

---

## 2. `wx.chooseMedia` / `wx.uploadFile` 能力边界与大文件处理

- **选片**：`wx.chooseMedia` 支持图片/视频混选（count 最多 20 **【需验证】**）；视频可直接从相册选或现场拍摄，拍摄 `maxDuration` 上限约 30s（相册选不受此限，受大小限制约束）。返回项含 `tempFilePath` 与视频 **`thumbTempFilePath`（首帧缩略图）**——§5 会用到。新版本还提供 `sizeLimit` 参数可前置拦截超大文件 **【需验证：sizeLimit 支持的基础库版本】**。
- **单次请求体限制**：微信对小程序网络请求有单包上限（常见说法 `wx.uploadFile` 约 10MB 级别）**【需验证：当前基础库对 wx.uploadFile 的确切大小上限】**——这意味着几百 MB 视频**绝不能**单次 `wx.uploadFile` 直传，必须分片。
- **分片方案**：cos-wx-sdk-v5 的 `sliceUploadFile` 内部用 `FileSystemManager.readFile` 的 position/length 按块读取（典型 1MB/片，可配置），逐片 `PUT UploadPart`，全部完成后 `CompleteMultipartUpload`。要点：
  - 并发数 SDK 可配（建议 3~6，兼顾速度与手机网络稳定性）；
  - 支持上传中断后续传（依赖 COS 分片记录）**【需验证：SDK 小程序版的断点续传持久化方式】**；
  - 分片大小要避开微信请求体上限；
  - 上传中/失败需在 UI 层暴露进度（SDK 提供 `onTaskReady`/progress 回调）。
- **并发上传**：多文件（资料图多张）建议端上做队列，同时最多 2~3 个任务，避免挤占分片并发与请求通道。
- **v1 简化建议**：打卡视频若产品上限制在 ≤ 100MB（如最长 60s~3min 1080p），仍在几百 MB 级别以下，分片方案同样适用；资料图片走简单上传即可。

---

## 3. 私有桶对象的播放与查看（签名 URL + Range）

- **机制**：私有桶不允许匿名读。后端用永久密钥为单个 object 生成**预签名 GET URL**（带 `q-signature` 等参数，`sign expired time` 可配），小程序把 URL 直接塞进 `<video src>` / `<image src>` 即可，媒体流不经过后端（符合 ADR 0005）。
- **有效期**：建议下载 URL 有效期 1~2 小时（前端按页面/会话向"签发端点"请求，而不是长期缓存签名 URL）。对 `<video>` 播放时长远小于有效期即可；若 URL 过期，重新签发（同 key 换新签名，COS 侧无需任何操作）。**不要**把签名 URL 存进数据库或日志。
- **Range 拖动**：COS 原生支持 HTTP `Range` 请求（返回 206 Partial Content），且 COS 的 URL 签名**不覆盖 Range 头**——`<video>` 拖动进度条时播放器对同一签名 URL 发起带 Range 的请求仍通过校验，**无需额外配置**。这一条是 ADR 里"COS 直链支持 Range 拖动播放"的技术依据。**【需验证：真机 `<video>` 对带 query 的 https URL 拖动表现，个别老播放器内核对带签名的 URL 有兼容问题，若有可加 `Range` 白名单相关的 Referer/UA 配置排除】**
- **缓存策略**：私有内容不应被公共缓存（CDN v1 不引入）。浏览器/播放器本地缓存由小程序自身管理即可；图片可用同一签名 URL 在会话内复用以提高命中。
- **注意**：`<image>`/`<video>` 域名要求——小程序 downloadFile 合法域名需包含 COS 桶域名（`<bucket>.cos.<region>.myqcloud.com`）；媒体标签的资源加载同样受域名校验约束（真机上需配置，开发工具可关校验）**【需验证：`<video>`/`<image>` 是否严格校验 downloadFile 域名列表】**。

---

## 4. 上传成功确认 / 落库模式

三步走（推荐组合）：

1. **客户端回执**：cos-wx-sdk-v5 上传完成回调给出 ETag/状态；小程序随后调后端 `POST /uploads/{id}/confirm`（携带 key、size、ETag、sha/时长等元数据）。
2. **后端 HEAD 校验**：后端收到 confirm 后用自身永久密钥对 `cos:HeadObject` 校验该 object 真实存在、`Content-Length` 与上报一致（必要时比对 ETag）。通过才把媒体记录置为 `ready` 并落库；不通过打回。这是防"客户端谎报/上传半途截断"的最低成本校验。
3. **（可选，v2）COS 服务端回调**：COS 支持 4.x 事件通知/云函数（SCF/函数计算）在 PutObject/CompleteMultipartUpload 后回调后端。它能兜底"客户端上传成功但 confirm 请求丢失"的场景，并支持定时任务清理孤儿分片/未确认对象。v1 用 confirm + HEAD 已够，后端定期（如每日）扫描"已签发但超时未确认"的上传记录做清理即可。

API 契约影响（供 ticket 011）：
- `POST /media/upload-ticket`：入参 `{kind: material|checkin, contentType, size, prefixHint?}`，出参：上传目标 key + STS 临时密钥（或 v2 兼容预签名 URL 字段）+ 过期时间 + `uploadId`（业务侧记录 ID，用于 confirm）。
- `POST /media/upload-ticket/{id}/confirm`：入参 `{key, size, etag, duration?, width?, height?, thumbKey?}`，出参：媒体对象（含签名播放 URL）。
- `GET /media/{id}/playback-url`（或卡片接口内嵌）：出参短期签名 GET URL。

---

## 5. 视频封面/首帧（不做转码）

| 方案 | 说明 | 评价 |
|---|---|---|
| **客户端首帧缩略图（推荐）** | `wx.chooseMedia` 对视频直接返回 `thumbTempFilePath`（首帧小图）；把该图作为 `thumb.jpg` 与视频一起上传（如 `videos/{id}/thumb.jpg`），confirm 时上报 `thumbKey` | 零后端/存储外成本、离线可用、实现一天内完成；缺点是用户无法选其他帧 |
| COS 数据万象 CI 首帧截图 | 开启 CI 后用 `?ci-process=snapshot&time=0` 之类的处理参数对视频取帧 **【需验证：接口路径、参数与计费】** | 免客户端逻辑、可取任意时间点帧；但引入 CI 依赖与费用，且取帧结果是即时处理产物，私有桶下也要签名访问 |
| 后端取帧（ffmpeg） | 后端不行——媒体不经过后端（ADR 0005），要么下载视频再处理，违背直传初衷 | 排除 |

**结论：v1 用客户端 `thumbTempFilePath` 上传首帧图作为封面**；将来需要"可换封面/封面精修"再评估 CI snapshot。

---

## 6. Go SDK 选型与 CAM 最小权限

### SDK

- **对象操作**：官方 `github.com/tencentyun/cos-go-sdk-v5`（`Bucket.Get/Put/Head/Object.Head`、`Object.OptionsObject` 支持签名 URL 生成 `Object.GetPresignedURL`/`PresignedURL`，用于下载签发与 HEAD 校验）。成熟、官方维护，无争议。
- **STS 临时密钥**：用腾讯云总 SDK 的 STS 模块 `github.com/tencentyun/tencentsdk-go`/`github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sts`（`GetFederationToken` 或 `AssumeRole`）**【需验证：当前推荐的 SDK 包路径与 GetFederationToken 的 policy 参数形状】**；也可用 `cos-go-sdk-v5` 附带的签名辅助工具手工拼。每次上传签发一枚（有效期建议 30~60 分钟，policy 限定到当次 key 前缀）。

### 最小权限设计

后端持有**两个**身份，前端永远不接触永久密钥：

1. **后端操作身份**（子账号 `homework-backend`，AccessKey 只在服务端）：允许
   - `cos:GetObject` / `cos:HeadObject`（下载签发不用它签名？——签名本身用本地密钥即可，但 Head 校验需要 HeadObject 权限；GetObject 供后端偶尔代取对象）
   - `cos:DeleteObject` / `cos:AbortMultipartUpload`（清理孤儿上传）
   - `cos:ListMultipartUploads` / `cos:ListParts`（清理前列举）
   - Resource：`qcs::cos:<region>:uid/<appid>:<bucket-appid>/*`
2. **STS 换取用的角色/子账号**（`homework-upload-issuer`）：只允许 STS 调用方把它"扮演"出来；其 policy 只含写路径：
   - `cos:PutObject`、`cos:InitiateMultipartUpload`、`cos:UploadPart`、`cos:CompleteMultipartUpload`、`cos:ListParts`、`cos:AbortMultipartUpload`
   - **不给** `cos:GetObject`（上传凭证不可读）与 `cos:DeleteObject`
   - Resource 限定到 `.../<bucket-appid>/uploads/*`（所有客户端写入统一走 `uploads/` 前缀，后端再按业务移动/重命名——或 v1 直接以 `uploads/{userId}/...` 为最终路径，省去 Copy 步骤；推荐后者 **【需验证：cos-wx-sdk-v5 policy 中 resource 路径匹配细节】**）
3. 会话策略注入：STS `GetFederationToken` 时把上述 policy 作为 session policy 传入，密钥有效期 = 上传会话时长。

---

## 最终推荐（供 ticket 011 引用）

- **上传：选型 A —— STS 临时密钥 + cos-wx-sdk-v5 直传**（分片上传刚需、签名不出后端、policy 可精确到前缀）。API 提供 `POST /media/upload-ticket`（返回 key + STS 凭证）+ `POST /media/upload-ticket/{id}/confirm`（后端 HEAD 校验后落库）。
- **下载/播放：后端签发预签名 GET URL**（有效期 1~2h），`<video>`/`<image>` 直连，Range 拖动无需额外配置。
- **封面：客户端 `thumbTempFilePath` 随视频上传**，confirm 时上报 `thumbKey`；不引入 CI/转码。
- **Go SDK：`cos-go-sdk-v5` + `tencentcloud-sdk-go/sts`**；CAM 上后端身份只留 Get/Head/Delete/List 清理权限，STS 角色 policy 只留写入 action 且限定 `uploads/` 前缀。

主要待验证清单：`wx.uploadFile` 单请求大小上限与 `chooseMedia.sizeLimit` 基础库版本；PostObject 字段兼容（若未来需 H5 复用）；cos-wx-sdk-v5 版本/断点续传细节；CI snapshot 接口与计费；真机 `<video>` 带签名 URL 的拖动表现；STS SDK 包路径与 policy 参数形状。
