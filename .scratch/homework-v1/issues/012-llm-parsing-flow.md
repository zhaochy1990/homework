---
id: 012
title: "Grilling: LLM 作业解析与确认流设计"
labels: [wayfinder:grilling]
status: open
assignee:
blocked-by: ["004", "010"]
---

## Question

设计"粘贴老师文字 → DeepSeek 解析 → 管理员确认编辑 → 入库"的完整流程：

- 解析 prompt 与输出 JSON schema（依赖 research/004 的结论）；
- 管理员确认页的交互流：解析结果的可视化编辑（增删改条目、合并/拆分）、重新解析、放弃；
- 失败路径：解析超时、JSON 不合法、敏感词拒绝时的降级（手动录入待办）；
- 入库事务：Session 创建 + Todo 批量写入的原子性；同班同科目同日期重复解析的合并/覆盖规则。

产出：`docs/design/homework-parsing.md` + prompt 草案 `backend/…/prompt` 位置约定，经用户确认后关闭。
