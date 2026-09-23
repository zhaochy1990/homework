---
id: 014
title: "Prototype: 打卡卡片海报设计"
labels: [wayfinder:prototype]
status: closed
assignee: claude
blocked-by: []
---

## Question

打卡卡片（后端 Go 生成 PNG）的版式与内容定型。已定元素：孩子名、日期、连续打卡天数（大数字，激励核心）、完成待办清单、科目名；可补充：鼓励语、产品 Logo/名称、二维码（小程序码）。需要定：版式（竖版 3:4 分享友好？）、风格（儿童友好 vs 简洁）、Go 图像生成选型（fogleman/gg + 中文字体方案，字体版权注意）。

产出：用 /prototype 出 2–3 版 HTML/CSS 视觉稿供用户挑选，定稿记入 `docs/design/checkin-card.md`（含元素坐标级描述，供 Go 实现照抄）。

## Resolution

产出 `docs/prototype/card-prototype.html`（三版视觉稿）+ `docs/design/checkin-card.md`（定稿）。

用户决策：**三套模板全部采用，每次打卡随机用一套**——`seed = checkin.ID % 3` 确定性选取，重试幂等（同一打卡永远生成同一张），长期三套均匀分布。

定稿要点（详见 checkin-card.md）：
- 画布 750×1000 PNG（3:4）；三模板：medal 奖牌风（暖金立体金牌）/ certificate 证书风（宋体大数字）/ journal 手账贴纸风（网格纸+胶带+便利贴）；
- 内容槽位与数据来源逐项映射；家长一句话与照片**不上海报**（成绩单口径）；
- 鼓励语保留：每模板固定文案池，`checkin.ID % len(pool)` 轮换；
- 小程序码保留（getUnlimitedQRCode，COS 缓存；失败降级为品牌名占位，不阻塞出卡）；
- Go 实现：fogleman/gg + embed 三套 OFL 字体（站酷快乐体/思源宋体/思源黑体，可商用可分发）；
- 生成管线：打卡事务后异步、重试 3 次、todo 后续编辑不重绘历史卡片（数据模型矩阵 #1）。
- 待验证：wxacode 配额/scene 长度；字体 embed 体积（超 15MB 则宋体子集化）。
