# API 契约 v1

> 产出自 wayfinder ticket「Grilling: API 契约设计」。上游依据：`docs/design/data-model.md`（010）、`docs/research/001-stride-auth.md`（认证契约）、`docs/research/003-cos-direct-upload.md`（上传/播放选型）。
> 作业解析／入库两个端点已随 ticket「Grilling: LLM 作业解析与确认流设计」（012）修订为按学科分组形态，细节见 `docs/design/homework-parsing.md`。

## 1. 通用约定

- **Base URL**：`https://<备案域名>/api/v1`（子域/路径由上线清单 ticket 定）。
- **鉴权**：`Authorization: Bearer <JWT>`。后端本地验签：RS256 白名单 + `iss=auth-service` + `exp` 必填 + **`aud` 自校验**（= 作业小程序 application 的 client_id，auth-service 不校验 aud，消费方必须补）。中间件把 `sub` 放入 request context；每请求不查库。
- **登录/刷新不经本服务**：小程序直接调 auth-service（`POST /oauth/token` grant_type=token_exchange；`POST /api/auth/refresh` + `X-Client-Id`）。本服务只验签。
- **错误形状**：`{"error": "<稳定机器码>", "message": "<中文>"}`，与 auth-service 完全一致。401=身份未建立（`unauthorized`/`invalid_token`），403=身份已建立不许可（`forbidden`/资源权限）。
- **分页**：`?page=&page_size=`（默认 page_size=20，上限 100），响应带 `{items, page, page_size, total}`。
- **时间语义**：日期参数与业务日期字段一律 `YYYY-MM-DD`（Asia/Shanghai 日历日）；时间戳一律 UTC ISO8601。时区硬编码 CST。
- **孩子上下文**：涉及孩子数据的端点显式含 `{childId}` 路径段，后端校验"当前用户是该孩子的监护人"；涉及班级的校验成员资格与角色。多孩家庭无隐式"当前孩子"状态。

## 2. 稳定错误码表（业务层，HTTP 语义对齐 auth-service）

| HTTP | error | 场景 |
|---|---|---|
| 400 | `bad_request` / `future_date_forbidden` | 参数错 / 给未来日期打卡 |
| 401 | `unauthorized` / `invalid_token` | 缺 Bearer / JWT 校验失败 |
| 403 | `forbidden` | 非 admin、非成员、非监护人 |
| 404 | `not_found` | 资源不存在或无权见 |
| 409 | `duplicate_session` | 同班同科同日期已有 Session |
| 409 | `no_homework_day_conflict` | 无作业日当天发作业 / 当天已有作业仍标记 |
| 409 | `not_all_ticked` | 打卡时未勾完全部待办 |
| 409 | `already_checked_in` | 重复打卡（唯一约束兜底） |
| 409 | `session_has_checkins` | 删除已有打卡的 Session |
| 409 | `todo_frozen` | 打卡后勾选被冻结 |
| 409 | `member_has_children` | 踢出有孩子在班的成员 |
| 409 | `last_guardian` | 移除最后一个监护人 |
| 409 | `class_not_empty` | 解散仍有孩子在班的班级 |
| 409 | `admin_immutable` | admin 互踢/创建者被操作 |
| 422 | `llm_parse_failed` | 解析失败（降级：重试 → 手动录入待办） |
| 422 | `content_blocked` | 作业原文或待办命中内容安全检测 |
| 422 | `upload_mismatch` | confirm 时 HEAD 校验不符 |
| 502 | `llm_unavailable` | DeepSeek 超时/不可达 |

## 3. 端点清单

### 3.1 身份与家庭

| 方法/路径 | 权限 | 说明 |
|---|---|---|
| `GET /me` | 登录 | 当前用户；首次请求 upsert `users` |
| `PATCH /me` | 登录 | `{nickname}` |
| `POST /children` | 登录 | `{name, avatarUploadId?}` → 创建者自动成为监护人 |
| `GET /children` | 登录 | 我的孩子 |
| `PATCH /children/{childId}` | 监护人 | `{name?, avatarUploadId?}` |
| `POST /children/{childId}/guardian-invites` | 监护人 | → `{inviteToken, expiresAt}`（72h） |
| `POST /guardianships/accept` | 登录 | `{inviteToken}` → 建立监护关系 |
| `DELETE /children/{childId}/guardians/{userId}` | 监护人 | 移除；最后一个 → 409 `last_guardian` |
| `DELETE /children/{childId}/guardians/me` | 监护人 | 退出监护（同上约束） |

### 3.2 班级与成员

| 方法/路径 | 权限 | 说明 |
|---|---|---|
| `POST /classes` | 登录 | `{name, visibility: public\|private, joinApproval: bool}` → 创建者=admin |
| `GET /classes/my` | 登录 | 我加入的班级（含角色） |
| `GET /classes/public?keyword=&page=` | 登录 | 搜索公开班（按名称） |
| `GET /classes/{classId}` | 成员 | 详情：我的角色、我在班内的孩子；admin 额外返回 `inviteCode`（分享邀请口令用） |
| `PATCH /classes/{classId}` | admin | 改名/可见性/审批开关 |
| `POST /join-requests` | 登录 | `{classId, inviteCode?}`。公开班无审批→自动通过（200, `status=approved`，即成 member）；公开班有审批→202 `status=pending`；私密班凭 `inviteCode`→直接 member（邀请码即授权，绕过审批开关）。重复申请 409 `already_exists` |
| `GET /classes/{classId}/join-requests?status=pending` | admin | 待审批列表 |
| `POST /join-requests/{id}/approve` \| `/reject` | admin | 审批 |
| `GET /classes/{classId}/members?page=` | 成员 | 成员列表 |
| `DELETE /classes/{classId}/members/{userId}` | admin | 踢人；有孩子在班→409 `member_has_children`；admin→409 `admin_immutable` |
| `POST /classes/{classId}/members/{userId}/promote` \| `demote` | 创建者 | 任命/撤销 admin |
| `DELETE /classes/{classId}/members/me` | 成员 | 退出班级（有孩子在班给出提示字段） |
| `DELETE /classes/{classId}` | 创建者 | 解散；班内有孩子→409 `class_not_empty`；软删除 |
| `GET /classes/{classId}/invite-qrcode` | admin | 微信小程序码：`page=pages/class/join`、`scene={classId}-{inviteCode}`，PNG；邀请口令前端即 `{classId}-{inviteCode}`，可在加入页手动输入 |

### 3.3 孩子入班

| 方法/路径 | 权限 | 说明 |
|---|---|---|
| `POST /children/{childId}/enrollments` | 监护人 | `{classId}`；监护人须已是该班 member；孩子入班后其监护人自动获得 member（数据模型规则） |
| `DELETE /children/{childId}/enrollments/{classId}` | 监护人 | 退班（勾选/打卡历史保留不再展示，streak 冻结） |
| `GET /classes/{classId}/children?page=` | 成员 | 班内孩子名单 |

### 3.4 教材库

| 方法/路径 | 权限 | 说明 |
|---|---|---|
| `POST /textbooks` | 登录 | `{subject, name, grade?, term?, units: [{name, sortOrder}]}`（任何用户，v1 无审核） |
| `GET /textbooks?subject=&keyword=&page=` | 登录 | 全局库检索 |
| `POST /textbooks/{textbookId}/units` | 登录 | 补充单元 |
| `PUT /classes/{classId}/textbooks` | admin | `{subject, textbookId}` 按科目选用 |
| `GET /classes/{classId}/textbooks` | 成员 | 已选教材（按科目） |

### 3.5 媒体上传（research/003 选型 A 落地）

```
POST /media/upload-tickets          {kind, contentType, sizeBytes}
  kind ∈ material_image | material_video | checkin_media | thumb | avatar
  → 200 {uploadId, objectKey, bucket, region,
         sts: {tmpSecretId, tmpSecretKey, sessionToken, expiredTime}}   // 30~60min，policy 限写 uploads/ 前缀

POST /media/upload-tickets/{uploadId}/confirm   {sizeBytes, etag, durationSec?, thumbObjectKey?}
  → 后端 HeadObject 校验（存在+大小一致）→ 200 {objectKey, playbackUrl}；不符 → 422 upload_mismatch
  → 超时未 confirm 的 ticket 由每日任务清理（孤儿对象/分片）
```

客户端流程：取 ticket → cos-wx-sdk-v5 直传（视频分片）→ confirm → 拿 `objectKey` 去创建业务资源。视频封面用 `wx.chooseMedia` 的 `thumbTempFilePath` 作为 thumb 一并上传，confirm 时上报 `thumbObjectKey`。打卡附件（照片/视频）同走 `checkin_media` kind。

### 3.6 学习资料

| 方法/路径 | 权限 | 说明 |
|---|---|---|
| `POST /classes/{classId}/materials` | admin | `{type: text\|image\|video, title?, body?, unitId?, uploadId?, thumbObjectKey?}`；创建时触发内容安全（文本 msgSecCheck 同步 / 媒体 mediaCheckAsync 异步） |
| `GET /classes/{classId}/materials?unitId=&type=&page=` | 成员 | 列表；先审后显（pending/blocked 不可见） |
| `GET /materials/{materialId}` | 成员 | 详情 + 预签名查看/播放 URL（1~2h，Range 原生支持） |
| `DELETE /materials/{materialId}` | admin | 删除（连带 COS 对象） |

### 3.7 作业（核心）

| 方法/路径 | 权限 | 说明 |
|---|---|---|
| `POST /classes/{classId}/homework/parse` | admin | `{date, rawText, defaultSubject?, retry?}` → **同步**调 DeepSeek（30s 超时，不自动重试；`retry=true` 为管理员点"重试"，temperature 提到 0.3、同模型同 prompt），返回**按学科分组**的草稿，**不入库**：`{groups: [{subject, target: new\|append, sessionId?, existingTodos?, kind, date, startDate?, endDate?, holidayName?, todos: [{content, estimatedMinutes}], notes}]}`。失败 → 502 `llm_unavailable` / 422 `llm_parse_failed`，前端给"重试"（同模型同 prompt，温度 0.3）与"手动填写"两条路 |
| `POST /classes/{classId}/homework` | admin | `{date, rawText, groups: [{subject, todos, notes?}]}`，**单事务**入库：先 `msgSecCheck`（命中 → 422 `content_blocked`），再按上学日历对每个分组判 `kind`（上学日含调休补班 → day；休息日 → 自动定位连续休息段 → holiday，start/end 自动算）；`target=append` 并入已有 Session、`new` 新建。冲突**整单**拒绝并在响应中指明学科：409 `duplicate_session` / `no_homework_day_conflict` |
| `PATCH /homework/{sessionId}` | admin | `{rawText?, todos: {add: [{content, sortOrder}], update: [{id, content?, sortOrder?}], remove: [id]}}`；todo id 不可变（ADR 0003）；remove 已勾选 todo 级联清勾选（前端二次确认） |
| `DELETE /homework/{sessionId}` | admin | 有任何打卡 → 409 `session_has_checkins` |
| `GET /classes/{classId}/homework?from=&to=` | 成员 | 班级视图（session + todos + 各孩子聚合勾选/打卡态，admin 管理用） |
| `GET /children/{childId}/homework?date=YYYY-MM-DD` | 监护人 | ★ **当日聚合视图**（首页数据源）：跨班 session（当日 day + 生效中的 holiday）+ todo 勾选态 + 打卡态 + 无作业日标记 |
| `GET /children/{childId}/homework/calendar?month=YYYY-MM` | 监护人 | 打卡日历（**单张全科目日历**，前端按符号渲染）：每日 `{date, kind: school_day\|rest\|no_homework\|holiday, subjects: {"语文": "checked\|missed\|none", …}}`；仅含过去与当天，未来日期 `subjects` 为空 |

### 3.8 勾选与打卡

| 方法/路径 | 权限 | 说明 |
|---|---|---|
| `POST /children/{childId}/todos/{todoId}/tick` | 监护人 | 勾选；session 已打卡 → 409 `todo_frozen` |
| `DELETE /children/{childId}/todos/{todoId}/tick` | 监护人 | 取消勾选（同上） |
| `POST /children/{childId}/homework/{sessionId}/checkin` | 监护人 | `{note?, media?: [{uploadId}]}`（附件照片/视频合计 ≤9，无独立发布入口）。校验：全部 todo 已勾（409 `not_all_ticked`）、非未来日期（400 `future_date_forbidden`）、未重复打卡（409 `already_checked_in`）→ 建打卡（终态）→ 附件落 `checkin_media` 并逐个过内容安全 → streak 事务内 +1 → 异步生成卡片 |
| `GET /children/{childId}/checkins?from=&to=&page=` | 监护人 | 打卡动态（朋友圈式流，offset 分页；客户端默认取最近 3 条、滚动到底翻页）：含 `note`、`media: [{type, playbackUrl, thumbUrl, durationSec, secStatus}]`、卡片状态 |
| `GET /checkins/{checkinId}/card` | 监护人 | `{imageUrl}`（海报预签名 URL；生成中→`{status: generating}`，客户端轮询） |

### 3.9 streak / 无作业日 / 日历

| 方法/路径 | 权限 | 说明 |
|---|---|---|
| `GET /children/{childId}/streaks` | 监护人 | `[{subject, currentCount, lastFulfillDate}]`（孩子×科目；惰性中断检测在读取前执行） |
| `POST /classes/{classId}/no-homework-days` | 成员（任意） | `{date}`；当日已有 session → 409 `no_homework_day_conflict`；标记当日起禁止发作业，各科目 streak +1（校准/惰性发放） |
| `DELETE /classes/{classId}/no-homework-days/{date}` | 成员（任意） | 撤销；已发放 +1 不回溯 |
| `GET /classes/{classId}/no-homework-days?from=&to=` | 成员 | 列表 |
| `GET /school-calendar?from=&to=` | 登录 | 法定节假日/调休日；仅 `verified=true` 记录参与业务判定 |

## 4. 关键时序

**发作业**：粘贴老师原文（可混科）→ `POST /homework/parse`（同步，转圈 3~15s，30s 超时）→ 确认页按学科分组编辑，每组标注"新建/追加"→ `POST /homework` 单事务入库。解析失败 → 管理员点"重试"（温度 0.3 重跑一次）→ 仍失败 → 手填页（多行文本框、单学科、原文并排展示）。

**打卡**：勾完全部 todo → 点「打卡」→ 弹层可附照片/视频（`upload-tickets(kind=checkin_media)` → 直传 → `confirm`，≤9 个）→ `POST .../checkin {note?, media?}` → 返回打卡记录 → 前端轮询 `GET /checkins/{id}/card` 至 `imageUrl` 就绪 → 保存/分享海报。

**资料上传**：`upload-tickets(kind=material_video)` → 直传 → `confirm` → `POST /classes/{id}/materials {uploadId, unitId?}` → 内容安全异步检测 → 通过后成员可见。

## 5. 与数据模型的联动修订

- `classes` 增加 `join_approval BOOL DEFAULT false`（Q1）；
- 新增表 `class_join_requests(id, class_id, user_id, status: pending|approved|rejected, created_at, decided_by?, decided_at)`，`unique(class_id, user_id, status=pending)` 防重复申请；
- `todos` 增加 `estimated_minutes INT NULL`（大模型估算时长；NULL = 未估，0/负数非法）；
- `homework_sessions` 增加 `notes TEXT`（"老师还提到"，来自解析的 `unparsed`，可编辑）；
- 作业读端点的响应形状：todo 带 `estimatedMinutes`，Session 带 `notes`；手填路径产出的 todo `estimatedMinutes` 恒为 null；
- `subject` 取值收紧为全局封闭枚举（ADR 0007），见 `docs/design/homework-parsing.md`。
