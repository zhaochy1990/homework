---
id: 010
title: "Grilling: 数据模型与状态机"
labels: [wayfinder:grilling]
status: closed
assignee: claude
blocked-by: []
---

## Question

把已定的领域决策（CONTEXT.md + ADR 0002/0003/0004）落成**MySQL 表结构（GORM）与状态机**，逐一确认：

- 实体与关系：用户/孩子/监护关系/班级/班级成员/班级选教材（按学科）/学习资料/Session/Todo/勾选状态/打卡/打卡视频/打卡卡片/无作业日/上学日历种子表；
- 边界矩阵：编辑/删除作业与待办、标记/取消无作业日、退班/移除监护人、假期 Session 跨度上的补录——各自对已有勾选、打卡、streak、卡片的影响（地图 fog 里的"操作影响矩阵"在本 ticket 毕业为一等决策）；
- streak 的计算口径实现化：实时累加列 vs 按履约记录推导（选一个并说明回算策略）；
- 状态机：Session/打卡/媒体审核（检测中→通过/拒绝）的生命周期。

产出：表结构设计文档 `docs/design/data-model.md`（含 ER 概图），经用户确认后关闭。

## Resolution

产出 `docs/design/data-model.md`（17 张表的 GORM 定义 + 履约/streak 计算规范 + 操作影响矩阵 10 条 + 状态机）。两轮 grilling 敲定的关键决策：

- Q1 删除规则：有打卡的 Session 禁删；已勾选 todo 可删但级联清勾选、需二次确认。
- Q2 迟建不追溯：Session 仅当 created_at 早于履约截止时构成义务（履约义务定义进 CONTEXT.md）。
- Q3 streak 存储：事件驱动计数器表（事务内 +1、同日同科目幂等）+ 惰性中断检测 + 每日校准任务对账。
- Q4 跨班同科目同日只 +1 一次；无作业日撤销不回溯已发放 +1。
- Q5 打卡即冻结：打卡后该 session 勾选锁定；admin 后加 todo 对已打卡者显示"新增未完成"。
- Q6 时区硬编码 Asia/Shanghai；日期字段存 CST 日期、时间戳存 UTC。
- 补卡：+1 计实际打卡日；不能恢复已中断 streak。
- 实现细节：todo 主键 ULID；users 由 JWT 首次请求 upsert；卡片异步生成重试；school_calendar.verified=false 不驱动业务逻辑。
