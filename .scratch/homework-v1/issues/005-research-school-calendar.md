---
id: 005
title: "Research: 2026–2027 法定节假日与调休表"
labels: [wayfinder:research]
status: open
assignee:
blocked-by: []
---

## Question

上学日历需要内置国务院办公厅发布的法定节假日与调休安排（v1 覆盖 2026、2027 两年）。产出：

1. 2026 全年、2027 全年（若已发布）的节假日安排：每个节日的放假日期范围与**调休补班的周末日期**（调休日是上学日，直接影响 Session 生成与周末合并）；
2. 数据以 JSON 结构化输出（日期、类型：holiday/adjusted-workday、名称），供后端作为种子数据；
3. **数据准确性是硬要求**：以国务院公告为权威来源。若本环境网络受限无法核实，必须输出"未经核实的草稿"并逐条标注待验证，不得把不确定的调休安排当作事实输出；宁缺毋假。

输出写入 `docs/research/005-school-calendar.md`（含 JSON 种子数据）。
