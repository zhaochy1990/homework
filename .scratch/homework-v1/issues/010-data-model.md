---
id: 010
title: "Grilling: 数据模型与状态机"
labels: [wayfinder:grilling]
status: open
assignee:
blocked-by: []
---

## Question

把已定的领域决策（CONTEXT.md + ADR 0002/0003/0004）落成**MySQL 表结构（GORM）与状态机**，逐一确认：

- 实体与关系：用户/孩子/监护关系/班级/班级成员/班级选教材（按学科）/学习资料/Session/Todo/勾选状态/打卡/打卡视频/打卡卡片/无作业日/上学日历种子表；
- 边界矩阵：编辑/删除作业与待办、标记/取消无作业日、退班/移除监护人、假期 Session 跨度上的补录——各自对已有勾选、打卡、streak、卡片的影响（地图 fog 里的"操作影响矩阵"在本 ticket 毕业为一等决策）；
- streak 的计算口径实现化：实时累加列 vs 按履约记录推导（选一个并说明回算策略）；
- 状态机：Session/打卡/媒体审核（检测中→通过/拒绝）的生命周期。

产出：表结构设计文档 `docs/design/data-model.md`（含 ER 概图），经用户确认后关闭。
