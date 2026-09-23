# 调研:DeepSeek API 现状与作业解析约束

> Ticket: 004 · 日期: 2026-09-22
>
> **重要声明**:本次调研时外部网络受限,无法访问 api-docs.deepseek.com 核实。
> 下文所有**模型名、价格、上下文长度、限流参数均基于既有知识整理,接入前以官方文档为准**:
> https://api-docs.deepseek.com/ (中文站: https://api-docs.deepseek.com/zh-cn/)
> 本文档保证可靠的部分是:接口形态(OpenAI 兼容)、JSON mode 的使用模式、解析坑与降级策略——这些与具体版本号无关。
>
> **勘误(2026-09-23,实测)**:用真实 API Key 调 `GET https://api.deepseek.com/models`,本账号端点暴露的模型名是 **`deepseek-flash`(DeepSeek-V4.1-Flash)** 与 **`deepseek-v4-pro`**——没有 `deepseek-chat` / `deepseek-reasoner`。下文已按实测名改写;顺带实测 `deepseek-flash` 的 `context_window=1,048,576`、`max_output_tokens=393,216`(§1.3 的 128K 旧数字已过时),价格仍未核实。

## 1. 模型名、端点、上下文与定价

### 1.1 模型名(配置化,硬编码禁止)

DeepSeek 的 API 层模型名历史上长期只有两个"别名"(stable aliases),底层版本升级时别名不变:

| API 模型名 | 用途 | 说明 |
|---|---|---|
| `deepseek-flash` | 非推理 chat 模型 | 对话/抽取类任务,速度快 |
| `deepseek-v4-pro` | 推理模型(DeepSeek-R 系列 / thinking 模式) | 复杂推理,有可见的思维链,延迟高、输出 token 多 |

关于版本:本账号端点已暴露显式 V4 名称（`deepseek-flash` = DeepSeek-V4.1-Flash），不再是纯别名滚动。模型名一律走配置，接入前用 `GET /models` 或官方文档确认当时映射。

**工程决策:模型名必须走配置**(环境变量/配置文件,如 `DEEPSEEK_MODEL=deepseek-flash`),并提供 `deepseek-v4-pro` 作为可选的"难例重试"档位。不要在代码里出现任何字面量模型名。

### 1.2 端点(OpenAI 兼容)

- Base URL: `https://api.deepseek.com`(也兼容 `https://api.deepseek.com/v1`,但 `/v1` 与版本号无关,仅是 OpenAI SDK 兼容惯例)
- Chat: `POST /chat/completions`(OpenAI SDK 里 `base_url="https://api.deepseek.com"` 即可直接用,`api_key` 用 DeepSeek 后台生成的 key)
- 不需要 `/v1` 也能工作;Go 侧推荐直接用 `openai-go` / `sashabaranov/go-openai` 改 base_url,或裸 `net/http`,无需官方 SDK。

### 1.3 上下文长度(以官方文档为准)

- `deepseek-flash` 实测 `context_window=1,048,576`(1M);`deepseek-v4-pro` 未查。作业文本远小于任何上限,上下文不是约束。
- 单次**最大输出**有独立上限(数千 tokens 量级),作业解析输出远小于该值,不构成约束。
- 作业文本典型长度:教师一段话 50–500 汉字 ≈ 50–500+ tokens,**上下文完全不是瓶颈**;瓶颈在延迟与限流。

### 1.4 定价量级(以官方文档为准)

历史价格(人民币/百万 tokens,大致量级,勿当作精确数字):

| 模型 | 输入(缓存命中/未命中) | 输出 |
|---|---|---|
| `deepseek-flash` | 低价(元级)/ 元级 | 元级 |
| `deepseek-v4-pro` | 略高于 chat | 约 2–4 倍于输入 |

- DeepSeek 价格在历次版本更新中**只降不升**,V3.2 一代已降到输入约 $0.0x–0.3/M、输出约 $0.4/M 的量级。
- 支持**上下文硬盘缓存**(前缀命中部分按大幅折扣计费)——我们的 system prompt + few-shot 固定前缀会天然命中缓存,实际成本进一步降低。
- 结论:**按"每个学生每次解析 < 1 分钱"的量级估算即可,成本不是设计约束**;接入前以官方价格页为准(https://api-docs.deepseek.com/zh-cn/quick_start/pricing)。

### 1.5 限流(以官方文档为准)

DeepSeek 官方对 API **没有公布硬性 QPS 上限**,按"高并发下返回 429"来设计即可。我们场景(一个班/一个年级的教师批量提交)并发极低,不构成风险。

## 2. JSON 结构化输出可靠性

### 2.1 response_format 支持

- DeepSeek API 支持 OpenAI 风格的 **JSON mode**:`response_format={"type": "json_object"}`。开启后模型被约束输出合法 JSON(仍可能在语义上不合规)。
- 使用约束(官方要求):
  1. prompt(system 或 user)中**必须包含 "json" 这个词**并给出格式示例,否则可能报错或输出空;
  2. JSON mode **不保证 schema 校验**——只保证"是合法 JSON",不保证字段名/类型正确;
  3. 截断(`finish_reason=length`)会产生不完整 JSON,必须防御(见 §4)。
- 截至既有知识,DeepSeek **未提供** OpenAI 那种 `json_schema` strict 模式。因此:**schema 的强制靠"prompt 给 schema + Go 侧严格校验 + 失败重试"三层实现**,不要指望 API 参数兜底。
- 也支持 function calling,但为了一次性抽取任务,JSON mode 更简单直接,没必要绕 tools。

### 2.2 输出 Schema 草案(供 012 号 ticket 的 prompt 设计参考)

设计原则:字段少而稳、枚举封闭、所有"模型拿不准"的信息归入显式的 `confidence`/`needs_review`,而不是靠模型硬编。

```jsonc
{
  "todos": [
    {
      "subject": "语文",              // 枚举: 语/数/英/物理/化学/生物/历史/地理/道法/体育/艺术/其他
      "task": "完成口算天天练第12页",   // 学生视角的一句话任务,保留原文关键词
      "task_type": "exercise",         // 枚举: exercise(练习题)/reading(朗读/阅读)/memorize(背诵)/
                                       //       writing(写作)/review(复习)/preview(预习)/practice_skill(打卡/练琴等)/other
      "estimated_minutes": 15,         // 整数;教师未说就按经验估,10–30 之间
      "due_hint": "明天",              // 原文中的截止时间原词,没有则 null
      "count": 1,                      // 重复次数(如"抄写生词两遍"= 2),默认 1
      "confidence": "high"             // 枚举: high/medium/low —— low 由前端提示确认
    }
  ],
  "unparsed": ["带一把彩笔"]            // 无法归类为 todo 但值得提醒的原文碎片;可为空数组
}
```

字段取舍的理由:

- **不设 `id` / `student_id`**:ID 关联是 Go 侧的事,模型只管文本→条目;
- **`task_type` 用封闭枚举**而非自由文本:前端图标/排序依赖它,自由文本必然漂移;
- **`estimated_minutes` 让模型估**而非强求原文:教师几乎从不写时长;接受估算值但该字段标注"估算",UI 上不伪装成精确值;
- **`confidence` + `unparsed` 是关键安全阀**:把"解析不了"显式表达出来,比让模型强行编条目可靠得多;
- **不要 `notes`/`reason` 等解释性字段**:徒增 token 与漂移面。

## 3. 中文 K12 作业文本的已知坑与 prompt 策略

### 3.1 文本坑清单

1. **口语化与缩略**:"口算天天练第12页"= 书名+页码;"黄冈同步 P23-24";"背 6 单元单词";"卷子订正"。模型必须知道这些是教辅名/页码引用,**保留原文引用**,不要翻译成泛化描述("做数学练习")——学生看泛化描述等于没看。
2. **页码/题号区间**:如"P45-46,做完对答案""口算第 12 页全部""只做双数题"。数字抽取要精确,错一位整条废。
3. **多任务一段话,无分隔标点**:"语文抄词听写数学口算英语读20分钟"——分词与归属(哪个词属于哪科)是难点,依赖学科常识词表。
4. **学科名省略**:教师常直接说"把古诗背了"(语文)不带主语。
5. **祈使句/责任对象歧义**:"家长检查并签字"——这是给家长的任务还是给学生的?"明天带彩笔"是准备事项。schema 里用 `task_type: other` + `unparsed` 兜住。
6. **时间表达**:"明天""周五前""下周一交"。不做日期解析(交给 Go 侧/前端),模型只回传 `due_hint` 原词,避免模型算错日期。
7. **量词陷阱**:"抄两遍"→ count=2;"读 20 分钟"→ 这本身就是 estimated_minutes 而非 count。二者别混淆。
8. **学生不识字/低年级**:输出 `task` 必须是学生(家长代读)能直接执行的一句话,不是教师教案语。
9. **错别字与谐音**:微信语音转文字常见("口算天天恋""地11课"),prompt 要声明容忍错别字并按最接近的常见教辅/课文理解。

### 3.2 Prompt 策略建议(原则,具体 prompt 在 012 号 ticket)

1. **固定 system + JSON schema 示例放在 system 里**(前缀稳定 → 吃到 DeepSeek 上下文缓存,省钱且输出更稳);
2. **给 2–3 个 few-shot,覆盖最难的形态**:一段话多科混合、页码引用、"带东西"类准备事项;
3. **明确写死三条规则**:(a) 引用教辅名/页码时逐字保留;(b) 拿不准就标 `confidence: low`,绝不编造;(c) 非任务信息进 `unparsed`;
4. **枚举值在 prompt 里全部列出**,禁止模型自造枚举;
5. **温度设 0(或尽量低)**:抽取任务不要创造性;
6. **max_tokens 设一个宽松上限**(如 2000):足够覆盖"一天作业≤20条",同时把失控输出截断在可重试范围;
7. **禁止链式思考输出到 JSON 里**:schema 不给 reason 字段,模型自然不会往里塞。

## 4. 失败与降级策略

按失败模式分层(Go 侧实现):

| 失败模式 | 检测 | 处置 |
|---|---|---|
| 网络错误 / 5xx / 429 | HTTP 状态、超时 | 指数退避重试 2 次(如 1s/4s),仍失败走降级 |
| 超时 | context deadline(建议 30–60s;推理档 `deepseek-v4-pro` 可放宽) | 同上;超时切到 `deepseek-flash` 重试一次(若首调用用的 `deepseek-v4-pro`) |
| JSON 语法失败 / 截断 | `encoding/json` unmarshal error 或 `finish_reason=length` | 先试一次自动修复(若截断:按行截到最后完整 `}`);再重试 1 次;仍失败降级 |
| JSON 合法但 schema 不符 | Go 侧 strict struct 校验(未知枚举值、缺 `todos` 字段) | 拼一个"你的输出不符合 schema,错误是 X,请严格按 schema 重新输出"的重试 prompt,重试 1 次 |
| 内容安全拒绝 | 4xx + 官方 content_filter 错误码 / 空回复 | 不重试,直接标记该条"需人工录入",不阻塞其他条目 |
| 空结果(todos 与 unparsed 全空) | 业务校验 | 视为失败,重试 1 次;再失败降级——教师明确布置了作业却解析为空大概率是 prompt/模型问题 |

**降级路径(逐级)**:

1. `deepseek-flash`(JSON mode)→ 2. 重试与自动修复 → 3. `deepseek-v4-pro`(难例,单次)→ 4. **人工兜底:作业文本原样存库,前端展示"待整理"状态,教师/家长手工拆条**。

最终必须有第 4 级:AI 永远只是加速,不能成为教师提交作业的单点故障。所有失败样本落库(原始文本 + 错误类型),作为后续 prompt 迭代(012 ticket 之后)的评测集。

**幂等与审计**:每次调用记录 request/response 摘要与耗时;同一文本重试时用同一 request_id 便于追查。

---

### 结论要点

- 模型名用 `deepseek-flash`(默认)/ `deepseek-v4-pro`(难例),**全部走配置**;名称以 `GET /models` / 官方文档为准。
- JSON mode 可用但无 schema 强制 → Go 侧 strict 校验 + 定向重试是必须的。
- schema 草案见 §2.2,核心安全阀是 `confidence` 与 `unparsed`。
- 降级终点是"原文存库 + 人工拆条",AI 失败不能阻塞提交。
