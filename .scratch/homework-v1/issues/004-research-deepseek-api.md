---
id: 004
title: "Research: DeepSeek API 现状与作业解析约束"
labels: [wayfinder:research]
status: closed
assignee: claude
blocked-by: []
---

## Question

作业文字 → Todo 解析选定 DeepSeek（用户指名"最新 V4 模型"，OpenAI 兼容接口，模型名做成配置）。需要弄清：

1. DeepSeek 当前 API 的模型名（v4 时代的 chat 模型与 reasoning 模型各叫什么）、OpenAI 兼容端点、上下文长度、定价量级；若网络受限，基于既有知识输出并显著标注"接入前以官方文档为准"；
2. JSON 结构化输出的可靠性：response_format/json mode 支持情况，解析作业文字所需的输出 schema 约束（作业条目、类型 hints、预估时长等字段的取舍）；
3. 中文 K12 作业文本解析的已知坑（口语化、缩写、"口算天天练第12页"这类引用）与 prompt 策略建议；
4. 失败与降级策略建议（超时、JSON 解析失败、内容安全拒绝）。

输出写入 `docs/research/004-deepseek-api.md`。此 ticket 只调研不设计 prompt——prompt 设计在 grilling ticket 012 做。

## Resolution

见 `docs/research/004-deepseek-api.md`(外部网络受限,模型名/价格/上下文均标注"接入前以官方文档为准")。要点:模型用 `deepseek-chat`(默认)/`deepseek-reasoner`(难例),全走配置;JSON mode 可用但无 schema 强制,需 Go 侧 strict 校验+定向重试;schema 草案含 `confidence`/`unparsed` 安全阀;降级终点是原文存库+人工拆条。
