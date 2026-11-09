# 架构决策记录（ADR）

ADR 记录会影响后续工作的选择及原因，例如架构边界、数据模型、技术栈和交付流程。普通实现细节写在代码或 PR 中。

| 编号 | 决策 | 状态 |
| --- | --- | --- |
| [0001](0001-documentation-and-ai-guidance.md) | 仓库内文档与简短 AI 入口 | 已采纳 |
| [0002](0002-initial-architecture-direction.md) | Go 单体、Docker 与 Vue 的初期架构方向 | 提议 |
| [0003](0003-http-routing-with-chi.md) | 使用 Chi 组织 HTTP 路由与中间件 | 已采纳，已实现 |
| [0004](0004-server-side-browser-sessions.md) | 浏览器采用服务端 Session + Cookie | 已采纳，HTTP 会话已实现 |
| [0005](0005-rbac-resource-authorization.md) | 使用 RBAC 与资源归属约束管理资源访问 | 已采纳，用户管理授权已实现，工作区归属授权待实现 |
| [0006](0006-terminal-before-ide.md) | 先实现 xterm.js 终端，后续集成浏览器 IDE | 已采纳，待实现 |
| [0007](0007-postgresql-user-persistence.md) | PostgreSQL 与最小用户持久化 | 已采纳，已实现 |
| [0008](0008-authentication-and-http-errors.md) | 密码认证、持久化会话与统一 HTTP 失败响应 | 已采纳，已实现 |
| [0009](0009-user-management-and-authorization.md) | 用户管理、固定角色授权与管理员保底 | 已采纳，已实现 |

## 使用方式

复制 [模板](template.md)，以递增的四位编号和英文短名命名，例如 `0007-workspace-state-model.md`。写清背景、决定、备选方案与后果，并更新本索引。

状态使用“提议”“已采纳”“已替代”或“已撤回”。尚待评审的技术选择标为“提议”；采纳后在对应 PR 中落实状态。变更既有决定时新增 ADR，在旧记录中链接替代记录，保留当时的理由。当前架构说明同步到 [架构文档](../architecture.md)。
