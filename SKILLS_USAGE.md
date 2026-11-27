# Skills 使用说明

本项目已在 `.codebuddy/skills/` 安装 27 个稳定版 skill（Engineering 20 个 + Productivity 7 个）。8 个 beta 和 4 个 misc 未安装。新装的 skill 可能需要重启 CodeBuddy 会话后才能被识别。

## 使用方式

- **手动使用**：在对话中输入 `/技能名` 调用。源仓库用 `disable-model-invocation: true` 标记这类技能；CodeBuddy 是否完全遵守该字段未确认，建议按下面的分类主动输入命令。
- **自动使用**：模型判断任务匹配时会自己调用，也可以手动输入 `/技能名` 强制使用。

## 场景速查

| 场景 | 使用的 skill | 方式 |
| --- | --- | --- |
| 首次在这个仓库启用工程技能（只需一次） | `setup-matt-pocock-skills` | 手动 |
| 不知道该用哪个技能 | `ask-matt` | 手动 |
| 需求模糊，开工前把方案问清楚 | `grill-me` | 手动 |
| 同上，并同步沉淀术语表和 ADR | `grill-with-docs` | 手动 |
| 把讨论结论整理成 spec 并发布 | `to-spec` | 手动 |
| 把计划/spec 拆成带依赖的工单 | `to-tickets` | 手动 |
| 按 spec 或工单写代码 | `implement` | 手动 |
| 大任务并行实现（子 agent 任务图） | `implement-spec` | 手动 |
| 规划超出单次会话的超大块工作 | `wayfinder` | 手动 |
| 处理 issue / 外部 PR 的分流 | `triage` | 手动 |
| 扫描代码库找架构深化机会 | `improve-codebase-architecture` | 手动 |
| 会话结束后复盘并改进 agent 环境 | `retro` | 手动 |
| 压缩当前对话，交给另一个 agent | `handoff` | 手动 |
| 跨会话教学某个概念 | `teach` | 手动 |
| 把决策做成问卷发给关键人 | `to-questionnaire` | 手动 |
| 上一句没表达清楚，要求重新解释 | `wait-what` | 手动 |
| 写测试或修 bug，走红-绿-重构 | `tdd` | 自动 |
| 疑难 bug / 性能回归排查 | `diagnosing-bugs` | 自动 |
| 审查代码 diff（规范 + 是否符合需求） | `code-review` | 自动 |
| 设计/改进模块接口、接缝 | `codebase-design` | 自动 |
| 建立或完善领域术语表、ADR | `domain-modeling` | 自动 |
| 基于一手资料调研并写成带引用的文档 | `research` | 自动 |
| 快速做一次性原型验证设计 | `prototype` | 自动 |
| 写 PR 正文 | `pr` | 自动 |
| 生成交互式 bash 向导 | `wizard` | 自动 |
| 编写给 agent 看的文档（SKILL.md、AGENTS.md 等） | `writing-for-agents` | 自动 |
| 需要通用追问/压力测试思路 | `grilling` | 自动 |

## 完整清单

### 需要手动输入（16 个）

`ask-matt`, `grill-me`, `grill-with-docs`, `handoff`, `implement`, `implement-spec`, `improve-codebase-architecture`, `retro`, `setup-matt-pocock-skills`, `teach`, `to-questionnaire`, `to-spec`, `to-tickets`, `triage`, `wait-what`, `wayfinder`

### 模型可自动触发（11 个）

`code-review`, `codebase-design`, `diagnosing-bugs`, `domain-modeling`, `grilling`, `pr`, `prototype`, `research`, `tdd`, `wizard`, `writing-for-agents`

## 推荐的常规流程

1. 仓库首次使用：`/setup-matt-pocock-skills`
2. 动手前：`/grill-with-docs`（或轻量版 `/grill-me`）
3. 形成 spec：`/to-spec`
4. 拆工单：`/to-tickets`
5. 实现：`/implement`（内部会驱动 `tdd` 和 `code-review`）
6. 结束后：`/retro`
