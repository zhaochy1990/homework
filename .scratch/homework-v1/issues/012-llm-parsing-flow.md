---
id: 012
title: "Grilling: LLM 作业解析与确认流设计"
labels: [wayfinder:grilling]
status: closed
assignee: claude
blocked-by: ["004", "010"]
---

## Question

设计"粘贴老师文字 → DeepSeek 解析 → 管理员确认编辑 → 入库"的完整流程：

- 解析 prompt 与输出 JSON schema（依赖 research/004 的结论）；
- 管理员确认页的交互流：解析结果的可视化编辑（增删改条目、合并/拆分）、重新解析、放弃；
- 失败路径：解析超时、JSON 不合法、敏感词拒绝时的降级（手动录入待办）；
- 入库事务：Session 创建 + Todo 批量写入的原子性；同班同科目同日期重复解析的合并/覆盖规则。

产出：`docs/design/homework-parsing.md` + prompt 草案 `backend/…/prompt` 位置约定，经用户确认后关闭。

## Resolution

产出 `docs/design/homework-parsing.md`（完整流程、失败分类、确认页与手填页规格、入库事务）+ prompt 草案 `backend/internal/homework/parse/prompts/parse_v1.md`；新增 `docs/adr/0007`（全局封闭学科枚举）。

三轮 grilling 敲定的决策：

**解析与 schema**
- Q1 **支持混科解析**：请求由 `{subject, date, rawText}` 改为 `{date, rawText, defaultSubject?}`，schema 每条待办自带 `subject`，入库时一个事务创建多个 Session。老师原文常常混科，硬拆三次粘贴等于把 AI 的收益还回去。
- Q2 schema 精简为 `{subject, content, estimated_minutes}`：删掉 `confidence`（改由"全部条目都需过目"承担）、`task_type`（无 UI 消费方）、`count`（并入 content 文本）、`due_hint`（Session 已携带日期与截止）。`estimated_minutes` 保留入库并展示。
- Q3 `unparsed`（"明天带彩笔""家长检查签字"）落进 `homework_sessions.notes`，确认页可编辑，作业页底部"老师还提到"展示；卡片不含。
- 学科取值域定为**全局封闭枚举**（ADR 0007），班级不维护学科集合——Streak 按孩子×学科聚合且跨班合并，学科名漂移会让它分裂。

**确认页**
- Q4 只做改文本／改时长／删／加／调序／改学科（可把一条移到别的分组）；**不做合并与拆分**（"增删改"可等价完成，为它们引入多选态与拖拽合并不划算）。重解析覆盖草稿并二次确认，放弃即返回。

**失败与降级**
- Q5 超时 30s、**不自动重试**；失败给"重试"按钮（同模型同 prompt，**温度 0→0.3**，Q1：温度 0 下同 prompt 重试会逐字复现同一失败）；重试仍失败 → 手填页。
- 手填页：**多行文本框一行一条**（Q4），**单学科**（Q2：文本框无分组能力，含多科分两次录入），原文并排只读展示，`estimatedMinutes` 恒为 null。不走启发式自动拆条——用户明确要求"不要做太复杂"。

**入库**
- Q6 **支持追加解析**：解析端点探测已有 Session 并在确认页标 `target=append`，展示已有条目（灰、只读）+ 新增条目（可编辑）；否则"老师中午补一条"会撞 409 走进死胡同。
- Q7 入库前对 `rawText` + 全部 content + notes 调 `msgSecCheck`，命中 → 422 `content_blocked` 整单拒绝（msgSecCheck 不返回位置，无法定位到条目）。解析端点不检测——解析不落库、不展示给他人。
- 单事务：内容安全 → 判 `kind` → 冲突校验（并发撞车整单 409 并指明学科）→ 写 Session + Todos。追加进已有 Session 的待办对已打卡孩子显示"新增未完成"，不影响打卡与 streak（数据模型矩阵 #2）。
- Q8 prompt 放 `parse_v1.md`（`go:embed`，文件头带版本号），模型名走配置，失败样本落结构化日志，**不建** `llm_parse_logs` 表。

**回填**：`todos.estimated_minutes`、`homework_sessions.notes`、全局学科枚举写入 `docs/design/data-model.md`；parse/homework 两端点、`content_blocked` 错误码、发作业时序写入 `docs/design/api-contract.md`；`CONTEXT.md` 新增「解析草稿」、修正「待办」定义（不再专属大模型产出）、收紧「学科」定义。
